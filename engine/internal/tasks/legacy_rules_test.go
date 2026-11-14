package tasks

import (
	"context"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestTaskRulesLegacyRowsRemainReadableWithoutBackfill(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	// A populated pre-lane row has no new optional fields. Keep a nonconforming
	// legacy title/empty goal to prove reads and unrelated writes do not rewrite it.
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	r.Title = "Old title with more than five words"
	r.Goal = ""
	r.NeedsYou = true
	r.FlagReason = "Old request"
	r.NeedsYouAt = nil
	r.CoverItemID = ""
	r.Items = []Item{{ID: "legacy-note", Kind: "note", Source: "owner", Text: "Retain this history"}}
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), r.ID)
	testkit.Must(t, err)
	var before string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", v.ID).Scan(&before))
	for i := 0; i < 2; i++ {
		testkit.Must(t, s.Hub.Repo.Migrate(nil))
	}
	var after string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", v.ID).Scan(&after))
	if before != after {
		t.Fatal("migrate rewrote old task")
	}
	*now = now.Add(8 * 24 * time.Hour)
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.Follows != "" || got.MergedInto != "" || got.Subject != nil || got.SubjectEdited || got.NeedsYou || got.Goal != "" || len(got.Items) != 1 {
		t.Fatal("bad legacy defaults")
	}
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if ended(got) || got.Title != r.Title || len(got.Items) != 1 || got.Goal != "" {
		t.Fatal("legacy tick lost record")
	}
}

func TestTaskRulesPendingReviewExpiresWithoutDeletingEvidence(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Review the venue", Goal: "Choose a venue", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, CheckIn: &CheckIn{Rule: "once", AtTime: "09:00", To: testkit.Phone, NextAt: *now}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	var raw string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT data FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=?", v.ID).Scan(&raw))
	var suggestion model.Suggestion
	testkit.Must(t, json.Unmarshal([]byte(raw), &suggestion))
	if !suggestion.ExpiresAt.Equal(suggestion.CreatedAt.Add(model.NeedsYouTTL)) {
		t.Fatal("suggestion uses a different TTL")
	}
	*now = now.Add(model.NeedsYouTTL - time.Second)
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if !got.NeedsYou {
		t.Fatal("pending review expired early")
	}
	*now = now.Add(time.Second)
	testkit.Must(t, s.Tick(context.Background()))
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.NeedsYou {
		t.Fatal("stale pending review surfaced")
	}
	var after string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT data FROM objects WHERE bucket='suggestions' AND id=?", suggestion.ID).Scan(&after))
	if after != raw {
		t.Fatal("expiry rewrote stored review")
	}
}
