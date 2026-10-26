package efforts

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Invented high-volume groups deliberately reuse vocabulary and document names.
// None of the background messages is a request to do work.
func e5PrecisionCorpus() []recallCase {
	cases := e4RecallCorpus()
	for _, shape := range []string{"recurring document", "generic pair", "rolling chain", "cross chat chain", "music and records", "separated bursts"} {
		ms := []model.Message{}
		for i := 0; i < 1200; i++ {
			m := model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Group, Sender: testkit.Phone, IsFromMe: i%3 == 1, Timestamp: testkit.Epoch.Add(-time.Duration(1201-i) * time.Minute)}
			switch shape {
			case "recurring document":
				m.Content = fmt.Sprintf("cafe gossip%d lunch", i)
				m.MediaType, m.Filename = "document", "Class-Research-Notes (1) [worksheet] 2026-10-01.pdf"
			case "generic pair":
				m.Content = fmt.Sprintf("class notes gossip%d", i)
			case "rolling chain", "cross chat chain":
				m.Content = fmt.Sprintf("topic%d topic%d topic%d", i/3, i/3+1, i/3+2)
				if shape == "cross chat chain" {
					m.ChatJID = fmt.Sprintf("invented-%d@g.us", (i/12)%32)
				}
			case "music and records":
				m.Content = "music track shared"
				m.MediaType, m.Filename = "audio", "Evening-Music (1).mp3"
			case "separated bursts":
				m.Content = "class notes"
				m.Timestamp = testkit.Epoch.Add(-time.Duration(1201-i) * 90 * time.Minute)
			}
			ms = append(ms, m)
		}
		cases = append(cases, recallCase{shape, false, ms})
	}
	// An actual task in the middle of the same ambient chatter must survive.
	ms := append([]model.Message{}, cases[len(cases)-6].ms...)
	for i, text := range []string{"Please review the estuary survey; due tomorrow", "Estuary survey measurements ready", "Estuary survey approved"} {
		m := model.Message{ID: fmt.Sprint("task-", i), ChatJID: testkit.Group, Sender: testkit.Phone, IsFromMe: i == 1, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(3-i) * time.Second)}
		if i > 0 {
			m.QuotedMessageID = "task-0"
		}
		if i == 1 {
			m.MediaType, m.Filename = "document", "Estuary-Survey.xlsx"
		}
		ms = append(ms, m)
	}
	cases = append(cases, recallCase{"explicit task in ambient group", true, ms})
	return cases
}

func TestE5PrecisionCorpus(t *testing.T) {
	tp, fp, positives, retained := 0, 0, 0, 0
	confidence, sources := map[string]int{}, map[string]int{}
	for _, tc := range e5PrecisionCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for _, m := range tc.ms {
				testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: m.ChatJID}))
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			xs, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 5000})
			testkit.Must(t, err)
			largest, chats := 0, 0
			for _, e := range xs {
				if len(e.MessageRefs) > largest {
					largest, chats = len(e.MessageRefs), len(e.Chats)
				}
			}
			if tc.positive {
				positives++
				found := false
				for _, e := range xs {
					want := len(tc.ms)
					if tc.name == "explicit task in ambient group" {
						want = 3
					}
					valid := len(e.MessageRefs) == want
					if tc.name == "explicit task in ambient group" {
						for _, r := range e.MessageRefs {
							valid = valid && strings.HasPrefix(r.MessageID, "task-")
						}
					}
					if valid && !found {
						tp++
						found = true
					} else {
						fp++
					}
				}
				if !found {
					t.Error("task fragmented or polluted by ambient messages")
				}
			} else {
				fp += len(xs)
				if len(xs) != 0 {
					t.Error("ambient chatter became an effort")
				}
			}
			if tc.name == "explicit task in ambient group" && len(xs) == 1 && len(xs[0].MessageRefs) == 3 {
				retained++
			}
			for _, e := range xs {
				if len(e.MessageRefs) > maxEffortMessages || len(e.Chats) > maxEffortChats {
					t.Error("unbounded effort")
				}
			}
			audit, err := s.Audit()
			testkit.Must(t, err)
			for _, axis := range []map[string]int{audit.Accepted.Messages, audit.Accepted.Chats, audit.Accepted.People, audit.Accepted.SpanDays, audit.Confidence, audit.TitleSources} {
				total := 0
				for _, n := range axis {
					total += n
				}
				if total != len(xs) {
					t.Error("accepted audit axes count different populations")
				}
			}
			for k, n := range audit.Confidence {
				confidence[k] += n
			}
			for k, n := range audit.TitleSources {
				sources[k] += n
			}
			t.Logf("positive=%v efforts=%d largest_messages=%d largest_chats=%d", tc.positive, len(xs), largest, chats)
		})
	}
	t.Logf("E5 corpus: positives=%d negatives=%d TP=%d FP=%d FN=%d precision=%.4f recall=%.4f ambient_task_exact=%d/1", positives, len(e5PrecisionCorpus())-positives, tp, fp, positives-tp, float64(tp)/float64(tp+fp), float64(tp)/float64(positives), retained)
	t.Logf("confidence_histogram=%v title_sources=%v", confidence, sources)
}

func TestE5BusyGroupTimeAndTopicSegments(t *testing.T) {
	g := newGrouper()
	g.ambient = map[string]bool{testkit.Group: true}
	ms := []model.Message{
		{ID: "request", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Please review the estuary survey; due tomorrow", Timestamp: testkit.Epoch.Add(-16 * time.Hour)},
		{ID: "reply", ChatJID: testkit.Group, IsFromMe: true, Content: "Estuary survey approved", QuotedMessageID: "request", Timestamp: testkit.Epoch.Add(-15 * time.Hour)},
		{ID: "shift", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Please prepare the cobalt poster; due tomorrow", QuotedMessageID: "request", Timestamp: testkit.Epoch.Add(-14 * time.Hour)},
		{ID: "later", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Please review the estuary survey; due tomorrow", QuotedMessageID: "request", Timestamp: testkit.Epoch.Add(-time.Hour)},
	}
	g.add(ms)
	cs := consolidateClusters(g.cs, g.ambient)
	if len(cs) != 2 || len(cs[0].ms) != 3 {
		t.Fatal("same-core continuation did not rejoin, or topic shift bridged tasks")
	}
	ms[0].Content = "estuary survey agreed"
	if explicitThread(ms[:2], g.ambient) {
		t.Fatal("a decision alone substituted for an explicit request")
	}
	ms[0].Content = "Please review the estuary survey"
	if explicitThread(ms[:2], g.ambient) {
		t.Fatal("ambient thread without artefact or deadline promoted")
	}
}

func TestE5AmbientThresholdInvalidatesCachedEfforts(t *testing.T) {
	s := qualitySeed(t, nil)
	for i := 0; i < 80; i++ {
		m := model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Group, Sender: testkit.Phone, IsFromMe: i%2 == 1, Content: "class notes", Timestamp: testkit.Epoch.Add(-time.Duration(100-i) * time.Minute)}
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
		if i != 2 && i != 30 && i != 31 && i != 79 {
			continue
		}
		xs, err := s.List(nil, Filter{IncludeUntitled: true})
		testkit.Must(t, err)
		cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
		testkit.Must(t, err)
		if !reflect.DeepEqual(xs, cold) {
			t.Fatal("cold and incremental threshold classification differ")
		}
		if i >= 31 && len(xs) != 0 {
			t.Fatal("ambient threshold retained a cached false effort")
		}
	}
}

func TestE5IncrementalAmbientTask(t *testing.T) {
	s := qualitySeed(t, nil)
	var scenario recallCase
	for _, tc := range e5PrecisionCorpus() {
		if tc.name == "explicit task in ambient group" {
			scenario = tc
		}
	}
	for _, m := range scenario.ms[:1200] {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	xs, err := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("ambient baseline promoted")
	}
	for _, m := range scenario.ms[1200:] {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	start := time.Now()
	xs, err = s.List(nil, Filter{IncludeUntitled: true})
	elapsed := time.Since(start)
	testkit.Must(t, err)
	if len(xs) != 1 || len(xs[0].MessageRefs) != 3 {
		t.Fatal("incremental task lost or polluted")
	}
	cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if !reflect.DeepEqual(xs, cold) {
		t.Fatal("incremental busy-group inference differs from cold")
	}
	t.Logf("incremental 1200 ambient messages + 3 task messages: %s", elapsed)
	if elapsed >= 200*time.Millisecond {
		t.Errorf("incremental ambient recompute exceeds 200ms: %s", elapsed)
	}
}

func TestE5BoundsAndNoReplyBridge(t *testing.T) {
	ms := []model.Message{}
	for i := 0; i < 1200; i++ {
		m := model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Please review the estuary survey; due tomorrow", IsFromMe: i%2 == 1, Timestamp: testkit.Epoch.Add(-time.Duration(1200-i) * time.Minute)}
		if i > 0 {
			m.QuotedMessageID = "0"
		}
		ms = append(ms, m)
	}
	cs := dedupeClusters(group(ms))
	total := 0
	for _, c := range cs {
		total += len(c.ms)
		if len(c.ms) > maxEffortMessages {
			t.Fatal("quotes bypassed sanity bound")
		}
	}
	if total != len(ms) || len(cs) < 2 {
		t.Fatal("overflow evidence lost instead of split")
	}
	root := ms[0]
	root.ID = "root"
	root.Content = "estuary survey"
	reply := ms[1]
	reply.ID = "reply"
	reply.Content = "cobalt poster"
	reply.QuotedMessageID = "root"
	bridge := ms[2]
	bridge.ID = "bridge"
	bridge.Content = "Please review the cobalt poster"
	bridge.QuotedMessageID = "reply"
	cs = dedupeClusters(group([]model.Message{root, reply, bridge}))
	if len(cs) != 2 || len(cs[0].ms) != 2 || len(cs[1].ms) != 1 {
		t.Fatal("reply introduced a new admission topic")
	}
}

func TestE5TitlesAndConfidence(t *testing.T) {
	for _, tc := range []struct {
		file, want string
		good       bool
	}{
		{"03_Estuary-Survey (1) [worksheet] 2026-10-01 record-42.xlsx", "Estuary survey", true},
		{"20261001_Orchid-Exhibition (2).pdf", "Orchid exhibition", true},
		{"Estuary_Survey_2026-10-01.pdf", "Estuary survey", true},
		{"Estuary_Survey_21 Oct 2026.pdf", "Estuary survey", true},
		{"123456789.pdf", "", false},
		{"Record-583928.pdf", "Record", false},
		{"Evening-Music (1).mp3", "Evening music", false},
		{"Evening-Music-Track.pdf", "Evening music track", false},
		{"IMG_1234.jpg", "Img", false},
		{"Author-Citation-2024.pdf", "Author citation", false},
	} {
		if tc.good && FilenameTitle(tc.file) != tc.want {
			t.Errorf("filename cleanup: %q", FilenameTitle(tc.file))
		}
		if goodFile(tc.file) != tc.good {
			t.Errorf("work document eligibility: %q", tc.file)
		}
	}
	ms := []model.Message{
		{ID: "a", ChatJID: testkit.Phone, Sender: testkit.Phone, Timestamp: testkit.Epoch.Add(-2 * time.Hour), MediaType: "document", Filename: "Unrelated-Garden-Plan.pdf"},
		{ID: "b", ChatJID: testkit.Phone, Sender: testkit.Phone, Timestamp: testkit.Epoch.Add(-time.Hour), Content: "Please review the estuary survey; due tomorrow"},
		{ID: "c", ChatJID: testkit.Phone, IsFromMe: true, Timestamp: testkit.Epoch, Content: "Estuary survey confirmed"},
	}
	title, source := titleWithSource(ms, words("estuary survey garden plan"))
	if title != "Estuary survey" || source != "request_phrase" {
		t.Fatal("attachment displaced request title")
	}
	title, source = titleWithSource(ms[:1], words("garden plan"))
	if title != "Unrelated garden plan" || source != "document_name" {
		t.Fatal("document fallback missing")
	}
	ms[0].Filename = "123456789.pdf"
	title, source = titleWithSource(ms[:1], words("garden plan"))
	if title != "" || source != "naming_queue" {
		t.Fatal("junk filename bypassed naming queue")
	}
	q := qualityRules{}
	base := []model.Message{
		{Content: "estuary survey", Sender: "one", Timestamp: testkit.Epoch.Add(-2 * time.Hour)},
		{Content: "estuary survey", IsFromMe: true, Timestamp: testkit.Epoch.Add(-time.Hour)},
		{Content: "estuary survey", Sender: "one", Timestamp: testkit.Epoch},
	}
	low := evidenceConfidence(base, q)
	for _, factor := range []string{"artefact", "deadline", "span", "participants"} {
		richer := append([]model.Message{}, base...)
		switch factor {
		case "artefact":
			richer[0].MediaType = "document"
			richer[0].Filename = "Estuary-Survey.pdf"
		case "deadline":
			richer[0].Content += " due tomorrow"
		case "span":
			richer[0].Timestamp = richer[0].Timestamp.Add(-24 * time.Hour)
		case "participants":
			richer[2].Sender = "two"
		}
		if evidenceConfidence(richer, q) <= low {
			t.Errorf("confidence ignored %s", factor)
		}
	}
	noisy := append([]model.Message{}, base...)
	noisy[2].Content += " incidental unrelated chatter"
	if evidenceConfidence(noisy, q) >= low {
		t.Fatal("confidence ignored cohesion")
	}
}
