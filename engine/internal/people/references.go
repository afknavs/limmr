package people

import (
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
)

// Circle roles and brief notes must follow the canonical person too.
func (s *Service) personReferences(ids map[string]bool, target string) ([]hub.Mutation, error) {
	xs := []hub.Mutation{}
	var summary PersonSummary
	haveSummary := s.Hub.Repo.Get("person_summaries", target, &summary) == nil
	for id := range ids {
		if id == target {
			continue
		}
		var v PersonSummary
		if s.Hub.Repo.Get("person_summaries", id, &v) == nil {
			if !haveSummary || v.WrittenAt.After(summary.WrittenAt) {
				summary = v
				summary.PersonID = target
				haveSummary = true
			}
			xs = append(xs, hub.Mutation{Bucket: "person_summaries", ID: id, Delete: true})
		}
	}
	if haveSummary {
		xs = append(xs, hub.Mutation{Bucket: "person_summaries", ID: target, Value: summary})
	}
	var canonical model.RelationshipInference
	have := s.Hub.Repo.Get("relationship_inferences", target, &canonical) == nil
	for id := range ids {
		if id == target {
			continue
		}
		var inference model.RelationshipInference
		if s.Hub.Repo.Get("relationship_inferences", id, &inference) == nil {
			if !have {
				xs = append(xs, hub.Mutation{Bucket: "relationship_inferences", ID: target, Value: inference})
				have = true
			}
			xs = append(xs, hub.Mutation{Bucket: "relationship_inferences", ID: id, Delete: true})
		}
	}
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	for _, c := range cs {
		changed := false
		members := []model.CirclePerson{}
		seen := map[string]bool{}
		// Prefer an existing canonical membership on conflicts.
		for _, p := range c.People {
			if p.PersonID == target {
				members = append(members, p)
				seen[target] = true
			}
		}
		for _, p := range c.People {
			if p.PersonID == target {
				continue
			}
			if ids[p.PersonID] {
				p.PersonID = target
				changed = true
			}
			if !seen[p.PersonID] {
				members = append(members, p)
				seen[p.PersonID] = true
			}
		}
		if changed {
			c.People = members
		}
		for i := range c.Topics {
			ps := []string{}
			for _, id := range c.Topics[i].People {
				if ids[id] {
					id = target
					changed = true
				}
				if !model.Contains(ps, id) {
					ps = append(ps, id)
				}
			}
			c.Topics[i].People = ps
		}
		if changed {
			xs = append(xs, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c})
		}
	}
	bs, e := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if e != nil {
		return nil, e
	}
	for _, b := range bs {
		changed := false
		for i, p := range b.PeopleNotes {
			if ids[p.PersonID] {
				b.PeopleNotes[i].PersonID = target
				changed = true
			}
		}
		if changed {
			xs = append(xs, hub.Mutation{Bucket: "briefs", ID: fmt.Sprintf("%s:%09d", b.CircleID, b.Version), Value: b})
		}
	}
	return xs, nil
}
