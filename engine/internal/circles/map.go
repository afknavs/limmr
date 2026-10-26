package circles

import (
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
	"unicode"
)

type MapNode struct {
	GroupsInCommonCount int                    `json:"groups_in_common_count,omitempty"`
	GroupsInCommon      []model.GroupInCommon  `json:"groups_in_common,omitempty"`
	CircleAffinity      []model.CircleAffinity `json:"circle_affinity,omitempty"`
	PrimaryCircleID     string                 `json:"primary_circle_id,omitempty"`
	HowKnown            string                 `json:"how_known,omitempty"`
	ID                  string                 `json:"id"`
	Kind                string                 `json:"kind"`
	Title               string                 `json:"title"`
	CircleID            string                 `json:"circle_id,omitempty"`
	Initials            string                 `json:"initials"`
	Weight              float64                `json:"weight"`
	LastActiveAt        *time.Time             `json:"last_active_at"`
	NewMessages         int                    `json:"new_messages"`
	Circles             []string               `json:"circles"`
}
type MapEdge struct {
	ID       string       `json:"id"`
	Source   string       `json:"source"`
	Target   string       `json:"target"`
	Kind     string       `json:"kind"`
	Label    string       `json:"label"`
	Origin   model.Origin `json:"origin"`
	Strength float64      `json:"strength"`
	State    string       `json:"state"`
}
type MapEvent struct {
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	By      string    `json:"by"`
	EdgeID  string    `json:"edge_id"`
	NodeIDs []string  `json:"node_ids"`
	Text    string    `json:"text"`
}
type MapSummary struct {
	AIThisWeek  int    `json:"ai_this_week"`
	Connections int    `json:"connections"`
	ByAI        int    `json:"by_ai"`
	ThisWeek    int    `json:"this_week"`
	Latest      string `json:"latest"`
}
type MapPulse struct {
	ID          string `json:"id"`
	NewMessages int    `json:"new_messages"`
}
type UnderstandingMap struct {
	Cursor       string     `json:"cursor,omitempty"`
	Delta        bool       `json:"delta,omitempty"`
	RemovedNodes []string   `json:"removed_nodes,omitempty"`
	RemovedEdges []string   `json:"removed_edges,omitempty"`
	Nodes        []MapNode  `json:"nodes"`
	Edges        []MapEdge  `json:"edges"`
	Pulses       []MapPulse `json:"pulses"`
	Events       []MapEvent `json:"events"`
	Summary      MapSummary `json:"summary"`
}

func initials(name string) string {
	parts := strings.Fields(name)
	out := []rune{}
	for _, p := range parts {
		if len(out) == 2 {
			break
		}
		for _, r := range p {
			out = append(out, unicode.ToUpper(r))
			break
		}
	}
	return string(out)
}
func edgeID(source, target, kind, key string) string {
	return "edge-" + dbutil.Digest(strings.Join([]string{source, target, kind, key}, "\x00"))[:24]
}
func (s *Service) Map() (any, error) { return s.MapSince(time.Time{}) }

// MapSince always returns the complete current graph. Only the event window is
// incremental, so polling never loses nodes, pending links or summary counts.
func (s *Service) MapSince(since time.Time) (any, error) { return s.mapView(since, "") }

// MapFocus loads the complete confirmed membership of one circle for the zoomed view.
func (s *Service) MapFocus(focus string) (any, error) { return s.mapView(time.Time{}, focus) }
func (s *Service) mapView(since time.Time, focus string) (any, error) {
	var h, m int64
	_ = s.Hub.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&h)
	_ = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&m)
	key := fmt.Sprintf("%d/%d/%d/%s", h, m, s.Hub.Now().Unix()/60, focus)
	s.shared().mapMu.Lock()
	defer s.shared().mapMu.Unlock()
	if key == s.shared().mapKey {
		out := s.shared().mapValue
		out.Events = mapEventsAfter(out.Events, since)
		return out, nil
	}
	out := UnderstandingMap{Nodes: []MapNode{}, Edges: []MapEdge{}, Pulses: []MapPulse{}, Events: []MapEvent{}}
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return nil, err
	}
	if focus != "" {
		found := false
		for _, c := range cs {
			if c.ID == focus && c.Confirmed {
				cs = []model.Circle{c}
				found = true
				break
			}
		}
		if !found {
			return nil, model.ErrNotFound
		}
	}
	ps, err := s.People.PeopleAll(nil, "", true)
	if err != nil {
		return nil, err
	}
	chats, _, err := s.Hub.Visible(nil)
	if err != nil {
		return nil, err
	}
	agents, err := s.Hub.Agents()
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	origin := func(o *model.Origin, at time.Time) model.Origin {
		if o == nil {
			return model.Origin{By: "Structure", ByKind: "structure", At: at}
		}
		v := *o
		switch v.ByKind {
		case "agent":
			if name, ok := names[v.By]; ok {
				v.By = name
			} else {
				v.By = "Removed integration"
			}
		case "you":
			v.By = "You"
		default:
			v.ByKind = "structure"
			v.By = "Structure"
		}
		return v
	}
	sourceOrigin := func(source string, at time.Time) model.Origin {
		kind := "structure"
		if source == "you" {
			kind = "you"
		} else if source != "" && source != "structure" {
			kind = "agent"
		}
		return origin(&model.Origin{By: source, ByKind: kind, At: at}, at)
	}
	nodes := map[string]*MapNode{}
	personByJID := map[string]string{}
	personBySender := map[string]string{}
	addNode := func(id, kind, title, circle string) {
		n := nodes[id]
		if n == nil {
			if focus == "" && len(nodes) >= 150 {
				return
			}
			n = &MapNode{ID: id, Kind: kind, Title: title, Initials: initials(title), Circles: []string{}}
			nodes[id] = n
		}
		if circle != "" && !model.Contains(n.Circles, circle) {
			n.Circles = append(n.Circles, circle)
			if n.CircleID == "" && kind == "topic" {
				n.CircleID = circle
			}
		}
	}
	for _, c := range cs {
		addNode(c.ID, "circle", c.Name, c.ID)
	}
	// Rank once, then reserve three real members per circle before the global budget.
	ps, err = s.People.RankForMap(ps, len(ps))
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	ranked := ps
	ps = []model.Person{}
	addPerson := func(p model.Person) {
		if !selected[p.ID] {
			selected[p.ID] = true
			ps = append(ps, p)
		}
	}
	for _, c := range cs {
		count := 0
		for _, p := range ranked {
			member := false
			for _, m := range c.People {
				if m.PersonID == p.ID {
					member = true
					break
				}
			}
			if member && (focus != "" || count < 3) {
				addPerson(p)
				count++
			}
		}
	}
	if focus == "" {
		for _, p := range ranked {
			if len(ps) >= 120 {
				break
			}
			addPerson(p)
		}
	}
	for _, p := range ps {
		title := p.Label
		if title == "" {
			title = p.Name
		}
		addNode(p.ID, "person", title, "")
		if n := nodes[p.ID]; n != nil {
			n.GroupsInCommonCount = len(p.GroupsInCommon)
			n.GroupsInCommon = p.GroupsInCommon[:min(3, len(p.GroupsInCommon))]
			n.CircleAffinity = p.CircleAffinity
			n.PrimaryCircleID = p.PrimaryCircleID
			n.HowKnown = p.HowKnown
			if n.HowKnown == "" && p.InferredRelationship != nil {
				n.HowKnown = p.InferredRelationship.HowKnown + " · Inferred on this Mac"
			}
		}
		for _, j := range p.JIDs {
			personByJID[j] = p.ID
			personBySender[j] = p.ID
			personBySender[strings.SplitN(j, "@", 2)[0]] = p.ID
		}
	}
	for _, circle := range cs {
		if len(circle.People) == 0 {
			for _, cc := range circle.Chats {
				for _, chat := range chats {
					if chat.JID == cc.ChatJID {
						addNode(chat.JID, "chat", chat.Name, circle.ID)
						break
					}
				}
			}
		}
	}
	// Chats are represented by people and circles; current chat detail fits the remaining budget.
	for _, c := range chats {
		if len(nodes) >= 145 || focus != "" {
			break
		}
		if s.Hub.Current(c) && (c.IsGroup || personByJID[c.JID] != "") {
			title := c.Name
			if !c.IsGroup {
				title = nodes[personByJID[c.JID]].Title
			}
			addNode(c.JID, "chat", title, "")
		}
	}
	for _, c := range cs {
		addNode(c.ID, "circle", c.Name, c.ID)
		for _, j := range c.Chats {
			if n := nodes[j.ChatJID]; n != nil {
				addNode(n.ID, n.Kind, n.Title, c.ID)
			}
		}
		for _, p := range c.People {
			if n := nodes[p.PersonID]; n != nil {
				addNode(n.ID, n.Kind, n.Title, c.ID)
			}
		}
		for _, t := range c.Topics {
			addNode(t.ID, "topic", t.Title, c.ID)
		}
	}
	edges := map[string]MapEdge{}
	events := map[string]MapEvent{}
	addEdge := func(source, target, kind, label, key, state string, o model.Origin) string {
		if nodes[source] == nil || nodes[target] == nil {
			return ""
		}
		id := edgeID(source, target, kind, key)
		strength := .7
		if o.ByKind == "structure" {
			strength = .4
		}
		if state == "suggested" {
			strength = .3
		}
		edges[id] = MapEdge{id, source, target, kind, label, o, strength, state}
		return id
	}
	addEvent := func(id, kind, edge, text string, ids []string, o model.Origin) {
		if o.At.IsZero() || o.At.Before(s.Hub.Now().Add(-14*24*time.Hour)) || o.At.After(s.Hub.Now()) {
			return
		}
		for _, id := range ids {
			if nodes[id] == nil {
				return
			}
		}
		events[id] = MapEvent{id, o.At, kind, o.By, edge, ids, text}
	}
	for _, c := range cs {
		state := "suggested"
		if c.Confirmed {
			state = "confirmed"
		}
		co := origin(c.Origin, c.UpdatedAt)
		addEvent("proposed-"+c.ID, "circle_proposed", "", co.By+" proposed circle "+c.Name, []string{c.ID}, co)
		if c.Confirmed && c.ConfirmOrigin != nil {
			o := origin(c.ConfirmOrigin, c.UpdatedAt)
			addEvent("confirmed-"+c.ID, "circle_confirmed", "", o.By+" confirmed circle "+c.Name, []string{c.ID}, o)
		}
		membershipOrigin := co
		if c.Confirmed && c.ConfirmOrigin != nil {
			membershipOrigin = origin(c.ConfirmOrigin, c.UpdatedAt)
		}
		for _, j := range c.Chats {
			o := membershipOrigin
			if j.Origin != nil && (c.ConfirmOrigin == nil || j.Origin.ChangeID != "") {
				o = origin(j.Origin, j.IncludedSince)
			}
			addEdge(c.ID, j.ChatJID, "membership", j.Role, "", state, o)
		}
		for _, p := range c.People {
			o := origin(p.Origin, c.UpdatedAt)
			kind := "membership"
			label := p.Role
			if label != "" && label != "member" {
				kind = "role"
			}
			id := addEdge(c.ID, p.PersonID, kind, label, "", state, o)
			if o.ChangeID == "" && o.ByKind != "structure" {
				addEvent("role-"+c.ID+"-"+p.PersonID, "role_set", id, o.By+" linked "+titleOf(nodes, p.PersonID)+" to "+c.Name+" as "+label, []string{c.ID, p.PersonID}, o)
			}
		}
		for _, t := range c.Topics {
			o := origin(t.Origin, t.LastActivityAt)
			id := addEdge(c.ID, t.ID, "topic", t.Title, "", state, o)
			if o.ChangeID == "" {
				addEvent("topic-"+t.ID, "topic_added", id, o.By+" added topic "+t.Title+" in "+c.Name, []string{c.ID, t.ID}, o)
			}
			for _, p := range t.People {
				addEdge(t.ID, p, "topic_person", t.Title, "", state, o)
			}
			for _, j := range t.Chats {
				addEdge(t.ID, j, "topic_chat", t.Title, "", state, o)
			}
		}
	}
	for _, p := range ps {
		if p.PrimaryCircleID != "" {
			addNode(p.ID, "person", p.Name, p.PrimaryCircleID)
			for _, a := range p.CircleAffinity {
				existing := false
				for _, edge := range edges {
					if edge.Source == a.CircleID && edge.Target == p.ID {
						existing = true
						break
					}
				}
				if a.CircleID == p.PrimaryCircleID && !existing {
					addEdge(a.CircleID, p.ID, "affinity", strings.Join(a.Reasons, "; "), "", "suggested", origin(nil, time.Time{}))
				}
			}
		}
	}
	// Existing WhatsApp structure is never attributed to an AI.
	ms, err := hub.List[model.Membership](s.Hub.Repo, "memberships")
	if err != nil {
		return nil, err
	}
	for _, m := range ms {
		addEdge(m.Group, m.Person, "membership", "Group member", "", "confirmed", origin(nil, time.Time{}))
		if g := nodes[m.Group]; g != nil {
			for _, cid := range g.Circles {
				if p := nodes[m.Person]; p != nil {
					addNode(p.ID, p.Kind, p.Title, cid)
				}
			}
		}
	}
	for j, p := range personByJID {
		addEdge(j, p, "dm", "Direct chat", "", "confirmed", origin(nil, time.Time{}))
	}
	// History survives subsequent role edits; current edges carry only the current origin.
	changes, err := hub.List[model.UnderstandingChange](s.Hub.Repo, "understanding_changes")
	if err != nil {
		return nil, err
	}
	for _, ch := range changes {
		if nodes[ch.CircleID] == nil || nodes[ch.TargetID] == nil {
			continue
		}
		o := origin(&ch.Origin, ch.Origin.At)
		kind := "role"
		text := o.By + " set " + titleOf(nodes, ch.TargetID) + " as " + ch.Label + " in " + titleOf(nodes, ch.CircleID)
		if ch.Kind == "edge_added" {
			kind = "membership"
			text = o.By + " linked " + titleOf(nodes, ch.TargetID) + " to " + titleOf(nodes, ch.CircleID)
		}
		if ch.Kind == "topic_added" {
			kind = "topic"
			text = o.By + " added topic " + ch.Label + " in " + titleOf(nodes, ch.CircleID)
		}
		id := edgeID(ch.CircleID, ch.TargetID, kind, "")
		if ch.Kind == "circle_confirmed" {
			continue
		} // emitted from the circle above
		addEvent(ch.ID, ch.Kind, id, text, []string{ch.CircleID, ch.TargetID}, o)
	}
	briefs, err := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if err != nil {
		return nil, err
	}
	latest := map[string]model.Brief{}
	for _, b := range briefs {
		if b.Version > latest[b.CircleID].Version {
			latest[b.CircleID] = b
		}
		o := sourceOrigin(b.WrittenBy, b.WrittenAt)
		addEvent(fmt.Sprintf("brief-%s-%d", b.CircleID, b.Version), "brief_updated", "", o.By+" updated the brief for "+titleOf(nodes, b.CircleID), []string{b.CircleID}, o)
	}
	for _, b := range latest {
		o := sourceOrigin(b.WrittenBy, b.WrittenAt)
		o.BriefVersion = b.Version
		o.Evidence = len(b.Evidence)
		for _, p := range b.PeopleNotes {
			addEdge(b.CircleID, p.PersonID, "contribution", p.Contribution, "", "confirmed", o)
		}
	}
	pc, err := s.People.Changes()
	if err != nil {
		return nil, err
	}
	facts, err := hub.List[model.Fact](s.Hub.Repo, "facts")
	if err != nil {
		return nil, err
	}
	rels, err := hub.List[model.Relation](s.Hub.Repo, "relations")
	if err != nil {
		return nil, err
	}
	for _, ch := range pc {
		if ch.Reverted {
			continue
		}
		o := sourceOrigin(ch.Source, ch.At)
		o.ChangeID = ch.ID
		ids := []string{}
		label := ""
		id := ""
		switch ch.Kind {
		case "relation":
			for _, r := range rels {
				if r.ID == ch.ID {
					a, b := r.A, r.B
					if a == "me" {
						addNode("me", "person", "You", "")
					}
					if b == "me" {
						addNode("me", "person", "You", "")
					}
					label = r.Kind
					id = addEdge(a, b, "relation", label, ch.ID, "confirmed", o)
					ids = []string{a, b}
				}
			}
		case "fact":
			for _, f := range facts {
				if f.ID == ch.ID && f.Status == "active" {
					o.Evidence = len(f.EvidenceMessageIDs)
					label = f.Text
					for _, j := range f.EvidenceChats {
						edge := addEdge(f.SubjectID, j, "fact", label, ch.ID, "confirmed", o)
						if id == "" {
							id = edge
							ids = []string{f.SubjectID, j}
						}
					}
				}
			}
		case "person":
			if ch.Before != nil && ch.After != nil {
				p := ch.After
				var current model.Person
				for _, person := range ps {
					if person.ID == p.ID {
						current = person
						break
					}
				}
				labels := []string{}
				for _, field := range [][3]string{
					{ch.Before.Role, p.Role, current.Role}, {ch.Before.Notes, p.Notes, current.Notes},
					{ch.Before.HowKnown, p.HowKnown, current.HowKnown}, {ch.Before.Label, p.Label, current.Label},
					{ch.Before.Circle, p.Circle, current.Circle},
				} {
					if field[0] != field[1] && field[1] != "" && field[1] == field[2] {
						labels = append(labels, field[1])
					}
				}
				if len(labels) == 0 {
					continue
				}
				label = strings.Join(labels, "; ")

				target := ""
				for _, c := range cs {
					if p.Circle != "" && (strings.EqualFold(c.Name, p.Circle) || c.ID == p.Circle) {
						target = c.ID
						break
					}
				}
				if target == "" && len(p.JIDs) > 0 {
					target = p.JIDs[0]
				}
				id = addEdge(target, p.ID, "people_change", label, ch.ID, "confirmed", o)
				ids = []string{target, p.ID}
			}
		}
		if id != "" || ch.Kind == "fact" {
			if len(ids) == 0 {
				ids = []string{ch.SubjectID}
			}
			addEvent("change-"+ch.ID, "edge_added", id, o.By+" connected "+titleOf(nodes, ch.SubjectID)+": "+label, ids, o)
		}
	}
	// Aggregate metadata only: no message content is copied into the map.
	type volume struct {
		n, recent int
		last      time.Time
	}
	volumes := map[string]volume{}
	bump := func(id string, at time.Time) {
		v := volumes[id]
		if at.After(s.Hub.Now().Add(-30 * 24 * time.Hour)) {
			v.n++
		}
		if at.After(s.Hub.Now().Add(-24 * time.Hour)) {
			v.recent++
		}
		if at.After(v.last) {
			v.last = at
		}
		volumes[id] = v
	}
	rows, err := s.Hub.Messages.DB.Query("SELECT chat_jid,sender,timestamp FROM messages WHERE julianday(timestamp)<=julianday(?)", s.Hub.Now())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var j, sender string
		var at time.Time
		if err = rows.Scan(&j, &sender, &at); err != nil {
			rows.Close()
			return nil, err
		}
		bump(j, at)
		p := personByJID[j]
		if p == "" {
			p = personBySender[sender]
		}
		if p != "" {
			bump(p, at)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		for _, j := range c.Chats {
			v := volumes[j.ChatJID]
			d := volumes[c.ID]
			d.n += v.n
			d.recent += v.recent
			if v.last.After(d.last) {
				d.last = v.last
			}
			volumes[c.ID] = d
		}
		for _, t := range c.Topics {
			js := t.Chats
			if len(js) == 0 {
				js = JIDs(c)
			}
			for _, j := range js {
				v := volumes[j]
				d := volumes[t.ID]
				d.n += v.n
				d.recent += v.recent
				if v.last.After(d.last) {
					d.last = v.last
				}
				volumes[t.ID] = d
			}
		}
	}
	maxVolume := 1
	for _, v := range volumes {
		if v.n > maxVolume {
			maxVolume = v.n
		}
	}
	personMax := 1
	for _, n := range nodes {
		if n.Kind == "person" && volumes[n.ID].n > personMax {
			personMax = volumes[n.ID].n
		}
	}
	for _, n := range nodes {
		v := volumes[n.ID]
		denominator := maxVolume
		if n.Kind == "person" {
			denominator = personMax
		}
		n.Weight = float64(v.n) / float64(denominator)
		n.NewMessages = v.recent
		if !v.last.IsZero() {
			at := v.last
			n.LastActiveAt = &at
		}
		sort.Strings(n.Circles)
		out.Nodes = append(out.Nodes, *n)
		if n.Kind == "circle" || n.Kind == "chat" {
			out.Pulses = append(out.Pulses, MapPulse{n.ID, v.recent})
		}
	}
	for _, e := range edges {
		out.Edges = append(out.Edges, e)
		out.Summary.Connections++
		if e.Origin.ByKind == "agent" {
			out.Summary.ByAI++
		}
		if e.Origin.At.After(s.Hub.Now().Add(-7*24*time.Hour)) && !e.Origin.At.After(s.Hub.Now()) {
			out.Summary.ThisWeek++
			if e.Origin.ByKind == "agent" {
				out.Summary.AIThisWeek++
			}
		}
	}
	allEvents := []MapEvent{}
	for _, e := range events {
		allEvents = append(allEvents, e)
	}
	sort.Slice(allEvents, func(i, j int) bool {
		if !allEvents[i].At.Equal(allEvents[j].At) {
			return allEvents[i].At.After(allEvents[j].At)
		}
		return allEvents[i].ID < allEvents[j].ID
	})
	if len(allEvents) > 0 {
		e := allEvents[0]
		age := s.Hub.Now().Sub(e.At)
		when := "just now"
		if age >= 24*time.Hour {
			when = fmt.Sprintf("%d d ago", int(age.Hours()/24))
		} else if age >= time.Hour {
			when = fmt.Sprintf("%d h ago", int(age.Hours()))
		} else if age >= time.Minute {
			when = fmt.Sprintf("%d min ago", int(age.Minutes()))
		}
		out.Summary.Latest = e.Text + " · " + when
	}
	for _, e := range allEvents {
		if len(out.Events) < 60 {
			out.Events = append(out.Events, e)
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool { return out.Edges[i].ID < out.Edges[j].ID })
	sort.Slice(out.Pulses, func(i, j int) bool { return out.Pulses[i].ID < out.Pulses[j].ID })
	boundMap(&out)
	out.Cursor = "p1." + dbutil.Digest(key)[:16]
	s.shared().mapKey = key
	s.shared().mapValue = out
	if len(s.shared().mapHistory) >= 8 {
		s.shared().mapHistory = map[string]UnderstandingMap{}
	}
	if s.shared().mapHistory == nil {
		s.shared().mapHistory = map[string]UnderstandingMap{}
	}
	s.shared().mapHistory[out.Cursor] = out
	out.Events = mapEventsAfter(out.Events, since)
	return out, nil
}
func mapEventsAfter(xs []MapEvent, since time.Time) []MapEvent {
	out := []MapEvent{}
	for _, e := range xs {
		if e.At.After(since) {
			out = append(out, e)
		}
	}
	return out
}
func titleOf(nodes map[string]*MapNode, id string) string {
	if n := nodes[id]; n != nil {
		return n.Title
	}
	return ""
}
