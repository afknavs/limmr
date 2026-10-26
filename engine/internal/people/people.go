// Package people maintains the shared map and projects it through chat access.
package people

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	parent            *Service
	Hub               *hub.Service
	mu                sync.Mutex
	cacheMu           sync.Mutex
	cache             map[string]peopleCache
	graphCache        map[string]graphCache
	statsCache        map[string]map[string]Stats
	ownerIDsKey       string
	ownerIDs          map[string]bool
	directoryRevision int64
	directoryLoaded   bool
	directoryPeople   []model.Person
}
type Stats struct {
	SharedGroups  []string   `json:"shared_groups"`
	DM30          int        `json:"dm_messages_30"`
	DM365         int        `json:"dm_messages_365"`
	LastDM        *time.Time `json:"last_dm"`
	GroupMessages int        `json:"messages_in_shared_groups"`
}
type Profile struct {
	Person    model.Person     `json:"person"`
	Groups    []model.Group    `json:"groups"`
	Facts     []model.Fact     `json:"facts"`
	Relations []model.Relation `json:"relations"`
	Stats     Stats            `json:"stats"`
}
type Change struct {
	ID        string        `json:"id"`
	At        time.Time     `json:"at"`
	Source    string        `json:"source"`
	Kind      string        `json:"kind"`
	SubjectID string        `json:"subject_id"`
	Before    *model.Person `json:"before,omitempty"`
	After     *model.Person `json:"after,omitempty"`
	Reverted  bool          `json:"reverted"`
}

func (s *Service) PutPerson(p model.Person) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if !model.Named(p) && p.Label == "" && model.Named(model.Person{Name: p.PushName}) {
		p.Name = p.PushName
	}
	if p.ID == "" {
		return model.ErrInvalid
	}
	if owner, e := s.Hub.Owner(); e == nil {
		for _, j := range p.JIDs {
			if model.Contains(owner.JIDs, j) {
				p.ID = "me"
			}
		}
	}
	var old model.Person
	err := s.Hub.Repo.Get("people", p.ID, &old)
	if err != nil && err != model.ErrNotFound {
		return err
	}
	if err == nil {
		for _, j := range old.JIDs {
			if !model.Contains(p.JIDs, j) {
				p.JIDs = append(p.JIDs, j)
			}
		}
		fallback := false
		for _, j := range p.JIDs {
			if p.Name == strings.SplitN(j, "@", 2)[0] {
				fallback = true
			}
		}
		if p.Name == "" || fallback {
			p.Name = old.Name
		}
		if p.PushName == "" {
			p.PushName = old.PushName
		}
		p.Business = p.Business || old.Business
		p.Broadcast = p.Broadcast || old.Broadcast
		p.OtherNames = append([]string{}, old.OtherNames...)
		for _, name := range []string{old.Name, old.PushName} {
			if name != "" && name != p.Name && name != p.PushName && !model.Contains(p.OtherNames, name) {
				p.OtherNames = append(p.OtherNames, name)
			}
		}
		p.Label = old.Label
		p.Circle = old.Circle
		p.Role = old.Role
		p.HowKnown = old.HowKnown
		p.Notes = old.Notes
		p.Photo = old.Photo
		p.ContactsPhoto = old.ContactsPhoto
		p.Details = old.Details
		p.DetailSuggestions = old.DetailSuggestions
	}
	if err == nil {
		compare := p
		compare.UpdatedAt = old.UpdatedAt
		if reflect.DeepEqual(compare, old) {
			return nil
		}
	}
	return s.Hub.Repo.Put("people", p.ID, p)
}
func (s *Service) PutGroup(g model.Group) error { return s.Hub.Repo.Put("groups", g.JID, g) }
func (s *Service) PutMembership(m model.Membership) error {
	return s.Hub.Repo.Put("memberships", m.Group+":"+m.Person, m)
}
func (s *Service) memberships() ([]model.Membership, error) {
	return hub.List[model.Membership](s.Hub.Repo, "memberships")
}
func visiblePerson(p model.Person, ms []model.Membership, allowed, hidden func(string) bool) bool {
	for _, j := range p.JIDs {
		if hidden(j) {
			return false
		}
	}
	for _, j := range p.JIDs {
		if allowed(j) {
			return true
		}
	}
	for _, m := range ms {
		if m.Person == p.ID && allowed(m.Group) {
			return true
		}
	}
	return false
}
func (s *Service) People(a *model.Agent, query string) ([]model.Person, error) {
	return s.PeopleAll(a, query, query != "")
}
func (s *Service) PeopleAll(a *model.Agent, query string, all bool) ([]model.Person, error) {
	key := s.cacheKey(a, "people", query, all)
	s.shared().cacheMu.Lock()
	cached, ok := s.shared().cache[key]
	s.shared().cacheMu.Unlock()
	if ok {
		return clonePeople(cached.people), nil
	}
	ps, e := s.directory()
	if e != nil {
		return nil, e
	}
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	ms, e := s.memberships()
	if e != nil {
		return nil, e
	}
	excluded := s.excludedSenders(a, ps, cs)
	relevant, e := s.relevant(a, ps, cs, ms)
	if e != nil {
		return nil, e
	}
	allowed := s.Hub.ChatAccess(a)
	hidden := s.Hub.HiddenChatAccess()
	if a == nil {
		hidden = func(string) bool { return false }
	}
	circlePeople, e := s.Hub.CirclePeople(a)
	if e != nil {
		return nil, e
	}
	// Participation means an actual visible chat, not a hypothetical unsorted DM.
	participants := map[string]bool{}
	if a != nil {
		chats, e := s.Hub.Messages.Chats()
		if e != nil {
			return nil, e
		}
		actual := map[string]bool{}
		for _, c := range chats {
			if allowed(c.JID) {
				actual[c.JID] = true
			}
		}
		aliases, e := s.Hub.Messages.Aliases()
		if e != nil {
			return nil, e
		}
		for alias, canonical := range aliases {
			if actual[canonical] {
				actual[alias] = true
			}
		}
		allowed = func(j string) bool { return actual[j] }
		rows, e := s.Hub.Messages.DB.Query("SELECT DISTINCT chat_jid,COALESCE(sender,'') FROM messages")
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var chat, sender string
			if e = rows.Scan(&chat, &sender); e != nil {
				rows.Close()
				return nil, e
			}
			if allowed(chat) {
				participants[sender] = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	out := []model.Person{}
	for _, p := range ps {
		if p.Label == "" && !model.Named(model.Person{Name: p.Name}) && model.Named(model.Person{Name: p.PushName}) {
			p.Name = p.PushName
		}
		if excluded[p.ID] || !model.Named(p) || (!all && !relevant[p.ID]) {
			continue
		}
		blocked := false
		for _, j := range p.JIDs {
			if hidden(j) {
				blocked = true
			}
		}
		participated := false
		for _, j := range p.JIDs {
			if participants[j] || participants[strings.SplitN(j, "@", 2)[0]] {
				participated = true
			}
		}
		if blocked || (a != nil && a.Permissions.Access.Ended(s.Hub.Now())) || (a != nil && !circlePeople[p.ID] && !participated && !visiblePerson(p, ms, allowed, hidden)) {
			continue
		}
		if a != nil {
			if a.Permissions.Access.Circles != nil {
				p.Circle = ""
			}
			originalJIDs := p.JIDs
			js := []string{}
			for _, j := range p.JIDs {
				if allowed(j) {
					js = append(js, j)
				}
			}
			p.JIDs = js
			if len(js) == 0 {
				// Unnamed WhatsApp contacts fall back to their phone user-part.
				// A group-only projection must not reveal that fallback either.
				for _, j := range originalJIDs {
					bare := strings.SplitN(j, "@", 2)[0]
					if p.Name == bare || p.Name == j {
						p.Name = "Unnamed person"
					}
					if p.PushName == bare || p.PushName == j {
						p.PushName = ""
					}
				}
				p.OtherNames = nil
				p.Label = ""
				p.Circle = ""
				p.Role = ""
				p.HowKnown = ""
				p.Notes = ""
				p.Photo = nil
				p.ContactsPhoto = nil
				p.Details = model.PersonDetails{}
				p.DetailSuggestions = nil
			}
		}
		if !model.Named(p) {
			continue
		}
		if strings.Contains(strings.ToLower(p.Name+" "+p.Label+" "+strings.Join(p.JIDs, " ")), strings.ToLower(query)) {
			p = withDerivedPhones(p)
			if p.ID == "me" {
				if name := s.Hub.Settings().Owner; name != "" {
					p.Label = name
				}
			}
			if p.Label != "" {
				p.Name = p.Label
			}
			out = append(out, p)
		}
	}
	chats, _, e := s.Hub.Visible(a)
	if e != nil {
		return nil, e
	}
	lastByJID := map[string]time.Time{}
	for _, c := range chats {
		if c.LastMessageTime != nil && c.LastMessageTime.After(lastByJID[c.JID]) {
			lastByJID[c.JID] = *c.LastMessageTime
		}
	}
	last := map[string]time.Time{}
	for _, p := range out {
		for _, j := range p.JIDs {
			if at := lastByJID[j]; at.After(last[p.ID]) {
				last[p.ID] = at
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == "me" {
			return out[j].ID != "me"
		}
		if out[j].ID == "me" {
			return false
		}
		if relevant[out[i].ID] != relevant[out[j].ID] {
			return relevant[out[i].ID]
		}
		x, y := last[out[i].ID], last[out[j].ID]
		if !x.Equal(y) {
			return x.After(y)
		}
		return out[i].ID < out[j].ID
	})
	if e := s.enrichAffinity(a, out, ms); e != nil {
		return nil, e
	}
	s.shared().cacheMu.Lock()
	if len(s.shared().cache) > 32 {
		s.shared().cache = nil
	}
	if s.shared().cache == nil {
		s.shared().cache = map[string]peopleCache{}
	}
	s.shared().cache[key] = peopleCache{clonePeople(out)}
	s.shared().cacheMu.Unlock()
	return out, nil
}
func (s *Service) Groups(a *model.Agent, query string) ([]model.Group, error) {
	gs, e := hub.List[model.Group](s.Hub.Repo, "groups")
	if e != nil {
		return nil, e
	}
	allowed := s.Hub.ChatAccess(a)
	out := []model.Group{}
	counts := map[string]int{}
	if a != nil {
		ps, e := s.People(a, "")
		if e != nil {
			return nil, e
		}
		ms, e := s.memberships()
		if e != nil {
			return nil, e
		}
		ids := map[string]bool{}
		for _, p := range ps {
			ids[p.ID] = true
		}
		for _, m := range ms {
			if ids[m.Person] {
				counts[m.Group]++
			}
		}
	}
	for _, g := range gs {
		if a != nil {
			g.MemberCount = counts[g.JID]
			if g.ParentJID != "" && !allowed(g.ParentJID) {
				g.ParentJID = ""
			}
		}
		if allowed(g.JID) && strings.Contains(strings.ToLower(g.Name), strings.ToLower(query)) {
			out = append(out, g)
		}
	}
	if e := s.enrichGroups(a, out); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Service) Profile(a *model.Agent, id string) (Profile, error) {
	ps, e := s.PeopleAll(a, "", true)
	if e != nil {
		return Profile{}, e
	}
	var p *model.Person
	for _, v := range ps {
		if v.ID == id {
			copy := v
			p = &copy
			break
		}
	}
	if p == nil {
		return Profile{}, model.ErrNotFound
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return Profile{}, e
	}
	ms, e := s.memberships()
	if e != nil {
		return Profile{}, e
	}
	out := Profile{Person: *p, Groups: []model.Group{}, Facts: []model.Fact{}, Relations: []model.Relation{}}
	for _, g := range gs {
		for _, m := range ms {
			if m.Group == g.JID && m.Person == id {
				out.Groups = append(out.Groups, g)
			}
		}
	}
	facts, e := hub.List[model.Fact](s.Hub.Repo, "facts")
	if e != nil {
		return out, e
	}
	for _, f := range facts {
		if f.Subject == "person" && f.SubjectID == id && (f.Status == "active" || (a == nil && f.Status == "suggested")) && s.factVisible(a, f) {
			out.Facts = append(out.Facts, f)
		}
	}
	rels, e := s.Relations(a)
	if e != nil {
		return out, e
	}
	for _, r := range rels {
		if r.A == id || r.B == id {
			out.Relations = append(out.Relations, r)
		}
	}
	var canonical model.Person
	if e = s.Hub.Repo.Get("people", p.ID, &canonical); e != nil {
		return out, e
	}
	out.Stats, e = s.ComputeStats(a, canonical, out.Groups)
	return out, e
}
func (s *Service) ComputeStats(a *model.Agent, p model.Person, gs []model.Group) (Stats, error) {
	ms, e := s.memberships()
	if e != nil {
		return Stats{}, e
	}
	stats, e := s.allStats(a, []model.Person{p}, gs, ms)
	return stats[p.ID], e
}
func (s *Service) Group(a *model.Agent, id string) (any, error) {
	gs, e := s.Groups(a, "")
	if e != nil {
		return nil, e
	}
	var chosen *model.Group
	for _, g := range gs {
		if g.JID == id {
			copy := g
			chosen = &copy
		}
	}
	if chosen == nil {
		return nil, model.ErrNotFound
	}
	ms, e := s.memberships()
	if e != nil {
		return nil, e
	}
	ps, e := s.PeopleAll(a, "", true)
	if e != nil {
		return nil, e
	}
	members := []map[string]any{}
	for _, m := range ms {
		if m.Group == id {
			for _, p := range ps {
				if p.ID == m.Person {
					members = append(members, map[string]any{"person": p, "admin": m.Admin})
				}
			}
		}
	}
	chosen.MemberCount = len(members)
	fs, e := hub.List[model.Fact](s.Hub.Repo, "facts")
	if e != nil {
		return nil, e
	}
	facts := []model.Fact{}
	for _, f := range fs {
		if f.Subject == "group" && f.SubjectID == id && (f.Status == "active" || (a == nil && f.Status == "suggested")) && s.factVisible(a, f) {
			facts = append(facts, f)
		}
	}
	return map[string]any{"group": chosen, "members": members, "facts": facts}, nil
}
func (s *Service) factVisible(a *model.Agent, f model.Fact) bool {
	for _, j := range f.EvidenceChats {
		if !s.Hub.CanChat(a, j) {
			return false
		}
	}
	return true
}
func (s *Service) AddFact(a *model.Agent, f model.Fact) (model.Fact, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if a != nil && !a.Permissions.WritePeople {
		return f, model.ErrForbidden
	}
	if f.Text == "" || len(f.Text) > 16000 {
		return f, model.ErrInvalid
	}
	if f.Subject == "person" {
		if _, e := s.Profile(a, f.SubjectID); e != nil {
			return f, e
		}
	} else if f.Subject == "group" {
		if _, e := s.Group(a, f.SubjectID); e != nil {
			return f, e
		}
	} else {
		return f, model.ErrInvalid
	}
	var err error
	f.EvidenceChats, err = s.evidenceChats(a, f.EvidenceMessageIDs)
	if err != nil {
		return f, err
	}
	f.ID = dbutil.ID()
	f.Source = "you"
	if a != nil {
		f.Source = a.ID
	}
	f.CreatedAt = s.Hub.Now().UTC()
	if f.Status != "suggested" {
		f.Status = "active"
	}
	return f, s.Hub.Repo.Apply(hub.Mutation{Bucket: "facts", ID: f.ID, Value: f}, hub.Mutation{Bucket: "changes", ID: f.ID, Value: Change{ID: f.ID, At: f.CreatedAt, Source: f.Source, Kind: "fact", SubjectID: f.SubjectID}})
}
func (s *Service) Update(a *model.Agent, id string, fields map[string]string) (model.Person, error) {
	values := map[string]any{}
	for k, v := range fields {
		values[k] = v
	}
	return s.UpdateProfile(a, id, values)
}
func (s *Service) UpdateProfile(a *model.Agent, id string, fields map[string]any) (model.Person, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if a != nil && !a.Permissions.WritePeople {
		return model.Person{}, model.ErrForbidden
	}
	profile, e := s.Profile(a, id)
	if e != nil {
		// An explicit edit may promote a named business from its chat. Keep all normal scope checks.
		var raw model.Person
		if e != model.ErrNotFound || s.Hub.Repo.Get("people", id, &raw) != nil || !model.Named(raw) {
			return model.Person{}, e
		}
		permitted := a == nil
		if a != nil {
			for _, j := range raw.JIDs {
				permitted = permitted || s.Hub.CanChat(a, j)
			}
		}
		if !permitted {
			return model.Person{}, model.ErrNotFound
		}
		profile.Person = raw
	}
	before := profile.Person
	// Never replace canonical JIDs with their projected subset.
	var p model.Person
	if e = s.Hub.Repo.Get("people", id, &p); e != nil {
		return p, e
	}
	before = p
	for k, raw := range fields {
		if k == "details" {
			if e := applyDetails(&p, raw, a); e != nil {
				return p, e
			}
			continue
		}
		if k == "other_names" {
			if a != nil {
				return p, model.ErrForbidden
			}
			b, err := json.Marshal(raw)
			if err != nil {
				return p, model.ErrInvalid
			}
			var names []string
			if json.Unmarshal(b, &names) != nil || len(names) > 30 {
				return p, model.ErrInvalid
			}
			p.OtherNames = []string{}
			for _, name := range names {
				name = strings.TrimSpace(name)
				if name == "" || len(name) > 200 {
					return p, model.ErrInvalid
				}
				if !model.Contains(p.OtherNames, name) {
					p.OtherNames = append(p.OtherNames, name)
				}
			}
			continue
		}
		v, ok := raw.(string)
		if !ok {
			return p, model.ErrInvalid
		}
		if len(v) > 16000 {
			return p, model.ErrInvalid
		}
		switch k {
		case "label":
			p.Label = v
		case "circle":
			p.Circle = v
		case "role":
			if a != nil {
				proposeDetail(&p, k, "", v, a.ID)
			} else {
				p.Role = v
			}
		case "how_known":
			if a != nil {
				proposeDetail(&p, k, "", v, a.ID)
			} else {
				p.HowKnown = v
			}
		case "notes":
			if a != nil {
				field := "notes"
				if pronouns := Pronouns(v); pronouns != "" {
					field, v = "pronouns", pronouns
				}
				proposeDetail(&p, field, "", v, a.ID)
			} else {
				p.Notes = v
			}
		default:
			return p, model.ErrInvalid
		}
	}
	if len(p.DetailSuggestions) > 200 {
		return p, model.ErrInvalid
	}
	p.UpdatedAt = s.Hub.Now().UTC()
	for i := range p.DetailSuggestions {
		if p.DetailSuggestions[i].CreatedAt.IsZero() {
			p.DetailSuggestions[i].CreatedAt = p.UpdatedAt
		}
	}
	source := "you"
	if a != nil {
		source = a.ID
	}
	ch := Change{ID: dbutil.ID(), At: p.UpdatedAt, Source: source, Kind: "person", SubjectID: id, Before: &before, After: &p}
	if e = s.Hub.Repo.Apply(hub.Mutation{Bucket: "people", ID: p.ID, Value: p}, hub.Mutation{Bucket: "changes", ID: ch.ID, Value: ch}); e != nil {
		return model.Person{}, e
	}
	// Writes return the same projection as reads, never the canonical private JIDs.
	updated, e := s.Profile(a, id)
	return updated.Person, e
}
func (s *Service) Relations(a *model.Agent) ([]model.Relation, error) {
	rs, e := hub.List[model.Relation](s.Hub.Repo, "relations")
	if e != nil {
		return nil, e
	}
	ps, e := s.People(a, "")
	if e != nil {
		return nil, e
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return nil, e
	}
	ok := map[string]bool{"me": true}
	for _, p := range ps {
		ok[p.ID] = true
	}
	for _, g := range gs {
		ok[g.JID] = true
	}
	out := []model.Relation{}
	for _, r := range rs {
		if ok[r.A] && ok[r.B] && s.factVisible(a, model.Fact{EvidenceChats: r.EvidenceChats}) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *Service) AddRelation(a *model.Agent, r model.Relation) (model.Relation, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if a != nil && !a.Permissions.WritePeople {
		return r, model.ErrForbidden
	}
	ps, e := s.People(a, "")
	if e != nil {
		return r, e
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return r, e
	}
	ok := map[string]bool{"me": true}
	for _, p := range ps {
		ok[p.ID] = true
	}
	for _, g := range gs {
		ok[g.JID] = true
	}
	if !ok[r.A] || !ok[r.B] {
		return r, model.ErrNotFound
	}
	if r.A == r.B || r.Kind == "" || len(r.Note) > 16000 {
		return r, model.ErrInvalid
	}
	r.EvidenceChats, e = s.evidenceChats(a, r.EvidenceMessageIDs)
	if e != nil {
		return r, e
	}
	r.ID = dbutil.ID()
	r.Source = "you"
	if a != nil {
		r.Source = a.ID
	}
	return r, s.Hub.Repo.Apply(hub.Mutation{Bucket: "relations", ID: r.ID, Value: r}, hub.Mutation{Bucket: "changes", ID: r.ID, Value: Change{ID: r.ID, At: s.Hub.Now().UTC(), Source: r.Source, Kind: "relation", SubjectID: r.A}})
}
func (s *Service) Changes() ([]Change, error) {
	xs, e := hub.List[Change](s.Hub.Repo, "changes")
	sort.Slice(xs, func(i, j int) bool { return xs[i].At.After(xs[j].At) })
	return xs, e
}
func (s *Service) Revert(id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var c Change
	if e := s.Hub.Repo.Get("changes", id, &c); e != nil {
		return e
	}
	if c.Reverted {
		return model.ErrConflict
	}
	mutation := hub.Mutation{}
	switch c.Kind {
	case "fact":
		var f model.Fact
		if e := s.Hub.Repo.Get("facts", id, &f); e != nil {
			return e
		}
		f.Status = "reverted"
		mutation = hub.Mutation{Bucket: "facts", ID: id, Value: f}
	case "relation":
		mutation = hub.Mutation{Bucket: "relations", ID: id, Delete: true}
	case "person":
		var current model.Person
		if e := s.Hub.Repo.Get("people", c.SubjectID, &current); e != nil {
			return e
		}
		if c.Before == nil || c.After == nil {
			return model.ErrInvalid
		}
		// Revert only fields this change edited. Directory identity refreshes and
		// unrelated later edits must survive; overlapping later edits conflict.
		for _, pair := range [][3]*string{
			{&current.Label, &c.Before.Label, &c.After.Label},
			{&current.Circle, &c.Before.Circle, &c.After.Circle},
			{&current.Role, &c.Before.Role, &c.After.Role},
			{&current.HowKnown, &c.Before.HowKnown, &c.After.HowKnown},
			{&current.Notes, &c.Before.Notes, &c.After.Notes},
		} {
			if *pair[1] == *pair[2] {
				continue
			}
			if *pair[0] != *pair[2] {
				return model.ErrConflict
			}
			*pair[0] = *pair[1]
		}
		if !reflect.DeepEqual(c.Before.Details, c.After.Details) {
			if !reflect.DeepEqual(current.Details, c.After.Details) {
				return model.ErrConflict
			}
			current.Details = c.Before.Details
		}
		if !reflect.DeepEqual(c.Before.DetailSuggestions, c.After.DetailSuggestions) {
			if !reflect.DeepEqual(current.DetailSuggestions, c.After.DetailSuggestions) {
				return model.ErrConflict
			}
			current.DetailSuggestions = c.Before.DetailSuggestions
		}
		current.UpdatedAt = s.Hub.Now().UTC()
		mutation = hub.Mutation{Bucket: "people", ID: current.ID, Value: current}
	default:
		return model.ErrInvalid
	}
	c.Reverted = true
	return s.Hub.Repo.Apply(mutation, hub.Mutation{Bucket: "changes", ID: id, Value: c})
}

type Node struct {
	GroupsInCommon  []model.GroupInCommon  `json:"groups_in_common"`
	CircleAffinity  []model.CircleAffinity `json:"circle_affinity"`
	PrimaryCircleID string                 `json:"primary_circle_id,omitempty"`
	ID              string                 `json:"id"`
	Kind            string                 `json:"kind"`
	Name            string                 `json:"name"`
	Weight          float64                `json:"weight"`
	Circle          string                 `json:"circle,omitempty"`
}
type Edge struct {
	A      string  `json:"a"`
	B      string  `json:"b"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"weight"`
	Recent bool    `json:"recent"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

func (s *Service) Graph(a *model.Agent, focus string, depth, limit int) (Graph, error) {
	key := s.cacheKey(a, "graph", focus, depth, limit)
	s.shared().cacheMu.Lock()
	cached, ok := s.shared().graphCache[key]
	s.shared().cacheMu.Unlock()
	if ok {
		return cloneGraph(cached.graph), nil
	}
	if a != nil && !a.Permissions.ReadPeople {
		return Graph{}, model.ErrForbidden
	}
	if limit <= 0 {
		limit = 300
	}
	if limit > 5000 {
		limit = 5000
	}
	if depth <= 0 {
		depth = 1
	}
	if depth > 4 {
		depth = 4
	}
	ps, e := s.People(a, "")
	if e != nil {
		return Graph{}, e
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return Graph{}, e
	}
	chats, _, e := s.Hub.Visible(a)
	if e != nil {
		return Graph{}, e
	}
	current := map[string]bool{}
	for _, chat := range chats {
		if s.Hub.Current(chat) {
			current[chat.JID] = true
		}
	}
	groups := []model.Group{}
	for _, g := range gs {
		if current[g.JID] || g.JID == focus {
			groups = append(groups, g)
		}
	}
	gs = groups
	ms, e := s.memberships()
	if e != nil {
		return Graph{}, e
	}
	all := []Node{{ID: "me", Kind: "me", Name: "You", Weight: 3}}
	edges := []Edge{}
	exists := map[string]bool{"me": true}
	stats, e := s.allStats(a, ps, gs, ms)
	if e != nil {
		return Graph{}, e
	}
	for _, p := range ps {
		if p.ID == "me" {
			all[0].Name = p.Name
			continue
		}
		exists[p.ID] = true
		name := p.Label
		if name == "" {
			name = p.Name
		}
		st := stats[p.ID]
		w := float64(st.DM30)
		all = append(all, Node{ID: p.ID, Kind: "person", Name: name, Weight: 3 + w, Circle: p.Circle, CircleAffinity: p.CircleAffinity, PrimaryCircleID: p.PrimaryCircleID, GroupsInCommon: p.GroupsInCommon})
		if st.DM365 > 0 {
			edges = append(edges, Edge{"me", p.ID, "dm", w + 1, st.LastDM != nil && st.LastDM.After(s.Hub.Now().Add(-24*time.Hour))})
		}
	}
	for _, g := range gs {
		exists[g.JID] = true
		all = append(all, Node{ID: g.JID, Kind: "group", Name: g.Name, Weight: 2})
		edges = append(edges, Edge{"me", g.JID, "member", 1, false})
	}
	for _, m := range ms {
		if exists[m.Group] && exists[m.Person] {
			edges = append(edges, Edge{m.Group, m.Person, "member", 1, false})
		}
	}
	rs, e := s.Relations(a)
	if e != nil {
		return Graph{}, e
	}
	for _, r := range rs {
		edges = append(edges, Edge{r.A, r.B, r.Kind, 1, false})
	}
	chosen := map[string]bool{}
	if focus != "" {
		if !exists[focus] {
			return Graph{}, model.ErrNotFound
		}
		chosen[focus] = true
		for d := 0; d < depth; d++ {
			next := map[string]bool{}
			for _, edge := range edges {
				if chosen[edge.A] {
					next[edge.B] = true
				}
				if chosen[edge.B] {
					next[edge.A] = true
				}
			}
			for id := range next {
				chosen[id] = true
			}
		}
	} else {
		for _, n := range all {
			chosen[n.ID] = true
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].ID == focus {
			return true
		}
		if all[j].ID == focus {
			return false
		}
		if all[i].ID == "me" {
			return true
		}
		if all[j].ID == "me" {
			return false
		}
		if all[i].Weight != all[j].Weight {
			return all[i].Weight > all[j].Weight
		}
		return all[i].ID < all[j].ID
	})
	allowed := s.Hub.ChatAccess(a)
	recentChats := map[string]bool{}
	cutoff := s.Hub.Now().Add(-24 * time.Hour)
	for _, query := range []struct {
		db   dbutil.Database
		text string
	}{
		{s.Hub.Messages.DB, "SELECT DISTINCT chat_jid FROM messages WHERE julianday(timestamp)>=julianday(?)"},
		{s.Hub.Repo.DB, "SELECT DISTINCT j.value FROM activity,json_each(activity.data,'$.chat_jids') j WHERE julianday(activity.at)>=julianday(?)"},
	} {
		rows, err := query.db.Query(query.text, cutoff)
		if err != nil {
			return Graph{}, err
		}
		for rows.Next() {
			var chat string
			if err = rows.Scan(&chat); err != nil {
				rows.Close()
				return Graph{}, err
			}
			if allowed(chat) {
				recentChats[chat] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Graph{}, err
		}
	}
	personRecent := map[string]bool{}
	for _, person := range ps {
		for _, chat := range person.JIDs {
			if recentChats[chat] {
				personRecent[person.ID] = true
			}
		}
	}
	for i := range edges {
		edge := &edges[i]
		if edge.Kind == "member" {
			if recentChats[edge.A] || recentChats[edge.B] {
				edge.Recent = true
			}
		} else if edge.Kind == "dm" && personRecent[edge.B] {
			edge.Recent = true
		}
	}
	out := Graph{Nodes: []Node{}, Edges: []Edge{}}
	included := map[string]bool{}
	for _, n := range all {
		if chosen[n.ID] && len(out.Nodes) < limit {
			out.Nodes = append(out.Nodes, n)
			included[n.ID] = true
		}
	}
	for _, e := range edges {
		if included[e.A] && included[e.B] {
			out.Edges = append(out.Edges, e)
		}
	}
	s.shared().cacheMu.Lock()
	if len(s.shared().graphCache) > 16 {
		s.shared().graphCache = nil
	}
	if s.shared().graphCache == nil {
		s.shared().graphCache = map[string]graphCache{}
	}
	s.shared().graphCache[key] = graphCache{cloneGraph(out)}
	s.shared().cacheMu.Unlock()
	return out, nil
}
func (s *Service) Export(a *model.Agent) (any, error) {
	if a != nil && !a.Permissions.ReadPeople {
		return nil, model.ErrForbidden
	}
	ps, e := s.People(a, "")
	if e != nil {
		return nil, e
	}
	gs, e := s.Groups(a, "")
	if e != nil {
		return nil, e
	}
	// AI access is this engine's own setting, not knowledge to export or sync.
	for i := range gs {
		gs[i].AgentAccess = []model.GroupAccess{}
	}
	ms, e := s.memberships()
	if e != nil {
		return nil, e
	}
	stats, e := s.allStats(a, ps, gs, ms)
	if e != nil {
		return nil, e
	}
	rs, e := s.Relations(a)
	if e != nil {
		return nil, e
	}
	facts, e := hub.List[model.Fact](s.Hub.Repo, "facts")
	if e != nil {
		return nil, e
	}
	allowed := s.Hub.ChatAccess(a)
	factsBySubject := map[string][]model.Fact{}
	for _, f := range facts {
		if f.Status != "active" {
			continue
		}
		ok := true
		for _, j := range f.EvidenceChats {
			if !allowed(j) {
				ok = false
			}
		}
		if ok {
			key := f.Subject + ":" + f.SubjectID
			factsBySubject[key] = append(factsBySubject[key], f)
		}
	}
	peopleByID := map[string]model.Person{}
	groupsByID := map[string]model.Group{}
	for _, p := range ps {
		peopleByID[p.ID] = p
	}
	for _, g := range gs {
		groupsByID[g.JID] = g
	}
	personGroups := map[string][]model.Group{}
	groupMembers := map[string][]map[string]any{}
	for _, m := range ms {
		p, pok := peopleByID[m.Person]
		g, gok := groupsByID[m.Group]
		if pok && gok {
			personGroups[p.ID] = append(personGroups[p.ID], g)
			groupMembers[g.JID] = append(groupMembers[g.JID], map[string]any{"person": p, "admin": m.Admin})
		}
	}
	profiles := []Profile{}
	for _, p := range ps {
		pr := Profile{Person: p, Groups: []model.Group{}, Facts: []model.Fact{}, Relations: []model.Relation{}, Stats: stats[p.ID]}
		pr.Groups = append(pr.Groups, personGroups[p.ID]...)
		pr.Facts = append(pr.Facts, factsBySubject["person:"+p.ID]...)
		for _, r := range rs {
			if r.A == p.ID || r.B == p.ID {
				pr.Relations = append(pr.Relations, r)
			}
		}
		profiles = append(profiles, pr)
	}
	groups := []any{}
	for _, g := range gs {
		members := groupMembers[g.JID]
		if members == nil {
			members = []map[string]any{}
		}
		fs := factsBySubject["group:"+g.JID]
		if fs == nil {
			fs = []model.Fact{}
		}
		g.MemberCount = len(members)
		groups = append(groups, map[string]any{"group": g, "members": members, "facts": fs})
	}
	return map[string]any{"people": profiles, "groups": groups, "relations": rs}, nil
}
func (s *Service) CacheStats() error {
	ps, e := s.People(nil, "")
	if e != nil {
		return e
	}
	gs, e := s.Groups(nil, "")
	if e != nil {
		return e
	}
	ms, e := s.memberships()
	if e != nil {
		return e
	}
	stats, e := s.allStats(nil, ps, gs, ms)
	if e != nil {
		return e
	}
	tx, e := s.Hub.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for id, st := range stats {
		if _, e = tx.Exec("INSERT INTO objects VALUES('stats',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data", id, dbutil.JSON(st)); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Service) evidenceChats(a *model.Agent, ids []string) ([]string, error) {
	chats := []string{}
	for _, id := range ids {
		// Reject ambiguous IDs, even across two chats: provenance must be exact.
		rows, e := s.Hub.Messages.DB.Query("SELECT chat_jid FROM messages WHERE id=?", id)
		if e != nil {
			return nil, e
		}
		js := []string{}
		for rows.Next() {
			var j string
			if e = rows.Scan(&j); e != nil {
				rows.Close()
				return nil, e
			}
			js = append(js, j)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(js) != 1 || !s.Hub.CanChat(a, js[0]) {
			return nil, model.ErrNotFound
		}
		chats = append(chats, js[0])
	}
	return chats, nil
}
