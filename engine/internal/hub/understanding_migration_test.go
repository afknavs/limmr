package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestUnderstandingMigrationBackfillsUnknownAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, e := hub.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	c := model.Circle{ID: "old", Name: "University", CreatedBy: "old-agent", Confirmed: true, UpdatedAt: at, People: []model.CirclePerson{{PersonID: "maya", Role: "HOD", Source: "old-agent"}}, Topics: []model.Topic{{ID: "topic", Title: "Materials"}}}
	if e = r.Put("circles", c.ID, c); e != nil {
		t.Fatal(e)
	}
	if e = r.Put("proposals", c.ID, model.Proposal{ID: c.ID, Circle: c}); e != nil {
		t.Fatal(e)
	}
	if _, e = r.DB.Exec("DELETE FROM hub_schema WHERE version=5"); e != nil {
		t.Fatal(e)
	}
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e = hub.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var got model.Circle
		if e = r.Get("circles", c.ID, &got); e != nil {
			t.Fatal(e)
		}
		for _, o := range []*model.Origin{got.Origin, got.ConfirmOrigin, got.People[0].Origin, got.Topics[0].Origin} {
			if o == nil || o.ByKind != "structure" || o.By != "Structure" || !o.At.Equal(at) {
				t.Fatal("migration guessed author")
			}
		}
		var proposal model.Proposal
		if e = r.Get("proposals", c.ID, &proposal); e != nil {
			t.Fatal(e)
		}
		if proposal.Circle.People[0].Origin == nil {
			t.Fatal("proposal not migrated")
		}
		if e = r.Close(); e != nil {
			t.Fatal(e)
		}
	}
}
