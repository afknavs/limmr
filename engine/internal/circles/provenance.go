package circles

import (
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
)

func (s *Service) origin(a *model.Agent) *model.Origin {
	o := &model.Origin{By: "you", ByKind: "you", At: s.Hub.Now().UTC()}
	if a != nil {
		o.By = a.ID
		o.ByKind = "agent"
	}
	return o
}

// stampChanges couples provenance and history to the same circle transaction.
func (s *Service) stampChanges(a *model.Agent, before, c *model.Circle) []hub.Mutation {
	ms := []hub.Mutation{}
	record := func(kind, target, label string) *model.Origin {
		o := s.origin(a)
		o.ChangeID = dbutil.ID()
		ch := model.UnderstandingChange{ID: o.ChangeID, CircleID: c.ID, TargetID: target, Kind: kind, Label: label, Origin: *o}
		ms = append(ms, hub.Mutation{Bucket: "understanding_changes", ID: ch.ID, Value: ch})
		return o
	}
	if !before.Confirmed && c.Confirmed {
		c.ConfirmOrigin = record("circle_confirmed", c.ID, c.Name)
	}
	for i := range c.Chats {
		chat := &c.Chats[i]
		var old *model.CircleChat
		for j := range before.Chats {
			if before.Chats[j].ChatJID == chat.ChatJID {
				old = &before.Chats[j]
				break
			}
		}
		if old == nil || old.Role != chat.Role {
			chat.Origin = record("edge_added", chat.ChatJID, chat.Role)
		} else {
			chat.Origin = old.Origin
		}
	}
	for i := range c.People {
		p := &c.People[i]
		var old *model.CirclePerson
		for j := range before.People {
			if before.People[j].PersonID == p.PersonID {
				old = &before.People[j]
				break
			}
		}
		if old == nil || old.Role != p.Role || old.Note != p.Note {
			p.Origin = record("role_set", p.PersonID, p.Role)
			p.Source = p.Origin.By
		} else {
			p.Origin = old.Origin
		}
	}
	for i := range c.Topics {
		t := &c.Topics[i]
		var old *model.Topic
		for j := range before.Topics {
			if before.Topics[j].ID == t.ID {
				old = &before.Topics[j]
				break
			}
		}
		if old == nil {
			t.Origin = record("topic_added", t.ID, t.Title)
		} else {
			t.Origin = old.Origin
		}
	}
	return ms
}
