package integration_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"testing"
	"time"
)

type r1Database struct {
	dbutil.Database
	begins, queries, peopleSnapshots int
}

func (d *r1Database) Begin() (*sql.Tx, error) { d.begins++; return d.Database.Begin() }
func (d *r1Database) Query(q string, args ...any) (*sql.Rows, error) {
	d.queries++
	if len(args) > 0 && args[0] == "people" {
		d.peopleSnapshots++
	}
	return d.Database.Query(q, args...)
}
func (d *r1Database) QueryRow(q string, args ...any) *sql.Row {
	d.queries++
	return d.Database.QueryRow(q, args...)
}

func r1Fixture(t *testing.T, r *rig, n int) (model.Circle, []string) {
	t.Helper()
	c := model.Circle{ID: "studio", Name: "Studio", Kind: "project", SendRule: "confirm", Confirmed: true}
	for i := 0; i < 3; i++ {
		jid := fmt.Sprintf("r1-group-%d@g.us", i)
		at := epoch.Add(-24 * time.Hour)
		must(t, r.m.PutChat(model.Chat{JID: jid, Name: "Studio workshop", IsGroup: true, LastMessageTime: &at}))
		must(t, r.p.PutGroup(model.Group{JID: jid, Name: "Studio workshop"}))
		c.Chats = append(c.Chats, model.CircleChat{ChatJID: jid})
	}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	order := []int{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("demo-%03d", i)
		jid := fmt.Sprintf("1555020%04d@s.whatsapp.net", i)
		at := epoch.Add(-time.Duration(i) * time.Minute)
		must(t, r.p.PutPerson(model.Person{ID: id, Name: fmt.Sprintf("Workshop member %03d", i), JIDs: []string{jid}}))
		must(t, r.m.PutChat(model.Chat{JID: jid, Name: "Workshop member", LastMessageTime: &at}))
		groups := 3
		if i%3 == 0 {
			groups = 2
		}
		for j := 0; j < groups; j++ {
			must(t, r.p.PutMembership(model.Membership{Group: c.Chats[j].ChatJID, Person: id}))
		}
		order = append(order, i)
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i]%3 != 0 && order[j]%3 == 0 })
	ids := []string{}
	for _, i := range order {
		ids = append(ids, fmt.Sprintf("demo-%03d", i))
	}
	return c, ids
}

func TestR1BatchAttributionRankingNoCapGroupingAndUndo(t *testing.T) {
	for _, n := range []int{40, 120} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			r := newRig(t)
			c, order := r1Fixture(t, r, n)
			db := &r1Database{Database: r.h.Repo.DB}
			r.h.Repo.DB = db
			start := time.Now()
			result, e := r.mc.CircleService().SuggestAffinity()
			must(t, e)
			elapsed := time.Since(start)
			counts := result.(map[string]int)
			if counts["added"] != n || counts["suggested"] != 0 || counts["skipped_cap"] != 0 {
				t.Fatalf("bad counts: %v", counts)
			}
			if db.begins != 1 || db.queries > 3*n+50 || db.peopleSnapshots > 3 {
				t.Fatalf("per-person transaction/recompute: %d transactions, %d reads, %d people snapshots", db.begins, db.queries, db.peopleSnapshots)
			}
			t.Logf("%d placements: %s, %d transaction, %d reads", n, elapsed, db.begins, db.queries)
			must(t, r.h.Repo.Get("circles", c.ID, &c))
			for i, p := range c.People {
				if p.PersonID != order[i] {
					t.Fatal("score/recency order differs", i, p.PersonID, order[i])
				}
				if p.Source != "structure" || p.Origin == nil || p.Origin.By != "Limmr" || p.Origin.ByKind != "structure" {
					t.Fatal("agent attribution on structure")
				}
			}
			changes, e := r.mc.CircleService().AutoChanges()
			must(t, e)
			if len(changes) != 1 || changes[0].Count != n || len(changes[0].ChangeIDs) != n || len(changes[0].PeopleChanges) != n || changes[0].Agent != "Limmr" {
				t.Fatal("batch did not group", changes)
			}
			acts, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "people_write"})
			must(t, e)
			if len(acts) != 1 || acts[0].AgentID != "Limmr" || acts[0].Count != n || acts[0].Note != fmt.Sprintf("Limmr placed %d people in Studio", n) {
				t.Fatal("activity attribution/count")
			}
			w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
			status(t, w, 200)
			var home struct {
				Changes []circles.AutoChange `json:"circle_changes"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &home))
			if len(home.Changes) != 1 || home.Changes[0].Count != n {
				t.Fatal("Home did not group all adds")
			}
			status(t, r.request(t, "POST", "/v1/circles/changes/undo", r.api.ControlToken, map[string]any{"change_ids": append(append([]string{}, changes[0].ChangeIDs...), "missing")}, nil), 404)
			must(t, r.h.Repo.Get("circles", c.ID, &c))
			if len(c.People) != n {
				t.Fatal("partial group undo")
			}
			status(t, r.request(t, "POST", "/v1/circles/changes/undo", r.token, map[string]any{"change_ids": changes[0].ChangeIDs}, nil), 403)
			status(t, r.request(t, "POST", "/v1/circle-changes/"+changes[0].ChangeIDs[0]+"/undo", r.api.ControlToken, nil, nil), 200)
			status(t, r.request(t, "POST", "/v1/circles/changes/undo", r.api.ControlToken, map[string]any{"change_ids": changes[0].ChangeIDs[1:]}, nil), 200)
			must(t, r.h.Repo.Get("circles", c.ID, &c))
			if len(c.People) != 0 {
				t.Fatal("undo missed people")
			}
			result, e = r.mc.CircleService().SuggestAffinity()
			must(t, e)
			if result.(map[string]int)["added"] != 0 {
				t.Fatal("undone members were re-added")
			}
		})
	}
}

func TestR1GateKeepsEverySuggestionReviewable(t *testing.T) {
	for _, gate := range []string{"off", "disabled", "work", "no-write"} {
		t.Run(gate, func(t *testing.T) {
			r := newRig(t)
			_, order := r1Fixture(t, r, 60)
			switch gate {
			case "off":
				settings := r.h.Settings()
				off := false
				settings.AutoCircles = &off
				must(t, r.h.SetSettings(settings))
			case "disabled":
				r.a.Enabled = false
				must(t, r.h.Repo.Put("agents", r.a.ID, r.a))
			case "work":
				r.a.Preset = "work"
				must(t, r.h.Repo.Put("agents", r.a.ID, r.a))
			case "no-write":
				r.a.Permissions.WritePeople = false
				must(t, r.h.Repo.Put("agents", r.a.ID, r.a))
			}
			result, e := r.mc.CircleService().SuggestAffinity()
			must(t, e)
			counts := result.(map[string]int)
			if counts["added"] != 0 || counts["suggested"] != 60 || counts["skipped_cap"] != 0 {
				t.Fatal("suggestions silently truncated", counts)
			}
			xs, e := r.mc.CircleService().Additions()
			must(t, e)
			if len(xs) != 1 || len(xs[0].People) != 60 {
				t.Fatal("not all reviewable")
			}
			for i, id := range xs[0].People {
				if id != order[i] {
					t.Fatal("suggestion order")
				}
			}
			_, e = r.mc.CircleService().ReviewAddition(xs[0].ID, true)
			must(t, e)
		})
	}
}

func TestR1UndoPreservesOwnerEditsAndIsAtomic(t *testing.T) {
	r := newRig(t)
	c, _ := r1Fixture(t, r, 40)
	s := r.mc.CircleService()
	_, e := s.SuggestAffinity()
	must(t, e)
	xs, e := s.AutoChanges()
	must(t, e)
	_, e = s.Mutate(c.ID, func(c *model.Circle) error { c.Name = "Workshop"; c.People[0].Role = "Organiser"; return nil })
	must(t, e)
	if e = s.UndoAutoGroup(xs[0].ChangeIDs); e == nil {
		t.Fatal("owner membership edit overwritten")
	}
	must(t, r.h.Repo.Get("circles", c.ID, &c))
	if len(c.People) != 40 || c.Name != "Workshop" || c.People[0].Role != "Organiser" {
		t.Fatal("conflict partly undid group")
	}
}

func TestR1HomeLimitsGroupsAndAllChangesRemainAccessible(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 5; i++ {
		c := model.Circle{ID: fmt.Sprint(i), Name: "Workshop", Confirmed: true}
		must(t, r.h.Repo.Put("circles", c.ID, c))
		change := circles.AutoChange{ID: c.ID, CircleID: c.ID, Name: c.Name, Agent: "Example AI", Action: "updated", At: epoch.Add(-time.Duration(i) * time.Minute)}
		must(t, r.h.Repo.Put("auto_circle_changes", c.ID, map[string]any{"Change": change, "After": c}))
	}
	var home struct {
		Changes []circles.AutoChange `json:"circle_changes"`
		More    int                  `json:"circle_changes_more"`
	}
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if len(home.Changes) != 3 || home.More != 2 {
		t.Fatal("Home limit", len(home.Changes), home.More)
	}
	var all struct {
		Changes []circles.AutoChange `json:"changes"`
	}
	w = r.request(t, "GET", "/v1/circles/changes", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &all))
	if len(all.Changes) != 5 {
		t.Fatal("more groups inaccessible")
	}
}

func TestR1InterleavedLegacyChangesGroupPerCircle(t *testing.T) {
	r := newRig(t)
	for _, id := range []string{"a", "b"} {
		must(t, r.h.Repo.Put("circles", id, model.Circle{ID: id, Name: "Workshop " + id, Confirmed: true}))
	}
	// A legacy run alternated between two circles change by change, seconds apart.
	for i := 0; i < 20; i++ {
		id := []string{"a", "b"}[i%2]
		c := model.Circle{ID: id, Name: "Workshop " + id, Confirmed: true}
		change := circles.AutoChange{ID: fmt.Sprintf("x%02d", i), CircleID: id, Name: c.Name, Agent: "Limmr", Action: "placed", ByKind: "structure", At: epoch.Add(-time.Duration(i*3) * time.Second)}
		must(t, r.h.Repo.Put("auto_circle_changes", change.ID, map[string]any{"Change": change, "After": c}))
	}
	var home struct {
		Changes []circles.AutoChange `json:"circle_changes"`
		More    int                  `json:"circle_changes_more"`
	}
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if len(home.Changes) != 2 || home.More != 0 || home.Changes[0].Count+home.Changes[1].Count != 20 {
		t.Fatal("interleaved legacy changes not grouped per circle", len(home.Changes), home.More)
	}
}
