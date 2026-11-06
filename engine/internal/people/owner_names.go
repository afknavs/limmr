package people

import (
	"crypto/sha256"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
)

func nameKey(v string) string { return strings.ToLower(strings.Join(strings.Fields(v), " ")) }
func (s *Service) ownerNames() map[string]bool {
	names := map[string]bool{}
	add := func(v string) {
		if k := nameKey(v); k != "" && k != "you" {
			names[k] = true
		}
	}
	add(s.Hub.Settings().Owner)
	add(s.Hub.OwnerProfile().Name)
	if owner, e := s.Hub.Owner(); e == nil {
		for _, v := range append([]string{owner.Name, owner.PushName, owner.Label}, owner.OtherNames...) {
			add(v)
		}
	}
	return names
}
func withoutOwnerNames(values []string, names map[string]bool) []string {
	out := values[:0]
	for _, v := range values {
		if !names[nameKey(v)] {
			out = append(out, v)
		}
	}
	return out
}

// CleanOwnerNames repairs legacy aliases only. It never removes a person, a JID,
// a saved contact name, or enrichment. Re-run when the owner's known names change.
func (s *Service) CleanOwnerNames() error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	names := s.ownerNames()
	key := fmt.Sprintf("owner_names_%x", sha256.Sum256([]byte(fmt.Sprint(names))))
	var done bool
	if s.Hub.Repo.Get("people_migrations", key, &done) == nil && done {
		return nil
	}
	ps, e := hub.List[model.Person](s.Hub.Repo, "people")
	if e != nil {
		return e
	}
	xs := []hub.Mutation{}
	for _, p := range ps {
		if p.ID == "me" {
			continue
		}
		n := len(p.OtherNames)
		push := p.PushName
		if names[nameKey(p.PushName)] {
			p.PushName = ""
		}
		p.OtherNames = withoutOwnerNames(p.OtherNames, names)
		if len(p.OtherNames) != n || p.PushName != push {
			xs = append(xs, hub.Mutation{Bucket: "people", ID: p.ID, Value: p})
		}
	}
	xs = append(xs, hub.Mutation{Bucket: "people_migrations", ID: key, Value: true})
	return s.Hub.Repo.Apply(xs...)
}
