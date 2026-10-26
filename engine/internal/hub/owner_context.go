package hub

import (
	"limmr/internal/model"
	"strings"
)

func (s *Service) PresetSummary(a model.Agent) string {
	if a.Preset == "own" {
		return "Your own AI: full access"
	}
	return "An AI you were given: " + s.AccessSummary(a.Permissions.Access)
}
func (s *Service) OwnerQuery(a *model.Agent, q string) string {
	p := s.VisibleOwnerProfile(a)
	key := strings.ToLower(strings.TrimSpace(q))
	key = strings.TrimPrefix(key, "my ")
	for _, v := range p.People {
		relation := strings.ToLower(v.Relation)
		if key == relation || (key == "mom" || key == "mother") && (relation == "mum" || relation == "mother") {
			if v.PersonID != nil {
				var person model.Person
				if s.Repo.Get("people", *v.PersonID, &person) == nil {
					return person.Name
				}
			}
			return v.Name
		}
	}
	if key == "university" || key == "school" {
		return strings.TrimSpace(strings.Split(p.Studies, ",")[0])
	}
	return q
}

func (s *Service) OwnerPersonAlias(a *model.Agent, q string) string {
	p := s.VisibleOwnerProfile(a)
	key := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(q)), "my ")
	found := ""
	for _, v := range p.People {
		relation := strings.ToLower(v.Relation)
		if key == relation || (key == "mom" || key == "mother") && (relation == "mum" || relation == "mother") {
			if v.PersonID != nil {
				if found != "" {
					return ""
				}
				found = *v.PersonID
			}
		}
	}
	return found
}
