package efforts

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

type recallCase struct {
	name     string
	positive bool
	ms       []model.Message
}

// Shared with the E4 end-to-end corpus so every E3 negative remains a gate.
func e3RecallCorpus() []recallCase {
	message := func(id, text string, day int, mine bool) model.Message {
		return model.Message{ID: id, ChatJID: testkit.Phone, Sender: testkit.Phone, Content: text, Timestamp: testkit.Epoch.AddDate(0, 0, -4+day), IsFromMe: mine}
	}
	texts := func(values ...string) []model.Message {
		ms := []model.Message{}
		for i, v := range values {
			ms = append(ms, message(fmt.Sprint(i), v, i, i%2 == 1))
		}
		return ms
	}
	doc := message("doc", "", 0, false)
	doc.Filename, doc.MediaType = "01_Community-Garden Plan.pdf", "document"
	reply := message("reply", "Can you review?", 1, true)
	reply.QuotedMessageID = "doc"
	passed := doc
	passed.ID, passed.ChatJID = "passed", testkit.Group
	groupReply := reply
	groupReply.ChatJID, groupReply.QuotedMessageID = testkit.Group, "passed"
	oneDayReply := reply
	oneDayReply.IsFromMe = false
	oneDayReply.Timestamp = doc.Timestamp.Add(time.Hour)
	unquoted := reply
	unquoted.QuotedMessageID = ""
	return []recallCase{
		{"two people planning over days", true, texts("festival budget?", "festival budget confirmed", "festival budget tomorrow")},
		{"group coordinating event", true, func() []model.Message {
			ms := texts("lantern festival venue?", "lantern festival tickets", "lantern festival confirmed")
			for i := range ms {
				ms[i].ChatJID = testkit.Group
			}
			return ms
		}()},
		{"document with reply", true, []model.Message{doc, reply}},
		{"document passed around", true, []model.Message{doc, passed, groupReply}},
		{"one meaningful term", false, texts("pottery", "pottery", "pottery")},
		{"two short messages", false, texts("budget proposal", "budget proposal")},
		{"unrelated topics", false, texts("budget proposal", "budget festival", "proposal venue")},
		{"participant names", false, texts("Maya Rao hai yaar", "Maya Rao hai yaar", "Maya Rao hai yaar")},
		{"hinglish chatter", false, texts("haan bhai accha yaar", "haan bhai accha yaar", "haan bhai accha yaar")},
		{"link only", false, texts("https://example.test/garden/plan", "https://example.test/garden/plan", "https://example.test/garden/plan")},
		{"security", false, texts("verification code 123456", "verification code 123456", "verification code 123456")},
		{"placeholder", false, texts("image omitted", "image omitted", "image omitted")},
		{"document unquoted acknowledgement", false, []model.Message{doc, unquoted}},
		{"document same-day monologue", false, []model.Message{doc, oneDayReply}},
	}
}

// Labelled invented conversations, scored through production filtering/grouping.
func TestE3RecallCorpus(t *testing.T) {
	cases := e3RecallCorpus()
	beforeTP, afterTP, beforeFP, afterFP, positives := 0, 0, 0, 0, 0
	q := newQuality([]model.Person{{Name: "Maya Rao"}}, nil, nil, "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGrouper()
			g.rules = q
			g.add(tc.ms)
			before, after := false, false
			for _, c := range dedupeClusters(g.cs) {
				before = before || len(repeatedTerms(c.ms, q)) >= 2
				after = after || len(sharedTerms(c.ms, q)) >= 2
			}
			if tc.positive {
				positives++
				if before {
					beforeTP++
				}
				if after {
					afterTP++
				}
			} else {
				if before {
					beforeFP++
				}
				if after {
					afterFP++
				}
			}
			if after != tc.positive {
				t.Fatalf("classified effort=%v, labelled=%v", after, tc.positive)
			}
		})
	}
	t.Logf("E3 corpus: %d conversations (%d positive, %d negative); before TP=%d FP=%d FN=%d precision=%.2f recall=%.2f; after TP=%d FP=%d FN=%d precision=%.2f recall=%.2f", len(cases), positives, len(cases)-positives, beforeTP, beforeFP, positives-beforeTP, float64(beforeTP)/float64(beforeTP+beforeFP), float64(beforeTP)/float64(positives), afterTP, afterFP, positives-afterTP, float64(afterTP)/float64(afterTP+afterFP), float64(afterTP)/float64(positives))
	if beforeTP != 2 || beforeFP != 0 || afterTP != 4 || afterFP != 0 {
		t.Fatal("recall/precision regression")
	}
}
