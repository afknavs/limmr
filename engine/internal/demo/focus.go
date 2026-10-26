package demo

import (
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
)

func seedFocus(h *hub.Service, p *people.Service) error {
	old := Epoch.AddDate(-2, 0, 0)
	for i, name := range []string{"Former school batch", "Old chess club", "Alumni notices"} {
		jid := Group(10 + i)
		if e := p.PutGroup(model.Group{JID: jid, Name: name, MemberCount: 24}); e != nil {
			return e
		}
		if e := h.Messages.PutChat(model.Chat{JID: jid, Name: name, LastMessageTime: &old}); e != nil {
			return e
		}
		if e := h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("old-%d", i), ChatJID: jid, Sender: DM(13), Content: "Archived plans from two years ago.", Timestamp: old}); e != nil {
			return e
		}
	}
	if e := p.PutPerson(model.Person{ID: "person-13", JIDs: []string{DM(13)}, Name: "Avery Lane", UpdatedAt: old}); e != nil {
		return e
	}
	if e := h.Messages.PutChat(model.Chat{JID: DM(13), Name: "Avery Lane", LastMessageTime: &old}); e != nil {
		return e
	}
	if e := h.Messages.PutMessage(model.Message{ID: "old-dm", IsFromMe: true, ChatJID: DM(13), Sender: DM(13), Content: "See you at the old chess club.", Timestamp: old}); e != nil {
		return e
	}
	parent := Group(13)
	if e := p.PutGroup(model.Group{JID: parent, Name: "Campus network", IsCommunity: true, MemberCount: 60}); e != nil {
		return e
	}
	if e := h.Messages.PutChat(model.Chat{JID: parent, Name: "Campus network", LastMessageTime: &Epoch}); e != nil {
		return e
	}
	for _, i := range []int{3, 4, 5} {
		var g model.Group
		if e := h.Repo.Get("groups", Group(i), &g); e != nil {
			return e
		}
		g.ParentJID = parent
		if e := p.PutGroup(g); e != nil {
			return e
		}
	}
	return seedPersonal(h, p)
}
