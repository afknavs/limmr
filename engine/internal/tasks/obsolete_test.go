package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

func TestR3InactivityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status string
		lifespan, days     int
		legacy, deadline   bool
		want               bool
	}{
		{name: "default at 20", days: 20},
		{name: "default at 21", days: 21, want: true},
		{name: "waiting at 21", status: "waiting", days: 21, want: true},
		{name: "quiet at 21", status: "quiet", days: 21, want: true},
		{name: "explicit 40 at 39", lifespan: 40, days: 39},
		{name: "explicit 40 at 40", lifespan: 40, days: 40, want: true},
		{name: "explicit 5 at 4", lifespan: 5, days: 4},
		{name: "explicit 5 at 5", lifespan: 5, days: 5, want: true},
		{name: "explicit 14 at 14", lifespan: 14, days: 14, want: true},
		{name: "legacy 14 at 14", lifespan: 14, legacy: true, days: 14},
		{name: "legacy 14 at 21", lifespan: 14, legacy: true, days: 21, want: true},
		{name: "legacy 30 at 29", lifespan: 30, legacy: true, days: 29},
		{name: "legacy 30 at 30", lifespan: 30, legacy: true, days: 30, want: true},
		{name: "end date protects", lifespan: 5, days: 100, deadline: true},
		{name: "test at one day", kind: "test", lifespan: 40, days: 1, want: true},
		{name: "test with default", kind: "test", days: 1, want: true},
		{name: "suggestion is not a task", status: "suggested", kind: "test", days: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, now := fixture(t)
			kind := tc.kind
			if kind == "" {
				kind = "long"
			}
			in := Create{Title: "Science fair poster", Kind: kind, LifespanDays: tc.lifespan}
			if tc.deadline {
				at := now.Add(200 * 24 * time.Hour)
				in.EndAt = &at
			}
			var v Task
			var err error
			if tc.status == "suggested" {
				v, err = s.Suggest(nil, in, Suggestion{DetectedAt: *now})
			} else {
				v, err = s.Create(nil, in)
			}
			testkit.Must(t, err)
			if v.LifespanSet != (tc.lifespan != 0) {
				t.Fatal("explicit marker", v.LifespanSet)
			}
			if tc.legacy {
				_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_remove(data,'$.lifespan_set') WHERE id=?", v.ID)
				testkit.Must(t, err)
			}
			if tc.status == "waiting" || tc.status == "quiet" {
				_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.status',?) WHERE id=?", tc.status, v.ID)
				testkit.Must(t, err)
			}
			*now = now.Add(time.Duration(tc.days)*24*time.Hour - time.Nanosecond)
			// The exact threshold is inclusive; none of the ending cases may fire early.
			if tc.want {
				testkit.Must(t, s.Tick(context.Background()))
				got, e := s.Get(nil, v.ID)
				testkit.Must(t, e)
				if ended(got) {
					t.Fatal("ended before boundary", got)
				}
			}
			*now = now.Add(time.Nanosecond)
			testkit.Must(t, s.Tick(context.Background()))
			got, err := s.Get(nil, v.ID)
			testkit.Must(t, err)
			if ended(got) != tc.want {
				t.Fatalf("want ended=%v, got %s", tc.want, got.Status)
			}
			changes := membershipChanges(t, s, v.ID)
			if tc.want {
				wantDays := tc.lifespan
				if kind == "test" {
					wantDays = 1
				} else if wantDays == 0 || tc.legacy && wantDays == 14 {
					wantDays = 21
				}
				reason := "no activity for " + fmt.Sprint(wantDays) + " days"
				if kind == "test" {
					reason = "test task, a day without activity"
				}
				history, e := s.History(v.ID)
				testkit.Must(t, e)
				if got.Status != "dropped" || got.CloseReason != "obsolete" || got.ClosedAt == nil ||
					!got.ClosedAt.Equal(*now) || len(changes) != 1 || changes[0].Rule != "obsolete" ||
					history[0].Text != "Ended: "+reason || history[0].ChangeID != changes[0].ID {
					t.Fatalf("closure/history: %+v %+v", got, history)
				}
			} else if len(changes) != 0 {
				t.Fatal("unexpected rule", changes)
			}
		})
	}
}

func TestR3ActivityAndReopen(t *testing.T) {
	for _, mode := range []string{"message", "owner note", "reopened", "legacy reopen event", "test"} {
		t.Run(mode, func(t *testing.T) {
			s, _, now, v := finishedFixture(t)
			start := *now
			*now = start.Add(20 * 24 * time.Hour)
			switch mode {
			case "message":
				// Do not call Incoming: Tick must notice activity before it decides expiry.
				testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new-plan", ChatJID: testkit.Phone, Content: "Another poster idea", Timestamp: *now}))
			case "owner note":
				_, err := s.OwnerNote(nil, v.ID, "", "Consider a green background", false)
				testkit.Must(t, err)
			default:
				_, err := s.Update(nil, v.ID, map[string]any{"status": "dropped"})
				testkit.Must(t, err)
				_, err = s.Reopen(v.ID)
				testkit.Must(t, err)
				// Exercise the guard independently of Reopen's refreshed LastActiveAt.
				_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.last_active_at',?) WHERE id=?", start.Format(time.RFC3339Nano), v.ID)
				testkit.Must(t, err)
				if mode == "legacy reopen event" {
					_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_remove(data,'$.reopened_at') WHERE id=?", v.ID)
					testkit.Must(t, err)
				}
				if mode == "test" {
					_, err = s.Update(nil, v.ID, map[string]any{"kind": "test"})
					testkit.Must(t, err)
				}
			}
			reset := *now
			*now = reset.Add(10 * 24 * time.Hour)
			testkit.Must(t, s.Tick(context.Background()))
			got, err := s.Get(nil, v.ID)
			testkit.Must(t, err)
			if ended(got) {
				t.Fatal("activity/reopen protection ignored", got)
			}
			*now = reset.Add(21 * 24 * time.Hour)
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(nil, v.ID)
			testkit.Must(t, err)
			if got.Status != "dropped" || got.CloseReason != "obsolete" {
				t.Fatal("clock did not resume", got)
			}
		})
	}
}

func TestR3UndoRestoresBeforeStateForThirtyDays(t *testing.T) {
	for _, age := range []time.Duration{0, 30*24*time.Hour - time.Nanosecond, 30 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			s, a, now, v := finishedFixture(t)
			_, err := s.Update(nil, v.ID, map[string]any{"status": "quiet", "lifespan_days": 5})
			testkit.Must(t, err)
			before := mustRaw(t, s, v.ID)
			*now = now.Add(5 * 24 * time.Hour)
			testkit.Must(t, s.Tick(context.Background()))
			changes := membershipChanges(t, s, v.ID)
			if len(changes) != 1 {
				t.Fatal(changes)
			}
			var snapshot record
			testkit.Must(t, json.Unmarshal(changes[0].Before, &snapshot))
			if dbutil.JSON(snapshot) != dbutil.JSON(before) {
				t.Fatal("wrong before-state")
			}
			if _, err = s.UndoMembershipChange(a, v.ID, changes[0].ID); !errors.Is(err, model.ErrForbidden) {
				t.Fatal("agent undo", err)
			}
			*now = now.Add(age)
			got, err := s.UndoMembershipChange(nil, v.ID, changes[0].ID)
			if age == 30*24*time.Hour {
				if !errors.Is(err, model.ErrConflict) {
					t.Fatal("expired undo", err)
				}
				return
			}
			testkit.Must(t, err)
			// Restore status and contents exactly; owner reopen protection is renewed.
			if got.Status != before.Status || got.ClosedAt != nil || got.CloseReason != "" ||
				got.LifespanDays != 5 || !got.LifespanSet || !got.LastActiveAt.Equal(before.LastActiveAt) ||
				got.Title != before.Title || !reflect.DeepEqual(got.Checklist, before.Checklist) {
				t.Fatal("undo", got)
			}
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(nil, v.ID)
			testkit.Must(t, err)
			if ended(got) {
				t.Fatal("undo immediately replayed")
			}
		})
	}
}

func TestR3FullTickSilentBeforeDueAttention(t *testing.T) {
	for _, kind := range []string{"long", "test"} {
		t.Run(kind, func(t *testing.T) {
			s, _, now := fixture(t)
			days := 21
			if kind == "test" {
				days = 1
			}
			v, err := s.Create(nil, Create{Title: "Science fair poster", Kind: kind, Chats: []Entity{{ID: testkit.Phone}}, People: []Entity{{ID: model.PersonID(testkit.Phone), Name: "Maya Rao"}}, CheckIn: &CheckIn{Rule: "once", AtTime: "09:00", To: testkit.Phone, NextAt: now.Add(time.Duration(days) * 24 * time.Hour)}})
			testkit.Must(t, err)
			*now = now.Add(time.Duration(days) * 24 * time.Hour)
			// Waiting nudge is also due; obsolete must take priority over both paths.
			_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.waiting',json(?)) WHERE id=?", dbutil.JSON(Waiting{On: &Entity{ID: model.PersonID(testkit.Phone), Name: "Maya Rao"}, What: "layout", Since: now.Add(-4 * 24 * time.Hour), NudgeAfterDays: 3, NudgeState: "none"}), v.ID)
			testkit.Must(t, err)
			count := finishTickCounts(t, s)
			recent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			history, err := s.History(v.ID)
			testkit.Must(t, err)
			stream, cancel := s.Hub.Subscribe()
			defer cancel()
			testkit.Must(t, s.Tick(context.Background()))
			testkit.Must(t, s.Tick(context.Background())) // no repeated rule line
			got, err := s.Get(nil, v.ID)
			testkit.Must(t, err)
			afterRecent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			afterHistory, err := s.History(v.ID)
			testkit.Must(t, err)
			if got.Status != "dropped" || got.NeedsYou || got.NeedsYouReason != "" ||
				len(stream) != 0 || !reflect.DeepEqual(count, finishTickCounts(t, s)) ||
				!reflect.DeepEqual(recent, afterRecent) || len(afterHistory) != len(history)+1 ||
				len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
				t.Fatal("automatic ending produced attention")
			}
			home, err := s.List(nil, "open", 100)
			testkit.Must(t, err)
			if len(home) != 0 {
				t.Fatal("closed task on Home", home)
			}
		})
	}
}

func TestR3LifespanUpdateIsExplicit(t *testing.T) {
	s, _, now, v := finishedFixture(t)
	if v.LifespanSet {
		t.Fatal("default marked explicit")
	}
	got, err := s.Update(nil, v.ID, map[string]any{"lifespan_days": 14})
	testkit.Must(t, err)
	if !got.LifespanSet {
		t.Fatal("explicit default value not recorded")
	}
	*now = now.Add(14 * 24 * time.Hour)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "dropped" {
		t.Fatal("explicit 14 used default 21")
	}
}

func TestR3SuggestionsKeepFourteenDayExpiry(t *testing.T) {
	s, _, now := fixture(t)
	v, err := s.Suggest(nil, Create{Title: "Scratch suggestion", Kind: "test"}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	*now = now.Add(13 * 24 * time.Hour)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "suggested" {
		t.Fatal("R3 ended a suggestion", got)
	}
	*now = now.Add(24 * time.Hour)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.CloseReason != "suggestion_expired" || len(membershipChanges(t, s, v.ID)) != 0 {
		t.Fatal("suggestion expiry changed", got)
	}
}

func TestR3DetectorNeverCreatesTestKind(t *testing.T) {
	s, a, now := fixture(t)
	a.Preset = "own"
	testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
	surfacingChat(t, s, a, *now, testkit.Group, "Study material", "exam material is due", true, 3, true)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.List(nil, "suggested", 100)
	testkit.Must(t, err)
	if len(got) == 0 {
		t.Fatal("detector did not produce suggestions")
	}
	for _, v := range got {
		if v.Kind == "test" {
			t.Fatal("detector guessed test kind", v)
		}
	}
}
