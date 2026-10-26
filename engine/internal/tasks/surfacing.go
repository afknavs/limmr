package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Extend this vocabulary as precision fixtures grow; no remote model is involved.
var deliverableWords = []string{"exam", "cv", "résumé", "resume", "application", "deadline", "due", "submit", "order", "book", "print", "fundraiser", "guide", "material"}

func topic(text string) string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range deliverableWords {
		for _, word := range words {
			if word == w {
				return w
			}
		}
	}
	return ""
}

// suggestedTitle names a chat-only suggestion the way the owner would say it:
// "Ravi's CV" for a DM or a group with one other known person, else "Group: Exam".
func suggestedTitle(chat, person, word string) string {
	switch word {
	case "":
		word = "ongoing work"
	case "cv":
		word = "CV"
	}
	if person != "" {
		return person + "'s " + word
	}
	r := []rune(word)
	return chat + ": " + string(unicode.ToUpper(r[0])) + string(r[1:])
}
func dismissalKeys(t Task) []string {
	out := []string{}
	if t.EffortID != "" {
		out = append(out, "effort:"+t.EffortID)
	}
	if t.Suggestion != nil {
		for _, id := range t.Suggestion.EffortIDs {
			out = append(out, "effort:"+id)
		}
	}
	// Keep chat/topic suppression after effort regrouping, renaming and task purging.
	key := topic(t.Title)
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(t.Title))
	}
	for _, ch := range t.Chats {
		out = append(out, "chat:"+ch.ID+":"+key)
	}
	return out
}

// Stable effort/chat-topic IDs let Y6 merge dismissals independently of task retention.
func (s *Service) storeDismissal(tx *sql.Tx, key, taskID string) error {
	data := map[string]any{"id": key, "task_id": taskID, "at": s.now().UTC()}
	_, err := tx.Exec("INSERT OR IGNORE INTO objects(bucket,id,data) VALUES('task_dismissals',?,?)", key, dbutil.JSON(data))
	return err
}

func (s *Service) suggestedEvent(tx *sql.Tx, r record) error {
	if err := s.event(tx, r, "suggested", "task_suggested", "Task suggested", map[string]string{"text": "Limmr suggested " + r.Title}); err != nil {
		return err
	}
	_, err := tx.Exec("INSERT INTO agent_updates(agent,at,kind,ref) SELECT id,?,'task_suggested',? FROM objects WHERE bucket='agents' AND json_extract(data,'$.preset')='own'", s.now().UTC().Format(time.RFC3339Nano), r.ID+":suggested")
	return err
}
func (s *Service) Keep(a *model.Agent, id string) (Task, error) { return s.resolve(a, id, "kept", "") }
func (s *Service) Dismiss(a *model.Agent, id, reason string) (Task, error) {
	if !length(reason, 1000) {
		return Task{}, model.ErrInvalid
	}
	return s.resolve(a, id, "not_a_task", reason)
}
func (s *Service) resolve(a *model.Agent, id, action, reason string) (Task, error) {
	if _, err := s.Get(a, id); err != nil {
		return Task{}, err
	}
	if a != nil && !a.Permissions.SaveSummaries {
		return Task{}, model.ErrForbidden
	}
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if r.Status != "suggested" {
			return model.ErrConflict
		}
		now := s.now()
		r.UpdatedAt = now
		who := "you"
		if a != nil {
			who = a.ID
		}
		if action == "kept" {
			r.Status = "active"
			r.ClosedAt = nil
			r.CloseReason = ""
		} else {
			r.Status = "dropped"
			r.ClosedAt = &now
			r.CloseReason = action
			if action == "not_a_task" {
				for _, key := range dismissalKeys(r.Task) {
					if err := s.storeDismissal(tx, key, r.ID); err != nil {
						return err
					}
				}
			}
		}
		r.NeedsYou = false
		r.NeedsYouReason = ""
		kind := action
		if action == "not_a_task" {
			kind = "dismissed"
		}
		return s.event(tx, *r, kind, kind, "Task "+kind, map[string]string{"by": who, "reason": reason})
	})
}

// Undo is owner-only and checks the archived snapshot before restoring a circle.
func (s *Service) Undo(id string) (Task, error) {
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if r.UndoChangeID == "" || !open(r.Task) {
			return model.ErrConflict
		}
		var raw string
		var ch hub.TaskSurfacingChange
		if err := tx.QueryRow("SELECT data FROM objects WHERE bucket='changes' AND id=?", r.UndoChangeID).Scan(&raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &ch); err != nil {
			return err
		}
		if ch.TaskID != id || !s.now().Before(ch.At.Add(24*time.Hour)) {
			return model.ErrConflict
		}
		if ch.Before != nil {
			if err := tx.QueryRow("SELECT data FROM objects WHERE bucket='circles' AND id=?", ch.Before.ID).Scan(&raw); err != nil {
				return err
			}
			var c model.Circle
			if err := json.Unmarshal([]byte(raw), &c); err != nil {
				return err
			}
			if dbutil.JSON(c) != dbutil.JSON(ch.After) {
				return model.ErrConflict
			}
			if _, err := tx.Exec("UPDATE objects SET data=? WHERE bucket='circles' AND id=?", dbutil.JSON(ch.Before), ch.Before.ID); err != nil {
				return err
			}
		}
		for _, key := range dismissalKeys(r.Task) {
			if err := s.storeDismissal(tx, key, id); err != nil {
				return err
			}
		}
		r.Status = "dropped"
		r.CloseReason = "owner"
		now := s.now()
		r.ClosedAt = &now
		r.UpdatedAt = now
		r.NeedsYou = false
		r.NeedsYouReason = ""
		r.UndoChangeID = ""
		if _, err := tx.Exec("DELETE FROM objects WHERE bucket='changes' AND id=?", ch.ID); err != nil {
			return err
		}
		return s.event(tx, *r, "undone", "undone", "Task tracking undone", map[string]string{"by": "you"})
	})
}

type candidate struct {
	effort efforts.Effort
	chat   model.Chat
}

// detect runs after List has completed effort grouping. Tick serializes this and the ten-minute gate.
func (s *Service) detect(ctx context.Context) error {
	if !s.lastDetection.IsZero() && s.now().Before(s.lastDetection.Add(10*time.Minute)) {
		return nil
	}
	if s.Efforts == nil || s.Hub.Messages == nil {
		return nil
	}
	es, err := s.Efforts.List(nil, efforts.Filter{Limit: 500, IncludeUntitled: true})
	if err != nil {
		return err
	}
	chats, err := s.Hub.Messages.Chats()
	if err != nil {
		return err
	}
	agents, err := hub.List[model.Agent](s.Hub.Repo, "agents")
	if err != nil {
		return err
	}
	groups, err := hub.List[model.Group](s.Hub.Repo, "groups")
	if err != nil {
		return err
	}
	people, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return err
	}
	memberships, err := hub.List[model.Membership](s.Hub.Repo, "memberships")
	if err != nil {
		return err
	}
	chatContext, err := s.chatContext()
	if err != nil {
		return err
	}
	byChat := map[string]model.Chat{}
	excluded := map[string]bool{}
	names := map[string]string{}
	for _, g := range groups {
		excluded[g.JID] = g.IsCommunity || g.MemberCount >= 100
	}
	for _, p := range people {
		name := p.Label
		if name == "" {
			name = p.Name
		}
		names[p.ID] = name
		// The owner's own chat carries their AIs' notifications, not shared work.
		if p.ID == "me" {
			for _, j := range p.JIDs {
				excluded[j] = true
			}
		}
		if p.Business || strings.Contains(strings.ToLower(p.Name), "bot") {
			for _, j := range p.JIDs {
				excluded[j] = true
			}
		}
	}
	for _, c := range chats {
		byChat[c.JID] = c
		name := strings.ToLower(c.Name)
		if strings.HasSuffix(c.JID, "@broadcast") || strings.HasSuffix(c.JID, "@newsletter") || strings.Contains(name, "newsletter") || strings.Contains(name, "bot") || !s.Hub.Current(c) {
			excluded[c.JID] = true
		}
	}
	candidates := []candidate{}
	effortChats := map[string]bool{}
	for _, e := range es {
		if e.Status == "done" || e.Status == "dormant" || e.Hidden || e.LastAt.Before(s.Hub.CurrentSince()) {
			for _, ch := range e.Chats {
				effortChats[ch.ID] = true
			}
			continue
		}
		candidates = append(candidates, candidate{effort: e})
		for _, ch := range e.Chats {
			effortChats[ch.ID] = true
		}
	}
	for _, ch := range chats {
		if !effortChats[ch.JID] {
			candidates = append(candidates, candidate{chat: ch})
		}
	}
	for _, c := range candidates {
		if err = ctx.Err(); err != nil {
			return err
		}
		e := c.effort
		cs := e.Chats
		if e.ID == "" {
			cs = []Entity{{ID: c.chat.JID, Name: c.chat.Name}}
		}
		if len(cs) == 0 {
			continue
		}
		eligible := true
		for _, ch := range cs {
			visible := false
			for _, a := range agents {
				if a.Enabled && a.Permissions.ReadMessages && s.Hub.CanChat(&a, ch.ID) {
					visible = true
				}
			}
			if excluded[ch.ID] || !visible || s.Hub.HiddenChat(ch.ID) {
				eligible = false
			}
		}
		if !eligible {
			continue
		}
		score := 0.0
		signals := []string{}
		var sender *model.Agent
		var bestCount int
		text := e.Title + " " + e.Summary
		days := map[string]bool{}
		for _, ch := range cs {
			rows, qerr := s.Hub.Messages.DB.Query("SELECT id,COALESCE(content,''),timestamp,is_from_me FROM messages WHERE chat_jid=? AND deleted_at IS NULL AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?)", ch.ID, s.now().AddDate(0, 0, -14), s.now())
			if qerr != nil {
				return qerr
			}
			ms := []model.Message{}
			for rows.Next() {
				var m model.Message
				m.ChatJID = ch.ID
				if err = rows.Scan(&m.ID, &m.Content, &m.Timestamp, &m.IsFromMe); err != nil {
					break
				}
				ms = append(ms, m)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				return err
			}
			counts := map[string]int{}
			for _, m := range ms {
				attributed, aerr := s.Hub.Attribute(m)
				if aerr != nil {
					return aerr
				}
				inEffort := e.ID == ""
				for _, ref := range e.MessageRefs {
					inEffort = inEffort || ref.ChatJID == ch.ID && ref.MessageID == m.ID
				}
				if inEffort {
					text += " " + m.Content
					if m.IsFromMe && attributed.Author.Type == "owner" {
						days[m.Timestamp.UTC().Format("2006-01-02")] = true
					}
				}
				// Count confirmed recorded sends, not reserved send_authors rows or contact claims.
				for _, a := range agents {
					if !a.Enabled || a.Preset != "own" || !a.Permissions.SaveSummaries || !s.visible(&a, Task{Chats: cs}) {
						continue
					}
					if attributed.Author.Type == "agent" && attributed.Author.AgentID == a.ID && m.IsFromMe {
						counts[a.ID]++
					}
				}
			}
			for i := range agents {
				a := &agents[i]
				if counts[a.ID] >= 2 && counts[a.ID] > bestCount {
					sender = a
					bestCount = counts[a.ID]
				}
			}
		}
		if sender != nil {
			score += 0.5
			signals = append(signals, fmt.Sprintf("%s has sent %d messages here in the last 14 days", sender.Name, bestCount))
		}
		if len(days) >= 3 {
			score += 0.25
			signals = append(signals, "You worked on this on at least 3 different days")
		}
		// Derived status is supported by the effort's latest message; do not age it from FirstAt.
		if (e.Status == "on_you" || e.Status == "waiting_on_them") && !e.LastAt.After(s.now().AddDate(0, 0, -2)) {
			score += 0.2
			signals = append(signals, "This has been waiting for at least 2 days")
		}
		word := topic(text)
		if word != "" {
			score += 0.15
			signals = append(signals, "The conversation mentions "+word)
		}
		small := true
		for _, ch := range cs {
			if byChat[ch.ID].IsGroup || strings.HasSuffix(ch.ID, "@g.us") {
				known := false
				for _, g := range groups {
					if g.JID == ch.ID {
						known = g.MemberCount > 0 && g.MemberCount <= 8
					}
				}
				small = small && known
			}
		}
		if small {
			score += 0.1
			signals = append(signals, "This is a small group or direct chat")
		}
		if score+1e-9 < 0.6 {
			continue
		}
		title := e.Title
		if title == "" {
			person := ""
			if strings.HasSuffix(cs[0].ID, "@g.us") {
				others := []string{}
				for _, m := range memberships {
					if m.Group == cs[0].ID && m.Person != "me" && names[m.Person] != "" {
						others = append(others, names[m.Person])
					}
				}
				if len(others) == 1 {
					person = others[0]
				}
			} else if n := byChat[cs[0].ID].Name; n != "" && !strings.HasPrefix(n, "+") {
				person = n
			}
			title = suggestedTitle(byChat[cs[0].ID].Name, person, word)
		}
		sig := Suggestion{Signals: signals, Confidence: min(1, score), DetectedAt: s.now(), EffortIDs: []string{}}
		if e.ID != "" {
			sig.EffortIDs = append(sig.EffortIDs, e.ID)
		}
		in := Create{Title: short(title, 60), Kind: "long", EffortID: e.ID, Chats: cs, People: mergeEntities(e.People, chatContext.peopleFor(cs))}
		blocked, berr := s.covered(Task{Title: in.Title, Chats: cs, EffortID: e.ID, Suggestion: &sig})
		if berr != nil {
			return berr
		}
		if blocked {
			continue
		}
		auto := sender != nil && s.mayAutomate(ctx)
		var actor *model.Agent
		if auto {
			actor = sender
		}
		if _, err = s.create(actor, in, &sig, auto); err != nil {
			return err
		}
	}
	s.lastDetection = s.now()
	return nil
}
func (s *Service) covered(t Task) (bool, error) {
	xs, err := s.records()
	if err != nil {
		return false, err
	}
	for _, r := range xs {
		if !open(r.Task) && r.Status != "suggested" {
			continue
		}
		if t.EffortID != "" && r.EffortID == t.EffortID {
			return true, nil
		}
		for _, a := range t.Chats {
			for _, b := range r.Chats {
				if a.ID == b.ID {
					return true, nil
				}
			}
		}
	}
	keys := dismissalKeys(t)
	sort.Strings(keys)
	for _, key := range keys {
		var n int
		if err = s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='task_dismissals' AND id=?", key).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// SuggestForAgent is for trusted activity detection and invented demo fixtures.
// A denied lease still surfaces a suggestion but cannot start tracking automatically.
func (s *Service) SuggestForAgent(ctx context.Context, a *model.Agent, in Create, sig Suggestion) (Task, error) {
	if a == nil || a.Preset != "own" {
		return Task{}, model.ErrForbidden
	}
	auto := s.mayAutomate(ctx)
	if !auto {
		return s.Suggest(nil, in, sig)
	}
	return s.create(a, in, &sig, true)
}

// mayAutomate asks the task-level hook first (tests), then the sync lease on the hub.
// An engine that has never paired holds its own lease, so both default to true.
func (s *Service) mayAutomate(ctx context.Context) bool {
	if s.MayRunAutomations != nil {
		return s.MayRunAutomations(ctx)
	}
	if s.Hub != nil && s.Hub.MayRunAutomations != nil {
		return s.Hub.MayRunAutomations(ctx)
	}
	return true
}
