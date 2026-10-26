// Package demo contains deterministic invented circles for Alex, a 20-year-old
// student and freelancer. It never constructs a real WhatsApp client.
package demo

import (
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/wa"
	"strings"
	"time"
)

var Epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func DM(id int) string    { return fmt.Sprintf("1555010%04d@s.whatsapp.net", id) }
func Group(id int) string { return fmt.Sprintf("120363000%03d@g.us", id) }
func Seed(h *hub.Service, p *people.Service, l *wa.Link) error {
	h.Now = func() time.Time { return Epoch }
	var done bool
	ready := func() {
		l.Emit(wa.LinkEvent{State: "ready", Account: &wa.Account{Name: "Alex", PhoneMasked: "•••• 00"}})
	}
	if h.Repo.Get("demo_meta", "understanding_v3", &done) == nil {
		ready()
		if e := seedRound10(h, p); e != nil {
			return e
		}
		if e := seedFocus(h, p); e != nil {
			return e
		}
		return seedEfforts(h)
	}
	if e := h.SetSettings(model.Settings{Owner: "Alex", HiddenChats: []string{}, RetentionDays: 180}); e != nil {
		return e
	}
	names := []string{"Alex", "Mum", "Dad", "Maya Rao", "Noor Chen", "Sam Patel", "Dr. Morgan", "Ms. Taylor", "Jordan Lee", "Riley Park", "Casey Stone", "Robin Bell"}
	for i, name := range names {
		id := i + 1
		at := Epoch.Add(-time.Duration(id) * time.Minute)
		person := model.Person{ID: fmt.Sprintf("person-%02d", id), JIDs: []string{DM(id)}, Name: name, PushName: name, UpdatedAt: at}
		person.Details.Emails = []model.LabeledValue{{Label: "personal", Value: strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@example.com", Source: "you"}}
		if id == 4 {
			person.HowKnown = "Classmate and collaborator on Atlas"
			person.Details.Birthday = "04-12"
			person.Details.Location = "Bengaluru"
			person.Details.Handles = []model.LabeledValue{{Label: "LinkedIn", Value: "linkedin.com/in/maya-rao-example", Source: "you"}}
			person.DetailSuggestions = []model.DetailSuggestion{
				{ID: "detail-email", Field: "emails", Label: "work", Value: "maya.studio@example.com", Source: "agent-2", Status: "suggested"},
				{ID: "detail-pronouns", Field: "pronouns", Value: "she/her", Source: "agent-2", Status: "suggested"},
			}
		}
		if id == 5 {
			person.Details.Birthday = "1999-08-23"
		}
		if e := p.PutPerson(person); e != nil {
			return e
		}
		if e := h.Messages.PutChat(model.Chat{JID: DM(id), Name: name, LastMessageTime: &at}); e != nil {
			return e
		}
	}
	groupNames := []string{"Family", "Close friends", "University class", "Unofficial class", "Design Club", "Internship team", "Cross-team studio", "Atlas renovation", "Flatmates"}
	memberships := [][]int{{1, 2, 3}, {1, 4, 5}, {1, 4, 5, 6, 7, 8}, {1, 4, 5, 6}, {1, 5, 6}, {1, 9, 10}, {1, 9, 10, 11}, {1, 4, 11, 12}, {1, 5, 6}}
	for i, name := range groupNames {
		jid := Group(i + 1)
		at := Epoch.Add(-time.Duration(i+1) * 10 * time.Minute)
		if e := p.PutGroup(model.Group{JID: jid, Name: name, Description: "Plans and updates for " + name, MemberCount: len(memberships[i])}); e != nil {
			return e
		}
		if e := h.Messages.PutChat(model.Chat{JID: jid, Name: name, LastMessageTime: &at}); e != nil {
			return e
		}
		for _, id := range memberships[i] {
			if e := p.PutMembership(model.Membership{Group: jid, Person: fmt.Sprintf("person-%02d", id)}); e != nil {
				return e
			}
		}
	}
	for i, name := range []string{"Claude Code", "Codex", "ChatGPT"} {
		transport := "local"
		if i == 2 {
			transport = "remote"
		}
		seen := Epoch.Add(-time.Duration(i+1) * 5 * time.Minute)
		a := model.Agent{ID: fmt.Sprintf("agent-%d", i+1), Name: name, Kind: []string{"claude_code", "codex", "chatgpt"}[i], Transport: transport, Enabled: true, CreatedAt: Epoch.AddDate(0, -1, 0), LastSeenAt: &seen, Permissions: model.Defaults(transport == "remote")}
		if e := h.Repo.Put("agents", a.ID, a); e != nil {
			return e
		}
	}
	type seedCircle struct {
		id, name, kind, summary, changed, ask, date, upcoming string
		chats                                                 []string
		people                                                []int
		roles                                                 []string
	}
	specs := []seedCircle{
		{"you", "You", "self", "Alex is 20, studying psychology and freelancing on a renovation project. Keep Thursday afternoon free for coursework.", "The weekly plan now includes a client review.", "Block two hours for revision.", "2026-10-20", "Weekly planning", []string{DM(1)}, []int{1}, []string{"you"}},
		{"family", "Family", "family", "Sunday lunch is at home. Mum is organising the meal and Dad can collect Alex from the station.", "Lunch moved to one o'clock.", "Tell Mum when the train arrives.", "2026-10-18", "Sunday lunch at 13:00", []string{Group(1), DM(2), DM(3)}, []int{1, 2, 3}, []string{"you", "mum", "dad"}},
		{"friends", "Close friends", "friends", "Maya and Noor are planning a Saturday walk after coursework. The route starts at the library.", "Maya found a shorter route.", "Choose the morning or afternoon slot.", "2026-10-24", "Library walk", []string{Group(2), DM(4), DM(5)}, []int{1, 4, 5}, []string{"you", "friend", "friend"}},
		{"university", "University", "university", "The class has a psychology assignment due Tuesday. Dr. Morgan clarified the marking rubric; Ms. Taylor is collecting workshop ideas. Noor is booking the club room.", "The unofficial class group shared a revised reading list.", "Check the assignment references before submission.", "2026-10-20", "Psychology assignment due", []string{Group(3), Group(4), Group(5), DM(7), DM(8)}, []int{1, 4, 5, 6, 7, 8}, []string{"you", "classmate", "club member", "classmate", "HOD", "class teacher"}},
		{"work", "Work", "work", "Jordan needs Alex's draft by Monday. The team agreed to a shorter onboarding note and Riley is testing the shared checklist with another team.", "Cross-team feedback removed two redundant steps.", "Send Jordan the revised draft after checking the examples.", "2026-10-19", "Manager review", []string{Group(6), DM(9), Group(7)}, []int{1, 9, 10, 11}, []string{"you", "manager", "teammate", "cross-team colleague"}},
		{"atlas", "Atlas renovation", "project", "Client Robin approved the living room layout. Contractor Casey needs the final tile choice; Maya, also Alex's classmate, is preparing material samples.", "Robin prefers darker bathroom tiles.", "Choose a tile sample and confirm the site visit.", "2026-10-21", "Site visit at 10:00", []string{DM(12), Group(8)}, []int{1, 4, 11, 12}, []string{"designer", "materials collaborator", "contractor", "client"}},
		{"flatmates", "Flatmates", "community", "The flat is splitting the utilities bill. Noor, also a Design Club member, has the meter photo and Sam will arrange the repair visit.", "The plumber can come Wednesday morning.", "Confirm someone can be home for the plumber.", "2026-10-21", "Plumber visit", []string{Group(9), DM(6)}, []int{1, 5, 6}, []string{"you", "flatmate", "flatmate"}},
	}
	cservice := &circles.Service{Hub: h, People: p}
	for specIndex, v := range specs {
		c := model.Circle{ID: v.id, Name: v.name, Kind: v.kind, Symbol: "circle.grid.2x2", Description: "Alex's " + v.name + " circle", SendRule: model.DefaultSendRule(v.kind), Pinned: v.id == "university" || v.id == "atlas", CreatedBy: "you", Confirmed: true, UpdatedAt: Epoch.Add(-2 * time.Hour), Chats: []model.CircleChat{}, People: []model.CirclePerson{}, Topics: []model.Topic{}}
		c.Origin = &model.Origin{By: "agent-2", ByKind: "agent", At: Epoch.AddDate(0, 0, -20)}
		for _, j := range v.chats {
			c.Chats = append(c.Chats, model.CircleChat{ChatJID: j, Role: "included chat", IncludedSince: Epoch.AddDate(0, -1, 0)})
		}
		for i, id := range v.people {
			c.People = append(c.People, model.CirclePerson{PersonID: fmt.Sprintf("person-%02d", id), Role: v.roles[i], Source: "agent-2", Origin: c.Origin})
		}
		if v.id == "atlas" {
			c.Topics = append(c.Topics, model.Topic{ID: "atlas-tiles", Title: "Bathroom tiles", Status: "active", Chats: v.chats, People: []string{"person-04", "person-11", "person-12"}, Keywords: []string{"tiles", "bathroom", "samples"}, LastActivityAt: Epoch.Add(-10 * time.Minute)}, model.Topic{ID: "atlas-site", Title: "Site visit", Status: "watching", Chats: v.chats, People: []string{"person-11", "person-12"}, Keywords: []string{"visit", "Wednesday"}, LastActivityAt: Epoch.Add(-30 * time.Minute)})
		}
		for i := range c.Topics {
			c.Topics[i].Origin = &model.Origin{By: "agent-1", ByKind: "agent", At: Epoch.AddDate(0, 0, -5)}
		}
		if e := h.Repo.Put("circles", c.ID, c); e != nil {
			return e
		}
		evidence := []string{}
		for i, j := range v.chats {
			for k := 0; k < 3; k++ {
				at := Epoch.Add(-time.Duration(180-k*80+i) * time.Minute)
				text := []string{v.summary, v.changed, v.ask}[k]
				id := fmt.Sprintf("%s-%02d-%02d", v.id, i, k)
				message := model.Message{ID: id, ChatJID: j, Sender: DM(v.people[min(i, len(v.people)-1)]), Content: text, Timestamp: at}
				if j == Group(5) && k == 2 {
					message.Content = ""
					message.MediaType = "sticker"
				}
				if e := h.Messages.PutMessage(message); e != nil {
					return e
				}
				if k == 0 {
					evidence = append(evidence, id)
				}
			}
		}
		asks := []model.OpenAsk{}
		covers := Epoch.Add(-30 * time.Minute)
		if v.id == "university" || v.id == "atlas" || v.id == "flatmates" {
			asks = append(asks, model.OpenAsk{Text: v.ask, ChatJID: v.chats[len(v.chats)-1], From: names[v.people[len(v.people)-1]-1], At: Epoch.Add(-20 * time.Minute)})
			covers = Epoch.Add(-2 * time.Hour)
		}
		b, e := cservice.WriteBrief(nil, c.ID, model.Brief{Summary: v.summary, WhatChanged: v.changed, OpenAsks: asks, Upcoming: []model.Upcoming{{Date: v.date, Text: v.upcoming}}, PeopleNotes: []model.PeopleNote{{PersonID: fmt.Sprintf("person-%02d", v.people[len(v.people)-1]), Contribution: v.roles[len(v.roles)-1] + " shared the latest plan."}}, Evidence: evidence, CoversUntil: covers})
		if e != nil {
			return e
		}
		b.WrittenBy = fmt.Sprintf("agent-%d", specIndex%3+1)
		b.WrittenAt = Epoch.Add(-time.Duration(13-specIndex*2) * 24 * time.Hour)
		if e = h.Repo.Put("briefs", fmt.Sprintf("%s:%09d", c.ID, b.Version), b); e != nil {
			return e
		}
		if e = h.Record(model.Activity{At: b.WrittenAt, AgentID: b.WrittenBy, Tool: "update_brief", Kind: "summary", ChatJIDs: circles.JIDs(c), MessageIDs: evidence, Note: b.Summary, Count: 1}); e != nil {
			return e
		}
	}
	for i, v := range []struct{ circle, chat, text, why string }{{"family", DM(2), "I'll check the train and let you know this evening.", "Mum needs an arrival time."}, {"university", DM(8), "Could we use the workshop room after class?", "The club needs a room."}, {"atlas", DM(12), "I'll bring the darker tile samples to Wednesday's visit.", "Robin asked to compare the darker samples."}} {
		x := model.Suggestion{ID: fmt.Sprintf("suggestion-%d", i+1), CircleID: v.circle, Kind: "reply", ChatJID: v.chat, DraftText: v.text, Variations: []string{v.text, "Thanks for the update. " + v.text, "Quick update: " + v.text}, Why: v.why, CreatedBy: "agent-2", CreatedAt: Epoch.Add(-time.Hour), ExpiresAt: Epoch.Add(24 * time.Hour), State: "pending"}
		if e := h.Repo.Put("suggestions", x.ID, x); e != nil {
			return e
		}
	}
	for i, v := range []struct {
		name, kind string
		chats      []string
	}{{"Reading weekend", "friends", []string{DM(4)}}, {"Studio exchange", "community", []string{Group(5)}}} {
		c := model.Circle{ID: fmt.Sprintf("proposal-%d", i+1), Name: v.name, Kind: v.kind, Symbol: "person.2", SendRule: model.DefaultSendRule(v.kind), CreatedBy: "agent-2", UpdatedAt: Epoch, Chats: []model.CircleChat{}, People: []model.CirclePerson{}, Topics: []model.Topic{}}
		c.CreatedBy = fmt.Sprintf("agent-%d", i+1)
		c.Origin = &model.Origin{By: c.CreatedBy, ByKind: "agent", At: Epoch.Add(-time.Duration(8-i*7) * 24 * time.Hour)}
		for _, j := range v.chats {
			c.Chats = append(c.Chats, model.CircleChat{ChatJID: j, Role: "shared activity", IncludedSince: c.Origin.At})
		}
		if e := h.Repo.Apply(hub.Mutation{Bucket: "circles", ID: c.ID, Value: c}, hub.Mutation{Bucket: "proposals", ID: c.ID, Value: model.Proposal{ID: c.ID, Circle: c, Why: "Recent conversations and owner-known context; review before keeping.", Source: c.CreatedBy}}); e != nil {
			return e
		}
	}
	// Attributed discoveries across two weeks, without extra message content.
	for _, v := range []struct {
		circle, person, agent string
		age                   time.Duration
	}{
		{"atlas", "person-04", "agent-2", 3 * 24 * time.Hour},
		{"university", "person-07", "agent-1", 6 * 24 * time.Hour},
		{"flatmates", "person-05", "agent-3", 25 * time.Minute},
	} {
		var c model.Circle
		if e := h.Repo.Get("circles", v.circle, &c); e != nil {
			return e
		}
		for i := range c.People {
			if c.People[i].PersonID == v.person {
				c.People[i].Origin = &model.Origin{By: v.agent, ByKind: "agent", At: Epoch.Add(-v.age)}
				c.People[i].Source = v.agent
			}
		}
		if e := h.Repo.Put("circles", c.ID, c); e != nil {
			return e
		}
	}
	if e := h.Record(model.Activity{At: Epoch.Add(-30 * time.Second), AgentID: "agent-2", Tool: "get_brief", Kind: "read", ChatJIDs: []string{Group(3)}}); e != nil {
		return e
	}
	for _, f := range []model.Fact{
		{ID: "a7-gmail-sample", Subject: "person", SubjectID: "person-04", Text: "Works with the Atlas design studio. Sample of a coming-soon source.", Source: "gmail", CreatedAt: Epoch.Add(-time.Hour), Status: "active", EvidenceMessageIDs: []string{}},
		{ID: "a7-suggested", Subject: "person", SubjectID: "person-04", Text: "Coordinates the studio material library.", Source: "agent-2", CreatedAt: Epoch, Status: "suggested", EvidenceMessageIDs: []string{}},
	} {
		if e := h.Repo.Put("facts", f.ID, f); e != nil {
			return e
		}
	}
	if e := h.Repo.Put("demo_meta", "understanding_v3", true); e != nil {
		return e
	}
	ready()
	if e := seedRound10(h, p); e != nil {
		return e
	}
	if e := seedFocus(h, p); e != nil {
		return e
	}
	return seedEfforts(h)
}
