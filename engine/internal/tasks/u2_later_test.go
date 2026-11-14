package tasks

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"path/filepath"
	"testing"
	"time"
)

func TestU2LaterPersistsProjectsAndPreservesExpiry(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.NeedsYou(a, v.ID, "Choose a time")
	testkit.Must(t, err)
	original, _ := s.raw(v.ID)
	*now = now.Add(time.Hour)
	until := now.Add(2 * time.Hour)
	got, err := s.Later(v.ID, &until)
	testkit.Must(t, err)
	if got.NeedsYou || got.LaterUntil == nil {
		t.Fatal("not snoozed")
	}
	assertHidden := func() {
		t.Helper()
		for _, agent := range []*model.Agent{nil, a} {
			got, err := s.Get(agent, v.ID)
			testkit.Must(t, err)
			cards, err := s.List(agent, "open", 100)
			testkit.Must(t, err)
			if got.NeedsYou || len(cards) != 1 || cards[0].NeedsYou || cards[0].LaterUntil == nil {
				t.Fatal("projection differs")
			}
		}
		people, err := s.NeedsYouPeople()
		testkit.Must(t, err)
		if len(people) > 0 {
			t.Fatal("red marker survived snooze")
		}
		feed, err := s.Feed(time.Time{})
		testkit.Must(t, err)
		if len(feed) != 1 || feed[0].NeedsYou || feed[0].LaterUntil == nil {
			t.Fatal("feed differs")
		}
	}
	assertHidden()
	// Reopen the on-disk store and construct a new runtime (no in-memory snooze).
	testkit.Must(t, s.Hub.Repo.Close())
	repo, err := hub.Open(filepath.Join(s.Hub.Home, "hub.db"))
	testkit.Must(t, err)
	t.Cleanup(func() { repo.Close() })
	s.Hub.Repo = repo
	s = New(s.Hub, s.Efforts)
	s.Location = time.UTC
	assertHidden()
	before, _ := s.raw(v.ID)
	var writes int
	testkit.Must(t, repo.DB.QueryRow("SELECT count(*) FROM sync_log WHERE bucket='tasks'").Scan(&writes))
	*now = now.Add(time.Minute)
	_, err = s.NeedsYou(a, v.ID, "Choose a time")
	testkit.Must(t, err)
	after, _ := s.raw(v.ID)
	var afterWrites int
	testkit.Must(t, repo.DB.QueryRow("SELECT count(*) FROM sync_log WHERE bucket='tasks'").Scan(&afterWrites))
	if dbutil.JSON(before) != dbutil.JSON(after) || writes != afterWrites {
		t.Fatal("repeat ask rewrote task")
	}
	*now = until
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if !got.NeedsYou {
		t.Fatal("snooze did not end")
	}
	after, _ = s.raw(v.ID)
	if !after.NeedsYouAt.Equal(*original.NeedsYouAt) {
		t.Fatal("snooze reset original expiry")
	}
	*now = original.NeedsYouAt.Add(model.NeedsYouTTL)
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.NeedsYou {
		t.Fatal("seven-day expiry reset")
	}
}

func TestU2LaterEarlyReturnOnlyForNewReasonOrPendingReview(t *testing.T) {
	for _, kind := range []string{"same", "different", "review", "old_review", "ordinary_message"} {
		t.Run(kind, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "long")
			_, err := s.NeedsYou(a, v.ID, "Choose a time")
			testkit.Must(t, err)
			if kind == "old_review" {
				testkit.Must(t, s.Hub.Repo.Put("suggestions", "old", model.Suggestion{ID: "old", TaskID: v.ID, State: "pending", CreatedAt: *now}))
			}
			*now = now.Add(time.Second)
			until := now.Add(3 * time.Hour)
			_, err = s.Later(v.ID, &until)
			testkit.Must(t, err)
			*now = now.Add(time.Second)
			switch kind {
			case "same":
				_, err = s.NeedsYou(a, v.ID, "Choose a time")
			case "different":
				_, err = s.NeedsYou(a, v.ID, "Review the new venue")
			case "review":
				err = s.Hub.Repo.Put("suggestions", "new", model.Suggestion{ID: "new", TaskID: v.ID, State: "pending", CreatedAt: *now})
			case "ordinary_message":
				err = s.Incoming(context.Background(), model.Message{ID: "ordinary", ChatJID: testkit.Phone, Sender: testkit.Phone, Timestamp: *now, Content: "Hello"})
			}
			testkit.Must(t, err)
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			want := kind == "different" || kind == "review"
			if got.NeedsYou != want || (got.LaterUntil == nil) != want {
				t.Fatalf("early return=%v want %v", got.NeedsYou, want)
			}
			_, err = s.Update(nil, v.ID, map[string]any{"title": v.Title})
			testkit.Must(t, err)
			raw, err := s.raw(v.ID)
			testkit.Must(t, err)
			if (raw.LaterUntil == nil) != want {
				t.Fatal("cleared state not persisted")
			}
		})
	}
}
func TestU2LaterDefaultCalendarAndUndo(t *testing.T) {
	s, a, now := fixture(t)
	s.Location, _ = time.LoadLocation("America/New_York")
	for _, hour := range []int{3, 4, 12, 23} {
		*now = time.Date(2026, 3, 7, hour, 30, 0, 0, s.Location)
		got := s.DefaultLater()
		day := 7
		if hour >= 4 {
			day = 8
		}
		if got.Day() != day || got.Hour() != 9 || got.Location() != s.Location {
			t.Fatal("wrong local default", got)
		}
	}
	v := create(t, s, a, "long")
	_, err := s.NeedsYou(a, v.ID, "Review")
	testkit.Must(t, err)
	_, err = s.Later(v.ID, nil)
	testkit.Must(t, err)
	got, err := s.BringBack(v.ID)
	testkit.Must(t, err)
	if !got.NeedsYou || got.LaterUntil != nil {
		t.Fatal("undo failed")
	}
	history, err := s.History(v.ID)
	testkit.Must(t, err)
	kinds := map[string]bool{}
	for _, e := range history {
		kinds[e.Kind] = true
	}
	if !kinds["later"] || !kinds["brought_back"] {
		t.Fatal("missing history")
	}
}
func TestU2DetectorChatCoverageAndPendingSet(t *testing.T) {
	cases := []struct {
		name    string
		records []record
		chats   []Entity
		want    bool
	}{
		{"union", []record{{Task: Task{Status: "active", Chats: []Entity{{ID: "a"}}}}, {Task: Task{Status: "waiting", Chats: []Entity{{ID: "b"}}}}}, []Entity{{ID: "a"}, {ID: "b"}}, true},
		{"quiet", []record{{Task: Task{Status: "quiet", Chats: []Entity{{ID: "a"}}}}}, []Entity{{ID: "a"}}, true},
		{"partial", []record{{Task: Task{Status: "active", Chats: []Entity{{ID: "a"}}}}}, []Entity{{ID: "a"}, {ID: "b"}}, false},
		{"pending unordered", []record{{Task: Task{Status: "suggested", Chats: []Entity{{ID: "b"}, {ID: "a"}}}}}, []Entity{{ID: "a"}, {ID: "b"}}, true},
		{"ended", []record{{Task: Task{Status: "done", Chats: []Entity{{ID: "a"}}}}}, []Entity{{ID: "a"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if detectorChatsCovered(c.chats, c.records) != c.want {
				t.Fatal("wrong coverage")
			}
		})
	}
}
