package tasks

import (
	"context"
	"testing"
	"time"

	"limmr/internal/testkit"
)

// Exercise the real writers, including both duplicate routes, rather than
// manufacturing change objects that could hide a missing history/change write.
func historyUndoFixture(t *testing.T, kind string) (*Service, *time.Time, string) {
	t.Helper()
	s, _, now, v := finishedFixture(t)
	switch kind {
	case "attach_evidence":
		_, err := s.AttachEvidence(v.ID, []Entity{{ID: testkit.Group}}, nil, nil, "detector", "the matching poster conversation")
		testkit.Must(t, err)
	case "owner_remove":
		_, err := s.RemoveMembers(nil, v.ID, []string{testkit.Phone}, nil)
		testkit.Must(t, err)
	case "duplicate_keep", "duplicate_detected":
		var src Task
		s, _, now, v, src, _ = duplicateFixture(t, "active")
		if kind == "duplicate_keep" {
			_, err := s.Keep(nil, src.ID)
			testkit.Must(t, err)
		} else {
			testkit.Must(t, s.attachDetected(src, v.ID))
		}
	case "finished":
		finishMessagePut(t, s, now, "Science fair poster is done", "owner", now.Add(time.Minute))
		_, err := s.applyFinished(mustRaw(t, s, v.ID))
		testkit.Must(t, err)
	case "obsolete", "test":
		if kind == "test" {
			_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.kind','test') WHERE id=?", v.ID)
			testkit.Must(t, err)
		}
		*now = now.Add(22 * 24 * time.Hour)
		_, err := s.applyObsolete(mustRaw(t, s, v.ID))
		testkit.Must(t, err)
	case "follows_work":
		s, v, _, _ = followsFixture(t)
		now = ptrTime(s.now())
		s.Hub.Now = func() time.Time { return *now }
		testkit.Must(t, s.applyFollowsWork(mustRaw(t, s, v.ID)))
	case "protocol_demote", "suggestion_expiry", "deadline":
		protocolSeed(t, s, *now)
		v.ID = "auto"
		if kind == "suggestion_expiry" {
			v.ID = "expired"
		}
		if kind == "deadline" {
			v.ID = "deadline"
		}
		writeProtocolApproval(t, s)
		testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), nil))
	default:
		t.Fatal(kind)
	}
	return s, now, v.ID
}

func TestHistoryUndoAllChangeWritersAndThirtyDays(t *testing.T) {
	for _, kind := range []string{"attach_evidence", "owner_remove", "duplicate_keep", "duplicate_detected", "finished", "deadline", "obsolete", "test", "follows_work", "protocol_demote", "suggestion_expiry"} {
		t.Run(kind, func(t *testing.T) {
			for _, expired := range []bool{false, true} {
				name := "before deadline"
				if expired {
					name = "at deadline"
				}
				t.Run(name, func(t *testing.T) {
					s, now, id := historyUndoFixture(t, kind)
					history, err := s.History(id)
					testkit.Must(t, err)
					var event Event
					for _, e := range history {
						if e.ChangeID != "" {
							event = e
							break
						}
					}
					if event.ChangeID == "" || event.Reason == "" || event.Undone || event.UndoUntil != now.Add(30*24*time.Hour).Format(time.RFC3339Nano) {
						t.Fatal("missing plain reason / change / undo window", history)
					}
					until, err := time.Parse(time.RFC3339Nano, event.UndoUntil)
					testkit.Must(t, err)
					*now = until.Add(-time.Nanosecond)
					if expired {
						*now = until
					}
					before := protocolState(t, s)
					_, err = s.UndoMembershipChange(nil, id, event.ChangeID)
					if expired {
						if err == nil || protocolState(t, s) != before {
							t.Fatal("expired undo changed state", err)
						}
						return
					}
					testkit.Must(t, err)
					history, err = s.History(id)
					testkit.Must(t, err)
					marked := false
					for _, e := range history {
						if e.ChangeID == event.ChangeID {
							marked = e.Undone && e.Reason == event.Reason && e.UndoUntil == event.UndoUntil
						}
					}
					if !marked {
						t.Fatal("original line did not become Undone", history)
					}
					if _, err := s.UndoMembershipChange(nil, id, event.ChangeID); err == nil {
						t.Fatal("undo reused")
					}
				})
			}
		})
	}
}
