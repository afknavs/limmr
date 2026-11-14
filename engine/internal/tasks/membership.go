package tasks

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/wa"
)

// Membership metadata is additive tasks.data JSON. Missing sources are inferred
// only for reads; legacy membership must never be assumed safe to auto-remove.
type Membership struct {
	Chats  map[string]string `json:"chats,omitempty"`
	People map[string]string `json:"people,omitempty"`
}
type RemovedMembers struct {
	Chats  []string `json:"chats,omitempty"`
	People []string `json:"people,omitempty"`
}
type MemberBefore struct {
	ID      string  `json:"id"`
	Entity  *Entity `json:"entity,omitempty"`
	Source  string  `json:"source,omitempty"`
	Removed bool    `json:"removed"`
}
type MembershipChange struct {
	OwnerRequest     *AskedByOwner   `json:"owner_request,omitempty"`
	Source           string          `json:"source,omitempty"`
	ID               string          `json:"id"`
	TaskID           string          `json:"task_id"`
	Kind             string          `json:"kind"`
	At               time.Time       `json:"at"`
	Text             string          `json:"text"`
	Reason           string          `json:"reason,omitempty"`
	Chats            []MemberBefore  `json:"chats,omitempty"`
	People           []MemberBefore  `json:"people,omitempty"`
	AddedRefs        []Ref           `json:"added_refs,omitempty"`
	Rule             string          `json:"rule,omitempty"`
	Evidence         any             `json:"evidence,omitempty"`
	Before           json.RawMessage `json:"before,omitempty"`
	SuggestionBefore json.RawMessage `json:"suggestion_before,omitempty"`
	SuggestionAfter  json.RawMessage `json:"suggestion_after,omitempty"`
}

func (t *Task) membershipMaps() {
	if t.Membership == nil {
		t.Membership = &Membership{}
	}
	if t.Membership.Chats == nil {
		t.Membership.Chats = map[string]string{}
	}
	if t.Membership.People == nil {
		t.Membership.People = map[string]string{}
	}
}
func withoutMembers(xs []Entity, ids []string) []Entity {
	out := []Entity{}
	for _, x := range xs {
		if !model.Contains(ids, x.ID) {
			out = append(out, x)
		}
	}
	return out
}
func member(xs []Entity, id string) *Entity {
	for _, x := range xs {
		if x.ID == id {
			copy := x
			return &copy
		}
	}
	return nil
}
func beforeMember(t Task, id string, people bool) MemberBefore {
	xs, removed := t.Chats, t.RemovedMembers.Chats
	var sources map[string]string
	if t.Membership != nil {
		sources = t.Membership.Chats
	}
	if people {
		xs, removed = t.People, t.RemovedMembers.People
		if t.Membership != nil {
			sources = t.Membership.People
		}
	}
	return MemberBefore{ID: id, Entity: member(xs, id), Source: sources[id], Removed: model.Contains(removed, id)}
}

// Existing sources are deliberately never overwritten by another writer.
func addMembers(t *Task, chats, people []Entity, source string) {
	t.membershipMaps()
	for _, group := range []struct {
		input   []Entity
		dest    *[]Entity
		removed []string
		sources map[string]string
	}{
		{chats, &t.Chats, t.RemovedMembers.Chats, t.Membership.Chats},
		{people, &t.People, t.RemovedMembers.People, t.Membership.People},
	} {
		for _, x := range group.input {
			if member(*group.dest, x.ID) != nil || model.Contains(group.removed, x.ID) {
				continue
			}
			*group.dest = append(*group.dest, x)
			group.sources[x.ID] = source
		}
	}
}
func validSource(source string) bool {
	switch source {
	case "owner", "detector", "merge", "circle_conversion", "backfill", "follows", "legacy":
		return true
	}
	for _, prefix := range []string{"agent:", "effort:"} {
		if strings.HasPrefix(source, prefix) && strings.TrimSpace(strings.TrimPrefix(source, prefix)) != "" {
			return true
		}
	}
	return false
}
func (s *Service) inferMembership(t *Task) {
	// Copy maps so a read can never modify the stored record or a caller's snapshot.
	m := &Membership{Chats: map[string]string{}, People: map[string]string{}}
	if t.Membership != nil {
		for k, v := range t.Membership.Chats {
			m.Chats[k] = v
		}
		for k, v := range t.Membership.People {
			m.People[k] = v
		}
	}
	t.Membership = m
	missing := false
	for _, x := range t.Chats {
		missing = missing || m.Chats[x.ID] == ""
	}
	for _, x := range t.People {
		missing = missing || m.People[x.ID] == ""
	}
	if !missing {
		return
	}
	var chats, people []Entity
	if t.EffortID != "" && s.Efforts != nil {
		if e, err := s.Efforts.Get(nil, t.EffortID); err == nil {
			chats, people = e.Chats, e.People
		}
	}
	for _, g := range []struct {
		xs, effort []Entity
		sources    map[string]string
	}{{t.Chats, chats, m.Chats}, {t.People, people, m.People}} {
		for _, x := range g.xs {
			if g.sources[x.ID] == "" {
				g.sources[x.ID] = "legacy"
				if member(g.effort, x.ID) != nil {
					g.sources[x.ID] = "effort:" + t.EffortID
				}
			}
		}
	}
}

func (s *Service) saveMembershipChange(tx *sql.Tx, r *record, ch MembershipChange) error {
	ch.ID = "chg_" + dbutil.ID()
	ch.TaskID = r.ID
	ch.At = s.now()
	if _, err := tx.Exec("INSERT INTO objects(bucket,id,data) VALUES('changes',?,?)", ch.ID, dbutil.JSON(ch)); err != nil {
		return err
	}
	r.UpdatedAt = ch.At
	payload := map[string]any{"change_id": ch.ID, "reason": ch.Reason, "undo_until": ch.At.Add(30 * 24 * time.Hour).Format(time.RFC3339Nano)}
	if ch.OwnerRequest != nil {
		payload["by"] = ch.OwnerRequest.AgentID
	}
	// Retain the history scope even after an undo consumes the change object.
	scope := changeChatScope(ch)
	payload["history_chats"] = scope
	if ch.Rule != "" {
		payload["rule"], payload["evidence"] = ch.Rule, ch.Evidence
		if ch.Rule == "finished" || ch.Rule == "obsolete" {
			payload["reason"] = strings.TrimPrefix(ch.Text, "Ended: ")
			payload["close_reason"] = r.CloseReason
		}
	}
	return s.event(tx, *r, ch.ID, ch.Kind, ch.Text, payload)
}

// AttachEvidence is engine-internal; MCP additions use the quoted-request wrapper.
func (s *Service) AttachEvidence(taskID string, chats, people []Entity, messageRefs []Ref, source, reason string) (Task, error) {
	return s.attachEvidenceFor(nil, taskID, chats, people, messageRefs, source, reason, nil)
}

func (s *Service) attachEvidenceFor(a *model.Agent, taskID string, chats, people []Entity, messageRefs []Ref, source, reason string, ask *AskedByOwner) (Task, error) {
	if !validSource(source) || strings.TrimSpace(reason) == "" {
		return Task{}, model.ErrInvalid
	}
	chats = append([]Entity{}, chats...)
	people = append([]Entity{}, people...)
	// Resolve labels from fixture/store data, never trust supplied display names.
	for i, ch := range chats {
		jid, err := s.Hub.ResolveChat(ch.ID)
		if err != nil {
			return Task{}, err
		}
		var name string
		if err = s.Hub.Messages.DB.QueryRow("SELECT COALESCE(name,'') FROM chats WHERE jid=?", jid).Scan(&name); err != nil {
			return Task{}, model.ErrNotFound
		}
		if s.Hub.HiddenChat(jid) || !s.Hub.CanChat(a, jid) {
			return Task{}, model.ErrNotFound
		}
		chats[i] = Entity{ID: jid, Name: name}
	}
	for i, p := range people {
		var person model.Person
		if err := s.Hub.Repo.Get("people", p.ID, &person); err != nil {
			return Task{}, model.ErrNotFound
		}
		if a != nil && !s.personVisible(a, person) {
			return Task{}, model.ErrNotFound
		}
		people[i] = Entity{ID: p.ID, Name: personName(person)}
	}
	hidden := s.Hub.HiddenChatAccess()
	return s.mutate(a, taskID, func(r *record, tx *sql.Tx) error {
		chatBefore, peopleBefore := map[string]MemberBefore{}, map[string]MemberBefore{}
		if ask != nil {
			for _, c := range chats {
				chatBefore[c.ID] = beforeMember(r.Task, c.ID, false)
			}
			for _, p := range people {
				peopleBefore[p.ID] = beforeMember(r.Task, p.ID, true)
			}
			for _, c := range chats {
				r.RemovedMembers.Chats = withoutIDs(r.RemovedMembers.Chats, c.ID)
			}
			for _, p := range people {
				r.RemovedMembers.People = withoutIDs(r.RemovedMembers.People, p.ID)
			}
			r.silentChange = true
		}
		ch, err := s.attachEvidence(r, chats, people, messageRefs, source, hidden, false)
		if err != nil {
			return err
		}
		if len(ch.Chats)+len(ch.People)+len(ch.AddedRefs) == 0 {
			return nil
		}
		labels := []string{}
		for _, b := range ch.Chats {
			labels = append(labels, member(r.Chats, b.ID).Name)
		}
		for _, b := range ch.People {
			labels = append(labels, member(r.People, b.ID).Name)
		}
		if len(labels) == 0 {
			labels = append(labels, "message evidence")
		}
		if ask != nil {
			for i, b := range ch.Chats {
				ch.Chats[i] = chatBefore[b.ID]
			}
			for i, b := range ch.People {
				ch.People[i] = peopleBefore[b.ID]
			}
		}
		ch.OwnerRequest = ask
		ch.Reason = strings.TrimSpace(reason)
		ch.Text = ownerRequestLine("Added "+strings.Join(labels, ", ")+" from "+strings.TrimSpace(reason), ask)
		return s.saveMembershipChange(tx, r, ch)
	})
}

// attachEvidence is the transactional core of AttachEvidence. Automatic rules
// may retain accessible evidence from another effort without adding its members.
func (s *Service) attachEvidence(r *record, chats, people []Entity, messageRefs []Ref, source string, hidden func(string) bool, allowUnlinked bool) (MembershipChange, error) {
	ch := MembershipChange{Kind: "attach_evidence", Source: source}
	for _, x := range mergeEntities(nil, chats) {
		if member(r.Chats, x.ID) == nil && !model.Contains(r.RemovedMembers.Chats, x.ID) {
			ch.Chats = append(ch.Chats, beforeMember(r.Task, x.ID, false))
		}
	}
	for _, x := range mergeEntities(nil, people) {
		if member(r.People, x.ID) == nil && !model.Contains(r.RemovedMembers.People, x.ID) {
			ch.People = append(ch.People, beforeMember(r.Task, x.ID, true))
		}
	}
	addMembers(&r.Task, chats, people, source)
	for _, ref := range messageRefs {
		if model.Contains(r.RemovedMembers.Chats, ref.ChatJID) {
			continue
		}
		if !allowUnlinked && member(r.Chats, ref.ChatJID) == nil || hidden(ref.ChatJID) {
			return ch, model.ErrNotFound
		}
		ms, err := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
		if err != nil {
			return ch, err
		}
		if len(ms) == 0 || ms[0].DeletedAt != nil {
			return ch, model.ErrNotFound
		}
		if !hasRef(r.EvidenceRefs, ref) {
			r.EvidenceRefs = append(r.EvidenceRefs, ref)
			ch.AddedRefs = append(ch.AddedRefs, ref)
		}
	}
	return ch, nil
}

func hasRef(xs []Ref, ref Ref) bool {
	for _, x := range xs {
		if x.ChatJID == ref.ChatJID && x.MessageID == ref.MessageID {
			return true
		}
	}
	return false
}

// RemoveMembers is owner-only, even for an own agent. Tombstones also suppress
// participants that exist only in the owner GET projection.
func (s *Service) RemoveMembers(a *model.Agent, taskID string, chats, people []string) (Task, error) {
	if a != nil {
		return Task{}, model.ErrForbidden
	}
	return s.removeMembers(nil, taskID, chats, people, nil)
}

func (s *Service) removeMembers(a *model.Agent, taskID string, chats, people []string, ask *AskedByOwner) (Task, error) {
	if model.Contains(people, "me") || model.Contains(chats, "me") {
		return Task{}, model.ErrInvalid
	}
	names := map[string]string{}
	for _, id := range people {
		var p model.Person
		if s.Hub.Repo.Get("people", id, &p) == nil {
			names[id] = personName(p)
		}
	}
	_, err := s.mutate(a, taskID, func(r *record, tx *sql.Tx) error {
		r.ownerActivity = a == nil
		if ask != nil {
			r.silentChange = true
		}
		ch := MembershipChange{Kind: "owner_remove", OwnerRequest: ask}
		labels := []string{}
		r.membershipMaps()
		for _, g := range []struct {
			ids     []string
			xs      *[]Entity
			removed *[]string
			sources map[string]string
			before  *[]MemberBefore
			people  bool
		}{
			{chats, &r.Chats, &r.RemovedMembers.Chats, r.Membership.Chats, &ch.Chats, false},
			{people, &r.People, &r.RemovedMembers.People, r.Membership.People, &ch.People, true},
		} {
			for _, id := range g.ids {
				if strings.TrimSpace(id) == "" {
					return model.ErrInvalid
				}
				if model.Contains(*g.removed, id) {
					continue
				}
				b := beforeMember(r.Task, id, g.people)
				*g.before = append(*g.before, b)
				name := id
				if b.Entity != nil && b.Entity.Name != "" {
					name = b.Entity.Name
				}
				if b.Entity == nil && g.people && names[id] != "" {
					name = names[id]
				}
				labels = append(labels, name)
				*g.removed = append(*g.removed, id)
				*g.xs = withoutMembers(*g.xs, []string{id})
				delete(g.sources, id)
			}
		}
		if len(labels) == 0 {
			return nil
		}
		ch.Text = ownerRequestLine("Removed "+strings.Join(labels, ", ")+" from this task", ask)
		ch.Reason = "You removed them from this task"
		return s.saveMembershipChange(tx, r, ch)
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(a, taskID)
}

// UndoMembershipChange restores only this change's members, not the task snapshot.
// Legacy auto-keep Undo retains its separate endpoint and 24-hour semantics.
func (s *Service) UndoMembershipChange(a *model.Agent, taskID, changeID string) (Task, error) {
	if a != nil {
		return Task{}, model.ErrForbidden
	}
	return s.undoMembershipChange(nil, taskID, changeID, nil)
}

func (s *Service) undoMembershipChange(a *model.Agent, taskID, changeID string, ask *AskedByOwner) (Task, error) {
	_, err := s.mutate(a, taskID, func(r *record, tx *sql.Tx) error {
		var raw string
		var ch MembershipChange
		if err := tx.QueryRow("SELECT data FROM objects WHERE bucket='changes' AND id=?", changeID).Scan(&raw); err != nil {
			if err == sql.ErrNoRows {
				return model.ErrConflict
			}
			return err
		}
		if err := json.Unmarshal([]byte(raw), &ch); err != nil {
			return err
		}
		if ch.TaskID != taskID || (ch.Kind != "attach_evidence" && ch.Kind != "owner_remove" && !strings.HasPrefix(ch.Kind, "rule_")) || s.now().Before(ch.At) || !s.now().Before(ch.At.Add(30*24*time.Hour)) {
			return model.ErrConflict
		}
		if strings.HasPrefix(ch.Kind, "rule_") {
			if err := s.restoreRuleChange(tx, r, ch); err != nil {
				return err
			}
			r.silentChange = true
			if ch.Rule == "finished" || ch.Rule == "obsolete" {
				r.ReopenedAt = ptrTime(s.now())
				// Same protections as owner Reopen; do not replay the undone ending.
				r.PauseChecklistClose = true
				if r.EndAt != nil && s.deadlinePassed(r.Task) {
					r.EndAt = nil
				}
				s.ownerTaskActivity(r)
			}
		}
		r.membershipMaps()
		for _, g := range []struct {
			before  []MemberBefore
			xs      *[]Entity
			removed *[]string
			sources map[string]string
		}{
			{ch.Chats, &r.Chats, &r.RemovedMembers.Chats, r.Membership.Chats},
			{ch.People, &r.People, &r.RemovedMembers.People, r.Membership.People},
		} {
			for _, b := range g.before {
				if ch.Kind == "rule_duplicate" && b.Entity == nil {
					// A later owner removal remains sticky after splitting an attachment.
					*g.xs = withoutMembers(*g.xs, []string{b.ID})
					delete(g.sources, b.ID)
					continue
				}
				*g.xs = withoutMembers(*g.xs, []string{b.ID})
				if b.Entity != nil {
					*g.xs = append(*g.xs, *b.Entity)
				}
				delete(g.sources, b.ID)
				if b.Source != "" {
					g.sources[b.ID] = b.Source
				}
				if ch.Rule == "follows_work" && b.Entity != nil {
					// Restoring a removed member is an explicit owner decision.
					g.sources[b.ID] = "owner"
				}
				kept := []string{}
				for _, id := range *g.removed {
					if id != b.ID {
						kept = append(kept, id)
					}
				}
				*g.removed = kept
				if b.Removed {
					*g.removed = append(*g.removed, b.ID)
				}
			}
		}
		refs := []Ref{}
		for _, ref := range r.EvidenceRefs {
			if !hasRef(ch.AddedRefs, ref) {
				refs = append(refs, ref)
			}
		}
		r.EvidenceRefs = refs
		if _, err := tx.Exec("DELETE FROM objects WHERE bucket='changes' AND id=?", changeID); err != nil {
			return err
		}
		r.UpdatedAt = s.now()
		if ask != nil {
			r.silentChange = true
		}
		payload := map[string]any{"undone_change_id": changeID, "history_chats": changeChatScope(ch)}
		if ask != nil {
			payload["by"] = ask.AgentID
		}
		return s.event(tx, *r, dbutil.ID(), "membership_undone", ownerRequestLine("Undone: "+ch.Text, ask), payload)
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(a, taskID)
}
