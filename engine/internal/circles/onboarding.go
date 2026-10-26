package circles

import (
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strconv"
	"time"
)

type proposalBatch struct {
	ID, AgentID string
	StartedAt   time.Time
}

func (s *Service) proposalBatch(agent string, at time.Time) (proposalBatch, error) {
	xs, e := hub.List[proposalBatch](s.Hub.Repo, "proposal_batches")
	if e != nil {
		return proposalBatch{}, e
	}
	for _, x := range xs {
		if x.AgentID == agent && !at.Before(x.StartedAt) && at.Before(x.StartedAt.Add(10*time.Minute)) {
			return x, nil
		}
	}
	return proposalBatch{dbutil.ID(), agent, at}, nil
}

// ReviewProposals validates the entire batch before committing any owner decision.
func (s *Service) ReviewProposals(ids []string, accept bool) ([]model.Circle, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	out := []model.Circle{}
	changes := []hub.Mutation{}
	seen := map[string]bool{}
	if len(ids) == 0 || len(ids) > 100 {
		return nil, model.ErrInvalid
	}
	for _, id := range ids {
		if seen[id] {
			return nil, model.ErrInvalid
		}
		seen[id] = true
		var p model.Proposal
		if e := s.Hub.Repo.Get("proposals", id, &p); e != nil {
			return nil, e
		}
		c, e := s.raw(id)
		if e != nil {
			return nil, e
		}
		if c.Confirmed {
			return nil, model.ErrConflict
		}
		if accept {
			before := c
			c.Confirmed = true
			c.UpdatedAt = s.Hub.Now().UTC()
			if e = s.validate(nil, &c); e != nil {
				return nil, e
			}
			changes = append(changes, s.stampChanges(nil, &before, &c)...)
			changes = append(changes, hub.Mutation{Bucket: "circles", ID: id, Value: c})
			out = append(out, c)
		} else {
			changes = append(changes, hub.Mutation{Bucket: "circles", ID: id, Delete: true}, hub.Mutation{Bucket: "dismissed_proposals", ID: id, Value: true})
		}
		changes = append(changes, hub.Mutation{Bucket: "proposals", ID: id, Delete: true})
	}
	return out, s.Hub.Repo.Apply(changes...)
}

// SortedChats returns membership in confirmed circles only (including excluded ones).
func (s *Service) SortedChats() (map[string]bool, error) {
	xs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	out := map[string]bool{}
	for _, c := range xs {
		if c.Confirmed {
			for _, j := range c.Chats {
				out[j.ChatJID] = true
			}
		}
	}
	if e != nil {
		return nil, e
	}
	aliases, e := s.Hub.Messages.Aliases()
	if e != nil {
		return nil, e
	}
	for alias, canonical := range aliases {
		if out[alias] || out[canonical] {
			out[alias] = true
			out[canonical] = true
		}
	}
	return out, nil
}

type SetupStep struct {
	Do    string   `json:"do"`
	Why   string   `json:"why"`
	How   string   `json:"how"`
	Tools []string `json:"tools"`
}
type SetupStatus struct {
	OwnerProfile model.OwnerProfile `json:"owner_profile"`
	CurrentDays  int                `json:"current_days"`
	Hints        SetupHints         `json:"hints"`
	Stage        string             `json:"stage"`
	Owner        string             `json:"owner"`
	Linked       bool               `json:"linked"`
	Chats        int                `json:"chats"`
	Circles      struct {
		Confirmed int `json:"confirmed"`
		Proposed  int `json:"proposed"`
	} `json:"circles"`
	UnsortedActiveChats int `json:"unsorted_active_chats"`
	People              struct {
		Total          int `json:"total"`
		WithoutDetails int `json:"without_details"`
	} `json:"people"`
	CirclesWithoutBrief []string    `json:"circles_without_brief"`
	NextSteps           []SetupStep `json:"next_steps"`
	SayToOwner          string      `json:"say_to_owner"`
}

func withoutDetails(p model.Person) bool {
	d := p.Details
	phones := 0
	for _, v := range d.Phones {
		if v.Source != "whatsapp" {
			phones++
		}
	}
	return p.HowKnown == "" && p.Notes == "" && p.Role == "" && phones == 0 && len(d.Emails) == 0 && len(d.Handles) == 0 && d.Birthday == "" && d.Location == "" && d.Pronouns == ""
}
func (s *Service) Setup(a *model.Agent, linked bool) (SetupStatus, error) {
	v := SetupStatus{Stage: "ready", Owner: s.Hub.Settings().Owner, Linked: linked, CirclesWithoutBrief: []string{}, NextSteps: []SetupStep{}, SayToOwner: "Limmr is ready. Ask me to catch up on a circle whenever you need."}
	chats, _, e := s.Hub.Visible(a)
	if e != nil {
		return v, e
	}
	v.CurrentDays = s.Hub.Settings().CurrentDays
	hints, currentPeople, e := s.hints(a, chats)
	if e != nil {
		return v, e
	}
	sorted, e := s.SortedChats()
	if e != nil {
		return v, e
	}
	v.Hints = hints
	if a != nil && (!a.Permissions.ReadMessages || !a.Permissions.ReadPeople) {
		v.Hints = SetupHints{RecentDMs: []DMHint{}, RecentGroups: []GroupHint{}, Communities: []CommunityHint{}}
	}
	active := 0
	for _, c := range chats {
		if s.Hub.Current(c) {
			active++
			if !sorted[c.JID] {
				v.UnsortedActiveChats++
			}
		}
	}
	v.Chats = active
	cs, e := s.List(a)
	if e != nil {
		return v, e
	}
	confirmedPeople := map[string]bool{}
	for _, c := range cs {
		if c.Confirmed && !c.Archived {
			v.Circles.Confirmed++
			for _, p := range c.People {
				confirmedPeople[p.PersonID] = true
			}
			b, e := s.Latest(a, c.ID)
			if e != nil {
				return v, e
			}
			n, err := s.newCount(JIDs(c), s.Hub.CurrentSince())
			if err != nil {
				return v, err
			}
			if b == nil && n > 0 {
				v.CirclesWithoutBrief = append(v.CirclesWithoutBrief, c.ID)
			}
		}
	}
	proposals, e := s.Proposals()
	if e != nil {
		return v, e
	}
	for _, p := range proposals {
		// Every proposal waiting for the owner counts, whichever AI made it, so a second AI doesn't propose the same
		// circles again. Another AI's proposal counts only when all of its chats are visible to this agent.
		if a == nil || p.Circle.CreatedBy == a.ID {
			v.Circles.Proposed++
		} else if allowed := s.Hub.ChatAccess(a); len(p.Circle.Chats) > 0 {
			all := true
			for _, x := range p.Circle.Chats {
				all = all && allowed(x.ChatJID)
			}
			if all {
				v.Circles.Proposed++
			}
		}
	}
	ps, e := s.People.People(a, "")
	if e != nil {
		return v, e
	}

	members, missing := 0, 0
	for _, p := range ps {
		if p.ID == "me" {
			continue
		}
		if !currentPeople[p.ID] {
			continue
		}
		v.People.Total++
		if withoutDetails(p) {
			v.People.WithoutDetails++
		}
		if confirmedPeople[p.ID] {
			members++
			if withoutDetails(p) {
				missing++
			}
		}
	}
	sort.Strings(v.CirclesWithoutBrief)
	switch {
	case v.Circles.Confirmed == 0 && v.Circles.Proposed == 0:
		v.Stage = "new"
		v.SayToOwner = "Connect your AI and ask it to set up Limmr. It will suggest circles for you to keep."
	case v.Circles.Proposed == 0 && v.Circles.Confirmed >= 3:
		// Remaining sorting/detail hints are AI work, not an owner setup obligation.
	case v.Circles.Proposed > 0 || v.UnsortedActiveChats*4 > active:
		v.Stage = "sorting"
		v.SayToOwner = "Review the suggested circles in Limmr, then I can help fill them in."
		if v.Circles.Proposed > 0 {
			v.SayToOwner = "There are " + strconv.Itoa(v.Circles.Proposed) + " suggested circles. Keep them in Limmr and I can help write their briefs."
		}
	case len(v.CirclesWithoutBrief) > 0 || missing*2 > members:
		v.Stage = "filling"
		v.SayToOwner = "Your circles are kept. I can help fill in people and write their first briefs."
	}
	v.OwnerProfile = s.Hub.VisibleOwnerProfile(a)
	p := model.Preset("own")
	if a != nil {
		p = a.Permissions
	}
	if p.WritePeople && p.ReadPeople && v.OwnerProfile.Thin() {
		v.NextSteps = append(v.NextSteps, SetupStep{"about_you", "About you helps choose useful circles", `Before sorting chats, learn who the owner is now. Explain in one sentence what you are doing. If you already know them (your memory or this conversation), draft the profile from that and show it to them; otherwise ask at most three short questions: where they study or work now, who's in their family, who they're closest to. Save with update_owner_profile for their confirmation, then continue.`, []string{"get_owner_profile", "update_owner_profile"}})
		v.SayToOwner = "Let's start with About you so your circles fit your life now."
	}
	if p.WritePeople && p.ReadMessages && p.ReadPeople {
		if v.Circles.Proposed == 0 && v.UnsortedActiveChats > 0 {
			v.NextSteps = append(v.NextSteps, SetupStep{"propose_circles", strconv.Itoa(v.UnsortedActiveChats) + " active chats are in no circle", `Call scan_chats first; read only chats that matter. Inspect list_circles first. Make a few circles (usually 3–6 total, not additional) about life now: current university/school, family, close people (partner/best friends), current work/projects. Use recent_dms, recent_groups and communities hints, then list_chats(sort:"active", unsorted:true, include_old:false, limit:40). A circle holds many chats; never one per group or WhatsApp Community. Skip chats outside current_days. Use owner-known context or recent messages. Former schools are not current; if unsure ask one short question: Is <name> your university now? Reuse kept circles; propose only missing life areas, and leave irrelevant chats unsorted. Avoid duplicates. Use owner_profile.studies for the current university, people with family relations for Family, the rest for Close people, and projects for active project circles. Link people by person_id after finding them in chats. Call propose_circle; own AIs auto-add only when the circle protocol passes, otherwise review in Limmr.`, []string{"list_circles", "list_chats", "get_chat", "propose_circle"}})
		}
		if missing > 0 {
			v.NextSteps = append(v.NextSteps, SetupStep{"fill_people", "People in kept circles need details", "Read recent chats for relationships the owner has mentioned (my sister, my girlfriend, my HOD), or use owner-known facts. Call add_relation with evidence_message_ids, update_person with how_known, and set_person_role in kept circles. Only evidence or owner-known facts; ask when unsure. Details are owner-reviewed suggestions.", []string{"find_person", "list_messages", "add_relation", "update_person", "set_person_role"}})
		}
	}
	if p.SaveSummaries && p.ReadMessages && p.ReadPeople && len(v.CirclesWithoutBrief) > 0 {
		v.NextSteps = append(v.NextSteps, SetupStep{"write_briefs", "Kept circles need their first briefs", "For circles with messages in current_days: get_brief, catch_up within the current window, answer, then update_brief with returned evidence_message_ids. If catch_up has no messages, skip writing a brief.", []string{"get_brief", "catch_up", "update_brief"}})
	}
	if a != nil {
		v.NextSteps = append(v.NextSteps, SetupStep{"get_updates", "Catch up on updates", "At the start of a conversation, call get_updates to catch up; keep its cursor for the next call. A standing feed needs an explicit create_watch grant. ChatGPT runs only when a conversation or scheduled task calls it.", []string{"get_updates"}})
	}
	return v, nil
}
func (s *Service) WriteAdminBrief(id string, b model.Brief) (model.Brief, error) {
	author := b.WrittenBy
	if author == "" {
		author = "you"
	}
	if author != "you" && author != "this-mac" {
		return b, model.ErrInvalid
	}
	if author == "this-mac" {
		a, e := s.Hub.Agent(author)
		if e != nil || !a.Enabled || a.Kind != "on_device" {
			return b, model.ErrForbidden
		}
	}
	b, e := s.writeBrief(nil, id, b, author)
	if e != nil {
		return b, e
	}
	e = s.Hub.Record(model.Activity{AgentID: author, Tool: "update_brief", Kind: "summary", ChatJIDs: b.ChatJIDs, MessageIDs: b.Evidence, Note: b.Summary + "\n" + b.WhatChanged, Count: 1})
	return b, e
}
