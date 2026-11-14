package tasks

import (
	"encoding/json"
	"strings"

	"limmr/internal/model"
)

func withoutIDs(ids []string, id string) []string {
	out := []string{}
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func statusReasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

func (s *Service) personVisible(a *model.Agent, p model.Person) bool {
	for _, jid := range p.JIDs {
		if s.Hub.CanChat(a, jid) && !s.Hub.HiddenChat(jid) {
			return true
		}
	}
	return false
}

// These wrappers preserve the control API's owner-only service contract while
// delegating a specific quoted owner decision through the existing operations.
func (s *Service) AddMembersWithOwnerRequest(a *model.Agent, id string, chats, people []Entity, request OwnerRequest) (Task, error) {
	ask, err := s.ownerRequest(a, &request)
	if err != nil {
		return Task{}, err
	}
	if len(chats)+len(people) == 0 {
		return Task{}, model.ErrInvalid
	}
	return s.attachEvidenceFor(a, id, chats, people, nil, "agent:"+ask.AgentID, "owner request", ask)
}

func (s *Service) RemoveMembersWithOwnerRequest(a *model.Agent, id string, chats, people []string, request OwnerRequest) (Task, error) {
	ask, err := s.ownerRequest(a, &request)
	if err != nil {
		return Task{}, err
	}
	t, err := s.Get(a, id)
	if err != nil {
		return Task{}, err
	}
	if len(chats)+len(people) == 0 {
		return Task{}, model.ErrInvalid
	}
	for _, jid := range chats {
		if member(t.Chats, jid) == nil {
			return Task{}, model.ErrNotFound
		}
	}
	for _, id := range people {
		if member(t.People, id) == nil {
			return Task{}, model.ErrNotFound
		}
	}
	return s.removeMembers(a, id, chats, people, ask)
}

// Include all earlier linked work when checking restored rule snapshots. No
// privileged owner read/mutation is used to bypass the caller's current access.
func changeChatScope(ch MembershipChange) []string {
	out := []string{}
	for _, b := range ch.Chats {
		out = append(out, b.ID)
	}
	for _, ref := range ch.AddedRefs {
		out = append(out, ref.ChatJID)
	}
	for _, raw := range []json.RawMessage{ch.Before, ch.SuggestionBefore, ch.SuggestionAfter} {
		var t Task
		if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
			continue
		}
		for _, c := range t.Chats {
			out = append(out, c.ID)
		}
		out = append(out, t.RemovedMembers.Chats...)
		for _, ref := range taskMessageRefs(t) {
			out = append(out, ref.ChatJID)
		}
	}
	return out
}

func (s *Service) UndoMembershipChangeWithOwnerRequest(a *model.Agent, id, changeID string, request OwnerRequest) (Task, error) {
	ask, err := s.ownerRequest(a, &request)
	if err != nil {
		return Task{}, err
	}
	t, err := s.Get(a, id)
	if err != nil {
		return Task{}, err
	}
	if err = s.authorize(a, t, true); err != nil {
		return Task{}, err
	}
	var ch MembershipChange
	if err = s.Hub.Repo.Get("changes", changeID, &ch); err != nil {
		return Task{}, model.ErrConflict
	}
	if ch.TaskID != id {
		return Task{}, model.ErrConflict
	}
	for _, jid := range changeChatScope(ch) {
		if !s.Hub.CanChat(a, jid) || s.Hub.HiddenChat(jid) {
			return Task{}, model.ErrNotFound
		}
	}
	for _, b := range ch.People {
		if b.Entity == nil {
			continue
		}
		var p model.Person
		if s.Hub.Repo.Get("people", b.ID, &p) != nil || !s.personVisible(a, p) {
			return Task{}, model.ErrNotFound
		}
	}
	for _, raw := range []json.RawMessage{ch.Before, ch.SuggestionBefore} {
		var before Task
		if len(raw) == 0 {
			continue
		}
		if json.Unmarshal(raw, &before) != nil || s.authorize(a, before, true) != nil {
			return Task{}, model.ErrNotFound
		}
	}
	// The same consumed-change and 30-day checks run transactionally below.
	return s.undoMembershipChange(a, id, changeID, ask)
}

// Legacy events without recorded scope keep a useful kind but redact their
// prose for agents; new history retains scope even after membership/rule undo.
func legacyHistoryLabel(kind string) string {
	return "Task history: " + strings.ReplaceAll(strings.TrimPrefix(kind, "task_"), "_", " ")
}
