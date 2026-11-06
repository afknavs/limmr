package tasks

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestN1QuietAfterSevenDaysAndActivityWakes(t *testing.T) {
	for _, kind := range []string{"message", "owner_note", "checklist", "attachment", "check_in"} {
		t.Run(kind, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "long")
			*now = now.Add(7*24*time.Hour - time.Second)
			testkit.Must(t, s.Tick(context.Background()))
			v, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if v.Status != "active" {
				t.Fatal("quiet too soon")
			}
			*now = now.Add(time.Second)
			testkit.Must(t, s.Tick(context.Background()))
			v, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if v.Status != "quiet" || v.NeedsYou {
				t.Fatal("not quiet after seven days")
			}
			listed, err := s.List(a, "open", 500)
			testkit.Must(t, err)
			if len(listed) != 0 {
				t.Fatal("quiet on Home")
			}
			listed, err = s.List(a, "quiet", 500)
			testkit.Must(t, err)
			if len(listed) != 1 {
				t.Fatal("quiet not findable")
			}
			*now = now.Add(time.Hour)
			switch kind {
			case "message":
				m := model.Message{ID: "new", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "New plan", Timestamp: *now}
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
				testkit.Must(t, s.Incoming(context.Background(), m))
			case "owner_note":
				_, err = s.OwnerNote(nil, v.ID, "", "Use the new syllabus", false)
			case "checklist":
				_, err = s.Checklist(a, v.ID, "add", []Step{{Text: "Review"}})
			case "attachment":
				_, err = s.Attach(a, v.ID, Item{Kind: "note", Text: "A useful attachment"})
			case "check_in":
				_, err = s.Update(a, v.ID, map[string]any{"status": "active", "check_in": CheckIn{Rule: "once", AtTime: "09:00", To: testkit.Phone, NextAt: now.Add(time.Hour)}})
				testkit.Must(t, err)
				*now = now.Add(time.Hour)
				err = s.Tick(context.Background())
			}
			testkit.Must(t, err)
			v, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if v.Status != "active" || !v.LastActiveAt.Equal(*now) {
				t.Fatalf("activity did not wake: %s %v", v.Status, v.LastActiveAt)
			}
			testkit.Must(t, s.Tick(context.Background()))
			v, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if v.Status != "active" {
				t.Fatal("immediate requiet")
			}
		})
	}
}
func TestN1OwnerAndAIQuietWakeAndMissedMessage(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	for _, actor := range []*model.Agent{nil, a} {
		*now = now.Add(time.Hour)
		_, err := s.Update(actor, v.ID, map[string]any{"status": "quiet"})
		testkit.Must(t, err)
		testkit.Must(t, s.Tick(context.Background()))
		v, err = s.Get(a, v.ID)
		testkit.Must(t, err)
		if v.Status != "quiet" {
			t.Fatal("manual quiet immediately woke")
		}
		_, err = s.Update(actor, v.ID, map[string]any{"status": "active"})
		testkit.Must(t, err)
	}
	_, err := s.Update(a, v.ID, map[string]any{"status": "quiet"})
	testkit.Must(t, err)
	*now = now.Add(time.Hour)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "missed", ChatJID: testkit.Phone, Timestamp: *now, Content: "Updated"}))
	testkit.Must(t, s.Tick(context.Background()))
	v, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if v.Status != "active" {
		t.Fatal("missed incoming event did not wake on tick")
	}
}

func TestN1AutomaticQuietAndWakeNotifyMountedHome(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Exam guide", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, CheckIn: &CheckIn{Rule: "once", AtTime: "09:00", To: testkit.Phone, NextAt: now.Add(8 * 24 * time.Hour)}})
	testkit.Must(t, err)
	events, cancel := s.Hub.Subscribe()
	defer cancel()
	for _, days := range []int{7, 1} {
		*now = now.Add(time.Duration(days) * 24 * time.Hour)
		testkit.Must(t, s.Tick(context.Background()))
		select {
		case event := <-events:
			if event.Tool != "task_changed" {
				t.Fatal("wrong refresh cue")
			}
		case <-time.After(time.Second):
			t.Fatal("mounted Home not notified")
		}
	}
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.Status != "active" || !got.NeedsYou {
		t.Fatal("due check-in failed to wake quiet task")
	}
}
