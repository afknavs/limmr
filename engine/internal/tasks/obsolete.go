package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"limmr/internal/dbutil"
)

// Default lifespan used to be stored as 14 without its provenance. Legacy
// non-14 values are explicit; a newly explicit 14 carries LifespanSet.
func inactivityDays(t Task) int {
	if t.Kind == "test" {
		return 1
	}
	if t.LifespanSet || t.LifespanDays != 14 {
		return t.LifespanDays
	}
	return 21
}

// obsoleteDue is shared by the live rule and the read-only protocol planner.
func obsoleteDue(r record, active, now time.Time) bool {
	if !duplicateOpen(r.Task) || r.EndAt != nil {
		return false
	}
	if active.IsZero() {
		active = r.CreatedAt
	}
	days := inactivityDays(r.Task)
	return days >= 1 && !now.Before(active.Add(time.Duration(days)*24*time.Hour)) &&
		(r.ReopenedAt == nil || !now.Before(r.ReopenedAt.Add(21*24*time.Hour)))
}

// Read conversation activity outside the hub writer, then recheck the task
// snapshot under the writer reservation. Expiry precedes nudge/check-in work.
func (s *Service) applyObsolete(old record) (bool, error) {
	if !duplicateOpen(old.Task) || old.EndAt != nil {
		return false, nil
	}
	active := s.latestActivity(old.Task)
	if active.IsZero() {
		active = old.CreatedAt
	}
	days := inactivityDays(old.Task)
	now := s.now()
	if !obsoleteDue(old, active, now) {
		return false, nil
	}
	ended := false
	_, err := s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
		r.silentChange = true
		if dbutil.JSON(r) != dbutil.JSON(old) {
			return nil
		}
		before := json.RawMessage(dbutil.JSON(r))
		closeTask(r, now, "obsolete")
		r.Status = "dropped"
		r.UpdatedAt = now
		// Retire any already-pending attention without producing an update.
		if _, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", r.ID); err != nil {
			return err
		}
		reason := fmt.Sprintf("no activity for %d days", days)
		if r.Kind == "test" {
			reason = "test task, a day without activity"
		}
		if err := s.saveRuleChange(tx, r, before, "obsolete", "Ended", reason,
			map[string]any{"last_active_at": active, "inactivity_days": days, "kind": r.Kind}, MembershipChange{}); err != nil {
			return err
		}
		ended = true
		return nil
	})
	return ended, err
}
