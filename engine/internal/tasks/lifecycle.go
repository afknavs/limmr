package tasks

import (
	"database/sql"
	"limmr/internal/model"
	"time"
)

func closureLabel(r record) string {
	if r.CloseReason == "lifespan" || r.CloseReason == "agent_done" || r.CloseReason == "done" && r.Progress.Total > 0 && r.Progress.Done == r.Progress.Total {
		return "Closed automatically"
	}
	return "Task closed"
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
