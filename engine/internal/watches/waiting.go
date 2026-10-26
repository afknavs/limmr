package watches

import (
	"context"
	"limmr/internal/triggers"
	"time"
)

type Waiting struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent"`
	Kind     string    `json:"kind"`
	Sentence string    `json:"sentence"`
	Where    string    `json:"where"`
	Since    time.Time `json:"since"`
	NewHits  int       `json:"new_hits"`
	Latest   *Hit      `json:"latest"`
}

func (s *Service) Waiting(ctx context.Context) ([]Waiting, error) {
	out := []Waiting{}
	records, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Status != triggers.Active || r.ExpiresAt != nil && !s.Hub.Now().Before(*r.ExpiresAt) {
			continue
		}
		a, err := s.Hub.Agent(r.CreatedVia.Agent)
		if err != nil || !a.Enabled || r.Owner != a.ID || a.ID == "this-mac" || !a.Permissions.ReadMessages || !s.scopeAllowed(a, r) {
			continue
		}
		d, err := s.Detail(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		sentence := a.Name + " is waiting for updates in " + d.Where + ": " + r.Goal
		if d.Kind == "handoff" {
			sentence = a.Name + " is waiting to help in " + d.Where + ": " + r.Goal
		}
		if d.Kind == "feed" {
			sentence = a.Name + " is following everything it can read."
		}
		out = append(out, Waiting{ID: r.ID, Agent: a.Name, Kind: d.Kind, Sentence: sentence, Where: d.Where, Since: r.CreatedAt, NewHits: d.NewHits, Latest: d.Latest})
	}
	return out, nil
}
