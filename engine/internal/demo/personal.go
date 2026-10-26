package demo

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
)

func seedPersonal(h *hub.Service, p *people.Service) error {
	var done bool
	if h.Repo.Get("demo_meta", "personal_f2", &done) == nil {
		return nil
	}
	if e := p.KeepOwner(model.Person{ID: "me", JIDs: []string{DM(1)}, Name: "Alex"}); e != nil {
		return e
	}
	raw := map[string]json.RawMessage{}
	values := map[string]any{"name": "Alex", "about": "Psychology student who enjoys design and community projects.", "studies": "Northfield University, BSc Psychology", "work": "", "people": []model.OwnerPerson{{Name: "Mum", Relation: "mum", PersonID: ptr("person-02")}, {Name: "Noor Chen", Relation: "sister", PersonID: ptr("person-05")}, {Name: "Maya Rao", Relation: "best friend", PersonID: ptr("person-04")}}, "projects": []model.OwnerProject{{Name: "Design Club 2026", With: []string{"Maya Rao"}}}}
	for k, v := range values {
		raw[k], _ = json.Marshal(v)
	}
	if _, e := h.UpdateOwnerProfile(nil, raw); e != nil {
		return e
	}
	return h.Repo.Put("demo_meta", "personal_f2", true)
}
func ptr(s string) *string { return &s }
