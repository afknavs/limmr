package hub

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"strings"
	"unicode/utf8"
)

func (s *Service) OwnerProfile() model.OwnerProfile {
	p := model.OwnerProfile{People: []model.OwnerPerson{}, Projects: []model.OwnerProject{}, Suggestions: []model.OwnerSuggestion{}}
	_ = s.Repo.Get("settings", "owner_profile", &p)
	if p.People == nil {
		p.People = []model.OwnerPerson{}
	}
	if p.Projects == nil {
		p.Projects = []model.OwnerProject{}
	}
	if p.Suggestions == nil {
		p.Suggestions = []model.OwnerSuggestion{}
	}
	if person, e := s.Owner(); e == nil {
		p.Person = &person
		if p.Name == "" {
			p.Name = person.Name
			if person.Label != "" {
				p.Name = person.Label
			}
		}
	}
	return p
}
func ownerPatch(p *model.OwnerProfile, raw map[string]json.RawMessage) error {
	if len(raw) == 0 {
		return model.ErrInvalid
	}
	for k, v := range raw {
		var dst any
		switch k {
		case "name":
			dst = &p.Name
		case "about":
			dst = &p.About
		case "studies":
			dst = &p.Studies
		case "work":
			dst = &p.Work
		case "people":
			dst = &p.People
		case "projects":
			dst = &p.Projects
		default:
			return model.ErrInvalid
		}
		if string(v) == "null" || json.Unmarshal(v, dst) != nil {
			return model.ErrInvalid
		}
	}
	if p.Name != "" && !model.Named(model.Person{Name: p.Name}) {
		return model.ErrInvalid
	}
	if utf8.RuneCountInString(p.About) > 600 || len(p.Name) > 100 || len(p.Studies) > 300 || len(p.Work) > 300 || len(p.People) > 50 || len(p.Projects) > 30 {
		return model.ErrInvalid
	}
	for _, v := range p.People {
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 100 || len(v.Relation) > 100 {
			return model.ErrInvalid
		}
	}
	for _, v := range p.Projects {
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 100 || len(v.With) > 50 {
			return model.ErrInvalid
		}
		for _, n := range v.With {
			if len(n) > 100 {
				return model.ErrInvalid
			}
		}
	}
	return nil
}

// Every AI edit is a reviewable suggestion; even full access cannot overwrite owner text.
func (s *Service) UpdateOwnerProfile(a *model.Agent, raw map[string]json.RawMessage) (model.OwnerProfile, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	p := s.OwnerProfile()
	test := p
	if e := ownerPatch(&test, raw); e != nil {
		return p, e
	}
	if a != nil {
		if len(p.Suggestions)+len(raw) > 100 {
			return p, model.ErrConflict
		}
		for k, v := range raw {
			var val any
			_ = json.Unmarshal(v, &val)
			p.Suggestions = append(p.Suggestions, model.OwnerSuggestion{ID: dbutil.ID(), Field: k, Value: val, Source: a.ID, At: s.Now().UTC()})
		}
		p.UpdatedBy = a.ID
	} else {
		p = test
		p.UpdatedBy = "you"
	}
	p.UpdatedAt = s.Now().UTC()
	if e := s.saveOwner(p); e != nil {
		return p, e
	}
	return s.OwnerProfile(), nil
}
func (s *Service) saveOwner(p model.OwnerProfile) error {
	p.Person = nil
	if data, e := json.Marshal(p); e != nil || len(data) > 32000 {
		return model.ErrInvalid
	}
	changes := []Mutation{{Bucket: "settings", ID: "owner_profile", Value: p}}
	if p.Name != "" {
		settings := s.Settings()
		settings.Owner = p.Name
		changes = append(changes, Mutation{Bucket: "settings", ID: "main", Value: settings})
		var person model.Person
		if s.Repo.Get("people", "me", &person) != nil {
			person = model.Person{ID: "me", JIDs: []string{}}
		}
		person.Label = p.Name
		person.Name = p.Name
		person.UpdatedAt = p.UpdatedAt
		changes = append(changes, Mutation{Bucket: "people", ID: "me", Value: person})
	}
	return s.Repo.Apply(changes...)
}
func (s *Service) ReviewOwnerSuggestion(id string, confirm bool) (model.OwnerProfile, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	p := s.OwnerProfile()
	for i, v := range p.Suggestions {
		if v.ID == id {
			if confirm {
				raw, _ := json.Marshal(v.Value)
				if e := ownerPatch(&p, map[string]json.RawMessage{v.Field: raw}); e != nil {
					return p, e
				}
			}
			p.Suggestions = append(p.Suggestions[:i], p.Suggestions[i+1:]...)
			p.UpdatedBy = "you"
			p.UpdatedAt = s.Now().UTC()
			if e := s.saveOwner(p); e != nil {
				return p, e
			}
			return s.OwnerProfile(), nil
		}
	}
	return p, model.ErrNotFound
}

// Scoped AIs cannot acquire the owner's family context through onboarding.
func (s *Service) VisibleOwnerProfile(a *model.Agent) model.OwnerProfile {
	p := s.OwnerProfile()
	if a != nil && (a.Permissions.Access.Circles != nil || a.Permissions.Chats.Mode != "all") {
		return model.OwnerProfile{Name: p.Name, People: []model.OwnerPerson{}, Projects: []model.OwnerProject{}, Suggestions: []model.OwnerSuggestion{}}
	}
	return p
}
func (s *Service) SwitchPreset(id, preset string) (model.Agent, error) {
	if preset != "own" && preset != "given" {
		return model.Agent{}, model.ErrInvalid
	}
	a, e := s.Agent(id)
	if e != nil {
		return a, e
	}
	if id == "this-mac" {
		return a, model.ErrInvalid
	}
	a.Preset = preset
	a.Permissions = model.Preset(preset)
	e = s.UpdateAgent(a)
	return s.PresentAgent(a), e
}
