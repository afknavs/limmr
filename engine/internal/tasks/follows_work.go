package tasks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/wa"
)

// Candidate is report-only; Kind disambiguates a chat from a person.
type Candidate struct {
	Member Entity `json:"member"`
	Kind   string `json:"kind"`
	Why    string `json:"why"`
}

// FollowsWorkTask is a read-only input snapshot. Loading is separate from the
// pure candidate function so a dry run can inspect it without running a tick.
type FollowsWorkTask struct {
	Task            Task
	CurrentEffort   *efforts.Effort
	ProtectedChats  map[string]bool
	ProtectedPeople map[string]bool
	EffortAliases   map[string]bool
}

// FollowsWorkCandidates lists only legacy members. It performs no IO and never
// edits its input, provenance, evidence or history. Applying it needs owner consent.
func FollowsWorkCandidates(task FollowsWorkTask) []Candidate {
	return followsWorkMembers(task, true)
}

func followsWorkMembers(task FollowsWorkTask, legacy bool) []Candidate {
	out := []Candidate{}
	if task.Task.EffortID == "" || task.CurrentEffort == nil {
		return out
	}
	for _, g := range []struct {
		kind             string
		members, current []Entity
		sources          map[string]string
		protected        map[string]bool
	}{
		{kind: "chat", members: task.Task.Chats, current: task.CurrentEffort.Chats, protected: task.ProtectedChats},
		{kind: "person", members: task.Task.People, current: task.CurrentEffort.People, protected: task.ProtectedPeople},
	} {
		if task.Task.Membership != nil {
			if g.kind == "chat" {
				g.sources = task.Task.Membership.Chats
			} else {
				g.sources = task.Task.Membership.People
			}
		}
		for _, m := range g.members {
			if m.ID == "me" || g.protected[m.ID] || member(g.current, m.ID) != nil {
				continue
			}
			source := g.sources[m.ID]
			eligible := source == "legacy" || source == "" // Missing, out-of-effort sources infer as legacy.
			if !legacy {
				eligible = source == "detector" || strings.HasPrefix(source, "effort:") && task.EffortAliases[strings.TrimPrefix(source, "effort:")]
			}
			if eligible {
				out = append(out, Candidate{Member: m, Kind: g.kind, Why: "No longer in the current effort; no task reference or owner-add history"})
			}
		}
	}
	return out
}

// FollowsWorkSnapshot only reads stores. Missing efforts/alias cycles and hidden
// task chats cannot prove departure, so they produce no actionable snapshot.
func (s *Service) FollowsWorkSnapshot(task Task) (FollowsWorkTask, error) {
	out := FollowsWorkTask{Task: task, ProtectedChats: map[string]bool{}, ProtectedPeople: map[string]bool{}, EffortAliases: map[string]bool{}}
	if task.EffortID == "" || s.Efforts == nil {
		return out, nil
	}
	c, err := s.chatContext()
	if err != nil {
		return out, err
	}
	for _, ch := range task.Chats {
		if c.hidden(ch.ID) {
			return out, nil
		}
	}
	id := task.EffortID
	for !out.EffortAliases[id] {
		out.EffortAliases[id] = true
		var edit struct {
			ReplacedBy string `json:"replaced_by"`
		}
		err = s.Hub.Repo.Get("effort_edits", id, &edit)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return out, err
		}
		if edit.ReplacedBy != "" {
			id = edit.ReplacedBy
			continue
		}
		e, eerr := s.Efforts.Get(nil, id)
		if errors.Is(eerr, model.ErrNotFound) {
			return out, nil
		}
		if eerr != nil {
			return out, eerr
		}
		out.CurrentEffort = &e.Effort
		break
	}
	if out.CurrentEffort == nil {
		return out, nil
	}
	addJID := func(j string) {
		if j == "" {
			return
		}
		j = c.canonical(j)
		out.ProtectedChats[j] = true
		if p, ok := c.byJID[j]; ok {
			out.ProtectedPeople[p.ID] = true
		}
	}
	addRef := func(ref *Ref) error {
		if ref == nil {
			return nil
		}
		addJID(ref.ChatJID)
		ms, e := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
		if e != nil {
			return e
		}
		for _, m := range ms {
			if !m.IsFromMe {
				addJID(m.Sender)
			}
		}
		return nil
	}
	texts := []string{}
	for _, it := range task.Items {
		if err = addRef(it.MessageRef); err != nil {
			return out, err
		}
		texts = append(texts, it.Title, it.Text, it.Description, it.OCRText, it.Caption, it.Chat, it.Sender)
	}
	for _, st := range task.Checklist {
		texts = append(texts, st.Text)
	}
	if task.Waiting != nil {
		if task.Waiting.On != nil {
			out.ProtectedPeople[task.Waiting.On.ID] = true
			addJID(task.Waiting.On.ID)
		}
		texts = append(texts, task.Waiting.What)
	}
	if task.CheckIn != nil {
		addJID(task.CheckIn.To)
		texts = append(texts, task.CheckIn.Note)
	}
	if task.Subject != nil {
		out.ProtectedPeople[task.Subject.ID] = true
		addJID(task.Subject.ID)
	}
	for _, ref := range task.SubjectEvidence {
		if err = addRef(&ref); err != nil {
			return out, err
		}
	}
	text := strings.Join(texts, "\n")
	for _, g := range []struct {
		members   []Entity
		protected map[string]bool
	}{{task.Chats, out.ProtectedChats}, {task.People, out.ProtectedPeople}} {
		for _, m := range g.members {
			for _, term := range []string{m.ID, m.Name} {
				if term != "" && regexp.MustCompile(`(?i)(^|[^\pL\pN])`+regexp.QuoteMeta(term)+`($|[^\pL\pN])`).MatchString(text) {
					g.protected[m.ID] = true
				}
			}
		}
	}
	// Owner additions remain protected even when legacy JSON loses its source.
	rows, err := s.Hub.Repo.DB.Query(`SELECT o.data FROM objects o JOIN task_events e ON json_extract(e.payload,'$.change_id')=o.id WHERE o.bucket='changes' AND e.task_id=?`, task.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var change MembershipChange
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &change); err != nil {
			return out, err
		}
		if change.Source != "owner" {
			continue
		}
		for _, m := range change.Chats {
			out.ProtectedChats[m.ID] = true
		}
		for _, m := range change.People {
			out.ProtectedPeople[m.ID] = true
		}
	}
	return out, rows.Err()
}

func (s *Service) applyFollowsWork(old record) error {
	if old.Purged || !duplicateOpen(old.Task) || old.EffortID == "" {
		return nil
	}
	snapshot, err := s.FollowsWorkSnapshot(old.Task)
	if err != nil {
		return err
	}
	candidates := followsWorkMembers(snapshot, false)
	if len(candidates) == 0 {
		return nil
	}
	_, err = s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
		r.silentChange = true
		if dbutil.JSON(r) != dbutil.JSON(old) {
			return nil
		}
		return s.removeFollowsWorkMembers(tx, r, json.RawMessage(dbutil.JSON(r)), candidates, snapshot.CurrentEffort.ID)
	})
	return err
}

// Shared transaction core: the protocol executes only the planner's candidates.
func (s *Service) removeFollowsWorkMembers(tx *sql.Tx, r *record, before json.RawMessage, candidates []Candidate, effortID string) error {
	change := MembershipChange{}
	labels := []string{}
	r.membershipMaps()
	for _, x := range candidates {
		labels = append(labels, x.Member.Name)
		if x.Kind == "chat" {
			change.Chats = append(change.Chats, beforeMember(r.Task, x.Member.ID, false))
			r.Chats = withoutMembers(r.Chats, []string{x.Member.ID})
			delete(r.Membership.Chats, x.Member.ID)
		} else {
			change.People = append(change.People, beforeMember(r.Task, x.Member.ID, true))
			r.People = withoutMembers(r.People, []string{x.Member.ID})
			delete(r.Membership.People, x.Member.ID)
		}
	}
	return s.saveRuleChange(tx, r, before, "follows_work", "Removed "+strings.Join(labels, " and "), "they belong to other work", map[string]any{"effort_id": effortID, "members": candidates}, change)
}
