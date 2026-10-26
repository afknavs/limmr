package people

import "limmr/internal/hub"

func (s *Service) shared() *Service {
	if s.parent != nil {
		return s.parent.shared()
	}
	return s
}
func (s *Service) WithHub(h *hub.Service) *Service { return &Service{Hub: h, parent: s.shared()} }
