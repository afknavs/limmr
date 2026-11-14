package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/hub"
)

const protocolMarker = "protocol_v1_applied_at"
const protocolReviewKeepText = "Kept in your review of old tasks"

// The planner's legacy readers use Database. Bind every hub read to the same
// writer transaction without changing the shared service or connection pool.
type protocolTransaction struct {
	dbutil.Database
	tx *sql.Tx
}

func (d protocolTransaction) Query(q string, args ...any) (*sql.Rows, error) {
	return d.tx.Query(q, args...)
}
func (d protocolTransaction) QueryRow(q string, args ...any) *sql.Row {
	return d.tx.QueryRow(q, args...)
}
func (d protocolTransaction) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.tx.QueryContext(ctx, q, args...)
}
func (d protocolTransaction) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return d.tx.QueryRowContext(ctx, q, args...)
}

type protocolApproval struct {
	ApprovedAt  time.Time `json:"approved_at"`
	KeepTaskIDs *[]string `json:"keep_task_ids"`
}
type protocolLogger interface{ Printf(string, ...any) }

// ApplyApprovedProtocolPass is called only at engine startup, after migrations
// and before starting task runtime or accepting control/sync writes. Missing or
// invalid approval is a logged no-op, never implicit permission to apply.
func (s *Service) ApplyApprovedProtocolPass(ctx context.Context, logger protocolLogger) error {
	log := func(format string, args ...any) {
		if logger != nil {
			logger.Printf("INFO protocol_v1: "+format, args...)
		}
	}
	var n int
	if err := s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='settings' AND id=?", protocolMarker).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	file, err := os.Open(filepath.Join(s.Hub.Home, "protocol-v1-approved.json"))
	if os.IsNotExist(err) {
		log("approval file absent; skipped")
		return nil
	}
	if err != nil {
		log("approval file unreadable; skipped")
		return nil
	}
	defer file.Close()
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		log("approval must be a regular JSON file under 1 MiB; skipped")
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024))
	decoder.DisallowUnknownFields()
	var approval protocolApproval
	if err = decoder.Decode(&approval); err != nil {
		log("malformed approval JSON or timestamp; skipped")
		return nil
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		log("approval must contain one JSON object; skipped")
		return nil
	}
	if approval.ApprovedAt.IsZero() || approval.KeepTaskIDs == nil {
		log("approval requires approved_at and keep_task_ids array; skipped")
		return nil
	}
	for _, id := range *approval.KeepTaskIDs {
		if strings.TrimSpace(id) == "" {
			log("approval contains an empty keep task ID; skipped")
			return nil
		}
	}
	tx, err := s.Hub.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Reserve the writer before checking the marker and loading the plan.
	if _, err = tx.Exec("UPDATE sync_state SET clock=clock WHERE id=1"); err != nil {
		return err
	}
	if err = tx.QueryRow("SELECT count(*) FROM objects WHERE bucket='settings' AND id=?", protocolMarker).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	now := s.now()
	var messages dbutil.Database
	if s.Hub.Messages != nil {
		messages = s.Hub.Messages.DB
	}
	reader := protocolTransaction{s.Hub.Repo.DB, tx}
	if messages == s.Hub.Repo.DB {
		messages = reader
	}
	plan, err := PlanProtocolPass(reader, now, s.Location, ProtocolOptions{KeepTaskIDs: *approval.KeepTaskIDs, Messages: messages})
	if err != nil {
		return err
	}
	unknown := map[string]bool{}
	for _, id := range *approval.KeepTaskIDs {
		if _, ok := plan.records[id]; !ok {
			unknown[id] = true
		}
	}
	// Use the plan's captured clock for all history, undo and close timestamps.
	writer := &Service{Hub: &hub.Service{Repo: s.Hub.Repo, Messages: s.Hub.Messages, Home: s.Hub.Home, Now: func() time.Time { return now }}, Location: s.Location}
	changed := false
	for _, task := range plan.Tasks {
		if err = ctx.Err(); err != nil {
			return err
		}
		r := plan.records[task.ID]
		before := dbutil.JSON(r)
		for _, action := range task.Actions {
			if err = writer.applyProtocolAction(tx, &r, action); err != nil {
				return fmt.Errorf("apply protocol action %s: %w", action.Rule, err)
			}
		}
		after := dbutil.JSON(r)
		if before != after {
			if _, err = tx.Exec("UPDATE tasks SET data=? WHERE id=?", after, r.ID); err != nil {
				return err
			}
			changed = true
		}
	}
	if _, err = tx.Exec("INSERT INTO objects(bucket,id,data) VALUES('settings',?,?)", protocolMarker, dbutil.JSON(now.UTC().Format(time.RFC3339Nano))); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if len(unknown) > 0 {
		log("ignored %d unknown keep_task_ids", len(unknown))
	}
	log("applied once; marker stored")
	if changed {
		s.Hub.NotifyTaskChanged()
	}
	return nil
}

func (s *Service) applyProtocolAction(tx *sql.Tx, r *record, a ProtocolAction) error {
	before := json.RawMessage(dbutil.JSON(r))
	switch {
	case a.Rule == "review" && a.Action == "keep":
		s.markProtocolKept(r)
		r.UpdatedAt = s.now()
		return s.event(tx, *r, "protocol_v1_kept", "protocol_kept", protocolReviewKeepText, map[string]string{"by": "you"})
	case a.Rule == "demote" && a.Action == "back to suggestion":
		r.Status, r.ClosedAt, r.CloseReason = "suggested", nil, ""
		r.EndedAsSuggestion = false
		r.Suggestion = &Suggestion{DetectedAt: s.now()}
		r.FinishHint = nil
		r.NeedsYou, r.NeedsYouReason, r.NeedsYouAt = false, "", nil
		r.EffortNeeds, r.FlagReason = false, ""
		if err := retireProtocolAttention(tx, r.ID); err != nil {
			return err
		}
		return s.saveRuleChange(tx, r, before, "protocol_demote", "Moved back to suggestions", "Limmr made this, you never asked for it", a.Evidence, MembershipChange{})
	case a.Rule == "deadline" && a.Action == "ends as done":
		if r.Status == "suggested" {
			r.EndedAsSuggestion = true
		}
		return s.finishTask(tx, r, before, "its end time passed", "lifespan", a.Evidence)
	case a.Rule == "finished" && a.Action == "ends as done":
		return s.finishTask(tx, r, before, a.Reason, "done", a.Evidence)
	case a.Rule == "obsolete" && a.Action == "ends as dropped":
		closeTask(r, s.now(), "obsolete")
		r.Status = "dropped"
		if err := retireProtocolAttention(tx, r.ID); err != nil {
			return err
		}
		return s.saveRuleChange(tx, r, before, "obsolete", "Ended", a.Reason, a.Evidence, MembershipChange{})
	case a.Rule == "suggestion_expiry" && a.Action == "expires":
		closeTask(r, s.now(), "suggestion_expired")
		r.Status, r.EndedAsSuggestion = "dropped", true
		if err := retireProtocolAttention(tx, r.ID); err != nil {
			return err
		}
		return s.saveRuleChange(tx, r, before, "suggestion_expiry", "Ended", a.Reason, a.Evidence, MembershipChange{})
	case a.Rule == "finished" && a.Action == "looks finished?":
		r.FinishHint, r.FinishHintAfter, r.FinishHintEffort = a.hint.FinishHint, a.hint.FinishHintAfter, a.hint.FinishHintEffort
		r.UpdatedAt = s.now()
	case a.Rule == "follows_work" && a.Action == "remove departed members":
		return s.removeFollowsWorkMembers(tx, r, before, a.members, r.EffortID)
	}
	// Review-only, missing-data and no-change actions do not write anything.
	return nil
}
func retireProtocolAttention(tx *sql.Tx, id string) error {
	_, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", id)
	return err
}
func (s *Service) markProtocolKept(r *record) {
	r.Origin = "owner"
	r.AutoKeptAt = nil
	r.AskedByOwner = &AskedByOwner{OwnerRequest: OwnerRequest{Text: protocolReviewKeepText}, AgentID: "you", At: s.now()}
}

// GetForOwner records an actual successful control-API open only. Internal reads,
// MCP reads and list/feed projections retain their read-only behavior.
func (s *Service) GetForOwner(id string) (Task, error) {
	task, err := s.Get(nil, id)
	if err != nil {
		return Task{}, err
	}
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.owner_opened_at',?) WHERE id=? AND json_extract(data,'$.owner_opened_at') IS NULL", s.now().UTC().Format(time.RFC3339Nano), id)
	return task, err
}
