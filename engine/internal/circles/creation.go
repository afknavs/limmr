package circles

import (
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"sort"
	"strings"
	"time"
	"unicode"
)

type AutoChange struct {
	ID            string            `json:"id"`
	CircleID      string            `json:"circle_id"`
	Name          string            `json:"name"`
	Agent         string            `json:"agent"`
	Action        string            `json:"action"`
	At            time.Time         `json:"at"`
	Count         int               `json:"count"`
	ChangeIDs     []string          `json:"change_ids"`
	PeopleIDs     []string          `json:"people_ids,omitempty"`
	PeopleChanges map[string]string `json:"people_changes,omitempty"`
	RunID         string            `json:"run_id,omitempty"`
	ByKind        string            `json:"by_kind,omitempty"`
}

// Internal snapshots are kept separate from the public Home payload.
type savedAuto struct {
	Change      AutoChange
	Before      *model.Circle
	After       model.Circle
	AddedPeople []model.CirclePerson `json:",omitempty"`
}

func (s *Service) creationRules(a model.Agent, c model.Circle, why string) (string, error) {
	cs, e := s.List(&a)
	if e != nil {
		return "", e
	}
	for _, old := range cs {
		if old.ID == c.ID || !old.Confirmed || old.Archived {
			continue
		}
		ch, ps := 0, 0
		for _, j := range c.Chats {
			if model.Contains(JIDs(old), j.ChatJID) {
				ch++
			}
		}
		for _, p := range c.People {
			for _, q := range old.People {
				if q.PersonID == p.PersonID {
					ps++
					break
				}
			}
		}
		if len(c.Chats) > 0 && ch*2 >= len(c.Chats) || len(c.People) > 0 && ps*2 >= len(c.People) {
			return "", guardError{fmt.Sprintf("Add these to %s with add_to_circle instead.", old.Name), model.ErrConflict}
		}
	}
	named := 0
	for _, p := range c.People {
		var person model.Person
		if p.PersonID != "me" && s.Hub.Repo.Get("people", p.PersonID, &person) == nil && model.Named(person) {
			named++
		}
	}
	chats, _, e := s.Hub.Visible(&a)
	if e != nil {
		return "", e
	}
	if c.Kind == "project" && (len(c.Chats) < 2 || named < 3) {
		return "One-off projects are tasks: use task create.", nil
	}
	if len(c.Chats) == 1 && c.Kind != "friends" && c.Kind != "family" {
		for _, chat := range chats {
			if chat.JID == c.Chats[0].ChatJID && !chat.IsGroup {
				return "Add a single direct chat to Close people instead of its own circle.", nil
			}
		}
	}
	if len(c.Chats) < 2 && named < 2 && c.Kind != "friends" && c.Kind != "family" {
		return "A single DM belongs in Close people. Add two chats or two named people, or explain the project's goal.", nil
	}
	current := false
	evidence := false
	for _, j := range c.Chats {
		for _, chat := range chats {
			if chat.JID == j.ChatJID {
				if s.Hub.Current(chat) {
					current = true
				}
				if chat.Name != "" && strings.Contains(strings.ToLower(why), strings.ToLower(chat.Name)) {
					evidence = true
				}
			}
		}
	}
	if !current {
		return "Choose a chat active in the current window.", nil
	}
	if !evidence { // A returned, recent message ID is valid evidence too.
		rows, e := s.Hub.Messages.DB.Query("SELECT id,chat_jid FROM messages WHERE julianday(timestamp)>=julianday(?)", s.Hub.CurrentSince())
		if e != nil {
			return "", e
		}
		for rows.Next() {
			var id, j string
			if e = rows.Scan(&id, &j); e != nil {
				rows.Close()
				return "", e
			}
			if model.Contains(JIDs(c), j) && len(id) > 0 && strings.Contains(why, id) {
				evidence = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return "", e
		}
	}
	if !evidence || len(why) > 2000 {
		return "Name the chats or recent message IDs in why.", nil
	}
	letters, digits := 0, 0
	for _, r := range c.Name {
		if unicode.IsLetter(r) {
			letters++
		}
		if unicode.IsDigit(r) {
			digits++
		}
	}
	if letters < 2 || digits > 0 || strings.ContainsAny(c.Name, "_@/") || strings.Contains(strings.ToLower(c.Name), "community") || c.Name == strings.ToUpper(c.Name) {
		return "Use a plain human name for a lasting life area.", nil
	}
	n := 0
	all, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return "", e
	}
	for _, old := range all {
		if old.Confirmed && !old.Archived {
			n++
		}
	}
	if n >= s.Hub.Settings().MaxCircles {
		return "Ask the owner before adding more circles.", nil
	}
	return "", nil
}
func (s *Service) saveAuto(a model.Agent, before *model.Circle, c model.Circle, action string) error {
	// Undo records are ephemeral; don't grow the polled Home path with lifetime history.
	if _, e := s.Hub.Repo.DB.Exec("DELETE FROM objects WHERE bucket='auto_circle_changes' AND julianday(json_extract(data,'$.Change.at'))<=julianday(?)", s.Hub.Now().Add(-24*time.Hour)); e != nil {
		return e
	}
	v := AutoChange{ID: dbutil.ID(), CircleID: c.ID, Name: c.Name, Agent: a.Name, Action: action, At: s.Hub.Now().UTC()}
	if e := s.Hub.Repo.Apply(hub.Mutation{Bucket: "circles", ID: c.ID, Value: c}, hub.Mutation{Bucket: "auto_circle_changes", ID: v.ID, Value: savedAuto{Change: v, Before: before, After: c}}); e != nil {
		return e
	}
	return s.Hub.Record(model.Activity{AgentID: a.ID, Kind: "people_write", Tool: "circle_auto_" + action, ChatJIDs: JIDs(c), Count: 1, Note: a.Name + " " + action + " " + c.Name + " · Undo available for 24 hours"})
}
func placementText(n int, name string) string {
	return fmt.Sprintf("Limmr placed %d people in %s", n, name)
}

func (s *Service) AutoChanges() ([]AutoChange, error) {
	// Filter before decoding, but group before imposing any Home row limit.
	rows, e := s.Hub.Repo.DB.Query("SELECT data FROM objects WHERE bucket='auto_circle_changes' AND julianday(json_extract(data,'$.Change.at'))>julianday(?)", s.Hub.Now().Add(-24*time.Hour))
	if e != nil {
		return nil, e
	}
	xs := []savedAuto{}
	for rows.Next() {
		var raw string
		var v savedAuto
		if e = rows.Scan(&raw); e == nil {
			e = json.Unmarshal([]byte(raw), &v)
		}
		if e != nil {
			rows.Close()
			return nil, e
		}
		xs = append(xs, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return nil, e
	}
	exists := map[string]bool{}
	for _, c := range cs {
		exists[c.ID] = true
	}
	sort.Slice(xs, func(i, j int) bool {
		a, b := xs[i].Change, xs[j].Change
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		// A batch shares a timestamp; keep its circle/actor together at ties.
		if a.CircleID != b.CircleID {
			return a.CircleID < b.CircleID
		}
		if a.Agent != b.Agent {
			return a.Agent < b.Agent
		}
		if a.RunID != b.RunID {
			return a.RunID < b.RunID
		}
		return a.ID > b.ID
	})
	out := []AutoChange{}
	oldest := []time.Time{} // per group, for the legacy 30-minute window
	for _, v := range xs {
		if !exists[v.Change.CircleID] {
			continue
		}
		ch := v.Change
		ch.Count = max(1, ch.Count)
		ch.ChangeIDs = []string{ch.ID}
		if len(v.AddedPeople) > 0 {
			ch.Count = len(v.AddedPeople)
			ch.PeopleIDs = nil
			ch.PeopleChanges = map[string]string{}
			for _, p := range v.AddedPeople {
				ch.PeopleIDs = append(ch.PeopleIDs, p.PersonID)
				ch.PeopleChanges[p.PersonID] = ch.ID
			}
		}
		// Match any open group, not just the last one: legacy runs interleaved two circles change by change.
		merged := false
		for i := len(out) - 1; i >= 0 && !merged; i-- {
			prev := &out[i]
			sameRun := ch.RunID != "" && ch.RunID == prev.RunID || ch.RunID == "" && prev.RunID == "" && oldest[i].Sub(ch.At) <= 30*time.Minute
			if prev.CircleID == ch.CircleID && prev.Agent == ch.Agent && prev.Action == ch.Action && prev.ByKind == ch.ByKind && sameRun {
				prev.Count += ch.Count
				prev.ChangeIDs = append(prev.ChangeIDs, ch.ID)
				prev.PeopleIDs = append(prev.PeopleIDs, ch.PeopleIDs...)
				if prev.PeopleChanges == nil && len(ch.PeopleChanges) > 0 {
					prev.PeopleChanges = map[string]string{}
				}
				for p, id := range ch.PeopleChanges {
					prev.PeopleChanges[p] = id
				}
				oldest[i] = ch.At
				merged = true
			}
		}
		if merged {
			continue
		}
		out = append(out, ch)
		oldest = append(oldest, ch.At)
	}
	return out, nil
}

func (s *Service) UndoAuto(id string) error { return s.UndoAutoGroup([]string{id}) }

// Plan both single and grouped undo with the same conflict checks. Nothing is
// written until every member passes, so a conflict cannot partly undo a group.
func (s *Service) undoAutoCircle(v savedAuto, c model.Circle) (*model.Circle, error) {
	if !s.Hub.Now().Before(v.Change.At.Add(24 * time.Hour)) {
		return nil, model.ErrConflict
	}
	if len(v.AddedPeople) > 0 {
		added := map[string]model.CirclePerson{}
		for _, p := range v.AddedPeople {
			added[p.PersonID] = p
		}
		remaining := []model.CirclePerson{}
		for _, p := range c.People {
			if old, ok := added[p.PersonID]; ok {
				now, _ := json.Marshal(p)
				then, _ := json.Marshal(old)
				if string(now) != string(then) {
					return nil, guardError{"This placement has changed since. Review it before undoing.", model.ErrConflict}
				}
			} else {
				remaining = append(remaining, p)
			}
		}
		c.People = remaining
		c.UpdatedAt = s.Hub.Now().UTC()
		return &c, nil
	}
	now, _ := json.Marshal(c)
	then, _ := json.Marshal(v.After)
	if string(now) != string(then) {
		return nil, guardError{"This circle has changed since. Review it before undoing.", model.ErrConflict}
	}
	return v.Before, nil
}

type Addition struct {
	ID       string   `json:"id"`
	CircleID string   `json:"circle_id"`
	Chats    []string `json:"chats"`
	People   []string `json:"people"`
	Why      string   `json:"why"`
	AgentID  string   `json:"agent_id"`
}

func (s *Service) appendTo(a *model.Agent, c model.Circle, chats, people []string) (model.Circle, error) {
	// Copy before appending so an Undo snapshot never shares a backing array.
	b, _ := json.Marshal(c)
	_ = json.Unmarshal(b, &c)
	for _, j := range chats {
		if !model.Contains(JIDs(c), j) {
			c.Chats = append(c.Chats, model.CircleChat{ChatJID: j, Origin: s.origin(a)})
		}
	}
	for _, id := range people {
		found := false
		for _, p := range c.People {
			found = found || p.PersonID == id
		}
		if !found {
			c.People = append(c.People, model.CirclePerson{PersonID: id, Source: a.ID, Origin: s.origin(a)})
		}
	}
	c.UpdatedAt = s.Hub.Now().UTC()
	return c, s.validate(a, &c)
}
func (s *Service) AddTo(a model.Agent, id string, chats, people []string, why string) (any, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if len(chats) > 50 || len(people) > 50 || len(chats)+len(people) == 0 {
		return nil, model.ErrInvalid
	}
	c, e := s.Circle(&a, id)
	if e != nil {
		return nil, e
	}
	raw, e := s.raw(id)
	if e != nil {
		return nil, e
	}
	if !c.Confirmed || len(c.Chats) != len(raw.Chats) {
		return nil, model.ErrForbidden
	}
	next, e := s.appendTo(&a, raw, chats, people)
	if e != nil {
		return nil, e
	}
	// Evidence must name a new chat/person; no stale-history additions from an AI.
	evidence := false
	for _, j := range chats {
		all, _, err := s.Hub.Visible(&a)
		if err != nil {
			return nil, err
		}
		for _, v := range all {
			if v.JID == j && s.Hub.Current(v) && strings.Contains(strings.ToLower(why), strings.ToLower(v.Name)) {
				evidence = true
			}
		}
	}
	for _, id := range people {
		var p model.Person
		if s.Hub.Repo.Get("people", id, &p) == nil && model.Named(p) && strings.Contains(strings.ToLower(why), strings.ToLower(p.Name)) {
			evidence = true
		}
	}
	if strings.TrimSpace(why) == "" || len(why) > 2000 {
		return nil, model.ErrInvalid
	}
	if a.Preset == "own" && evidence && s.Hub.Settings().AutoCirclesEnabled() {
		return next, s.saveAuto(a, &raw, next, "updated")
	}
	var pending int
	if e := s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='circle_additions' AND json_extract(data,'$.agent_id')=?", a.ID).Scan(&pending); e != nil {
		return nil, e
	}
	if pending >= 8 {
		return nil, guardError{"Ask the owner to review the suggested additions first.", model.ErrConflict}
	}
	v := Addition{dbutil.ID(), id, chats, people, why, a.ID}
	return v, s.Hub.Repo.Put("circle_additions", v.ID, v)
}
func (s *Service) ReviewAddition(id string, keep bool) (any, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var v Addition
	if e := s.Hub.Repo.Get("circle_additions", id, &v); e != nil {
		return nil, e
	}
	ms := []hub.Mutation{{Bucket: "circle_additions", ID: id, Delete: true}}
	if keep {
		c, e := s.raw(v.CircleID)
		if e != nil {
			return nil, e
		}
		if v.AgentID == "structure" {
			if !c.Confirmed || c.Archived {
				return nil, model.ErrConflict
			}
			for _, id := range v.People {
				found := false
				for _, p := range c.People {
					found = found || p.PersonID == id
				}
				if !found {
					c.People = append(c.People, model.CirclePerson{PersonID: id, Source: "you", Note: v.Why, Origin: s.origin(nil)})
				}
			}
			c.UpdatedAt = s.Hub.Now().UTC()
			if e = s.validate(nil, &c); e != nil {
				return nil, e
			}
		} else {
			a, err := s.Hub.Agent(v.AgentID)
			if err != nil {
				return nil, err
			}
			c, e = s.appendTo(&a, c, v.Chats, v.People)
		}
		if e != nil {
			return nil, e
		}
		ms = append(ms, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c})
	}
	return map[string]bool{"ok": true}, s.Hub.Repo.Apply(ms...)
}

func (s *Service) Additions() ([]Addition, error) {
	xs, e := hub.List[Addition](s.Hub.Repo, "circle_additions")
	if e != nil {
		return nil, e
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].ID < xs[j].ID })
	return xs, nil
}

// Newest first unwinds legacy full-circle snapshots too. One mutation set keeps
// group undo atomic, including when a later owner edit prevents an undo.
func (s *Service) UndoAutoGroup(ids []string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if len(ids) == 0 || len(ids) > 10000 {
		return model.ErrInvalid
	}
	xs := []savedAuto{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var v savedAuto
		if e := s.Hub.Repo.Get("auto_circle_changes", id, &v); e != nil {
			return e
		}
		xs = append(xs, v)
	}
	sort.SliceStable(xs, func(i, j int) bool {
		if !xs[i].Change.At.Equal(xs[j].Change.At) {
			return xs[i].Change.At.After(xs[j].Change.At)
		}
		return len(xs[i].After.People)+len(xs[i].After.Chats) > len(xs[j].After.People)+len(xs[j].After.Chats)
	})
	circles := map[string]*model.Circle{}
	ms := []hub.Mutation{}
	for _, v := range xs {
		c, ok := circles[v.Change.CircleID]
		if !ok {
			raw, e := s.raw(v.Change.CircleID)
			if e != nil {
				return e
			}
			c = &raw
		}
		if c == nil {
			return model.ErrConflict
		}
		next, e := s.undoAutoCircle(v, *c)
		if e != nil {
			return e
		}
		circles[v.Change.CircleID] = next
		ms = append(ms, hub.Mutation{Bucket: "auto_circle_changes", ID: v.Change.ID, Delete: true})
	}
	for id, c := range circles {
		if c == nil {
			ms = append(ms, hub.Mutation{Bucket: "circles", ID: id, Delete: true})
		} else {
			ms = append(ms, hub.Mutation{Bucket: "circles", ID: id, Value: *c})
		}
	}
	if e := s.Hub.Repo.Apply(ms...); e != nil {
		return e
	}
	return s.Hub.Record(model.Activity{AgentID: "you", Kind: "people_write", Tool: "undo_circle_change", Count: len(xs)})
}

// ProjectTask converts undersized project proposals before any circle is stored.
func (s *Service) ProjectTask(a model.Agent, c model.Circle, why string) (string, error) {
	if c.Kind != "project" {
		return "", nil
	}
	named := map[string]bool{}
	chats := map[string]bool{}
	for _, p := range c.People {
		var person model.Person
		if p.PersonID != "me" && s.Hub.Repo.Get("people", p.PersonID, &person) == nil && model.Named(person) {
			named[p.PersonID] = true
		}
	}
	for _, ch := range c.Chats {
		chats[ch.ChatJID] = true
	}
	if len(chats) >= 2 && len(named) >= 3 {
		return "", nil
	}
	if len(c.Chats) == 0 || len(c.Chats) > 50 || len(c.People) > 50 || strings.TrimSpace(why) == "" || len(why) > 2000 {
		return "", model.ErrInvalid
	}
	current := false
	visible, _, err := s.Hub.Visible(&a)
	if err != nil {
		return "", err
	}
	for _, ch := range visible {
		if chats[ch.JID] && s.Hub.Current(ch) {
			current = true
		}
	}
	if !current {
		return "", model.ErrInvalid
	}
	in := tasks.Create{Title: c.Name, Kind: "long", Chats: []tasks.Entity{}, People: []tasks.Entity{}}
	for _, ch := range c.Chats {
		in.Chats = append(in.Chats, tasks.Entity{ID: ch.ChatJID})
	}
	for _, p := range c.People {
		var person model.Person
		if err = s.Hub.Repo.Get("people", p.PersonID, &person); err != nil {
			return "", err
		}
		in.People = append(in.People, tasks.Entity{ID: p.PersonID, Name: person.Name})
	}
	sig := tasks.Suggestion{Signals: []string{why}, Confidence: 1, DetectedAt: s.Hub.Now(), EffortIDs: []string{}}
	svc := &tasks.Service{Hub: s.Hub}
	task, err := svc.Suggest(&a, in, sig)
	return task.ID, err
}
