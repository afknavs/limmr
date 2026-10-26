package efforts

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestE4WeakHistogramAndIncrementalAudit(t *testing.T) {
	s := qualitySeed(t, nil)
	put := func(id, chat, text string, hour int, mine bool, quote string) {
		testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: chat}))
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: id, ChatJID: chat, Sender: testkit.Phone, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(400-hour) * time.Hour), IsFromMe: mine, QuotedMessageID: quote}))
	}
	// E7 ignores the single-word filler before clustering. The remaining
	// candidates are a pair just below three mentions; and a three-mention
	// monologue. Same chat markers do not inflate participant counts.
	put("single", testkit.Phone, "pottery", 0, false, "")
	put("pair-a", testkit.Phone, "cobalt budget", 1, false, "")
	put("pair-b", testkit.Phone, "cobalt budget", 2, true, "")
	for i := 0; i < 3; i++ {
		put(fmt.Sprint("mono", i), testkit.Phone, "quartz exhibition", 24+i, false, "")
	}
	// Six quoted messages, two speakers, two mentions, eight-day span, no
	// deadline/artefact: volume alone must not promote it.
	for i := 0; i < 6; i++ {
		text, quote, hour := []string{"", "", "I'll bring chairs", "Reserve the west entrance", "I'll arrange transport", "confirmed"}[i], "resume0", 200+i
		if i < 2 {
			text = "orchid workshop"
			quote = ""
			hour = i + 3
		}
		put(fmt.Sprint("resume", i), testkit.Group, text, hour, i%2 == 1, quote)
	}
	a, err := s.Audit()
	testkit.Must(t, err)
	if a.CandidatesFiltered["too-weak"] != 3 || a.Named != 0 || a.Internal != 0 {
		t.Fatalf("unexpected aggregate: %+v", a)
	}
	if !reflect.DeepEqual(a.TooWeak.MessageCount, map[string]int{"1": 0, "2": 1, "3-5": 1, "6-10": 1, "11-20": 0, "21+": 0}) {
		t.Fatalf("message bins: %v", a.TooWeak.MessageCount)
	}
	if a.TooWeak.Participants["1"] != 1 || a.TooWeak.Participants["2"] != 2 || a.TooWeak.SpanDays["7-13"] != 1 || a.TooWeak.TopicSignal["0"] != 0 || a.TooWeak.TopicSignal["2"] != 2 || a.TooWeak.TopicSignal["3"] != 1 || a.TooWeak.ChatKind["group"] != 1 {
		t.Fatalf("threshold bins wrong: %+v", a.TooWeak)
	}
	for _, axis := range []map[string]int{a.TooWeak.MessageCount, a.TooWeak.Participants, a.TooWeak.SpanDays, a.TooWeak.TopicSignal, a.TooWeak.Artefacts, a.TooWeak.ChatKind} {
		total := 0
		for _, n := range axis {
			total += n
		}
		if total != 3 {
			t.Fatal("histogram axes count different populations")
		}
	}
	data, err := json.Marshal(a)
	testkit.Must(t, err)
	for _, private := range []string{"Maya", "cobalt", "quartz", "orchid", testkit.Phone, testkit.Group} {
		if strings.Contains(string(data), private) {
			t.Fatal("identifying audit output")
		}
	}
	// Crossing the three-mention threshold removes precisely one weak candidate
	// from every histogram, including an incremental cached snapshot.
	put("pair-c", testkit.Phone, "cobalt budget", 399, false, "")
	updated, err := s.Audit()
	testkit.Must(t, err)
	if updated.Named != 1 || updated.CandidatesFiltered["too-weak"] != 2 || updated.TooWeak.MessageCount["2"] != 0 {
		t.Fatalf("stale histogram: %+v", updated)
	}
	cold, err := (&Service{Hub: s.Hub}).Audit()
	testkit.Must(t, err)
	if !reflect.DeepEqual(updated, cold) {
		t.Fatal("incremental and cold audits differ")
	}
}

func TestE4ShortCrossChatArrivalAndUnnamedQueue(t *testing.T) {
	for _, tc := range e4RecallCorpus() {
		if !strings.HasPrefix(tc.name, "group and direct task") {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for _, m := range tc.ms[:5] {
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if len(xs) != 0 {
				t.Fatal("five-message uncorroborated burst promoted")
			}
			testkit.Must(t, s.Hub.Messages.PutMessage(tc.ms[5]))
			xs, err = s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if len(xs) != 1 || len(xs[0].Chats) != 2 || len(xs[0].MessageRefs) != 6 || !reflect.DeepEqual(xs, cold) {
				t.Fatal("incremental cross-chat merge lost evidence")
			}
		})
	}
	s := qualitySeed(t, []string{"budget for the proposal", "budget is for proposal", "budget and a proposal"})
	xs, total, err := s.Unnamed(nil, 100)
	testkit.Must(t, err)
	if total != 1 || len(xs) != 1 {
		t.Fatal("strong untitled effort missing from naming queue")
	}
	a, err := s.Audit()
	testkit.Must(t, err)
	if a.Internal != 1 || a.NamingQueued != 1 || a.CandidatesFiltered["untitled"] != 0 {
		t.Fatal("untitled effort reported as discarded")
	}
	_, err = s.Update(nil, xs[0].ID, Update{Title: ptr("Festival budget"), OnlyUnnamed: true, ExpectedRevision: xs[0].Revision})
	testkit.Must(t, err)
	_, total, err = s.Unnamed(nil, 100)
	testkit.Must(t, err)
	a, err = s.Audit()
	testkit.Must(t, err)
	if total != 0 || a.NamingQueued != 0 || a.Named != 1 {
		t.Fatal("named effort remained queued")
	}
}

func TestE4WeakArtefactAndChatHistograms(t *testing.T) {
	for _, tc := range []struct {
		name, kind        string
		file, link, mixed bool
	}{
		{"file", "file", true, false, false},
		{"link", "link", false, true, false},
		{"file and link across chats", "file-and-link", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for i := 0; i < 2; i++ {
				m := model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "estuary survey", Timestamp: testkit.Epoch.Add(-time.Duration(2-i) * time.Hour), IsFromMe: i == 1}
				if i == 0 && tc.file {
					m.Content = ""
					m.MediaType = "document"
					m.Filename = "Estuary-Survey.pdf"
				}
				if i == 1 && tc.link {
					m.Content += " https://example.test/shared/measurements"
				}
				if i == 0 && tc.mixed {
					m.ChatJID = testkit.Group
				}
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			a, err := s.Audit()
			testkit.Must(t, err)
			chat := "direct"
			if tc.mixed {
				chat = "mixed"
			}
			if a.CandidatesFiltered["too-weak"] != 1 || a.TooWeak.Artefacts[tc.kind] != 1 || a.TooWeak.ChatKind[chat] != 1 || a.TooWeak.Participants["2"] != 1 || a.TooWeak.TopicSignal["2"] != 1 {
				t.Fatalf("wrong artefact bins: %+v", a)
			}
		})
	}
}

func TestE4RelaxedEvidenceStillNeedsCorroboration(t *testing.T) {
	for _, tc := range e4RecallCorpus() {
		if tc.name != "deadline burst 6 messages" && tc.name != "group and direct task reverse=false" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for i, m := range tc.ms {
				if tc.name == "deadline burst 6 messages" {
					// The owner replies, but only the other person mentions the
					// topic. A second speaker alone is not corroboration.
					m.IsFromMe = i == 2
				} else {
					// Work nouns are not distinctive cross-chat topic labels.
					m.Content = strings.ReplaceAll(m.Content, "copper", "deadline")
				}
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if len(xs) != 0 {
				t.Fatal("uncorroborated or generic topic promoted")
			}
		})
	}
}
