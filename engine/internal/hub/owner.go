package hub

import (
	"limmr/internal/model"
	"strings"
)

// OwnerName is the name agents and contacts see: the saved name, else the linked account's WhatsApp name.
func (s *Service) OwnerName() string {
	if name := strings.TrimSpace(s.Settings().Owner); name != "" {
		return name
	}
	var p model.Person
	if s.Repo.Get("people", "me", &p) == nil && p.Name != "You" {
		return strings.TrimSpace(p.Name)
	}
	return ""
}

// Owner is created only from a linked account, never from a synthetic default.
func (s *Service) Owner() (model.Person, error) {
	var p model.Person
	e := s.Repo.Get("people", "me", &p)
	if e == nil && s.Settings().Owner != "" {
		p.Label = s.Settings().Owner
	}
	return p, e
}
func (s *Service) OwnerChat() string {
	p, e := s.Owner()
	if e != nil {
		return ""
	}
	for _, j := range p.JIDs {
		if strings.HasSuffix(j, "@s.whatsapp.net") {
			return j
		}
	}
	if len(p.JIDs) > 0 {
		return p.JIDs[0]
	}
	return ""
}

// ResolveChat shares the owner alias contract across reads and sends. Permissions are
// still checked against the resolved JID by each caller.
func (s *Service) ResolveChat(chat string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(chat)) {
	case "me", "myself":
		if own := s.OwnerChat(); own != "" {
			return own, nil
		}
		return "", model.ErrNotFound
	default:
		return chat, nil
	}
}
