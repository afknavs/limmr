package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"time"
)

// CreationConflict is a reviewable refusal, never a successful task creation.
// Candidates contain only tasks visible to this caller.
type CreationConflict struct {
	Candidates []TaskSummary `json:"candidates"`
}

func (e *CreationConflict) Error() string {
	return "Possible duplicate tasks: update one, or repeat create with different_because explaining the different outcome"
}
func (e *CreationConflict) Unwrap() error { return model.ErrConflict }

func ended(t Task) bool { return t.Status == "done" || t.Status == "dropped" }
func validTitle(v string) bool {
	return strings.TrimSpace(v) != "" && length(v, 60) && len(strings.Fields(v)) <= 5
}
func (s *Service) deadlinePassed(t Task) bool {
	return deadlinePassed(t, s.now(), s.Location)
}

func deadlinePassed(t Task, now time.Time, loc *time.Location) bool {
	if t.EndAt == nil || ended(t) {
		return false
	}
	if loc == nil {
		loc = time.Local
	}
	y, m, d := t.EndAt.In(loc).Date()
	return !now.Before(time.Date(y, m, d+1, 0, 0, 0, 0, loc))
}
func related(a, b Task) bool {
	for _, x := range a.Chats {
		for _, y := range b.Chats {
			if x.ID == y.ID {
				return true
			}
		}
	}
	for _, x := range a.People {
		if x.ID == "me" {
			continue
		}
		for _, y := range b.People {
			if x.ID == y.ID {
				return true
			}
		}
	}
	return false
}
func similar(a, b string) bool {
	a = strings.Join(strings.Fields(strings.ToLower(a)), " ")
	b = strings.Join(strings.Fields(strings.ToLower(b)), " ")
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aw, bw := coverWords(a), coverWords(b)
	n := 0
	for w := range aw {
		if bw[w] {
			n++
		}
	}
	// At least half the words in both outcomes, with two shared words to avoid
	// matching every outcome that happens to contain a person's name.
	return n >= 2 && n*2 >= len(aw) && n*2 >= len(bw)
}
func duplicate(a, b Task) bool {
	return related(a, b) && (a.EffortID != "" && a.EffortID == b.EffortID || similar(a.Title, b.Title) || similar(a.Goal, b.Goal) || similar(a.Title, b.Goal) || similar(a.Goal, b.Title))
}
func (s *Service) creationCandidates(tx *sql.Tx, visible func(Task) bool, t Task) ([]TaskSummary, error) {
	rows, err := tx.Query("SELECT data FROM tasks ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskSummary{}
	for rows.Next() {
		var raw string
		var r record
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		if !r.Purged && !ended(r.Task) && !s.deadlinePassed(r.Task) && visible(r.Task) && duplicate(t, r.Task) {
			out = append(out, TaskSummary{ID: r.ID, Title: r.Title, Goal: r.Goal, Status: r.Status})
		}
	}
	return out, rows.Err()
}

// Subject is deliberately distinct from People (participants). Evidence must be
// an accessible, undeleted message from, or explicitly naming, that person.
func (s *Service) validateSubject(a *model.Agent, t Task) error {
	if t.Subject == nil {
		return nil
	}
	if len(t.SubjectEvidence) == 0 {
		return model.ErrInvalid
	}
	var p model.Person
	if err := s.Hub.Repo.Get("people", t.Subject.ID, &p); err != nil {
		return model.ErrNotFound
	}
	t.Subject.Name = personName(p)
	supported := false
	for _, ref := range t.SubjectEvidence {
		linked := false
		for _, ch := range t.Chats {
			linked = linked || ch.ID == ref.ChatJID
		}
		if !linked || s.Hub.HiddenChat(ref.ChatJID) || !s.Hub.CanChat(a, ref.ChatJID) {
			return model.ErrNotFound
		}
		var sender, text string
		var fromMe bool
		if err := s.Hub.Messages.DB.QueryRow("SELECT coalesce(sender,''),coalesce(content,''),is_from_me FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", ref.ChatJID, ref.MessageID).Scan(&sender, &text, &fromMe); err != nil {
			return model.ErrNotFound
		}
		for _, jid := range p.JIDs {
			supported = supported || !fromMe && (sender == jid || sender == "" && ref.ChatJID == jid)
		}
		name := personName(p)
		supported = supported || name != "" && strings.Contains(" "+strings.Join(strings.Fields(strings.ToLower(text)), " ")+" ", " "+strings.ToLower(name)+" ")
	}
	if !supported {
		return model.ErrInvalid
	}
	return nil
}

func (s *Service) SetCover(a *model.Agent, id, item string) (Task, error) {
	t, err := s.Get(a, id)
	if err != nil {
		return Task{}, err
	}
	if err = s.authorize(a, t, true); err != nil {
		return Task{}, err
	}
	// Explicit call is the only agent path that may replace a pinned cover.
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		found := false
		for _, it := range r.Items {
			found = found || it.ID == item && it.MediaType == "image"
		}
		if !found {
			return model.ErrInvalid
		}
		r.CoverItemID = item
		r.Cover = nil
		r.CoverRemoved = false
		s.chooseCover(&r.Task)
		r.UpdatedAt = s.now()
		return s.event(tx, *r, dbutil.ID(), "cover_set", "Task cover selected", actorCredit(a))
	})
}

// ConvertCircle archives the original in the same transaction that creates the
// task. All chats and people survive, even when a caller sees only a projection.
func (s *Service) ConvertCircle(a *model.Agent, id string, in Create) (Task, error) {
	var c model.Circle
	if err := s.Hub.Repo.Get("circles", id, &c); err != nil {
		return Task{}, err
	}
	if c.Archived {
		return Task{}, model.ErrConflict
	}
	if !s.Hub.CircleAccess(a, c) {
		return Task{}, model.ErrNotFound
	}
	if a != nil && (a.Preset != "own" && c.CreatedBy != a.ID || !a.Permissions.SaveSummaries) {
		return Task{}, model.ErrForbidden
	}
	in.Chats = nil
	in.People = nil
	for _, ch := range c.Chats {
		if s.Hub.HiddenChat(ch.ChatJID) || !s.Hub.CanChat(a, ch.ChatJID) {
			return Task{}, model.ErrNotFound
		}
		in.Chats = append(in.Chats, Entity{ID: ch.ChatJID})
	}
	for _, p := range c.People {
		var person model.Person
		if err := s.Hub.Repo.Get("people", p.PersonID, &person); err != nil {
			return Task{}, err
		}
		in.People = append(in.People, Entity{ID: p.PersonID, Name: personName(person)})
	}
	if in.Title == "" {
		in.Title = c.Name
	}
	if in.Goal == "" {
		in.Goal = c.Goal
	}
	if in.Kind == "" {
		in.Kind = "long"
	}
	in.convertCircle = &c
	return s.Create(a, in)
}

// Merge keeps both originals and copies B's timeline into A with stable event
// IDs. A's goal, subject, status, schedules and pinned cover remain authoritative.
func (s *Service) Merge(a *model.Agent, into, from string) (Task, error) {
	return s.merge(a, into, from, nil)
}
func (s *Service) MergeWithOwnerRequest(a *model.Agent, into, from string, request OwnerRequest) (Task, error) {
	ask, err := s.ownerRequest(a, &request)
	if err != nil {
		return Task{}, err
	}
	return s.merge(a, into, from, ask)
}
func (s *Service) merge(a *model.Agent, into, from string, ask *AskedByOwner) (Task, error) {
	if into == from {
		return Task{}, model.ErrInvalid
	}
	for _, id := range []string{into, from} {
		t, err := s.Get(a, id)
		if err != nil {
			return Task{}, err
		}
		if err = s.authorize(a, t, true); err != nil {
			return Task{}, err
		}
	}
	visible, err := s.visibilitySnapshot(a)
	if err != nil {
		return Task{}, err
	}
	return s.mutate(a, into, func(dst *record, tx *sql.Tx) error {
		if ended(dst.Task) {
			return model.ErrConflict
		}
		var raw string
		var src record
		if err := tx.QueryRow("SELECT data FROM tasks WHERE id=?", from).Scan(&raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &src); err != nil {
			return err
		}
		if !visible(src.Task) {
			return model.ErrNotFound
		}
		if a != nil && a.Preset != "own" && src.CreatedBy.AgentID != a.ID {
			return model.ErrForbidden
		}
		if src.MergedInto != "" || src.Purged {
			return model.ErrConflict
		}
		addMembers(&dst.Task, src.Chats, src.People, "merge")
		for _, ref := range src.EvidenceRefs {
			if !model.Contains(dst.RemovedMembers.Chats, ref.ChatJID) && !hasRef(dst.EvidenceRefs, ref) {
				dst.EvidenceRefs = append(dst.EvidenceRefs, ref)
			}
		}
		for _, it := range src.Items {
			dst.Items = appendItem(dst.Items, it)
		}
		dst.OwnerNotes = append(dst.OwnerNotes, src.OwnerNotes...)
		dst.Checklist = append(dst.Checklist, src.Checklist...)
		dst.UpdatesSent = append(dst.UpdatesSent, src.UpdatesSent...)
		for i := range dst.Checklist {
			dst.Checklist[i].Order = i
		}
		now := s.now()
		src.EndedAsSuggestion = src.Status == "suggested" || src.EndedAsSuggestion
		closeTask(&src, now, "merged")
		src.Status = "dropped"
		src.MergedInto = into
		src.UpdatedAt = now
		if _, err := tx.Exec("INSERT OR IGNORE INTO task_events SELECT ?||id,?,at,kind,text,payload FROM task_events WHERE task_id=?", "merge:"+from+":", into, from); err != nil {
			return err
		}
		if err := s.closureEvent(tx, src, dbutil.ID()); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", from); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(src), from); err != nil {
			return err
		}
		dst.UpdatedAt = now
		return s.event(tx, *dst, dbutil.ID(), "merged", ownerRequestLine("Task history and files merged", ask), map[string]string{"from": from, "by": actorCredit(a)["by"]})
	})
}

func reasonValid(v string) bool { return length(v, 1000) && len(strings.Fields(v)) >= 3 }
func (s *Service) validateCreate(in Create) error {
	if !validTitle(in.Title) {
		return fmt.Errorf("title must be at most five words and 60 characters: %w", model.ErrInvalid)
	}
	if in.DifferentBecause != "" && !reasonValid(in.DifferentBecause) {
		return model.ErrInvalid
	}
	return nil
}

// Snapshot access before entering a single-connection write transaction.
func (s *Service) visibilitySnapshot(a *model.Agent) (func(Task) bool, error) {
	if a == nil {
		return func(Task) bool { return true }, nil
	}
	allowed := s.Hub.ChatAccess(a)
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return nil, err
	}
	circles := map[string]bool{}
	for _, c := range cs {
		circles[c.ID] = c.Archived || s.Hub.CircleAccess(a, c)
	}
	return func(t Task) bool {
		if !a.Permissions.ReadMessages {
			return false
		}
		for _, id := range t.CircleIDs {
			if !circles[id] {
				return false
			}
		}
		for _, ch := range t.Chats {
			if !allowed(ch.ID) {
				return false
			}
		}
		return true
	}, nil
}
