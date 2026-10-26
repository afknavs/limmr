package people

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"time"
)

// KeepOwner promotes existing aliases without losing enrichment or references.
func (s *Service) KeepOwner(p model.Person) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	all, e := hub.List[model.Person](s.Hub.Repo, "people")
	if e != nil {
		return e
	}
	ids := map[string]bool{"me": true}
	for _, old := range all {
		match := old.ID == "me"
		for _, j := range p.JIDs {
			match = match || model.Contains(old.JIDs, j)
		}
		if !match {
			continue
		}
		ids[old.ID] = true
		if old.Label != "" {
			p.Label = old.Label
		}
		if old.Role != "" {
			p.Role = old.Role
		}
		if old.HowKnown != "" {
			p.HowKnown = old.HowKnown
		}
		if old.Notes != "" {
			p.Notes = old.Notes
		}
		if old.Circle != "" {
			p.Circle = old.Circle
		}
		if len(old.Details.Phones) > 0 || len(old.Details.Emails) > 0 || len(old.Details.Handles) > 0 || old.Details.Birthday != "" || old.Details.Location != "" || old.Details.Pronouns != "" {
			p.Details = old.Details
		}
		p.DetailSuggestions = append(p.DetailSuggestions, old.DetailSuggestions...)
		for _, j := range old.JIDs {
			if !model.Contains(p.JIDs, j) {
				p.JIDs = append(p.JIDs, j)
			}
		}
	}
	p.ID = "me"
	if p.Name == "" {
		p.Name = s.Hub.Settings().Owner
	}
	if p.Name == "" {
		p.Name = "You"
	}
	xs := []hub.Mutation{{Bucket: "people", ID: "me", Value: p}}
	for id := range ids {
		if id != "me" {
			xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Delete: true})
		}
	}
	ms, e := s.memberships()
	if e != nil {
		return e
	}
	for _, m := range ms {
		if !ids[m.Person] || m.Person == "me" {
			continue
		}
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: m.Group + ":" + m.Person, Delete: true})
		m.Person = "me"
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: m.Group + ":me", Value: m})
	}
	fs, e := hub.List[model.Fact](s.Hub.Repo, "facts")
	if e != nil {
		return e
	}
	for _, f := range fs {
		if f.Subject == "person" && ids[f.SubjectID] {
			f.SubjectID = "me"
			xs = append(xs, hub.Mutation{Bucket: "facts", ID: f.ID, Value: f})
		}
	}
	rs, e := hub.List[model.Relation](s.Hub.Repo, "relations")
	if e != nil {
		return e
	}
	for _, r := range rs {
		if ids[r.A] || ids[r.B] {
			if ids[r.A] {
				r.A = "me"
			}
			if ids[r.B] {
				r.B = "me"
			}
			xs = append(xs, hub.Mutation{Bucket: "relations", ID: r.ID, Value: r})
		}
	}
	refs, e := s.personReferences(ids, "me")
	if e != nil {
		return e
	}
	xs = append(xs, refs...)

	if len(p.JIDs) > 0 {
		chat := p.JIDs[0]
		if e = s.Hub.Messages.PutChat(model.Chat{JID: chat, Name: p.Name}); e != nil {
			return e
		}
		var you model.Circle
		if s.Hub.Repo.Get("circles", "you", &you) != nil {
			you = model.Circle{ID: "you", Name: "You", Kind: "self", Confirmed: true, SendRule: "free", Chats: []model.CircleChat{{ChatJID: chat}}, People: []model.CirclePerson{{PersonID: "me", Role: "You"}}}
			xs = append(xs, hub.Mutation{Bucket: "circles", ID: "you", Value: you})
		}
	}
	return s.Hub.Repo.Apply(xs...)
}

// RecordReceipt receives passive incoming WhatsApp receipts only.
func (s *Service) RecordReceipt(chat, sender string, ids []string, status string, at time.Time) error {
	return s.Hub.RecordReceipt(chat, sender, ids, status, at)
}
