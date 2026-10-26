// Package circles implements the shared context protocol over permission-scoped chats.
package circles

import (
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	parent     *Service
	Hub        *hub.Service
	People     *people.Service
	mu         sync.Mutex
	mapMu      sync.Mutex
	mapKey     string
	mapValue   UnderstandingMap
	mapHistory map[string]UnderstandingMap
}

// guardError exposes a fixed, actionable message without reflecting stored data.
type guardError struct {
	message string
	cause   error
}

func (e guardError) Error() string { return e.message }
func (e guardError) Unwrap() error { return e.cause }

func (s *Service) raw(id string) (model.Circle, error) {
	var c model.Circle
	e := s.Hub.Repo.Get("circles", id, &c)
	return c, e
}
func JIDs(c model.Circle) []string {
	out := []string{}
	for _, x := range c.Chats {
		out = append(out, x.ChatJID)
	}
	sort.Strings(out)
	return out
}
func (s *Service) personIDs(a *model.Agent) (map[string]bool, error) {
	if a == nil {
		return s.People.OwnerCircleIDs()
	}
	xs, e := s.People.PeopleAll(a, "", true)
	out := map[string]bool{}
	for _, p := range xs {
		out[p.ID] = true
	}
	return out, e
}
func (s *Service) project(a *model.Agent, c model.Circle) (model.Circle, error) {
	ps, e := s.personIDs(a)
	if e != nil {
		return c, e
	}
	return s.projectWith(a, c, s.Hub.ChatAccess(a), ps)
}

// List shares the scope and people snapshots across all circles in one request.
func (s *Service) projectWith(a *model.Agent, c model.Circle, allowed func(string) bool, ps map[string]bool) (model.Circle, error) {
	if a == nil {
		people := []model.CirclePerson{}
		for _, p := range c.People {
			if ps[p.PersonID] {
				people = append(people, p)
			}
		}
		c.People = people
		return c, nil
	}
	if !s.Hub.CircleAccess(a, c) {
		return model.Circle{}, model.ErrNotFound
	}
	chats := []model.CircleChat{}
	people := []model.CirclePerson{}
	topics := []model.Topic{}
	for _, x := range c.Chats {
		if allowed(x.ChatJID) {
			chats = append(chats, x)
		}
	}
	if len(chats) == 0 && len(c.Chats) > 0 {
		return model.Circle{}, model.ErrNotFound
	}
	full := len(chats) == len(c.Chats)
	for _, x := range c.People {
		if ps[x.PersonID] {
			if !full {
				x.Note = ""
			}
			people = append(people, x)
		}
	}
	for _, t := range c.Topics {
		ok := true
		for _, j := range t.Chats {
			if !allowed(j) {
				ok = false
			}
		}
		for _, id := range t.People {
			if !ps[id] {
				ok = false
			}
		}
		if ok && (len(t.Chats) > 0 || full) {
			topics = append(topics, t)
		}
	}
	c.Chats = chats
	c.People = people
	c.Topics = topics
	if !full {
		c.Description = ""
	}
	return c, nil
}
func (s *Service) Circle(a *model.Agent, id string) (model.Circle, error) {
	c, e := s.raw(id)
	if e != nil {
		return c, e
	}
	return s.project(a, c)
}
func (s *Service) List(a *model.Agent) ([]model.Circle, error) {
	xs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	out := []model.Circle{}
	if len(xs) == 0 {
		return out, nil
	}
	ps, e := s.personIDs(a)
	if e != nil {
		return nil, e
	}
	allowed := s.Hub.ChatAccess(a)
	for _, c := range xs {
		if !c.Confirmed && (a == nil || c.CreatedBy != a.ID) {
			continue
		}
		c, e = s.projectWith(a, c, allowed, ps)
		if e == model.ErrNotFound {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, nil
}
func (s *Service) validate(a *model.Agent, c *model.Circle) error {
	if a != nil && (len(c.Chats) > 50 || len(c.People) > 50) {
		return model.ErrInvalid
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Goal) > 600 || len(c.Name) > 200 || !model.Contains([]string{"self", "family", "friends", "university", "work", "project", "community", "other"}, c.Kind) || !model.Contains([]string{"free", "confirm", "ask_in_limmr", "never"}, c.SendRule) || len(c.Description) > 10000 || len(c.Symbol) > 100 {
		return model.ErrInvalid
	}
	if c.Symbol == "" {
		c.Symbol = "circle.grid.2x2"
	}
	for _, r := range c.Symbol {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return model.ErrInvalid
		}
	}
	cs, _, e := s.Hub.Visible(a)
	if e != nil {
		return e
	}
	chats := map[string]bool{}
	for _, x := range cs {
		chats[x.JID] = true
	}
	ps, e := s.personIDs(a)
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for i, x := range c.Chats {
		if !chats[x.ChatJID] {
			return model.ErrNotFound
		}
		if seen[x.ChatJID] {
			return model.ErrInvalid
		}
		seen[x.ChatJID] = true
		if x.IncludedSince.IsZero() {
			c.Chats[i].IncludedSince = s.Hub.Now().UTC()
		}
	}
	seen = map[string]bool{}
	for i, x := range c.People {
		if x.Source == "" {
			c.People[i].Source = c.CreatedBy
		}
		if !ps[x.PersonID] {
			return model.ErrNotFound
		}
		if seen[x.PersonID] {
			return model.ErrInvalid
		}
		seen[x.PersonID] = true
	}
	for i := range c.Topics {
		t := &c.Topics[i]
		if t.ID == "" {
			t.ID = dbutil.ID()
		}
		if t.Status == "" {
			t.Status = "active"
		}
		if t.LastActivityAt.IsZero() {
			t.LastActivityAt = s.Hub.Now().UTC()
		}
		if t.Chats == nil {
			t.Chats = []string{}
		}
		if t.People == nil {
			t.People = []string{}
		}
		if t.Keywords == nil {
			t.Keywords = []string{}
		}
		if strings.TrimSpace(t.Title) == "" || !model.Contains([]string{"active", "done", "watching"}, t.Status) {
			return model.ErrInvalid
		}
		for _, j := range t.Chats {
			if !model.Contains(JIDs(*c), j) {
				return model.ErrNotFound
			}
		}
		for _, id := range t.People {
			found := false
			for _, p := range c.People {
				if p.PersonID == id {
					found = true
				}
			}
			if !found {
				return model.ErrNotFound
			}
		}
	}
	if c.Chats == nil {
		c.Chats = []model.CircleChat{}
	}
	if c.People == nil {
		c.People = []model.CirclePerson{}
	}
	if c.Topics == nil {
		c.Topics = []model.Topic{}
	}
	return nil
}
func (s *Service) Create(a *model.Agent, c model.Circle, why string) (model.Circle, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if len(why) > 2000 {
		return c, model.ErrInvalid
	}
	c.ID = dbutil.ID()
	c.Confirmed = false
	c.CreatedBy = "you"
	if a != nil {
		c.CreatedBy = a.ID
	}
	if c.SendRule == "" {
		c.SendRule = model.DefaultSendRule(c.Kind)
	}
	c.UpdatedAt = s.Hub.Now().UTC()
	if e := s.validate(a, &c); e != nil {
		return c, e
	}
	if a != nil {
		current := false
		chats, err := s.Hub.Messages.Chats()
		if err != nil {
			return c, err
		}
		for _, j := range c.Chats {
			for _, chat := range chats {
				if chat.JID == j.ChatJID {
					current = current || s.Hub.Current(chat)
				}
			}
		}
		if !current {
			return c, fmt.Errorf("Choose chats active in the current window: %w", model.ErrInvalid)
		}
		ps, err := s.Proposals()
		if err != nil {
			return c, err
		}
		n := 0
		for _, p := range ps {
			if p.Circle.CreatedBy == a.ID {
				n++
			}
		}
		if n >= 8 {
			return c, guardError{"Ask the owner to review the suggested circles first.", model.ErrConflict}
		}
	}
	c.Origin = s.origin(a)
	c.ConfirmOrigin = nil
	for i := range c.Chats {
		c.Chats[i].Origin = s.origin(a)
	}
	for i := range c.People {
		c.People[i].Origin = s.origin(a)
		c.People[i].Source = c.CreatedBy
	}
	for i := range c.Topics {
		c.Topics[i].Origin = s.origin(a)
	}
	batch, e := s.proposalBatch(c.CreatedBy, c.UpdatedAt)
	if e != nil {
		return c, e
	}
	p := model.Proposal{ID: c.ID, Circle: c, Why: why, Source: c.CreatedBy, CreatedBy: c.CreatedBy, BatchID: batch.ID, CreatedAt: c.UpdatedAt}
	if a != nil {
		reason, err := s.creationRules(*a, c, why)
		if err != nil {
			return c, err
		}
		c.ReviewReason = reason
		p.Circle = c
		if reason == "" && a.Preset == "own" && s.Hub.Settings().AutoCirclesEnabled() {
			c.Confirmed = true
			c.ConfirmedBy = "auto"
			c.ConfirmOrigin = &model.Origin{By: "auto", ByKind: "agent", At: c.UpdatedAt}
			return c, s.saveAuto(*a, nil, c, "added")
		}
	}
	return c, s.Hub.Repo.Apply(hub.Mutation{Bucket: "proposal_batches", ID: batch.ID, Value: batch}, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c}, hub.Mutation{Bucket: "proposals", ID: c.ID, Value: p})
}

// Mutate serializes read-modify-write updates, keeping proposal previews exact.
func (s *Service) Mutate(id string, f func(*model.Circle) error) (model.Circle, error) {
	return s.MutateAs(nil, id, f)
}

func (s *Service) MutateAs(a *model.Agent, id string, f func(*model.Circle) error) (model.Circle, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	c, e := s.raw(id)
	if e != nil {
		return c, e
	}
	// Deep copy: callbacks can mutate slice elements in place.
	rawBefore, _ := json.Marshal(c)
	var before model.Circle
	_ = json.Unmarshal(rawBefore, &before)
	if e = f(&c); e != nil {
		return c, e
	}
	c.UpdatedAt = s.Hub.Now().UTC()
	if e = s.validate(nil, &c); e != nil {
		return c, e
	}
	ms := s.stampChanges(a, &before, &c)
	ms = append(ms, hub.Mutation{Bucket: "circles", ID: id, Value: c})
	if c.Confirmed {
		ms = append(ms, hub.Mutation{Bucket: "proposals", ID: id, Delete: true})
	} else {
		var p model.Proposal
		if e = s.Hub.Repo.Get("proposals", id, &p); e != nil {
			return c, e
		}
		p.Circle = c
		ms = append(ms, hub.Mutation{Bucket: "proposals", ID: id, Value: p})
	}
	return c, s.Hub.Repo.Apply(ms...)
}
func (s *Service) Delete(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if _, e := s.raw(id); e != nil {
		return e
	}
	return s.Hub.Repo.Apply(hub.Mutation{Bucket: "circles", ID: id, Delete: true}, hub.Mutation{Bucket: "proposals", ID: id, Delete: true}, hub.Mutation{Bucket: "dismissed_proposals", ID: id, Value: true})
}
func (s *Service) Confirm(id string) (model.Circle, error) {
	return s.Mutate(id, func(c *model.Circle) error { c.Confirmed = true; return nil })
}
func (s *Service) Proposals() ([]model.Proposal, error) {
	xs, e := hub.List[model.Proposal](s.Hub.Repo, "proposals")
	for i := range xs {
		if xs[i].CreatedBy == "" {
			xs[i].CreatedBy = xs[i].Source
		}
		if xs[i].BatchID == "" {
			xs[i].BatchID = "legacy-" + xs[i].ID
		}
		if xs[i].CreatedAt.IsZero() {
			xs[i].CreatedAt = xs[i].Circle.UpdatedAt
		}
	}
	return xs, e
}
func (s *Service) Context(a *model.Agent, id string) (model.Circle, error) {
	// Virtual context for MCP read(mode=since) across the current visible scope.
	if id == "_visible" && a != nil {
		chats, _, err := s.Hub.Visible(a)
		c := model.Circle{ID: id, Chats: []model.CircleChat{}}
		for _, chat := range chats {
			c.Chats = append(c.Chats, model.CircleChat{ChatJID: chat.JID})
		}
		return c, err
	}
	c, e := s.Circle(a, id)
	if e == nil {
		return c, nil
	}
	if e != model.ErrNotFound {
		return c, e
	}
	xs, e := s.List(a)
	if e != nil {
		return c, e
	}
	for _, v := range xs {
		for _, t := range v.Topics {
			if t.ID == id {
				if len(t.Chats) > 0 {
					chats := []model.CircleChat{}
					for _, cc := range v.Chats {
						if model.Contains(t.Chats, cc.ChatJID) {
							chats = append(chats, cc)
						}
					}
					v.Chats = chats
				}

				return v, nil
			}
		}
	}
	return c, model.ErrNotFound
}
func (s *Service) Briefs(a *model.Agent, id string) ([]model.Brief, error) {
	c, e := s.Context(a, id)
	if e != nil {
		return nil, e
	}
	xs, e := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if e != nil {
		return nil, e
	}
	out := []model.Brief{}
	for _, b := range xs {
		if b.CircleID != c.ID {
			continue
		}
		ok := true
		for _, j := range b.ChatJIDs {
			if a != nil && !model.Contains(JIDs(c), j) {
				ok = false
			}
		}
		if a != nil {
			visiblePeople, e := s.personIDs(a)
			if e != nil {
				return nil, e
			}
			for _, note := range b.PeopleNotes {
				if !visiblePeople[note.PersonID] {
					ok = false
				}
			}
		}
		if a != nil && len(b.ChatJIDs) == 0 && len(c.Chats) > 0 {
			ok = false
		}
		if ok {
			if e = s.presentAsks(c, &b); e != nil {
				return nil, e
			}
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}
func (s *Service) Latest(a *model.Agent, id string) (*model.Brief, error) {
	xs, e := s.Briefs(a, id)
	if e != nil {
		return nil, e
	}
	c, e := s.Context(a, id)
	if e != nil {
		return nil, e
	}
	// A newer brief for a subset of chats must not advance full-circle refresh
	// coverage. Topic briefs and partial-agent briefs remain in version history.
	for _, b := range xs {
		covers := true
		for _, j := range JIDs(c) {
			if !model.Contains(b.ChatJIDs, j) {
				covers = false
			}
		}
		if covers {
			return &b, nil
		}
	}
	return nil, nil
}
func (s *Service) cursorKey(a model.Agent, id string, c model.Circle) string {
	return a.ID + ":" + id + ":" + dbutil.Digest(strings.Join(JIDs(c), "\n"))
}
func (s *Service) Cursor(a model.Agent, id string, c model.Circle) time.Time {
	var at time.Time
	_ = s.Hub.Repo.Get("cursors", s.cursorKey(a, id, c), &at)
	return at
}
func (s *Service) GetBrief(a model.Agent, id string) (any, error) {
	c, e := s.Context(&a, id)
	if e != nil {
		return nil, e
	}
	b, e := s.Latest(&a, id)
	return map[string]any{"circle": c, "brief": b, "cursor": s.Cursor(a, id, c), "chats": c.Chats, "people": c.People}, e
}
func (s *Service) WriteBrief(a *model.Agent, id string, b model.Brief) (model.Brief, error) {
	return s.writeBrief(a, id, b, "you")
}
func (s *Service) writeBrief(a *model.Agent, id string, b model.Brief, author string) (model.Brief, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	c, e := s.Context(a, id)
	if e != nil {
		return b, e
	}
	if len(strings.Fields(b.Summary)) > 120 || strings.TrimSpace(b.Summary) == "" || len(b.WhatChanged) > 65536 {
		return b, model.ErrInvalid
	}
	b.CircleID = c.ID
	b.ChatJIDs = JIDs(c)
	b.WrittenBy = author
	if a != nil {
		b.WrittenBy = a.ID
		cursor := s.Cursor(*a, id, c)
		if b.CoversUntil.IsZero() {
			b.CoversUntil = cursor
		}
		if b.CoversUntil.After(cursor) {
			return b, model.ErrInvalid
		}
	}
	if b.CoversUntil.After(s.Hub.Now()) {
		return b, model.ErrInvalid
	}
	for _, item := range b.Upcoming {
		if _, e = time.Parse("2006-01-02", item.Date); e != nil {
			return b, model.ErrInvalid
		}
	}
	for _, n := range b.PeopleNotes {
		found := false
		for _, p := range c.People {
			if p.PersonID == n.PersonID {
				found = true
			}
		}
		if !found {
			return b, model.ErrNotFound
		}
	}
	for _, id := range b.Evidence {
		var count int
		for _, j := range b.ChatJIDs {
			var at time.Time
			e = s.Hub.Messages.DB.QueryRow("SELECT timestamp FROM messages WHERE id=? AND chat_jid=?", id, j).Scan(&at)
			if e == nil {
				count++
				if at.After(b.CoversUntil) {
					return b, model.ErrInvalid
				}
			}
		}
		if count != 1 {
			return b, model.ErrNotFound
		}
	}
	xs, e := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if e != nil {
		return b, e
	}
	b.Version = 1
	for _, x := range xs {
		if x.CircleID == c.ID && x.Version >= b.Version {
			b.Version = x.Version + 1
		}
	}
	if b.Version == 1 && len(b.Evidence) == 0 {
		return b, guardError{"Catch up first; there's nothing to summarise yet.", model.ErrInvalid}
	}
	b.WrittenAt = s.Hub.Now().UTC()
	if e = s.normalizeAsks(c, &b); e != nil {
		return b, e
	}
	if b.OpenAsks == nil {
		b.OpenAsks = []model.OpenAsk{}
	}
	if b.Upcoming == nil {
		b.Upcoming = []model.Upcoming{}
	}
	if b.PeopleNotes == nil {
		b.PeopleNotes = []model.PeopleNote{}
	}
	if b.Evidence == nil {
		b.Evidence = []string{}
	}
	return b, s.Hub.Repo.Put("briefs", fmt.Sprintf("%s:%09d", c.ID, b.Version), b)
}
func (s *Service) Revert(id string, version int) (model.Brief, error) {
	var b model.Brief
	if e := s.Hub.Repo.Get("briefs", fmt.Sprintf("%s:%09d", id, version), &b); e != nil {
		return b, e
	}
	return s.WriteBrief(nil, id, b)
}
func (s *Service) Suggest(a model.Agent, x model.Suggestion) (model.Suggestion, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	c, e := s.Circle(&a, x.CircleID)
	if e != nil {
		return x, e
	}
	if !c.Confirmed {
		return x, model.ErrConflict
	}
	if x.ChatJID != "" && !model.Contains(JIDs(c), x.ChatJID) {
		return x, model.ErrNotFound
	}
	if !model.Contains([]string{"reply", "action", "reminder", "archive"}, x.Kind) || strings.TrimSpace(x.Why) == "" || len(x.DraftText) > 65536 || x.Kind == "reply" && (x.ChatJID == "" || x.DraftText == "") {
		return x, model.ErrInvalid
	}
	if x.Kind == "archive" {
		n, err := s.newCount(JIDs(c), s.Hub.Now().AddDate(0, 0, -60))
		if err != nil {
			return x, err
		}
		if n > 0 || c.Kind == "self" {
			return x, model.ErrConflict
		}
	}
	if e = hub.ValidateVariations(x.Variations); e != nil {
		return x, e
	}
	x.ID = dbutil.ID()
	x.CreatedBy = a.ID
	x.CreatedAt = s.Hub.Now().UTC()
	x.ExpiresAt = x.CreatedAt.Add(24 * time.Hour)
	x.State = "pending"
	if e = s.Hub.Repo.Put("suggestions", x.ID, x); e != nil {
		return x, e
	}
	return s.Hub.PresentSuggestion(x)
}

// Patch decodes only present fields while preserving absent ones.
func Patch(v any, data map[string]json.RawMessage, allowed ...string) error {
	for k, v := range data {
		if string(v) == "null" {
			return model.ErrInvalid
		}
		if !model.Contains(allowed, k) {
			return model.ErrInvalid
		}
	}
	b, _ := json.Marshal(data)
	if json.Unmarshal(b, v) != nil {
		return model.ErrInvalid
	}
	return nil
}
