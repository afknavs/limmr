package people

import (
	"encoding/json"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"slices"
	"strings"
)

type peopleCache struct{ people []model.Person }
type graphCache struct{ graph Graph }

// Copy nested mutable storage without a JSON encode/decode round trip. Keep
// empty details arrays compatible with PersonDetails.MarshalJSON normalization.
func clonePeople(ps []model.Person) []model.Person {
	out := slices.Clone(ps)
	for i := range out {
		p := &out[i]
		p.JIDs = slices.Clone(p.JIDs)
		if p.Photo != nil {
			v := *p.Photo
			p.Photo = &v
		}
		if p.ContactsPhoto != nil {
			v := *p.ContactsPhoto
			p.ContactsPhoto = &v
		}
		if p.Details.Sources != nil {
			sources := map[string]string{}
			for k, v := range p.Details.Sources {
				sources[k] = v
			}
			p.Details.Sources = sources
		}
		p.Details.Phones = append([]model.LabeledValue{}, p.Details.Phones...)
		p.Details.Emails = append([]model.LabeledValue{}, p.Details.Emails...)
		p.Details.Handles = append([]model.LabeledValue{}, p.Details.Handles...)
		p.DetailSuggestions = slices.Clone(p.DetailSuggestions)
		for j := range p.DetailSuggestions {
			p.DetailSuggestions[j].EvidenceMessageIDs = slices.Clone(p.DetailSuggestions[j].EvidenceMessageIDs)
			p.DetailSuggestions[j].EvidenceChats = slices.Clone(p.DetailSuggestions[j].EvidenceChats)
		}
		p.CircleAffinity = slices.Clone(p.CircleAffinity)
		for j := range p.CircleAffinity {
			p.CircleAffinity[j].Reasons = slices.Clone(p.CircleAffinity[j].Reasons)
		}
		p.GroupsInCommon = slices.Clone(p.GroupsInCommon)
		for j := range p.GroupsInCommon {
			g := &p.GroupsInCommon[j]
			g.CircleIDs = slices.Clone(g.CircleIDs)
			if g.LastActiveAt != nil {
				v := *g.LastActiveAt
				g.LastActiveAt = &v
			}
		}
		if p.InferredRelationship != nil {
			v := *p.InferredRelationship
			p.InferredRelationship = &v
		}
	}
	return out
}

// Counters are updated by SQLite triggers, including SQL outside the services.
// Permission and time are part of the key; scopes never share projections.
func (s *Service) cacheKey(a *model.Agent, kind string, args ...any) string {
	var h, m int64
	_ = s.Hub.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&h)
	_ = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&m)
	var scope any
	if a != nil {
		scope = []any{a.ID, a.Enabled, a.Permissions}
	}
	b, _ := json.Marshal([]any{scope, args, s.Hub.Settings()})
	var activity int64
	if kind == "graph" {
		_ = s.Hub.Repo.DB.QueryRow("SELECT COALESCE(max(id),0) FROM activity").Scan(&activity)
	}
	return fmt.Sprintf("%s/%d/%d/%d/%d/%s", kind, h, m, activity, s.Hub.Now().Unix()/60, b)
}
func (s *Service) relevant(a *model.Agent, ps []model.Person, cs []model.Circle, ms []model.Membership) (map[string]bool, error) {
	out := map[string]bool{"me": true}
	chats, _, e := s.Hub.Visible(a)
	if e != nil {
		return nil, e
	}
	current := map[string]bool{}
	dm := map[string]bool{}
	visibleDM := map[string]bool{}
	for _, c := range chats {
		if !c.IsGroup {
			visibleDM[c.JID] = true
		}
	}
	sentRows, e := s.Hub.Messages.DB.Query("SELECT DISTINCT chat_jid FROM messages WHERE is_from_me=1")
	if e != nil {
		return nil, e
	}
	for sentRows.Next() {
		var j string
		if e = sentRows.Scan(&j); e != nil {
			sentRows.Close()
			return nil, e
		}
		if visibleDM[j] {
			dm[j] = true
		}
	}
	e = sentRows.Err()
	sentRows.Close()
	if e != nil {
		return nil, e
	}
	for _, c := range chats {

		if s.Hub.Current(c) {
			current[c.JID] = true
		}
	}
	senders := map[string]bool{}
	rows, e := s.Hub.Messages.DB.Query("SELECT DISTINCT chat_jid,COALESCE(sender,'') FROM messages WHERE julianday(timestamp)>=julianday(?)", s.Hub.CurrentSince())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var j, v string
		if e = rows.Scan(&j, &v); e != nil {
			rows.Close()
			return nil, e
		}
		if current[j] && strings.HasSuffix(j, "@g.us") {
			senders[v] = true
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, p := range ps {
		if addedDetails(p) {
			out[p.ID] = true
		}
		for _, j := range p.JIDs {
			if dm[j] || senders[j] || senders[strings.SplitN(j, "@", 2)[0]] {
				out[p.ID] = true
			}
		}
	}
	allowed := s.Hub.ChatAccess(a)
	for _, c := range cs {
		if c.Confirmed && (a == nil || s.Hub.CircleAccess(a, c)) {
			jids := circleJIDs(c)
			for _, m := range ms {
				if model.Contains(jids, m.Group) && allowed(m.Group) {
					out[m.Person] = true
				}
			}
			for _, p := range c.People {
				out[p.PersonID] = true
			}
			for _, p := range ps {
				for _, j := range p.JIDs {
					if model.Contains(jids, j) {
						out[p.ID] = true
					}
				}
			}
		}
	}
	fs, e := hub.List[model.Fact](s.Hub.Repo, "facts")
	if e != nil {
		return nil, e
	}
	for _, f := range fs {
		if f.Status == "active" && f.Subject == "person" {
			out[f.SubjectID] = true
		}
	}
	rs, e := hub.List[model.Relation](s.Hub.Repo, "relations")
	if e != nil {
		return nil, e
	}
	for _, r := range rs {
		out[r.A] = true
		out[r.B] = true
	}
	return out, nil
}

func cloneGraph(g Graph) Graph {
	b, _ := json.Marshal(g)
	var out Graph
	_ = json.Unmarshal(b, &out)
	return out
}

func cloneStats(xs map[string]Stats) map[string]Stats {
	if xs == nil {
		return nil
	}
	out := make(map[string]Stats, len(xs))
	for id, v := range xs {
		v.SharedGroups = slices.Clone(v.SharedGroups)
		if v.LastDM != nil {
			at := *v.LastDM
			v.LastDM = &at
		}
		out[id] = v
	}
	return out
}

func addedDetails(p model.Person) bool {
	if p.Label != "" || p.Role != "" || p.HowKnown != "" || p.Notes != "" || len(p.Details.Emails) > 0 || len(p.Details.Handles) > 0 || p.Details.Location != "" || p.Details.Pronouns != "" || p.Details.Birthday != "" || len(p.DetailSuggestions) > 0 {
		return true
	}
	for _, v := range p.Details.Phones {
		if v.Source != "whatsapp" {
			return true
		}
	}
	return false
}

// Businesses/newsletters remain searchable as chats. Person endpoints omit them,
// even for all=true, unless someone explicitly added context or circle membership.
func (s *Service) excludedSenders(a *model.Agent, ps []model.Person, cs []model.Circle) map[string]bool {
	explicit := map[string]bool{"me": true}
	for _, c := range cs {
		if c.Confirmed && (a == nil || s.Hub.CircleAccess(a, c)) {
			jids := circleJIDs(c)
			for _, p := range c.People {
				explicit[p.PersonID] = true
			}
			for _, p := range ps {
				for _, j := range p.JIDs {
					if model.Contains(jids, j) {
						explicit[p.ID] = true
					}
				}
			}
		}
	}
	out := map[string]bool{}
	for _, p := range ps {
		if explicit[p.ID] || addedDetails(p) {
			continue
		}
		business := p.Business || p.Broadcast
		for _, j := range p.JIDs {
			business = business || strings.HasSuffix(j, "@newsletter") || strings.HasSuffix(j, "@broadcast")
		}
		if business {
			out[p.ID] = true
		}
	}
	return out
}
func circleJIDs(c model.Circle) []string {
	js := []string{}
	for _, j := range c.Chats {
		js = append(js, j.ChatJID)
	}
	return js
}
