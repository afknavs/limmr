package efforts

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func e4RecallCorpus() []recallCase {
	cases := e3RecallCorpus()
	exchange := func(texts ...string) []model.Message {
		ms := []model.Message{}
		for i, text := range texts {
			ms = append(ms, model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(120-i) * time.Minute), IsFromMe: i%2 == 1})
		}
		return ms
	}
	burst := func(n int) []model.Message {
		ms := exchange("ceramics exhibition", "ceramics exhibition due tomorrow", "I'll take the east wall", "Could you bring the plinths?", "I'll arrange transport", "confirmed", "The van arrives at nine", "I'll bring tape", "Meet at the side entrance", "agreed")[:n]
		for i := 2; i < len(ms); i++ {
			ms[i].QuotedMessageID = "1"
		}
		return ms
	}
	for _, n := range []int{6, 8, 10} {
		cases = append(cases, recallCase{fmt.Sprintf("deadline burst %d messages", n), true, burst(n)})
	}
	for _, days := range []int{8, 14} {
		ms := burst(8)
		for i := 0; i < 4; i++ {
			ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -days)
		}
		cases = append(cases, recallCase{fmt.Sprintf("resume after %d days", days), true, ms})
	}
	for _, link := range []bool{false, true} {
		ms := exchange("estuary survey", "estuary survey measurements look consistent", "confirmed")
		if link {
			ms[0].Content += " https://example.test/shared/sheet"
		} else {
			ms[0].Content = ""
			ms[0].MediaType, ms[0].Filename = "document", "Estuary-Survey.xlsx"
		}
		cases = append(cases, recallCase{fmt.Sprintf("artefact led link=%v", link), true, ms})
	}
	files := exchange("", "", "confirmed", "agreed")
	files[0].MediaType, files[0].Filename = "document", "Orchid-Exhibition.pdf"
	files[1].MediaType, files[1].Filename = "document", "Orchid-Exhibition.pptx"
	cases = append(cases, recallCase{"two file versions and decision", true, files})
	for _, reverse := range []bool{false, true} {
		ms := exchange("copper exhibition venue", "copper exhibition tickets", "I'll book the hall", "copper slides rehearsal", "copper slides deadline tomorrow", "I'll fix the opening")
		for i := range ms {
			if (i < 3) != reverse {
				ms[i].ChatJID = testkit.Group
			}
			// The same external participant carries the task between chats.
			ms[i].IsFromMe = false
		}
		ms[1].IsFromMe, ms[4].IsFromMe = true, true
		ms[2].QuotedMessageID, ms[5].QuotedMessageID = "0", "3"
		cases = append(cases, recallCase{fmt.Sprintf("group and direct task reverse=%v", reverse), true, ms})
	}
	cases = append(cases, recallCase{"strong without clean phrase", true, exchange("budget for the proposal", "budget is for proposal", "budget and a proposal")})
	for _, text := range []string{"Verification code 123456", "image omitted", "hello good morning", "Forwarded: https://example.test/news", "Forwarded: festival budget proposal"} {
		cases = append(cases, recallCase{"junk " + text, false, exchange(text, text, text, text, text, text)})
	}
	self := burst(8)
	for i := range self {
		self[i].ChatJID = "me"
	}
	cases = append(cases, recallCase{"self task notes", false, self})
	monologue := burst(8)
	for i := range monologue {
		monologue[i].IsFromMe = false
	}
	cases = append(cases, recallCase{"same day deadline monologue", false, monologue})
	noDeadline := burst(8)
	noDeadline[1].Content = "ceramics exhibition"
	cases = append(cases, recallCase{"two mentions without work evidence", false, noDeadline})
	singleFile := exchange("", "Can you review?", "confirmed")
	singleFile[0].MediaType, singleFile[0].Filename = "document", "Estuary-Survey.xlsx"
	cases = append(cases, recallCase{"file without topic corroboration", false, singleFile})
	// A common work noun or the owner alone cannot bridge unrelated chats.
	for _, common := range []string{"deadline", "copper"} {
		ms := exchange(common+" exhibition venue", common+" exhibition tickets", "I'll book the hall", common+" slides rehearsal", common+" slides due tomorrow", "I'll fix the opening")
		for i := range ms {
			ms[i].IsFromMe = false
			if i < 3 {
				ms[i].ChatJID = testkit.Group
			} else {
				ms[i].Sender = "15550100002@s.whatsapp.net"
				ms[i].ChatJID = ms[i].Sender
			}
		}
		ms[2].QuotedMessageID, ms[5].QuotedMessageID = "0", "3"
		cases = append(cases, recallCase{"unrelated chats common " + common, false, ms})
	}
	return cases
}

// End-to-end scoring includes source filtering, grouping, quality, and the
// unnamed path. Count every returned negative effort, not just named ones.
func TestE4RecallCorpus(t *testing.T) {
	tp, fp, positives, exact := 0, 0, 0, 0
	for _, tc := range e4RecallCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for _, m := range tc.ms {
				testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: m.ChatJID}))
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if tc.positive {
				positives++
				if len(xs) > 0 {
					tp++
				}
				if len(xs) == 1 && len(xs[0].MessageRefs) == len(tc.ms) {
					exact++
				}
			} else {
				fp += len(xs)
			}
			t.Logf("label=%v efforts=%d", tc.positive, len(xs))
			if (len(xs) > 0) != tc.positive {
				t.Fatalf("classified effort=%v, labelled=%v", len(xs) > 0, tc.positive)
			}
			if tc.positive && (len(xs) != 1 || len(xs[0].MessageRefs) != len(tc.ms)) {
				t.Fatal("task fragmented or evidence lost")
			}
		})
	}
	t.Logf("E4 corpus: positive=%d negative=%d TP=%d FP=%d FN=%d precision=%.4f recall=%.4f complete_grouping=%d/%d", positives, len(e4RecallCorpus())-positives, tp, fp, positives-tp, float64(tp)/float64(tp+fp), float64(tp)/float64(positives), exact, positives)
}
