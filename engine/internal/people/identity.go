package people

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
)

// NormalizeMapping joins identities when WhatsApp learns a phone number for a
// LID. Profile enrichment, provenance and memberships move in one transaction.
// The phone record wins conflicting fields; missing fields use the LID record.
func (s *Service) NormalizeMapping(lid, pn string) error {
	if !strings.HasSuffix(lid, "@lid") || !strings.HasSuffix(pn, "@s.whatsapp.net") {
		return model.ErrInvalid
	}
	target := model.PersonID(pn)
	if p, e := s.Hub.Owner(); e == nil && (model.Contains(p.JIDs, pn) || model.Contains(p.JIDs, lid)) {
		target = "me"
	}
	var done string
	if s.Hub.Repo.Get("normalized_pairs", lid, &done) == nil && done == pn {
		return nil
	}
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	rows, err := s.Hub.Repo.DB.Query("SELECT data FROM objects WHERE bucket='people' AND id IN (SELECT person_id FROM people_jids WHERE jid IN (?,?) UNION SELECT id FROM objects WHERE bucket='people' AND id IN (?,?,?)) ORDER BY id", lid, pn, lid, pn, target)
	if err != nil {
		return err
	}
	ps := []model.Person{}
	for rows.Next() {
		var raw string
		var p model.Person
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			rows.Close()
			return err
		}
		ps = append(ps, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	ids := map[string]bool{}
	matched := []model.Person{}
	for _, p := range ps {
		if p.ID == lid || p.ID == pn || model.Contains(p.JIDs, lid) || model.Contains(p.JIDs, pn) {
			ids[p.ID] = true
			matched = append(matched, p)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	if len(matched) == 1 && matched[0].ID == target && model.Contains(matched[0].JIDs, lid) {
		return s.Hub.Repo.Put("normalized_pairs", lid, pn)
	}
	primary := model.Person{ID: target, JIDs: []string{pn, lid}}
	for _, p := range matched {
		if p.ID == pn || model.Contains(p.JIDs, pn) {
			primary = p
			break
		}
	}
	for _, p := range matched {
		if p.ID == target {
			primary = p
			break
		}
	}
	for _, p := range matched {
		for _, name := range append(append([]string{}, p.OtherNames...), p.Name, p.PushName, p.Label) {
			if name != "" && name != primary.Name && name != primary.Label && !model.Contains(primary.OtherNames, name) {
				primary.OtherNames = append(primary.OtherNames, name)
			}
		}
		primary.Business = primary.Business || p.Business
		primary.Broadcast = primary.Broadcast || p.Broadcast
		for _, pair := range [][2]*string{{&primary.Name, &p.Name}, {&primary.PushName, &p.PushName}, {&primary.Label, &p.Label}, {&primary.Circle, &p.Circle}, {&primary.Role, &p.Role}, {&primary.HowKnown, &p.HowKnown}, {&primary.Notes, &p.Notes}} {
			if *pair[0] == "" {
				*pair[0] = *pair[1]
			}
		}
		mergeValues := func(dst *[]model.LabeledValue, src []model.LabeledValue) {
			for _, v := range src {
				found := false
				for _, old := range *dst {
					if old.Label == v.Label && old.Value == v.Value {
						found = true
					}
				}
				if !found {
					*dst = append(*dst, v)
				}
			}
		}
		mergeValues(&primary.Details.Phones, p.Details.Phones)
		mergeValues(&primary.Details.Emails, p.Details.Emails)
		mergeValues(&primary.Details.Handles, p.Details.Handles)
		for _, pair := range [][2]*string{{&primary.Details.Birthday, &p.Details.Birthday}, {&primary.Details.Location, &p.Details.Location}, {&primary.Details.Pronouns, &p.Details.Pronouns}} {
			if *pair[0] == "" {
				*pair[0] = *pair[1]
			}
		}
		for _, v := range p.DetailSuggestions {
			found := false
			for _, old := range primary.DetailSuggestions {
				if old.ID == v.ID {
					found = true
				}
			}
			if !found {
				primary.DetailSuggestions = append(primary.DetailSuggestions, v)
			}
		}
		if p.UpdatedAt.After(primary.UpdatedAt) {
			primary.UpdatedAt = p.UpdatedAt
		}
		for _, j := range p.JIDs {
			if !model.Contains(primary.JIDs, j) {
				primary.JIDs = append(primary.JIDs, j)
			}
		}
	}
	primary.ID = target
	for _, j := range []string{pn, lid} {
		if !model.Contains(primary.JIDs, j) {
			primary.JIDs = append(primary.JIDs, j)
		}
	}
	if len(matched) == 1 && matched[0].ID == target {
		return s.Hub.Repo.Apply(hub.Mutation{Bucket: "people", ID: target, Value: primary}, hub.Mutation{Bucket: "normalized_pairs", ID: lid, Value: pn}, hub.Mutation{Bucket: "stats", ID: target, Delete: true})
	}
	xs := []hub.Mutation{}
	for id := range ids {
		if id != target {
			xs = append(xs, hub.Mutation{Bucket: "person_aliases", ID: id, Value: map[string]any{"from_id": id, "to_id": target, "at": s.Hub.Now().UTC()}})
		}
	}
	for id := range ids {
		if id != target {
			xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Delete: true})
		}
		xs = append(xs, hub.Mutation{Bucket: "stats", ID: id, Delete: true})
	}
	xs = append(xs, hub.Mutation{Bucket: "people", ID: target, Value: primary})
	ms, err := s.memberships()
	if err != nil {
		return err
	}
	merged := map[string]model.Membership{}
	for _, m := range ms {
		if !ids[m.Person] {
			continue
		}
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: m.Group + ":" + m.Person, Delete: true})
		m.Person = target
		m.Admin = m.Admin || merged[m.Group].Admin
		merged[m.Group] = m
	}
	for _, m := range merged {
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: m.Group + ":" + target, Value: m})
	}
	fs, err := hub.List[model.Fact](s.Hub.Repo, "facts")
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.Subject == "person" && ids[f.SubjectID] {
			f.SubjectID = target
			xs = append(xs, hub.Mutation{Bucket: "facts", ID: f.ID, Value: f})
		}
	}
	rs, err := hub.List[model.Relation](s.Hub.Repo, "relations")
	if err != nil {
		return err
	}
	for _, r := range rs {
		if !ids[r.A] && !ids[r.B] {
			continue
		}
		if ids[r.A] {
			r.A = target
		}
		if ids[r.B] {
			r.B = target
		}
		xs = append(xs, hub.Mutation{Bucket: "relations", ID: r.ID, Value: r})
	}
	cs, err := s.Changes()
	if err != nil {
		return err
	}
	for _, c := range cs {
		if !ids[c.SubjectID] {
			continue
		}
		c.SubjectID = target
		if c.Before != nil {
			c.Before.ID = target
		}
		if c.After != nil {
			c.After.ID = target
		}
		xs = append(xs, hub.Mutation{Bucket: "changes", ID: c.ID, Value: c})
	}
	refs, e := s.personReferences(ids, target)
	if e != nil {
		return e
	}
	xs = append(xs, refs...)
	xs = append(xs, hub.Mutation{Bucket: "normalized_pairs", ID: lid, Value: pn})
	return s.Hub.Repo.Apply(xs...)
}
