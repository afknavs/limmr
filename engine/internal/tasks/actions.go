package tasks

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"strings"
	"time"
)

// Decode rejects unknown fields and trailing JSON without echoing input.
func Decode(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return model.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return model.ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return model.ErrInvalid
	}
	return nil
}
func (s *Service) Update(a *model.Agent, id string, fields map[string]any) (Task, error) {
	type patch struct {
		Title          *string         `json:"title"`
		Goal           *string         `json:"goal"`
		Kind           *string         `json:"kind"`
		Status         *string         `json:"status"`
		LifespanDays   *int            `json:"lifespan_days"`
		SummaryAttempt *SummaryAttempt `json:"summary_attempt"`
		Summary        *Summary        `json:"summary"`
		CheckIn        json.RawMessage `json:"check_in"`
		Waiting        json.RawMessage `json:"waiting"`
		Reason         *string         `json:"reason"`
		CloseReason    *string         `json:"close_reason"`
	}
	var p patch
	if err := Decode(fields, &p); err != nil {
		return Task{}, err
	}
	// This Mac claims one attempt before generation; persisted in the task so reopening
	// a window or restarting the app cannot repeatedly invoke the model for the same input.
	if p.SummaryAttempt != nil {
		if a != nil {
			return Task{}, model.ErrForbidden
		}
		r, err := s.raw(id)
		if err != nil {
			return Task{}, err
		}
		latest, err := s.latest(r.Task)
		if err != nil {
			return Task{}, err
		}
		if len(latest) == 0 {
			return Task{}, model.ErrConflict
		}
		m := latest[0]
		if p.SummaryAttempt.ChatJID != m.ChatJID || p.SummaryAttempt.MessageID != m.MessageID || !p.SummaryAttempt.Timestamp.Equal(m.Timestamp) {
			return Task{}, model.ErrConflict
		}
	}
	var ci *CheckIn
	var w *Waiting
	if p.CheckIn != nil {
		var raw any
		if json.Unmarshal(p.CheckIn, &raw) != nil {
			return Task{}, model.ErrInvalid
		}
		if err := Decode(raw, &ci); err != nil {
			return Task{}, model.ErrInvalid
		}
	}
	if p.Waiting != nil {
		var raw any
		if json.Unmarshal(p.Waiting, &raw) != nil {
			return Task{}, model.ErrInvalid
		}
		if err := Decode(raw, &w); err != nil {
			return Task{}, model.ErrInvalid
		}
	}
	if ci != nil {
		if ci.NextAt.IsZero() {
			ci.NextAt = s.next(*ci, s.now())
		}
		ci.LastAt = nil
	}
	if w != nil {
		if w.Since.IsZero() {
			w.Since = s.now()
		}
		if w.NudgeAfterDays == 0 {
			w.NudgeAfterDays = 3
		}
		w.NudgeState = "none"
		w.NudgeSuggestionID = ""
	}
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		if r.Status == "suggested" && (p.Status != nil || p.Waiting != nil || p.CheckIn != nil) {
			return model.ErrConflict
		}
		if p.Status != nil && *p.Status == "suggested" {
			return model.ErrInvalid
		}
		if p.SummaryAttempt != nil {
			if strings.TrimSpace(r.Summary.Text) != "" {
				return model.ErrConflict
			}
			if p.Summary == nil {
				if r.SummaryAttempt != nil && !p.SummaryAttempt.Timestamp.After(r.SummaryAttempt.Timestamp) {
					return model.ErrConflict
				}
				r.SummaryAttempt = p.SummaryAttempt
			} else if r.SummaryAttempt == nil || (r.SummaryAttempt.ChatJID != p.SummaryAttempt.ChatJID || r.SummaryAttempt.MessageID != p.SummaryAttempt.MessageID || !r.SummaryAttempt.Timestamp.Equal(p.SummaryAttempt.Timestamp)) || p.Summary.UpdatedBy != "This Mac" {
				return model.ErrConflict
			}
		}
		if p.Title != nil {
			r.Title = strings.TrimSpace(*p.Title)
		}
		if p.Goal != nil {
			r.Goal = *p.Goal
		}
		if p.Kind != nil {
			r.Kind = *p.Kind
		}
		if p.Status != nil {
			r.Status = *p.Status
			if open(r.Task) {
				r.ClosedAt = nil
				r.CloseReason = ""
			}
		}
		if p.LifespanDays != nil {
			r.LifespanDays = *p.LifespanDays
		}
		if p.Summary != nil {
			r.Summary = *p.Summary
			r.Summary.UpdatedAt = s.now()
			r.Summary.UpdatedBy = "you"
			if a == nil && p.Summary.UpdatedBy == "This Mac" {
				r.Summary.UpdatedBy = "This Mac"
			}
			if a != nil {
				r.Summary.UpdatedBy = a.ID
			}
		}
		if p.CheckIn != nil {
			r.CheckIn = ci
			r.DraftCheckIn = ""
			r.DraftSlot = ""
		}
		if p.Waiting != nil {
			if r.Waiting != nil && r.Waiting.NudgeSuggestionID != "" {
				if err := dismiss(tx, r.Waiting.NudgeSuggestionID); err != nil {
					return err
				}
			}
			r.Waiting = w
			r.DraftNudge = ""
			if w != nil {
				r.Status = "waiting"
			} else if r.Status == "waiting" {
				r.Status = "active"
			}
			kind := "waiting_set"
			if w == nil {
				kind = "task_waiting_cleared"
			}
			if err := s.event(tx, *r, dbutil.ID(), kind, "Waiting changed", nil); err != nil {
				return err
			}
		}
		// Human-readable completion explanations live in the summary; close_reason remains its contract enum.
		if p.Reason != nil {
			if r.Status != "done" && r.Status != "dropped" || !length(*p.Reason, 1500) {
				return model.ErrInvalid
			}
			r.Summary = Summary{Text: *p.Reason, UpdatedAt: s.now(), UpdatedBy: "you"}
			if a != nil {
				r.Summary.UpdatedBy = a.ID
			}
		}
		if p.CloseReason != nil {
			if !model.Contains([]string{"done", "lifespan", "owner"}, *p.CloseReason) {
				return model.ErrInvalid
			}
			r.CloseReason = *p.CloseReason
		}
		r.UpdatedAt = s.now()
		return s.refreshNeeds(tx, r)
	})
}
func dismiss(tx *sql.Tx, id string) error {
	_, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND id=? AND json_extract(data,'$.state')='pending'", id)
	return err
}
func (s *Service) Attach(a *model.Agent, id string, it Item) (Task, error) {
	t, err := s.Get(a, id)
	if err != nil {
		return Task{}, err
	}
	if it.MessageRef != nil {
		found := false
		for _, ch := range t.Chats {
			found = found || ch.ID == it.MessageRef.ChatJID
		}
		if !found || !s.Hub.CanChat(a, it.MessageRef.ChatJID) || s.Hub.HiddenChat(it.MessageRef.ChatJID) {
			return Task{}, model.ErrNotFound
		}
	}
	it.ID = "itm_" + dbutil.ID()
	it.AddedAt = s.now()
	it.AddedBy = "you"
	it.Source = "owner"
	if a != nil {
		it.AddedBy = a.ID
		it.Source = "ai"
	}
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		r.Items = appendItem(r.Items, it)
		r.UpdatedAt = s.now()
		return nil
	})
}
func (s *Service) RemoveItem(a *model.Agent, id, item string) (Task, error) {
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		for i, it := range r.Items {
			if it.ID == item {
				r.Items = append(r.Items[:i], r.Items[i+1:]...)
				r.UpdatedAt = s.now()
				return nil
			}
		}
		return model.ErrNotFound
	})
}
func (s *Service) Checklist(a *model.Agent, id, op string, steps []Step) (Task, error) {
	if !model.Contains([]string{"add", "done", "undo", "remove", "reorder"}, op) || len(steps) == 0 {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		now := s.now()
		if op == "reorder" {
			if len(steps) != len(r.Checklist) {
				return model.ErrInvalid
			}
			ordered := []Step{}
			seen := map[string]bool{}
			for _, st := range steps {
				found := false
				for _, old := range r.Checklist {
					if old.ID == st.ID && !seen[st.ID] {
						old.Order = len(ordered)
						ordered = append(ordered, old)
						seen[st.ID] = true
						found = true
						break
					}
				}
				if !found {
					return model.ErrInvalid
				}
			}
			r.Checklist = ordered
		} else {
			for _, st := range steps {
				if op == "add" {
					st.ID = "stp_" + dbutil.ID()
					st.Done = false
					st.DoneAt = nil
					st.Order = len(r.Checklist)
					r.Checklist = append(r.Checklist, st)
					continue
				}
				idx := -1
				for i, old := range r.Checklist {
					if old.ID == st.ID {
						idx = i
						break
					}
				}
				if idx < 0 {
					return model.ErrNotFound
				}
				old := &r.Checklist[idx]
				switch op {
				case "done":
					if !old.Done {
						old.Done = true
						old.DoneAt = &now
						if err := s.event(tx, *r, "step:"+old.ID+":"+dbutil.ID(), "step_done", "Step completed", nil); err != nil {
							return err
						}
					}
				case "undo":
					if old.Done {
						if err := s.event(tx, *r, "step:"+old.ID+":"+dbutil.ID(), "step_reopened", "Step marked incomplete", nil); err != nil {
							return err
						}
					}
					old.Done = false
					old.DoneAt = nil
				case "remove":
					r.Checklist = append(r.Checklist[:idx], r.Checklist[idx+1:]...)
				}
			}
		}
		for i := range r.Checklist {
			r.Checklist[i].Order = i
		}
		r.UpdatedAt = now
		return nil
	})
}
func (s *Service) Draft(a *model.Agent, id, kind, text string) (Task, error) {
	if strings.TrimSpace(text) == "" || !length(text, 2000) {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		switch kind {
		case "check_in":
			if r.CheckIn == nil {
				return model.ErrConflict
			}
			r.DraftCheckIn = text
			r.DraftSlot = r.CheckIn.NextAt.Format(time.RFC3339Nano)
		case "nudge":
			if r.Waiting == nil {
				return model.ErrConflict
			}
			r.DraftNudge = text
		default:
			return model.ErrInvalid
		}
		r.UpdatedAt = s.now()
		return nil
	})
}
func (s *Service) NeedsYou(a *model.Agent, id, reason string) (Task, error) {
	if !length(reason, 300) {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		r.FlagReason = strings.TrimSpace(reason)
		r.UpdatedAt = s.now()
		return s.refreshNeeds(tx, r)
	})
}
func (s *Service) SkipNudge(a *model.Agent, id string) (Task, error) {
	return s.mutate(a, id, func(r *record, tx *sql.Tx) error {
		if r.Waiting == nil {
			return model.ErrConflict
		}
		if err := dismiss(tx, r.Waiting.NudgeSuggestionID); err != nil {
			return err
		}
		r.Waiting.NudgeState = "skipped"
		r.FlagReason = ""
		r.EffortNeeds = false
		r.EffortAcknowledged = true
		r.UpdatedAt = s.now()
		return s.refreshNeeds(tx, r)
	})
}

// UpdateStep is the control API's partial Step edit. Completion times are trusted engine times.
func (s *Service) UpdateStep(id, step string, text *string, done *bool, order *int) (Task, error) {
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		idx := -1
		for i, st := range r.Checklist {
			if st.ID == step {
				idx = i
				break
			}
		}
		if idx < 0 {
			return model.ErrNotFound
		}
		st := &r.Checklist[idx]
		if text != nil {
			st.Text = *text
		}
		if done != nil && *done != st.Done {
			st.Done = *done
			st.DoneAt = nil
			if *done {
				now := s.now()
				st.DoneAt = &now
				if err := s.event(tx, *r, "step:"+st.ID+":"+dbutil.ID(), "step_done", "Step completed", nil); err != nil {
					return err
				}
			} else {
				if err := s.event(tx, *r, "step:"+st.ID+":"+dbutil.ID(), "step_reopened", "Step marked incomplete", nil); err != nil {
					return err
				}
			}
		}
		if order != nil {
			if *order < 0 || *order >= len(r.Checklist) {
				return model.ErrInvalid
			}
			value := *st
			r.Checklist = append(r.Checklist[:idx], r.Checklist[idx+1:]...)
			r.Checklist = append(r.Checklist, Step{})
			copy(r.Checklist[*order+1:], r.Checklist[*order:])
			r.Checklist[*order] = value
		}
		for i := range r.Checklist {
			r.Checklist[i].Order = i
		}
		r.UpdatedAt = s.now()
		return nil
	})
}

// OnDeviceNudge is used by the existing control-only This Mac worker. An AI's
// saved draft always wins; the local worker can replace a queued plain template.
func (s *Service) OnDeviceNudge(id, text string) (Task, error) {
	if strings.TrimSpace(text) == "" || !length(text, 2000) {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if r.Waiting == nil {
			return model.ErrConflict
		}
		if r.DraftNudge != "" {
			return nil
		}
		if r.Waiting.NudgeState == "queued" {
			result, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.draft_text',?,'$.body',?,'$.variations',json(?),'$.draft_source','this-mac') WHERE bucket='suggestions' AND id=? AND json_extract(data,'$.state')='pending' AND json_extract(data,'$.draft_source')='template'", text, text, dbutil.JSON([]string{text}), r.Waiting.NudgeSuggestionID)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return model.ErrConflict
			}
		} else if r.Waiting.NudgeState != "none" {
			return model.ErrConflict
		}
		r.DraftNudge = text
		r.UpdatedAt = s.now()
		return nil
	})
}
