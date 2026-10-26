package hub

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"reflect"
	"time"
)

// Only an add-only snapshot with an affinity_proposed key for every added person
// is identifiable as the old structural path. Never relabel ordinary AI edits.
func (r *Repo) migrateStructure() error {
	var done int
	if e := r.DB.QueryRow("SELECT count(*) FROM hub_schema WHERE version=8").Scan(&done); e != nil {
		return e
	}
	if done > 0 {
		return nil
	}
	type change struct {
		ID        string    `json:"id"`
		CircleID  string    `json:"circle_id"`
		Name      string    `json:"name"`
		Agent     string    `json:"agent"`
		Action    string    `json:"action"`
		At        time.Time `json:"at"`
		Count     int       `json:"count"`
		ByKind    string    `json:"by_kind,omitempty"`
		PeopleIDs []string  `json:"people_ids,omitempty"`
	}
	type saved struct {
		Change      change
		Before      *model.Circle
		After       model.Circle
		AddedPeople []model.CirclePerson `json:",omitempty"`
	}
	xs, e := List[saved](r, "auto_circle_changes")
	if e != nil {
		return e
	}
	cs, e := List[model.Circle](r, "circles")
	if e != nil {
		return e
	}
	circles := map[string]model.Circle{}
	for _, c := range cs {
		circles[c.ID] = c
	}
	ms := []Mutation{}
	dirty := map[string]bool{}
	type match struct {
		at    time.Time
		note  string
		name  string
		actor string
		count int
		until time.Time
	}
	matches := []match{}
	for _, v := range xs {
		if v.Change.ByKind == "structure" || v.Change.Action != "updated" || v.Before == nil {
			continue
		}
		before := *v.Before
		after := v.After
		// All non-membership fields (except the update timestamp) must be identical.
		before.People = nil
		after.People = nil
		before.UpdatedAt = after.UpdatedAt
		if !reflect.DeepEqual(before, after) {
			continue
		}
		prior := map[string]model.CirclePerson{}
		for _, p := range v.Before.People {
			prior[p.PersonID] = p
		}
		added := []model.CirclePerson{}
		valid := true
		for _, p := range v.After.People {
			if old, ok := prior[p.PersonID]; ok {
				if !reflect.DeepEqual(old, p) {
					valid = false
				}
				delete(prior, p.PersonID)
				continue
			}
			var seen bool
			if err := r.Get("affinity_proposed", v.Change.CircleID+":"+p.PersonID, &seen); err != nil {
				if err != model.ErrNotFound {
					return err
				}
				valid = false
			}
			if !seen {
				valid = false
			}
			added = append(added, p)
		}
		if !valid || len(prior) > 0 || len(added) == 0 {
			continue
		}
		// Bound attribution to the interval before the next change by this actor
		// in this circle, including unrelated changes that must retain their actor.
		until := v.Change.At.Add(5 * time.Second)
		for _, next := range xs {
			if next.Change.CircleID == v.Change.CircleID && next.Change.Agent == v.Change.Agent && next.Change.At.After(v.Change.At) && next.Change.At.Before(until) {
				until = next.Change.At
			}
		}
		matches = append(matches, match{v.Change.At, v.Change.Agent + " updated " + v.Change.Name + " · Undo available for 24 hours", v.Change.Name, added[0].Source, len(added), until})
		c := circles[v.Change.CircleID]
		for i, p := range added {
			replacement := p
			replacement.Source = "structure"
			replacement.Origin = &model.Origin{By: "Limmr", ByKind: "structure", At: v.Change.At}
			for j, live := range c.People {
				if reflect.DeepEqual(live, p) {
					c.People[j] = replacement
				}
			}
			added[i] = replacement
			v.Change.PeopleIDs = append(v.Change.PeopleIDs, p.PersonID)
		}
		if c.ID != "" {
			circles[c.ID] = c
			dirty[c.ID] = true
		}
		v.Change.Agent = "Limmr"
		v.Change.Action = "placed"
		v.Change.ByKind = "structure"
		v.Change.Count = len(added)
		v.AddedPeople = added
		ms = append(ms, Mutation{Bucket: "auto_circle_changes", ID: v.Change.ID, Value: v})
	}

	for id := range dirty {
		c := circles[id]
		ms = append(ms, Mutation{Bucket: "circles", ID: c.ID, Value: c})
	}
	// Apply records, live provenance and matching activity attribution atomically.
	tx, e := r.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, m := range ms {
		data, err := json.Marshal(m.Value)
		if err != nil {
			return err
		}
		if _, e = tx.Exec("UPDATE objects SET data=? WHERE bucket=? AND id=?", string(data), m.Bucket, m.ID); e != nil {
			return e
		}
	}
	for _, m := range matches {
		// One matching activity per change, rather than relabelling every nearby
		// "updated" activity. Original saveAuto records activity just after Change.at.
		if _, e = tx.Exec(`UPDATE activity SET data=json_set(data,'$.agent_id','Limmr','$.tool','circle_structure_placed','$.count',?,'$.note',?) WHERE id=(SELECT id FROM activity WHERE json_extract(data,'$.tool')='circle_auto_updated' AND json_extract(data,'$.note')=? AND json_extract(data,'$.agent_id')=? AND julianday(at)>=julianday(?) AND julianday(at)<julianday(?) ORDER BY julianday(at),id LIMIT 1)`, m.count, fmt.Sprintf("Limmr placed %d people in %s", m.count, m.name), m.note, m.actor, m.at, m.until); e != nil {
			return e
		}
	}

	if _, e = tx.Exec("INSERT INTO hub_schema VALUES(8)"); e != nil {
		return e
	}
	return tx.Commit()
}
