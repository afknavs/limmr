package circles

import (
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
)

type affinityCandidate struct {
	person model.Person
	score  float64
	recent time.Time
	why    string
}

// Structural placements are exempt from the agent-edit size limit. Compute people
// once, then commit each circle and all of its undo records in one mutation set.
func (s *Service) SuggestAffinity() (any, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	ps, e := s.People.People(nil, "")
	if e != nil {
		return nil, e
	}
	agents, e := s.Hub.Agents()
	if e != nil {
		return nil, e
	}
	auto := false
	for _, a := range agents {
		if a.Enabled && a.Preset == "own" && a.Permissions.WritePeople {
			auto = s.Hub.Settings().AutoCirclesEnabled()
			break
		}
	}
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	chats, e := s.Hub.Messages.Chats()
	if e != nil {
		return nil, e
	}
	recency := map[string]time.Time{}
	for _, c := range chats {
		if c.LastMessageTime != nil {
			recency[c.JID] = *c.LastMessageTime
		}
	}
	seen, e := s.Hub.Repo.DB.Query("SELECT id FROM objects WHERE bucket='affinity_proposed'")
	if e != nil {
		return nil, e
	}
	proposed := map[string]bool{}
	for seen.Next() {
		var key string
		if e = seen.Scan(&key); e != nil {
			seen.Close()
			return nil, e
		}
		proposed[key] = true
	}
	e = seen.Err()
	seen.Close()
	if e != nil {
		return nil, e
	}
	circles := map[string]model.Circle{}
	manual := map[string]bool{}
	members := map[string]bool{}
	for _, c := range cs {
		circles[c.ID] = c
		for _, m := range c.People {
			members[c.ID+":"+m.PersonID] = true
			if m.Source == "you" || m.Origin != nil && m.Origin.ByKind == "you" {
				manual[m.PersonID] = true
			}
		}
	}
	candidates := map[string][]affinityCandidate{}
	for _, p := range ps {
		key := p.PrimaryCircleID + ":" + p.ID
		c := circles[p.PrimaryCircleID]
		if p.PrimaryCircleID == "" || p.Circle != "" || !c.Confirmed || c.Archived || manual[p.ID] || members[key] || proposed[key] {
			continue
		}
		v := affinityCandidate{person: p}
		for _, a := range p.CircleAffinity {
			if a.CircleID == c.ID {
				v.score = a.Score
				v.why = p.Name + ": " + strings.Join(a.Reasons, "; ")
			}
		}
		for _, j := range p.JIDs {
			if recency[j].After(v.recent) {
				v.recent = recency[j]
			}
		}
		for _, g := range p.GroupsInCommon {
			if model.Contains(g.CircleIDs, c.ID) && g.LastActiveAt != nil && g.LastActiveAt.After(v.recent) {
				v.recent = *g.LastActiveAt
			}
		}
		if v.score >= .65 {
			candidates[c.ID] = append(candidates[c.ID], v)
		}
	}
	added, suggested := 0, 0
	run := dbutil.ID()
	at := s.Hub.Now().UTC()
	for _, c := range cs {
		xs := candidates[c.ID]
		if len(xs) == 0 {
			continue
		}
		sort.Slice(xs, func(i, j int) bool {
			if xs[i].score != xs[j].score {
				return xs[i].score > xs[j].score
			}
			if !xs[i].recent.Equal(xs[j].recent) {
				return xs[i].recent.After(xs[j].recent)
			}
			return xs[i].person.ID < xs[j].person.ID
		})
		ms := []hub.Mutation{}
		if auto {
			for _, x := range xs {
				member := model.CirclePerson{PersonID: x.person.ID, Source: "structure", Note: x.why, Origin: &model.Origin{By: "Limmr", ByKind: "structure", At: at}}
				c.People = append(c.People, member)
				change := AutoChange{ID: dbutil.ID(), CircleID: c.ID, Name: c.Name, Agent: "Limmr", Action: "placed", At: at, Count: 1, RunID: run, ByKind: "structure", PeopleIDs: []string{x.person.ID}}
				ms = append(ms, hub.Mutation{Bucket: "auto_circle_changes", ID: change.ID, Value: savedAuto{Change: change, AddedPeople: []model.CirclePerson{member}}})
			}
			c.UpdatedAt = at
			ms = append(ms, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c})
		} else {
			// One reviewable suggestion per circle; no eight-person silent truncation.
			v := Addition{ID: dbutil.ID(), CircleID: c.ID, Chats: []string{}, People: []string{}, AgentID: "structure"}
			reasons := []string{}
			for _, x := range xs {
				v.People = append(v.People, x.person.ID)
				reasons = append(reasons, x.why)
			}
			v.Why = strings.Join(reasons, "\n")
			ms = append(ms, hub.Mutation{Bucket: "circle_additions", ID: v.ID, Value: v})
		}
		for _, x := range xs {
			ms = append(ms, hub.Mutation{Bucket: "affinity_proposed", ID: c.ID + ":" + x.person.ID, Value: true})
		}
		if e = s.Hub.Repo.Apply(ms...); e != nil {
			return nil, e
		}
		if auto {
			added += len(xs)
			if e = s.Hub.Record(model.Activity{AgentID: "Limmr", Kind: "people_write", Tool: "circle_structure_placed", Count: len(xs), Note: placementText(len(xs), c.Name)}); e != nil {
				return nil, e
			}
		} else {
			suggested += len(xs)
		}
	}
	return map[string]int{"added": added, "suggested": suggested, "skipped_cap": 0}, nil
}
