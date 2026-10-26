package demo

import (
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"time"
)

func seedRound10(h *hub.Service, p *people.Service) error {
	var done bool
	if h.Repo.Get("demo_meta", "round10", &done) == nil {
		return nil
	}
	// Upgrade the previous demo circle id without duplicating it.
	var work model.Circle
	if e := h.Repo.Get("circles", "work", &work); e == model.ErrNotFound {
		if e = h.Repo.Get("circles", "internship", &work); e != nil {
			return e
		}
		work.ID = "work"
		work.Name = "Work"
		mutations := []hub.Mutation{{Bucket: "circles", ID: "work", Value: work}, {Bucket: "circles", ID: "internship", Delete: true}}
		briefs, e := hub.List[model.Brief](h.Repo, "briefs")
		if e != nil {
			return e
		}
		for _, b := range briefs {
			if b.CircleID == "internship" {
				old := fmt.Sprintf("internship:%09d", b.Version)
				b.CircleID = "work"
				mutations = append(mutations, hub.Mutation{Bucket: "briefs", ID: old, Delete: true}, hub.Mutation{Bucket: "briefs", ID: fmt.Sprintf("work:%09d", b.Version), Value: b})
			}
		}
		if e = h.Repo.Apply(mutations...); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	service := &circles.Service{Hub: h, People: p}
	var e error
	until := Epoch.AddDate(0, 0, 30)
	ended := Epoch.Add(-24 * time.Hour)
	for _, v := range []struct {
		id, name string
		until    time.Time
	}{{"acme-assistant", "Acme Assistant", until}, {"ended-assistant", "Past Assistant", ended}} {
		seen := Epoch
		a := model.Agent{LastSeenAt: &seen, ID: v.id, Name: v.name, Kind: "chatgpt", Transport: "remote", CreatedAt: Epoch.AddDate(0, 0, -7), Enabled: true, Permissions: model.Defaults(true)}
		at := v.until
		a.Permissions.Access = model.Access{Circles: model.CircleSelection{"work"}, Until: &at}
		if e = h.Repo.Put("agents", a.ID, a); e != nil {
			return e
		}
	}
	// Replace the earlier two proposals with the four-circle ChatGPT batch.
	for _, id := range []string{"proposal-1", "proposal-2"} {
		if e = h.Repo.Apply(hub.Mutation{Bucket: "circles", ID: id, Delete: true}, hub.Mutation{Bucket: "proposals", ID: id, Delete: true}); e != nil {
			return e
		}
	}
	a, e := h.Agent("agent-3")
	if e != nil {
		return e
	}
	// Demo ChatGPT has owner-granted write access for the setup story.
	a.Permissions = model.Defaults(false)
	if e = h.UpdateAgent(a); e != nil {
		return e
	}
	for i, v := range []struct{ name, kind, chat string }{{"Reading weekend", "friends", DM(4)}, {"Studio exchange", "community", Group(5)}, {"Sunday plans", "family", Group(1)}, {"Workshop project", "project", Group(8)}} {
		chat := fmt.Sprintf("12036390000%d@g.us", i)
		if e = h.Messages.PutChat(model.Chat{JID: chat, Name: v.name, IsGroup: true, LastMessageTime: &Epoch}); e != nil {
			return e
		}
		if _, e = service.Create(&a, model.Circle{Name: v.name, Kind: v.kind, Chats: []model.CircleChat{{ChatJID: chat}}}, "Shared conversations suggest this circle; keep or edit it in Limmr."); e != nil {
			return e
		}
	}
	return h.Repo.Put("demo_meta", "round10", true)
}
