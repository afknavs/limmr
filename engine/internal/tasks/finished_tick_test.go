package tasks

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

type finishTickFootprint struct {
	broadcasts map[string]int
	events     map[string]int
	recent     map[string]int
	attention  []int
}

func finishTickCounts(t *testing.T, s *Service) []int {
	t.Helper()
	counts := []int{}
	for _, query := range []string{
		"SELECT count(*) FROM activity",
		"SELECT count(*) FROM agent_updates",
		"SELECT count(*) FROM objects WHERE bucket='suggestions'",
		"SELECT count(*) FROM objects WHERE bucket='drafts'",
		"SELECT count(*) FROM objects WHERE bucket='approvals'",
	} {
		var n int
		testkit.Must(t, s.Hub.Repo.DB.QueryRow(query).Scan(&n))
		counts = append(counts, n)
	}
	return counts
}

// Compare complete ticks, including detection and lifecycle work. A finish may
// avoid ordinary lifecycle work, but may never add an attention side effect.
func TestR2FullTickAddsNoAttention(t *testing.T) {
	for _, mode := range []string{"owner", "contact", "recorded AI", "no reference", "question", "negation", "contraction", "future", "future completed", "going to", "substring", "done effort", "dormant effort", "deadline", "checklist", "detector duplicate"} {
		t.Run(mode, func(t *testing.T) {
			run := func(evidence bool) finishTickFootprint {
				s, a, now, v := finishedFixture(t)
				wantDone, wantHint, wantRule := false, false, ""
				switch mode {
				case "done effort", "dormant effort":
					id := finishEffortFixture(t, s, now)
					status := "moving"
					if evidence {
						status = map[string]string{"done effort": "done", "dormant effort": "dormant"}[mode]
						wantHint = true
					}
					_, err := s.Efforts.Update(nil, id, efforts.Update{Status: &status})
					testkit.Must(t, err)
					_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.effort_id',?) WHERE id=?", id, v.ID)
					testkit.Must(t, err)
				case "deadline":
					if evidence {
						_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.end_at',?) WHERE id=?", now.AddDate(0, 0, -1).Format(time.RFC3339Nano), v.ID)
						testkit.Must(t, err)
						wantDone, wantRule = true, "rule_finished"
					}
				case "checklist":
					// Seed the checklist without closing before the subscribed tick.
					steps := []Step{{ID: "delivery", Text: "Deliver poster", Done: evidence}}
					_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.end_condition','checklist_done','$.checklist',json(?)) WHERE id=?", dbutil.JSON(steps), v.ID)
					testkit.Must(t, err)
					wantDone = evidence
					if evidence {
						wantRule = "rule_finished"
					}
				case "detector duplicate":
					a.Preset = "own"
					testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
					surfacingChat(t, s, a, *now, testkit.Group, "Study material", "exam material is due", true, 3, true)
					refs := []Ref{}
					if !evidence {
						// The same detection has nothing new to attach in the control.
						for i := 0; i < 4; i++ {
							refs = append(refs, Ref{ChatJID: testkit.Group, MessageID: fmt.Sprintf("%s-%d", testkit.Group, i)})
						}
					}
					_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.title','Shared work: Exam','$.goal','Shared work: Exam','$.chats',json(?),'$.evidence_refs',json(?)) WHERE id=?", dbutil.JSON([]Entity{{ID: testkit.Group}}), dbutil.JSON(refs), v.ID)
					testkit.Must(t, err)
					if evidence {
						wantRule = "rule_duplicate"
					}
				default:
					text, author := "Reviewing the poster", "owner"
					if mode == "contact" {
						author = "contact"
					} else if mode == "recorded AI" {
						author = "agent"
					}
					if evidence {
						text = map[string]string{
							"owner": "Submitted the poster", "contact": "Submitted the poster", "recorded AI": "Submitted the poster", "no reference": "done",
							"question": "submitted?", "negation": "not submitted yet", "contraction": "haven't submitted the poster", "future": "will submit tomorrow",
							"future completed": "will have submitted the poster tomorrow", "going to": "going to send the poster", "substring": "undone poster",
						}[mode]
						wantDone = mode == "owner"
						wantHint = mode == "contact" || mode == "recorded AI" || mode == "no reference"
						if wantDone {
							wantRule = "rule_finished"
						}
					}
					finishMessagePut(t, s, now, text, author, now.Add(time.Minute))
				}
				counts := finishTickCounts(t, s)
				var eventCursor int
				testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT coalesce(max(rowid),0) FROM task_events").Scan(&eventCursor))
				beforeRecent, err := s.Recently(nil, "")
				testkit.Must(t, err)
				stream, cancel := s.Hub.Subscribe()
				defer cancel()
				testkit.Must(t, s.Tick(context.Background()))
				out := finishTickFootprint{broadcasts: map[string]int{}, events: map[string]int{}, recent: map[string]int{}, attention: finishTickCounts(t, s)}
				for len(stream) > 0 {
					out.broadcasts[dbutil.JSON(<-stream)]++
				}
				for i := range out.attention {
					out.attention[i] -= counts[i]
					if out.attention[i] != 0 {
						t.Fatalf("evidence=%v added activity/updates/suggestions/drafts/approvals: %v", evidence, out.attention)
					}
				}
				if len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
					t.Fatal("tick attempted outbound delivery")
				}
				rows, err := s.Hub.Repo.DB.Query("SELECT kind,text FROM task_events WHERE rowid>? AND kind NOT LIKE 'rule_%' ORDER BY rowid", eventCursor)
				testkit.Must(t, err)
				for rows.Next() {
					var kind, text string
					testkit.Must(t, rows.Scan(&kind, &text))
					out.events[kind+": "+text]++
				}
				testkit.Must(t, rows.Err())
				testkit.Must(t, rows.Close())
				afterRecent, err := s.Recently(nil, "")
				testkit.Must(t, err)
				for _, line := range afterRecent {
					out.recent[line]++
				}
				for _, line := range beforeRecent {
					out.recent[line]--
				}
				got, err := s.Get(nil, v.ID)
				testkit.Must(t, err)
				if (got.Status == "done") != wantDone || (got.FinishHint != nil) != wantHint || got.NeedsYou {
					t.Fatalf("evidence=%v tick did not exercise expected outcome: %+v", evidence, got)
				}
				changes := membershipChanges(t, s, v.ID)
				if wantRule == "" && len(changes) != 0 || wantRule != "" && (len(changes) != 1 || changes[0].Kind != wantRule) {
					t.Fatalf("evidence=%v expected rule %q, got %+v", evidence, wantRule, changes)
				}
				t.Logf("evidence=%v broadcasts=%v non-rule events=%v recent delta=%v activity/updates/suggestions/drafts/approvals=%v", evidence, out.broadcasts, out.events, out.recent, out.attention)
				return out
			}
			control, withEvidence := run(false), run(true)
			if !reflect.DeepEqual(control.attention, withEvidence.attention) {
				t.Fatal("automatic rule changed attention", control, withEvidence)
			}
			for name, pair := range map[string][2]map[string]int{
				"broadcasts":      {control.broadcasts, withEvidence.broadcasts},
				"non-rule events": {control.events, withEvidence.events},
				"Recently":        {control.recent, withEvidence.recent},
			} {
				for event, count := range pair[1] {
					if count > 0 && count > pair[0][event] {
						t.Fatalf("automatic rule added %s: %s (%d > %d)", name, event, count, pair[0][event])
					}
				}
			}
		})
	}
}
