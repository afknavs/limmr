package circles

import (
	"limmr/internal/hub"
	"limmr/internal/people"
)

func (s *Service) shared() *Service {
	if s.parent != nil {
		return s.parent.shared()
	}
	return s
}
func (s *Service) WithServices(h *hub.Service, p *people.Service) *Service {
	return &Service{Hub: h, People: p, parent: s.shared()}
}
