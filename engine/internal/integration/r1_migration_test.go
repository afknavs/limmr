package integration_test

import (
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestR1MigrationRelabelsOnlyStructuralAddsAndGroupUndo(t *testing.T) {
	r := newRig(t)
	c := model.Circle{ID: "legacy", Name: "Workshop", Kind: "project", SendRule: "confirm", Confirmed: true}
	type saved struct {
		Change circles.AutoChange
		Before *model.Circle
		After  model.Circle
	}
	note := "Example AI updated Workshop · Undo available for 24 hours"
	for i := 0; i < 40; i++ {
		before := c
		before.People = append([]model.CirclePerson{}, c.People...)
		at := epoch.Add(time.Duration(i-40) * time.Second)
		p := model.CirclePerson{PersonID: fmt.Sprintf("legacy-person-%02d", i), Source: "demo-ai", Origin: &model.Origin{By: "demo-ai", ByKind: "agent", At: at}}
		c.People = append(append([]model.CirclePerson{}, c.People...), p)
		c.UpdatedAt = at
		id := fmt.Sprintf("legacy-%02d", i)
		must(t, r.h.Repo.Put("auto_circle_changes", id, saved{circles.AutoChange{ID: id, CircleID: c.ID, Name: c.Name, Agent: "Example AI", Action: "updated", At: at}, &before, c}))
		must(t, r.h.Repo.Put("affinity_proposed", c.ID+":"+p.PersonID, true))
		must(t, r.h.Record(model.Activity{At: at.Add(time.Millisecond), AgentID: "demo-ai", Kind: "people_write", Tool: "circle_auto_updated", Note: note, Count: 1}))
	}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	// Same actor, note and timestamp, but a different circle/person without an affinity key.
	before := model.Circle{ID: "ordinary", Name: "Workshop", Confirmed: true, Kind: "project", SendRule: "confirm"}
	after := before
	after.People = []model.CirclePerson{{PersonID: "ordinary-member", Source: "demo-ai"}}
	change := circles.AutoChange{ID: "ordinary-change", CircleID: before.ID, Name: before.Name, Agent: "Example AI", Action: "updated", At: epoch.Add(-time.Hour)}
	must(t, r.h.Repo.Put("auto_circle_changes", change.ID, saved{change, &before, after}))
	must(t, r.h.Repo.Put("circles", after.ID, after))
	must(t, r.h.Record(model.Activity{At: change.At, AgentID: "demo-ai", Kind: "people_write", Tool: "circle_auto_updated", Note: note, Count: 1}))
	// An unrelated AI add in the same circle, half a second later, must keep
	// its activity attribution even though the actor and note are identical.
	c.People[0].Role = "Organiser"
	nearBefore := c
	nearBefore.People = append([]model.CirclePerson{}, c.People...)
	c.People = append(c.People, model.CirclePerson{PersonID: "unrelated-nearby", Source: "demo-ai"})
	nearAt := epoch.Add(-500 * time.Millisecond)
	c.UpdatedAt = nearAt
	nearChange := circles.AutoChange{ID: "ordinary-nearby", CircleID: c.ID, Name: c.Name, Agent: "Example AI", Action: "updated", At: nearAt}
	must(t, r.h.Repo.Put("auto_circle_changes", nearChange.ID, saved{nearChange, &nearBefore, c}))
	must(t, r.h.Record(model.Activity{At: nearAt.Add(time.Millisecond), AgentID: "demo-ai", Kind: "people_write", Tool: "circle_auto_updated", Note: note, Count: 1}))
	must(t, r.h.Repo.Put("circles", c.ID, c))
	_, e := r.h.Repo.DB.Exec("DELETE FROM hub_schema WHERE version=8")
	must(t, e)
	path := filepath.Join(r.home, "hub.db")
	// Open a separate handle as a launch would; all data is invented and local.
	migrated, e := hub.Open(path)
	must(t, e)
	defer func() { migrated.Close() }()
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			must(t, migrated.Close())
			migrated, e = hub.Open(path)
			must(t, e)
		}
		var row struct {
			Change      circles.AutoChange
			AddedPeople []model.CirclePerson
		}
		must(t, migrated.Get("auto_circle_changes", "legacy-39", &row))
		if row.Change.Agent != "Limmr" || row.Change.ByKind != "structure" || len(row.AddedPeople) != 1 {
			t.Fatal("structural attribution not migrated")
		}
		row.Change = circles.AutoChange{}
		row.AddedPeople = nil
		must(t, migrated.Get("auto_circle_changes", change.ID, &row))
		if row.Change.Agent != "Example AI" || row.Change.ByKind != "" {
			t.Fatal("ordinary AI change relabelled")
		}
		var live model.Circle
		must(t, migrated.Get("circles", c.ID, &live))
		if live.People[0].Role != "Organiser" || live.People[0].Source != "demo-ai" || live.People[39].Source != "structure" {
			t.Fatal("owner edit lost or unchanged provenance not migrated")
		}
	}
	acts, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "people_write"})
	must(t, e)
	n := 0
	for _, a := range acts {
		if a.AgentID == "Limmr" {
			n++
			if a.Note != "Limmr placed 1 people in Workshop" {
				t.Fatal("activity text")
			}
		}
	}
	if n != 40 {
		t.Fatal("activity migration failed", n)
	}
	s := r.mc.CircleService()
	xs, e := s.AutoChanges()
	must(t, e)
	var group circles.AutoChange
	for _, v := range xs {
		if v.CircleID == c.ID {
			group = v
		}
	}
	if group.Count != 40 || len(group.ChangeIDs) != 40 {
		t.Fatal("legacy rows not grouped", group.Count)
	}
	// Undoing the untouched 39 members preserves the changed first member.
	ids := []string{}
	for person, id := range group.PeopleChanges {
		if person != "legacy-person-00" {
			ids = append(ids, id)
		}
	}
	must(t, s.UndoAutoGroup(ids))
	must(t, r.h.Repo.Get("circles", c.ID, &c))
	if len(c.People) != 2 || c.People[0].Role != "Organiser" || c.People[1].PersonID != "unrelated-nearby" {
		t.Fatal("legacy group undo failed")
	}
	if e = s.UndoAuto("legacy-00"); e == nil {
		t.Fatal("migration allowed undo over owner edit")
	}
}
