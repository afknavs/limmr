package watches

import (
	"limmr/internal/model"
	"limmr/internal/triggers"
)

// Current access is rechecked before a watch consumes messages or starts a run,
// as well as when its owner reads it over MCP. Narrowing a grant cannot keep an
// old excluded-circle watch alive through a chat shared with an allowed circle.
func (s *Service) scopeAllowed(a model.Agent, r triggers.Record) bool {
	if a.Permissions.Access.Ended(s.Hub.Now()) {
		return false
	}
	allowed := s.Hub.ChatAccess(&a)
	for _, j := range r.Scope.Chats {
		if !allowed(j) {
			return false
		}
	}
	for _, id := range r.Scope.Circles {
		var c model.Circle
		if s.Hub.Repo.Get("circles", id, &c) != nil || !c.Confirmed || !s.Hub.CircleAccess(&a, c) {
			return false
		}
		for _, j := range c.Chats {
			if !allowed(j.ChatJID) {
				return false
			}
		}
	}
	if len(r.Scope.People)+len(r.Match.People) > 0 {
		ps, e := s.People.People(&a, "")
		if e != nil {
			return false
		}
		ids := map[string]bool{}
		for _, p := range ps {
			ids[p.ID] = true
		}
		for _, xs := range [][]string{r.Scope.People, r.Match.People} {
			for _, id := range xs {
				if !ids[id] {
					return false
				}
			}
		}
	}
	return true
}
