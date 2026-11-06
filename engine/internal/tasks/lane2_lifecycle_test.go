package tasks

import (
	"context"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestLane2DueCheckInFiresAndSurvivesRestart(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Review workshop", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, CheckIn: &CheckIn{Rule: "once", AtTime: "09:00", To: testkit.Phone, NextAt: now.Add(time.Hour)}})
	testkit.Must(t, err)
	*now = now.Add(2 * time.Hour)
	testkit.Must(t, s.Tick(context.Background()))
	restarted := New(s.Hub, s.Efforts)
	testkit.Must(t, restarted.Tick(context.Background()))
	var updates, queued int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates WHERE agent=? AND kind='task_due'", a.ID).Scan(&updates))
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.kind')='check_in' AND json_extract(data,'$.state')='pending'", v.ID).Scan(&queued))
	if updates != 1 || queued != 1 {
		t.Fatalf("due check-in not delivered exactly once: updates=%d queued=%d", updates, queued)
	}
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	scheduled, fired := false, false
	for _, e := range got.History {
		scheduled = scheduled || e.Kind == "check_in_scheduled"
		fired = fired || e.Kind == "due"
	}
	if !scheduled || !fired || !got.NeedsYou || got.CheckIn != nil {
		t.Fatal("missing visible schedule/fire/review state")
	}
}

func TestLane2QuietExpiryTracksTextActivityAndUndo(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Review workshop", Kind: "short", LifespanDays: 2, Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	*now = now.Add(48 * time.Hour)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "recent", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "An ordinary update", Timestamp: now.Add(-time.Hour)}))
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "active" {
		t.Fatal("active conversation closed")
	}
	// Summary/timer updates must not prolong quiet tasks.
	*now = now.Add(49 * time.Hour)
	_, err = s.Update(a, v.ID, map[string]any{"summary": Summary{Text: "Still waiting"}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.CloseReason != "lifespan" {
		t.Fatal("quiet task did not close")
	}
	automatic := false
	for _, e := range got.History {
		automatic = automatic || e.Text == "Closed automatically"
	}
	if !automatic {
		t.Fatal("automatic close missing from history")
	}
	_, err = s.Reopen(v.ID)
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "active" {
		t.Fatal("undo immediately reclosed")
	}
}

func TestLane2EndDateAgentDoneAndChecklistCondition(t *testing.T) {
	s, a, now := fixture(t)
	end := now.Add(time.Hour)
	v, err := s.Create(a, Create{Title: "Deadline", Kind: "long", EndAt: &end, EndCondition: "agent_done"})
	testkit.Must(t, err)
	*now = now.Add(2 * time.Hour)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "done" {
		t.Fatal("end date not honored")
	}
	_, err = s.Reopen(v.ID)
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "active" || got.EndAt != nil {
		t.Fatal("expired end date replayed on undo")
	}
	_, err = s.Update(a, v.ID, map[string]any{"status": "done"})
	testkit.Must(t, err)
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.CloseReason != "agent_done" {
		t.Fatal("agent completion not credited")
	}
	v, err = s.Create(a, Create{Title: "Checklist", Kind: "long", EndCondition: "checklist_done", Checklist: []Step{{Text: "Review", Done: true}}})
	testkit.Must(t, err)
	if v.Status != "done" {
		t.Fatal("explicit checklist end condition ignored")
	}
	_, err = s.Reopen(v.ID)
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.Status != "active" {
		t.Fatal("checklist closure Undo immediately reclosed")
	}

}

func TestLane2ReplyCheckInAndMediaCollection(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Review", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, CheckIn: &CheckIn{Rule: "reply", AtTime: "09:00", To: testkit.Phone, NextAt: *now}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.CheckIn == nil {
		t.Fatal("reply check-in fired without reply")
	}
	*now = now.Add(time.Minute)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "file", ChatJID: testkit.Phone, Sender: testkit.Phone, MediaType: "document", Filename: "Workshop.pdf", Timestamp: *now}))
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if got.CheckIn != nil || len(got.Items) != 1 || got.Items[0].Kind != "file" || got.Items[0].MessageRef == nil {
		t.Fatal("reply check-in or file collection missing")
	}
}

func TestLane2TaskShowsAllKnownGroupMembers(t *testing.T) {
	s, a, _ := fixture(t)
	for i := 0; i < 18; i++ {
		id := fmt.Sprint("participant-", i)
		testkit.Must(t, s.Hub.Repo.Put("people", id, model.Person{ID: id, Name: fmt.Sprint("Participant ", i)}))
		testkit.Must(t, s.Hub.Repo.Put("memberships", id, model.Membership{Group: testkit.Group, Person: id}))
	}
	v, err := s.Create(a, Create{Title: "Workshop", Kind: "long", Chats: []Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	got, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	count := 0
	for _, p := range got.People {
		if strings.HasPrefix(p.ID, "participant-") {
			count++
		}
	}
	if count != 18 {
		t.Fatalf("partial participants: %d", count)
	}
}

func TestLane2LegacyHomeCardFindsCurrentCircles(t *testing.T) {
	s, a, _ := fixture(t)
	testkit.Must(t, s.Hub.Repo.Put("circles", "lane2-circle", model.Circle{ID: "lane2-circle", Name: "Workshop", Chats: []model.CircleChat{{ChatJID: testkit.Group}}}))
	v, err := s.Create(a, Create{Title: "Workshop", Kind: "long", Chats: []Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.circle_ids',json('[]')) WHERE id=?", v.ID)
	testkit.Must(t, err)
	cards, err := s.List(nil, "open", 100)
	testkit.Must(t, err)
	if len(cards) != 1 || !model.Contains(cards[0].CircleIDs, "lane2-circle") {
		t.Fatal("older task lost current circles")
	}
}

func TestLane2QuietDismissDoesNotResuggestOldWork(t *testing.T) {
	for _, status := range []string{"done", "dropped"} {
		t.Run(status, func(t *testing.T) {
			s, a, now := fixture(t)
			v, err := s.Create(a, Create{Title: "Review CV", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
			testkit.Must(t, err)
			_, err = s.Update(nil, v.ID, map[string]any{"status": status})
			testkit.Must(t, err)
			candidate := Task{Title: v.Title, Chats: v.Chats}
			covered, err := s.covered(candidate)
			testkit.Must(t, err)
			if !covered {
				t.Fatal("old work would immediately reappear")
			}
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "fresh-work", ChatJID: testkit.Phone, Content: "There is new work", Timestamp: now.Add(time.Hour)}))
			covered, err = s.covered(candidate)
			testkit.Must(t, err)
			if covered {
				t.Fatal("new activity cannot surface work")
			}
		})
	}
}
