package circles

import (
	"limmr/internal/hub"
	"limmr/internal/model"
)

// Archived is owner-only: the caller is the admin route, never an AI tool.
func (s *Service) Archived() ([]model.Circle, error) {
	xs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	out := []model.Circle{}
	for _, c := range xs {
		if c.Archived {
			projected, e := s.project(nil, c)
			if e != nil {
				return nil, e
			}
			out = append(out, projected)
		}
	}
	return out, err
}
