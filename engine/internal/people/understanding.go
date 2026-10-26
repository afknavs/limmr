package people

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"math"
	"sort"
	"strings"
	"time"
)

type UnderstandingContact struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	DM30        int        `json:"dm_messages_30"`
	DM365       int        `json:"dm_messages_365"`
	LastContact *time.Time `json:"last_contact"`
	// JIDs are owner-only, solely for the private on-device message window.
	JIDs []string `json:"jids,omitempty"`
}

func (s *Service) PeopleToUnderstand(a *model.Agent) ([]UnderstandingContact, error) {
	if a != nil && (a.Preset != "own" || !a.Permissions.ReadPeople || !a.Permissions.WritePeople) {
		return nil, model.ErrForbidden
	}
	ps, e := s.PeopleAll(a, "", true)
	if e != nil {
		return nil, e
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return nil, e
	}
	ms, e := s.memberships()
	if e != nil {
		return nil, e
	}
	stats, e := s.allStats(a, ps, gs, ms)
	if e != nil {
		return nil, e
	}
	rels, e := hub.List[model.Relation](s.Hub.Repo, "relations")
	if e != nil {
		return nil, e
	}
	known := map[string]bool{}
	for _, r := range rels {
		if r.A == "me" {
			known[r.B] = true
		}
		if r.B == "me" {
			known[r.A] = true
		}
	}
	out := []UnderstandingContact{}
	for _, p := range ps {
		st := stats[p.ID]
		if p.ID == "me" || hasRelationship(p) || known[p.ID] || st.DM365 == 0 {
			continue
		}
		v := UnderstandingContact{ID: p.ID, Name: p.Name, DM30: st.DM30, DM365: st.DM365, LastContact: st.LastDM}
		if a == nil {
			v.JIDs = p.JIDs
		}
		out = append(out, v)
	}
	score := func(v UnderstandingContact) float64 {
		if v.LastContact == nil {
			return 0
		}
		days := math.Max(0, s.Hub.Now().Sub(*v.LastContact).Hours()/24)
		return float64(v.DM30*4+v.DM365) / (1 + days/14)
	}
	sort.Slice(out, func(i, j int) bool {
		if score(out[i]) == score(out[j]) {
			return out[i].ID < out[j].ID
		}
		return score(out[i]) > score(out[j])
	})
	if len(out) > 40 {
		out = out[:40]
	}
	return out, nil
}
func (s *Service) SaveInference(id string, v model.RelationshipInference) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var p model.Person
	if e := s.Hub.Repo.Get("people", id, &p); e != nil {
		return e
	}
	if hasRelationship(p) {
		return model.ErrConflict
	}
	v.Label = strings.TrimSpace(v.Label)
	v.HowKnown = strings.TrimSpace(v.HowKnown)
	if v.Label == "" || v.HowKnown == "" || len(v.Label) > 120 || len(v.HowKnown) > 500 || math.IsNaN(v.Confidence) || v.Confidence < .65 || v.Confidence > 1 {
		return model.ErrInvalid
	}
	v.Status = "inferred"
	return s.Hub.Repo.Put("relationship_inferences", id, v)
}
func (s *Service) ReviewInference(id string, confirm bool) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var v model.RelationshipInference
	if e := s.Hub.Repo.Get("relationship_inferences", id, &v); e != nil {
		return e
	}
	if confirm {
		var p model.Person
		if e := s.Hub.Repo.Get("people", id, &p); e != nil {
			return e
		}
		if hasRelationship(p) {
			return model.ErrConflict
		}
		p.Role = v.Label
		p.HowKnown = v.HowKnown
		p.UpdatedAt = s.Hub.Now().UTC()
		return s.Hub.Repo.Apply(hub.Mutation{Bucket: "people", ID: id, Value: p}, hub.Mutation{Bucket: "relationship_inferences", ID: id, Delete: true})
	}
	return s.Hub.Repo.Delete("relationship_inferences", id)
}

// RankForMap keeps the recent-contact ordering while allowing frequent contacts
// and quiet circles a place in the bounded projection.
func (s *Service) RankForMap(ps []model.Person, limit int) ([]model.Person, error) {
	gs, e := s.Groups(nil, "")
	if e != nil {
		return nil, e
	}
	ms, e := s.memberships()
	if e != nil {
		return nil, e
	}
	stats, e := s.allStats(nil, ps, gs, ms)
	if e != nil {
		return nil, e
	}
	rank := func(p model.Person) float64 {
		v := stats[p.ID]
		if v.LastDM == nil {
			return 0
		}
		days := math.Max(0, s.Hub.Now().Sub(*v.LastDM).Hours()/24)
		return float64(1+v.DM30*4+v.DM365) / (1 + days/14)
	}
	sort.SliceStable(ps, func(i, j int) bool { return rank(ps[i]) > rank(ps[j]) })
	selected := map[string]bool{}
	out := []model.Person{}
	represented := map[string]bool{}
	for _, p := range ps {
		if p.PrimaryCircleID != "" && !represented[p.PrimaryCircleID] && len(out) < limit {
			out = append(out, p)
			selected[p.ID] = true
			represented[p.PrimaryCircleID] = true
		}
	}
	for _, p := range ps {
		if !selected[p.ID] && len(out) < limit {
			out = append(out, p)
		}
	}
	return out, nil
}

func hasRelationship(p model.Person) bool {
	return strings.TrimSpace(p.HowKnown) != "" || (strings.TrimSpace(p.Role) != "" && !strings.EqualFold(strings.TrimSpace(p.Role), "member"))
}
