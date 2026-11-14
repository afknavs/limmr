package sync_test

import (
	"limmr/internal/dbutil"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"testing"
	"time"
)

func TestU2LaterSyncRoundTripAndNoop(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "study", "Study", nil)
	pair(t, a, b)
	_, err := a.tasks.NeedsYou(nil, "study", "Choose a time")
	must(t, err)
	until := a.now.Add(3 * time.Hour)
	_, err = a.tasks.Later("study", &until)
	must(t, err)
	exchange(t, a, b)
	for _, e := range []*engine{a, b} {
		v, err := e.tasks.Get(nil, "study")
		must(t, err)
		if v.NeedsYou || v.LaterAt == nil || v.LaterUntil == nil || !v.LaterUntil.Equal(until) {
			t.Fatal("lost snooze in sync")
		}
	}
	var before, after int
	must(t, b.h.Repo.DB.QueryRow("SELECT count(*) FROM sync_log WHERE bucket='tasks'").Scan(&before))
	b.now = b.now.Add(time.Minute)
	must(t, b.clock())
	_, err = b.tasks.NeedsYou(nil, "study", "Choose a time")
	must(t, err)
	must(t, b.h.Repo.DB.QueryRow("SELECT count(*) FROM sync_log WHERE bucket='tasks'").Scan(&after))
	if before != after {
		t.Fatal("repeat synced ask wrote task")
	}
	_, err = b.tasks.BringBack("study")
	must(t, err)
	exchange(t, b, a)
	v, err := a.tasks.Get(nil, "study")
	must(t, err)
	if !v.NeedsYou || v.LaterUntil != nil {
		t.Fatal("bring back did not sync")
	}
}

func TestU2ArchivedCircleSyncStatusRetainsRestoreData(t *testing.T) {
	a := newEngine(t)
	for _, id := range []string{"old-a", "old-b"} {
		put(t, a, "circles", id, model.Circle{ID: id, Name: "Old study", Confirmed: true, Archived: id == "old-a"})
	}
	_, err := a.h.Repo.DB.Exec("INSERT INTO sync_reviews VALUES(?,?)", "review-old", dbutil.JSON(map[string]any{"id": "review-old", "bucket": "circles", "ids": []string{"old-a", "old-b"}, "name": "Old study", "state": "pending"}))
	must(t, err)
	status, err := a.s.Status()
	must(t, err)
	if len(status["merge_reviews"].([]syncer.Review)) != 0 {
		t.Fatal("sync status exposes archive")
	}
	raw, err := a.s.Reviews()
	must(t, err)
	if len(raw) != 1 {
		t.Fatal("lost restore record")
	}
	put(t, a, "circles", "old-a", model.Circle{ID: "old-a", Name: "Old study", Confirmed: true})
	status, err = a.s.Status()
	must(t, err)
	if len(status["merge_reviews"].([]syncer.Review)) != 1 {
		t.Fatal("restored review missing")
	}
}
