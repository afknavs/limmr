package tasks

import (
	"database/sql"
	"limmr/internal/model"
	"time"
)

// Stored close_reason values remain the wire contract; history explains them.
func closureReason(r record) string {
	if r.closureReason != "" {
		return r.closureReason
	}
	switch r.CloseReason {
	case "obsolete":
		return "no activity"
	case "lifespan":
		return "its end time passed"
	case "agent_done":
		return "the AI marked it done"
	case "merged":
		return "merged into another task"
	case "owner":
		return "you set it aside"
	case "failed":
		return "it was marked unsuccessful"
	case "not_a_task":
		return "it was dismissed as not a task"
	case "suggestion_expired":
		return "the suggestion expired"
	default:
		return "you marked it done"
	}
}
func (s *Service) closureEvent(tx *sql.Tx, r record, key string) error {
	reason := closureReason(r)
	return s.event(tx, r, key, "task_closed", "Ended: "+reason, map[string]string{"reason": reason, "close_reason": r.CloseReason, "merged_into": r.MergedInto})
}

func (s *Service) latestActivity(t Task) time.Time {
	latest := t.LastActiveAt
	for _, c := range t.Chats {
		if s.Hub.HiddenChat(c.ID) {
			continue
		}
		var at time.Time
		if s.Hub.Messages.DB.QueryRow("SELECT timestamp FROM messages WHERE chat_jid=? AND deleted_at IS NULL ORDER BY julianday(timestamp) DESC,id DESC LIMIT 1", c.ID).Scan(&at) == nil && at.After(latest) {
			latest = at
		}
	}
	return latest
}
func (s *Service) hasReply(r record, after time.Time) bool {
	for _, c := range r.Chats {
		if s.Hub.HiddenChat(c.ID) {
			continue
		}
		var n int
		if s.Hub.Messages.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM messages WHERE chat_jid=? AND is_from_me=0 AND deleted_at IS NULL AND julianday(timestamp)>julianday(?))", c.ID, after).Scan(&n) == nil && n > 0 {
			return true
		}
	}
	return false
}

// Restore closed tasks without replaying an expired timer. The user gets a fresh quiet period.
func (s *Service) Reopen(id string) (Task, error) {
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if r.Status != "done" && r.Status != "dropped" {
			return model.ErrConflict
		}
		r.ReopenedAt = ptrTime(s.now())
		s.ownerTaskActivity(r)
		r.Status = "active"
		if r.Waiting != nil {
			r.Status = "waiting"
		}
		r.ClosedAt = nil
		r.CloseReason = ""
		r.LastActiveAt = s.now()
		r.UpdatedAt = s.now()
		r.PauseChecklistClose = true
		if r.EndAt != nil && !r.EndAt.After(s.now()) {
			r.EndAt = nil
		}
		return s.event(tx, *r, "reopened:"+s.now().Format(time.RFC3339Nano), "reopened", "Closure undone", nil)
	})
}

func wakeTask(r *record) {
	r.Status = "active"
	if r.Waiting != nil {
		r.Status = "waiting"
	}
	r.ClosedAt = nil
	r.CloseReason = ""
}
