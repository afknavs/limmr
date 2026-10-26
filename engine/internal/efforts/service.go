package efforts

import (
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Service struct {
	parent    *Service
	Hub       *hub.Service
	mu        sync.Mutex
	cache     map[string]cached
	published atomic.Pointer[readySnapshot]
	yield     func() // compute-lock protected; background work only
}

func (s *Service) shared() *Service {
	if s.parent != nil {
		return s.parent.shared()
	}
	return s
}
func (s *Service) WithHub(h *hub.Service) *Service { return &Service{Hub: h, parent: s.shared()} }

type cached struct {
	mr        int64
	hr        int64
	ar        int64
	minute    int64
	rowid     int64
	groups    *grouper
	ms        []model.Message
	source    []model.Message
	surfacing *surfacingContext
	revision  string
	efforts   []Effort
	messages  map[string]model.Message
	names     map[string]Entity
	actions   []model.Activity
	filtered  map[string]int
	cohesion  map[string]clusterQuality
	weak      WeakHistogram
}

type clusterQuality struct {
	size     int
	strong   bool
	terms    map[string]bool
	weakBins [6]string
}

// Visibility is applied before inference, including for the owner. An override
// with any inaccessible reference is withheld in full: its free text may name it.
func (s *Service) visible(a *model.Agent) (map[string]string, error) {
	cs, _, err := s.Hub.Visible(a)
	if err != nil {
		return nil, err
	}
	var settings model.Settings
	if err := s.Hub.Repo.Get("settings", "main", &settings); err != nil {
		return nil, err
	}
	hidden := settings.HiddenChats
	aliases, err := s.Hub.Messages.Aliases()
	if err != nil {
		return nil, err
	}
	canonical := func(j string) string {
		if v, ok := aliases[j]; ok {
			return v
		}
		return j
	}
	hiddenSet := map[string]bool{}
	for _, j := range hidden {
		hiddenSet[canonical(j)] = true
	}
	out := map[string]string{}
	for _, c := range cs {
		if !hiddenSet[canonical(c.JID)] {
			out[c.JID] = c.Name
		}
	}
	return out, nil
}
func (s *Service) snapshot(a *model.Agent) (cached, error) {
	return s.snapshotMode(a, false)
}
func (s *Service) snapshotMode(a *model.Agent, readyOnly bool) (cached, error) {
	if readyOnly && s.shared().published.Load() == nil {
		return cached{}, errNotReady
	}
	chats, err := s.visible(a)
	if err != nil {
		return cached{}, err
	}
	aliases, err := s.Hub.Messages.Aliases()
	if err != nil {
		return cached{}, err
	}
	ids := []string{}
	for j := range chats {
		ids = append(ids, j)
	}
	sort.Strings(ids)
	scopeParts := []string{}
	for _, id := range ids {
		scopeParts = append(scopeParts, id+"\x00"+chats[id])
	}
	scopeParts = append(scopeParts, "aliases\x00"+dbutil.JSON(aliases))
	scope := dbutil.Digest(strings.Join(scopeParts, "\n"))
	if a != nil {
		scope = a.ID + "\n" + scope
	}
	var mr, hr, ar, activityCount int64
	if err = s.Hub.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&mr); err != nil {
		return cached{}, err
	}
	if err = s.Hub.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&hr); err != nil {
		return cached{}, err
	}
	if err = s.Hub.Repo.DB.QueryRow("SELECT (SELECT COALESCE(MAX(id),0) FROM activity), (SELECT COUNT(*) FROM activity)").Scan(&ar, &activityCount); err != nil {
		return cached{}, err
	}
	rev := fmt.Sprintf("%d/%d/%d/%d/%d", mr, hr, ar, activityCount, s.Hub.Now().Unix()/60)
	if readyOnly {
		if p := s.shared().published.Load(); p != nil && p.scope == scope && p.data.revision == rev {
			return p.data, nil
		}
		return cached{}, errNotReady
	}
	if c, ok := s.shared().cache[scope]; ok && c.revision == rev {
		if a == nil {
			s.publish(scope, c)
		}
		return c, nil
	}
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return cached{}, err
	}
	circles, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return cached{}, err
	}
	rules := newQuality(ps, circles, aliases, s.Hub.Settings().Owner)
	directoryGroups, err := hub.List[model.Group](s.Hub.Repo, "groups")
	if err != nil {
		return cached{}, err
	}
	for jid, name := range chats {
		if strings.HasSuffix(jid, "@g.us") {
			continue
		}
		for _, w := range rawWords(name) {
			rules.names[w] = true
		}
		if strings.EqualFold(strings.TrimSpace(name), "You") {
			rules.self[jid] = true
			for alias, canonical := range aliases {
				if canonical == jid || alias == jid {
					rules.self[alias] = true
					rules.self[canonical] = true
				}
			}
		}
	}
	c := cached{revision: rev, mr: mr, hr: hr, ar: ar, minute: s.Hub.Now().Unix() / 60, messages: map[string]model.Message{}, efforts: []Effort{}, filtered: map[string]int{}}
	old, exists := s.shared().cache[scope]
	incremental := exists && old.hr == hr && old.groups != nil && old.surfacing != nil && (len(old.source) == 0 || !old.source[0].Timestamp.Before(s.Hub.Now().AddDate(0, 0, -90)))
	var fresh []model.Message
	load := func(after int64) (int, error) {
		rows, e := s.Hub.Messages.DB.Query(`SELECT rowid,id,chat_jid,COALESCE(sender,''),COALESCE(content,''),timestamp,COALESCE(is_from_me,0),COALESCE(filename,''),COALESCE(quoted_message_id,''),COALESCE(media_type,''),deleted_at FROM messages WHERE rowid>? AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?) ORDER BY julianday(timestamp),chat_jid,id`, after, s.Hub.Now().AddDate(0, 0, -90), s.Hub.Now())
		if e != nil {
			return 0, e
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var m model.Message
			var rowid int64
			if e = rows.Scan(&rowid, &m.ID, &m.ChatJID, &m.Sender, &m.Content, &m.Timestamp, &m.IsFromMe, &m.Filename, &m.QuotedMessageID, &m.MediaType, &m.DeletedAt); e != nil {
				return 0, e
			}
			n++
			if rowid > c.rowid {
				c.rowid = rowid
			}
			if _, ok := chats[m.ChatJID]; ok {
				fresh = append(fresh, m)
			}
		}
		return n, rows.Err()
	}
	if incremental {
		for k, v := range old.filtered {
			if k != "too-weak" {
				c.filtered[k] = v
			}
		}
		c.rowid = old.rowid
		if mr != old.mr {
			n, e := load(old.rowid)
			if e != nil {
				return c, e
			}
			if int64(n) != mr-old.mr || (len(fresh) > 0 && len(old.ms) > 0 && fresh[0].Timestamp.Before(old.ms[len(old.ms)-1].Timestamp)) {
				incremental = false
			}
		}
	}
	if !incremental {
		fresh = nil
		c.filtered = map[string]int{}
		c.rowid = 0
		if _, e := load(0); e != nil {
			return c, e
		}
	}
	// Invalidate before extending any cached indexes; cancellation later in
	// grouping must never reuse partially advanced context.
	delete(s.shared().cache, scope)
	if incremental {
		c.source = old.source
		if len(fresh) > 0 {
			c.source = append(append([]model.Message{}, old.source...), fresh...)
		}
		c.surfacing = old.surfacing
	} else {
		c.source = fresh
		c.surfacing = newSurfacingContext()
	}
	c.surfacing.add(fresh)
	rules.configure(c.surfacing, chats, directoryGroups, aliases, s.Hub.Settings().Owner, ps)
	// Newly recognized senders invalidate all their older evidence, including
	// explicit assignments. Refilter from the scoped source, not just arrivals.
	if incremental && !sameSet(rules.business, old.groups.rules.business) {
		incremental = false
		fresh = c.source
		c.filtered = map[string]int{}
	}
	eligible := make([]model.Message, 0, len(fresh))
	for _, m := range fresh {
		if reason := rules.reason(m); reason != "" {
			c.filtered[reason]++
		} else {
			eligible = append(eligible, m)
		}
	}
	fresh = eligible
	if !incremental {
		c.groups = newGrouper()
		c.ms = fresh
	} else {
		c.groups = old.groups
		c.ms = append(old.ms, fresh...)
		c.messages = old.messages
	}
	c.groups.rules = rules

	// Drop the cached pointer before extending its index: a cancelled query later
	// in this recompute must not leave a partially updated cache to reuse.
	delete(s.shared().cache, scope)
	ambient := ambientGroups(c.ms)
	if incremental && !sameSet(ambient, c.groups.ambient) {
		// Crossing the ambient threshold can invalidate previously accepted
		// chatter. Rebuild once; never leave old efforts behind in the cache.
		incremental = false
		c.groups = newGrouper()
		c.groups.rules = rules
		fresh = c.ms
	}
	c.groups.ambient = ambient
	c.groups.yield = s.shared().yield
	c.groups.addFiltered(fresh)
	c.groups.yield = nil
	messageIndex := c.ms
	if incremental {
		messageIndex = fresh
	}
	for _, m := range messageIndex {
		c.messages[key(ref(m))] = m
	}

	names := map[string]Entity{}
	for _, p := range ps {
		n := p.Label
		if n == "" {
			n = p.Name
		}
		for _, j := range p.JIDs {
			names[j] = Entity{p.ID, n}
		}
	}
	c.names = names
	briefs, err := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if err != nil {
		return c, err
	}
	latest := map[string]model.Brief{}
	for _, b := range briefs {
		if b.Version > latest[b.CircleID].Version {
			latest[b.CircleID] = b
		}
	}
	overrides, err := hub.List[override](s.Hub.Repo, "effort_edits")
	if err != nil {
		return c, err
	}
	sort.SliceStable(overrides, func(i, j int) bool { return overrides[i].Revision < overrides[j].Revision })
	edits := map[string]override{}
	assigned := map[string]string{}
	for _, o := range overrides {
		safe := true
		for _, r := range append(append([]Ref{}, o.Refs...), o.Excluded...) {
			if _, ok := c.messages[key(r)]; !ok {
				safe = false
			}
		}
		if !safe || !eligibleOverride(o, rules, c.messages, c.groups.ambient) {
			continue
		}
		edits[o.ID] = o
		if o.ReplacedBy == "" {
			for _, r := range o.Refs {
				assigned[key(r)] = o.ID
			}
		}
	}
	projected, rejected := rules.ownerClusters(c.groups.cs, c.groups.ambient)
	projected = mergeDirectFragments(projected)
	groups := consolidateCooperatively(projected, c.groups.ambient, s.shared().yield)
	c.weak = newWeakHistogram()
	c.cohesion = make(map[string]clusterQuality, len(groups)+len(rejected))
	for _, g := range rejected {
		first := key(ref(g.ms[0]))
		quality, reuse := old.cohesion[first]
		if !incremental || !reuse || quality.size != len(g.ms) || quality.strong {
			quality = clusterQuality{size: len(g.ms), weakBins: rejectionBins(g.ms, rules)}
		}
		c.cohesion[first] = quality
		c.filtered["too-weak"]++
		c.weak.addBins(quality.weakBins)
	}
	placed := map[string]bool{}
	byID := map[string][]model.Message{}
	byIDTerms := map[string]map[string]bool{}
	for gi, g := range groups {
		if gi%64 == 0 && s.shared().yield != nil {
			s.shared().yield()
		}
		first := key(ref(g.ms[0]))
		quality, reuse := old.cohesion[first]
		// Incremental mode guarantees append-only evidence and unchanged quality
		// rules. A merged/extended cluster changes size and must be checked again.
		if !incremental || !reuse || quality.size != len(g.ms) {
			ts := sharedTerms(g.ms, rules)
			quality = clusterQuality{size: len(g.ms), strong: len(ts) >= 2 && explicitThread(g.ms, c.groups.ambient), terms: ts}
			if !quality.strong {
				quality.weakBins = rejectionBins(g.ms, rules)
			}
		}
		c.cohesion[first] = quality
		if !quality.strong || !taskEvidence(g.ms) || !rules.involved(g.ms) {
			if quality.strong {
				quality.weakBins = rejectionBins(g.ms, rules)
			}
			c.filtered["too-weak"]++
			c.weak.addBins(quality.weakBins)
			continue
		}
		id := "effort-" + dbutil.Digest(first)[:24]
		if len(overrides) == 0 {
			byIDTerms[id] = quality.terms
		}
		for _, m := range g.ms {
			if dest, ok := assigned[key(ref(m))]; ok {
				byID[dest] = append(byID[dest], m)
				placed[key(ref(m))] = true
				continue
			}
			o := edits[id]
			excluded := false
			for _, r := range o.Excluded {
				if r == ref(m) {
					excluded = true
				}
			}
			if !excluded {
				target := id
				if o.ReplacedBy != "" {
					target = o.ReplacedBy
				}
				byID[target] = append(byID[target], m)
			}
		}
	}
	// Explicit attach/split can include messages that were not inferred as work.
	for k, id := range assigned {
		m := c.messages[k]
		if !placed[k] {
			byID[id] = append(byID[id], m)
		}
	}
	previous := map[string]Effort{}
	if incremental && old.hr == hr && old.minute == c.minute {
		for _, x := range old.efforts {
			previous[x.ID] = x
		}
	}
	settings := s.Hub.Settings()
	for id, items := range byID {
		if len(items) == 0 {
			continue
		}
		prior, hasPrior := previous[id]
		if hasPrior && len(prior.MessageRefs) == len(items) && !statusOverrideExpired(edits[id], prior.Chats, c.source) {
			oldEffort := prior
			oldEffort.Title, oldEffort.titleSource = oldEffort.baseTitle, oldEffort.baseTitleSource
			oldEffort.AgentActionRefs = []int64{}
			oldEffort.LastAt = items[0].Timestamp
			for _, m := range items {
				if m.Timestamp.After(oldEffort.LastAt) {
					oldEffort.LastAt = m.Timestamp
				}
			}
			c.efforts = append(c.efforts, oldEffort)
			continue
		}
		e := makeEffort(items, names, chats, circles, s.Hub.Now(), rules, byIDTerms[id])
		e.ID = id
		last := items[len(items)-1]
		if !last.IsFromMe && e.Status == "moving" {
			text := strings.ToLower(last.Content)
			owner := strings.ToLower(settings.Owner)
			if (owner != "" && words(text)[owner] || rules.addressed(last)) && (strings.Contains(text, "?") || taskRequest(last)) {
				e.Status = "on_you"
			}
			for _, b := range latest {
				for _, ask := range b.OpenAsks {
					if ask.ChatJID == last.ChatJID && !last.Timestamp.Before(ask.At) && (strings.Contains(last.Content, ask.Text) || strings.Contains(ask.Text, last.Content)) {
						e.Status = "on_you"
					}
				}
			}
			e.Summary = concreteSummary(items, e.Status, names)
		}
		if o, ok := edits[id]; ok {
			if o.ReplacedBy != "" {
				continue
			}
			update := o.Update
			if statusOverrideExpired(o, e.Chats, c.source) {
				update.Status = nil
			}
			apply(&e, update)
			e.titleAutomatic = o.Update.Title != nil && !deliberateTitle(o)
			if e.Due != nil {
				supported := false
				for _, m := range items {
					if d := messageDue(m); d != nil && d.Equal(*e.Due) {
						supported = true
					}
				}
				if !supported {
					e.Due = nil
				}
			}
			e.CreatedBy = o.By
		}
		e.Summary = cleanSummary(e.Summary, e.Status)
		e.baseTitle, e.baseTitleSource = e.Title, e.titleSource
		c.efforts = append(c.efforts, e)
	}
	dedupeTitles(c.efforts, c, rules)
	// Fetch every retained activity page, then attach by exact message refs.
	before := int64(0)
	for {
		xs, e := s.Hub.Activity(a, hub.ActivityFilter{Before: before, Limit: 500})
		if e != nil {
			return c, e
		}
		for _, x := range xs {
			safe := true
			for _, j := range x.ChatJIDs {
				if _, ok := chats[j]; !ok {
					safe = false
				}
			}
			if safe {
				c.actions = append(c.actions, x)
			}
		}
		if len(xs) < 500 {
			break
		}
		before = xs[len(xs)-1].ID
	}
	lookup := map[string][]int{}
	byChat := map[string][]int{}
	actionTerms := map[int]map[string]bool{}
	for i, e := range c.efforts {
		for _, chat := range e.Chats {
			byChat[chat.ID] = append(byChat[chat.ID], i)
		}
		for _, r := range e.MessageRefs {
			lookup[key(r)] = append(lookup[key(r)], i)
		}
	}
	for _, x := range c.actions {
		hits := map[int]bool{}
		for _, j := range x.ChatJIDs {
			for _, m := range x.MessageIDs {
				for _, i := range lookup[key(Ref{j, m})] {
					hits[i] = true
				}
			}
		}
		// Summaries and other purposeful actions may not carry message IDs.
		// Attach only a unique keyword match inside an explicitly referenced chat.
		if len(x.MessageIDs) == 0 {
			ts := terms(x.Note + " " + x.Query)
			candidates := map[int]bool{}
			for _, chat := range x.ChatJIDs {
				for _, i := range byChat[chat] {
					candidates[i] = true
				}
			}
			best, score, tied := -1, 0, false
			for i := range candidates {
				if actionTerms[i] == nil {
					actionTerms[i] = terms(c.efforts[i].Title + " " + c.efforts[i].search)
				}
				n := 0
				for t := range ts {
					if actionTerms[i][t] {
						n++
					}
				}
				if n > score {
					best, score, tied = i, n, false
				} else if n == score && n > 0 {
					tied = true
				}
			}
			if best >= 0 && !tied {
				hits[best] = true
			}
		}
		for i := range hits {
			c.efforts[i].AgentActionRefs = append(c.efforts[i].AgentActionRefs, x.ID)
			if x.At.After(c.efforts[i].LastAt) {
				c.efforts[i].LastAt = x.At
			}
		}
	}
	sort.Slice(c.efforts, func(i, j int) bool {
		a, b := c.efforts[i], c.efforts[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.LastAt.Equal(b.LastAt) {
			return a.ID < b.ID
		}
		return a.LastAt.After(b.LastAt)
	})
	if s.shared().cache == nil || len(s.shared().cache) > 16 {
		s.shared().cache = map[string]cached{}
	}
	s.shared().cache[scope] = c
	if a == nil {
		s.publish(scope, c)
	}
	return c, nil
}

// A new chat message also expires status edits on the incremental-cache path,
// even when that message does not become evidence for the effort itself.
func statusOverrideExpired(o override, chats []Entity, messages []model.Message) bool {
	if o.StatusThrough == nil {
		return false
	}
	for _, message := range messages {
		if !message.Timestamp.After(*o.StatusThrough) {
			continue
		}
		for _, chat := range chats {
			if message.ChatJID == chat.ID {
				return true
			}
		}
	}
	return false
}

func apply(e *Effort, u Update) {
	if u.Title != nil {
		e.Title = *u.Title
		e.titleSource = "assigned_title"
	}
	if u.Summary != nil {
		e.Summary = *u.Summary
	}
	if u.Status != nil {
		e.Status = *u.Status
	}
	if u.Pinned != nil {
		e.Pinned = *u.Pinned
	}
	if u.Hidden != nil {
		e.Hidden = *u.Hidden
	}
	if u.Due != nil {
		e.Due = nil
		if *u.Due != "" {
			d, _ := time.Parse(time.RFC3339, *u.Due)
			e.Due = &d
		}
	}
}
func (s *Service) List(a *model.Agent, f Filter) ([]Effort, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	return s.list(a, f)
}
func (s *Service) list(a *model.Agent, f Filter) ([]Effort, error) {
	c, e := s.snapshot(a)
	if e != nil {
		return nil, e
	}
	return filterSnapshot(c, f), nil
}
func filterSnapshot(c cached, f Filter) []Effort {
	out := []Effort{}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 500)
	for _, v := range c.efforts {
		if f.Person != "" {
			matches := false
			for _, p := range v.People {
				if p.ID == f.Person {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
		}
		if v.Hidden || (!f.IncludeUntitled && !goodTitle(v.Title)) || f.Status != "" && v.Status != f.Status || f.Circle != "" && !model.Contains(v.CircleIDs, f.Circle) {
			continue
		}
		if query := strings.ToLower(strings.TrimSpace(f.Query)); query != "" {
			var text strings.Builder
			text.WriteString(v.Title + " " + v.Summary + " " + v.search)
			for _, p := range v.People {
				text.WriteString(" " + p.Name)
			}
			for _, p := range v.Chats {
				text.WriteString(" " + p.Name)
			}
			if !strings.Contains(strings.ToLower(text.String()), query) {
				continue
			}
		}
		out = append(out, v)
		if len(out) >= limit {
			break
		}
	}
	return out
}
func (s *Service) Get(a *model.Agent, id string) (Detail, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	return s.get(a, id)
}
func (s *Service) get(a *model.Agent, id string) (Detail, error) {
	c, e := s.snapshot(a)
	if e != nil {
		return Detail{}, e
	}
	for _, v := range c.efforts {
		if v.ID != id || (v.Hidden && a != nil) {
			continue
		}
		d := Detail{Effort: v, Timeline: []Item{}}
		for _, r := range v.MessageRefs {
			m := c.messages[key(r)]
			if m.Sender == "" {
				if m.IsFromMe {
					m.Sender = "me"
				} else {
					m.Sender = "unknown"
				}
			}
			d.Timeline = append(d.Timeline, Item{ID: key(r), At: m.Timestamp, Message: &TimelineMessage{Message: m, Content: m.Content}})
		}
		for _, x := range c.actions {
			for _, i := range v.AgentActionRefs {
				if x.ID == i {
					v := x
					d.Timeline = append(d.Timeline, Item{ID: fmt.Sprint(x.ID), At: x.At, Action: &v})
					break
				}
			}
		}
		sort.Slice(d.Timeline, func(i, j int) bool {
			if d.Timeline[i].At.Equal(d.Timeline[j].At) {
				return d.Timeline[i].ID < d.Timeline[j].ID
			}
			return d.Timeline[i].At.After(d.Timeline[j].At)
		})
		if len(d.Timeline) > 50 {
			d.Timeline = d.Timeline[:50]
		}
		return d, nil
	}
	return Detail{}, model.ErrNotFound
}
func validate(u Update) error {
	if u.Title != nil && (!goodTitle(*u.Title) || len([]rune(*u.Title)) > 40) {
		return model.ErrInvalid
	}
	if u.Summary != nil && len([]rune(*u.Summary)) > 500 {
		return model.ErrInvalid
	}
	if u.Status != nil && !model.Contains([]string{"on_you", "waiting_on_them", "moving", "done", "dormant"}, *u.Status) {
		return model.ErrInvalid
	}
	if u.Due != nil && *u.Due != "" {
		if _, e := time.Parse(time.RFC3339, *u.Due); e != nil {
			return model.ErrInvalid
		}
	}
	return nil
}
func author(a *model.Agent) string {
	if a == nil {
		return "you"
	}
	return a.Name
}
func (s *Service) Update(a *model.Agent, id string, u Update) (Effort, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if e := validate(u); e != nil {
		return Effort{}, e
	}
	d, e := s.get(a, id)
	if e != nil {
		return Effort{}, e
	}
	if a != nil && !goodTitle(d.Title) && (u.Title != nil || u.Summary != nil) {
		owner, err := s.snapshot(nil)
		if err != nil {
			return Effort{}, err
		}
		visible, err := s.visible(a)
		if err != nil {
			return Effort{}, err
		}
		found := false
		for _, effort := range owner.efforts {
			if effort.ID != id {
				continue
			}
			found = true
			for _, chat := range effort.Chats {
				if _, ok := visible[chat.ID]; !ok {
					return Effort{}, model.ErrNotFound
				}
			}
		}
		if !found {
			return Effort{}, model.ErrNotFound
		}
	}
	if u.Due != nil && *u.Due != "" {
		requested, _ := time.Parse(time.RFC3339, *u.Due)
		supported := false
		snapshot, e := s.snapshot(a)
		if e != nil {
			return Effort{}, e
		}
		for _, r := range d.MessageRefs {
			if v := messageDue(snapshot.messages[key(r)]); v != nil && v.Equal(requested) {
				supported = true
			}
		}
		if !supported {
			return Effort{}, model.ErrInvalid
		}
	}
	if u.OnlyUnnamed {
		if goodTitle(d.Title) || d.Hidden {
			return d.Effort, nil
		}
		if u.ExpectedRevision != "" {
			c, err := s.snapshot(a)
			if err != nil {
				return Effort{}, err
			}
			if namingRevision(d.Effort, c) != u.ExpectedRevision {
				return d.Effort, nil
			}
		} else if u.ExpectedLastAt == nil || !d.LastAt.Equal(*u.ExpectedLastAt) {
			return d.Effort, nil
		}
	}
	o := override{ID: id, Refs: d.MessageRefs, By: author(a)}
	if e = s.Hub.Repo.Get("effort_edits", id, &o); e != nil && e != model.ErrNotFound {
		return Effort{}, e
	}
	if e = s.checkOverride(a, o); e != nil {
		return Effort{}, e
	}
	if e = s.Hub.Repo.DB.QueryRow("SELECT value+1 FROM p1_revision WHERE id=1").Scan(&o.Revision); e != nil {
		return Effort{}, e
	}
	o.Refs = append([]Ref{}, d.MessageRefs...)
	if u.Status != nil {
		through := s.Hub.Now().UTC()
		o.StatusThrough = &through
	}
	// Preserve title origin before a pin/status/hide edit changes the last actor.
	if o.Update.Title != nil && o.TitleOrigin == "" {
		o.TitleOrigin = titleOrigin(o)
	}
	if u.Title != nil {
		o.TitleOrigin = "user"
		if a != nil {
			o.TitleOrigin = "agent"
		}
		if u.OnlyUnnamed {
			o.TitleOrigin = "machine"
		}
	}
	// Merge only supplied fields, so later heuristic passes retain all agent edits.
	b, _ := json.Marshal(o.Update)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	b, _ = json.Marshal(u)
	var n map[string]any
	_ = json.Unmarshal(b, &n)
	for k, v := range n {
		m[k] = v
	}
	b, _ = json.Marshal(m)
	_ = json.Unmarshal(b, &o.Update)
	o.By = author(a)
	if u.OnlyUnnamed {
		o.By = "This Mac"
	}
	if e = s.Hub.Repo.Put("effort_edits", id, o); e != nil {
		return Effort{}, e
	}
	s.shared().cache = nil
	apply(&d.Effort, o.Update)
	d.titleAutomatic = o.Update.Title != nil && !deliberateTitle(o)
	d.CreatedBy = o.By
	return d.Effort, nil
}

// Do not merge hidden free-text fields into a projected, otherwise visible edit.
func (s *Service) checkOverride(a *model.Agent, o override) error {
	c, e := s.snapshot(a)
	if e != nil {
		return e
	}
	for _, r := range append(append([]Ref{}, o.Refs...), o.Excluded...) {
		if _, ok := c.messages[key(r)]; !ok {
			return model.ErrNotFound
		}
	}
	return nil
}

// Regroup commits the new assignment and source exclusions atomically.
func (s *Service) Regroup(a *model.Agent, action, id, other string, refs []Ref) (Effort, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	d, e := s.get(a, id)
	if e != nil {
		return Effort{}, e
	}
	dest := override{ID: id, Refs: append([]Ref{}, d.MessageRefs...), By: author(a)}
	if e = s.Hub.Repo.Get("effort_edits", id, &dest); e != nil && e != model.ErrNotFound {
		return Effort{}, e
	}
	if e = s.checkOverride(a, dest); e != nil {
		return Effort{}, e
	}
	if dest.Update.Title != nil && dest.TitleOrigin == "" {
		dest.TitleOrigin = titleOrigin(dest)
	}
	if e = s.Hub.Repo.DB.QueryRow("SELECT value+1 FROM p1_revision WHERE id=1").Scan(&dest.Revision); e != nil {
		return Effort{}, e
	}
	mutations := []hub.Mutation{}
	switch action {
	case "merge":
		if id == other {
			return Effort{}, model.ErrInvalid
		}
		x, e := s.get(a, other)
		if e != nil {
			return Effort{}, e
		}
		dest.Refs = append(append([]Ref{}, d.MessageRefs...), x.MessageRefs...)
		var stored override
		if e = s.Hub.Repo.Get("effort_edits", other, &stored); e == nil {
			if e = s.checkOverride(a, stored); e != nil {
				return Effort{}, e
			}
		} else if e != model.ErrNotFound {
			return Effort{}, e
		}
		old := override{ID: other, Refs: x.MessageRefs, By: author(a), ReplacedBy: id}
		mutations = append(mutations, hub.Mutation{Bucket: "effort_edits", ID: other, Value: old})
	case "attach", "split":
		if len(refs) == 0 || len(refs) > 500 {
			return Effort{}, model.ErrInvalid
		}
		c, e := s.snapshot(a)
		if e != nil {
			return Effort{}, e
		}
		for _, r := range refs {
			if _, ok := c.messages[key(r)]; !ok {
				return Effort{}, model.ErrNotFound
			}
			if action == "split" {
				found := false
				for _, v := range d.MessageRefs {
					if r == v {
						found = true
					}
				}
				if !found {
					return Effort{}, model.ErrInvalid
				}
			}
		}
		if action == "split" {
			if len(refs) >= len(d.MessageRefs) {
				return Effort{}, model.ErrInvalid
			}
			dest.Excluded = append(dest.Excluded, refs...)
			keep := []Ref{}
			for _, r := range d.MessageRefs {
				found := false
				for _, v := range refs {
					if r == v {
						found = true
					}
				}
				if !found {
					keep = append(keep, r)
				}
			}
			dest.Refs = keep
			mutations = append(mutations, hub.Mutation{Bucket: "effort_edits", ID: id, Value: dest})
			id = "effort-" + dbutil.ID()
			dest = override{ID: id, Refs: refs, By: author(a), Revision: dest.Revision}
		} else {
			dest.Refs = append(append([]Ref{}, d.MessageRefs...), refs...)
		}
	default:
		return Effort{}, model.ErrInvalid
	}
	dest.By = author(a)
	mutations = append(mutations, hub.Mutation{Bucket: "effort_edits", ID: id, Value: dest})
	if e = s.Hub.Repo.Apply(mutations...); e != nil {
		return Effort{}, e
	}
	s.shared().cache = nil
	x, e := s.get(a, id)
	return x.Effort, e
}
func (s *Service) CircleUpdates() ([]CircleUpdate, error) {
	xs, e := s.List(nil, Filter{Limit: 500})
	if e != nil {
		return nil, e
	}
	return s.circleUpdates(xs)
}
func (s *Service) circleUpdates(xs []Effort) ([]CircleUpdate, error) {
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	seen := map[string]time.Time{}
	if e = s.Hub.Repo.Get("effort_seen", "circles", &seen); e != nil && e != model.ErrNotFound {
		return nil, e
	}
	out := []CircleUpdate{}
	for _, c := range cs {
		if !c.Confirmed || c.Archived {
			continue
		}
		for _, x := range xs {
			// Seeing a shared purpose in one circle also acknowledges that same
			// evidence in its other circles; it must not rotate back as a new row.
			seenAt := seen[c.ID]
			for _, id := range x.CircleIDs {
				if seen[id].After(seenAt) {
					seenAt = seen[id]
				}
			}
			if model.Contains(x.CircleIDs, c.ID) && x.Status != "dormant" && goodTitle(x.Title) && (x.Status == "on_you" || dueSoon(x.Due, s.Hub.Now()) || x.LastAt.After(seenAt)) {
				out = append(out, CircleUpdate{Entity{c.ID, c.Name}, x.ID, x.Title, x.Status, x.Summary, x.Due, x.LastAt, x.LastAt.After(seenAt)})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Status == "on_you") != (b.Status == "on_you") {
			return a.Status == "on_you"
		}
		if a.Due != nil && b.Due == nil {
			return true
		}
		if a.Due == nil && b.Due != nil {
			return false
		}
		if a.Due != nil && !a.Due.Equal(*b.Due) {
			return a.Due.Before(*b.Due)
		}
		if !a.LastAt.Equal(b.LastAt) {
			return a.LastAt.After(b.LastAt)
		}
		return a.EffortID+a.Circle.ID < b.EffortID+b.Circle.ID
	})
	used := map[string]bool{}
	result := []CircleUpdate{}
	for _, x := range out {
		if !used[x.EffortID] && len(result) < 4 {
			result = append(result, x)
			used[x.EffortID] = true
		}
	}
	return result, nil
}
func (s *Service) Seen(through ...map[string]time.Time) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return e
	}
	seen := map[string]time.Time{}
	if e = s.Hub.Repo.Get("effort_seen", "circles", &seen); e != nil && e != model.ErrNotFound {
		return e
	}
	eligible := map[string]bool{}
	for _, c := range cs {
		if c.Confirmed && !c.Archived {
			eligible[c.ID] = true
		}
	}
	marks := map[string]time.Time{}
	if len(through) == 0 {
		for id := range eligible {
			marks[id] = s.Hub.Now()
		}
	} else {
		marks = through[0]
	}
	for id, at := range marks {
		if !eligible[id] || at.IsZero() || at.After(s.Hub.Now()) {
			return model.ErrInvalid
		}
	}
	for id, at := range marks {
		if at.After(seen[id]) {
			seen[id] = at
		}
	}
	return s.Hub.Repo.Put("effort_seen", "circles", seen)
}
