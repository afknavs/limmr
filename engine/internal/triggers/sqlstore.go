package triggers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"limmr/internal/dbutil"
	"sort"
	"time"
)

// SQLStore uses the hub's connection pool. Every mutation is transactional;
// count-and-reserve uses BEGIN IMMEDIATE even if the pool has several connections.
type SQLStore struct {
	DB    dbutil.Database
	Event func(context.Context, string, Record, *RunRecord) error
}

type WatchState struct {
	NewHits   int
	Digest    string
	Replies   int
	ChangedAt time.Time
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func encoded(v any) string     { b, _ := json.Marshal(v); return string(b) }
func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (s *SQLStore) event(ctx context.Context, kind string, r Record, run *RunRecord) error {
	if s.Event != nil {
		return s.Event(ctx, kind, r, run)
	}
	return nil
}
func (s *SQLStore) Create(ctx context.Context, r Record) error {
	if err := validate(r); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expiry any
	if r.ExpiresAt != nil {
		expiry = stamp(*r.ExpiresAt)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO trigger_watches(id,owner,status,expires_at,definition_json) VALUES(?,?,?,?,?)`, r.ID, r.Owner, r.Status, expiry, encoded(r))
	if err != nil {
		var n int
		if tx.QueryRowContext(ctx, "SELECT count(*) FROM trigger_watches WHERE id=?", r.ID).Scan(&n) == nil && n > 0 {
			return ErrExists
		}
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO trigger_watch_state(watch_id,changed_at) VALUES(?,?)", r.ID, stamp(r.CreatedAt)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.event(ctx, "created", r, nil)
}
func (s *SQLStore) Get(ctx context.Context, id string) (Record, error) {
	var raw string
	var r Record
	err := s.DB.QueryRowContext(ctx, "SELECT definition_json FROM trigger_watches WHERE id=?", id).Scan(&raw)
	if err != nil {
		return r, missing(err)
	}
	err = json.Unmarshal([]byte(raw), &r)
	return r, err
}
func (s *SQLStore) List(ctx context.Context) ([]Record, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT definition_json FROM trigger_watches ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var raw string
		var r Record
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *SQLStore) Update(ctx context.Context, id string, fn func(*Record) error) error {
	// The first statement takes SQLite's write reservation before reading.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE trigger_watches SET status=status WHERE id=?", id); err != nil {
		return err
	}
	var raw string
	var r Record
	if err = tx.QueryRowContext(ctx, "SELECT definition_json FROM trigger_watches WHERE id=?", id).Scan(&raw); err != nil {
		return missing(err)
	}
	if err = json.Unmarshal([]byte(raw), &r); err != nil {
		return err
	}
	before := copyValue(r)
	if err = fn(&r); err != nil {
		return err
	}
	if r.ID != id {
		return ErrNotFound
	}
	if err = validate(r); err != nil {
		return err
	}
	var expiry any
	if r.ExpiresAt != nil {
		expiry = stamp(*r.ExpiresAt)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE trigger_watches SET owner=?,status=?,expires_at=?,definition_json=? WHERE id=?", r.Owner, r.Status, expiry, encoded(r), id); err != nil {
		return err
	}
	if before.Status != r.Status {
		if _, err = tx.ExecContext(ctx, "UPDATE trigger_watch_state SET changed_at=? WHERE watch_id=?", stamp(time.Now()), id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	kind := "updated"
	if before.Status != r.Status {
		kind = string(r.Status)
		if r.Status == Active {
			kind = "resumed"
		}
	}
	if len(r.Instructions) > len(before.Instructions) {
		kind = "instruction_added"
	}
	return s.event(ctx, kind, r, nil)
}
func (s *SQLStore) Delete(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM trigger_watches WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}
func (s *SQLStore) PutHit(ctx context.Context, h Hit) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO trigger_hits VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, h.ID, h.WatchID, stamp(h.At), h.Action, h.State, encoded(h.Messages))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		_, err = tx.ExecContext(ctx, "UPDATE trigger_watch_state SET new_hits=new_hits+? WHERE watch_id=?", len(h.Messages), h.WatchID)
	} else {
		res, err = tx.ExecContext(ctx, "UPDATE trigger_hits SET state=?,messages_json=? WHERE id=? AND watch_id=?", h.State, encoded(h.Messages), h.ID, h.WatchID)
		if err == nil {
			affected, e := res.RowsAffected()
			err = e
			if err == nil && affected == 0 {
				return ErrNotFound
			}
		}
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if n > 0 && (h.Action == Digest || h.Action == ParkSuggestion) {
		r, err := s.Get(ctx, h.WatchID)
		if err != nil {
			return err
		}
		return s.event(ctx, string(h.Action)+"_hit", r, &RunRecord{HandoffID: h.WatchID, HitID: h.ID, StartedAt: h.At, State: "recorded"})
	}
	return nil
}
func (s *SQLStore) Hits(ctx context.Context, id string, limit int) ([]Hit, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,at,action,state,messages_json FROM trigger_hits WHERE watch_id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		h := Hit{WatchID: id}
		var at, raw string
		if err = rows.Scan(&h.ID, &at, &h.Action, &h.State, &raw); err != nil {
			return nil, err
		}
		if h.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &h.Messages); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].At.Equal(out[j].At) {
			return out[i].ID < out[j].ID
		}
		return out[i].At.Before(out[j].At)
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
func (s *SQLStore) PutRun(ctx context.Context, r RunRecord) error {
	var end any
	if !r.FinishedAt.IsZero() {
		end = stamp(r.FinishedAt)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO trigger_runs SELECT ?,?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM trigger_hits WHERE id=? AND watch_id=?) ON CONFLICT(id) DO UPDATE SET finished_at=excluded.finished_at,exit_code=excluded.exit_code,timed_out=excluded.timed_out,log_path=excluded.log_path,state=excluded.state WHERE trigger_runs.handoff_id=excluded.handoff_id AND trigger_runs.hit_id=excluded.hit_id`, r.ID, r.HandoffID, r.HitID, stamp(r.StartedAt), end, r.ExitCode, r.TimedOut, r.LogPath, r.State, r.HitID, r.HandoffID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	w, err := s.Get(ctx, r.HandoffID)
	if err != nil {
		return err
	}
	return s.event(ctx, "run", w, &r)
}
func (s *SQLStore) Runs(ctx context.Context, id string) ([]RunRecord, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id,hit_id,started_at,finished_at,exit_code,timed_out,coalesce(log_path,''),state FROM trigger_runs WHERE handoff_id=?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunRecord{}
	for rows.Next() {
		r := RunRecord{HandoffID: id}
		var at string
		var end sql.NullString
		if err = rows.Scan(&r.ID, &r.HitID, &at, &end, &r.ExitCode, &r.TimedOut, &r.LogPath, &r.State); err != nil {
			return nil, err
		}
		if r.StartedAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		if end.Valid {
			if r.FinishedAt, err = time.Parse(time.RFC3339Nano, end.String); err != nil {
				return nil, err
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out, rows.Err()
}
func (s *SQLStore) ReplyCount(ctx context.Context, id string, now time.Time) (int, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return 0, err
	}
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM trigger_replies WHERE handoff_id=? AND julianday(at)>julianday(?)", id, stamp(now.Add(-time.Hour))).Scan(&n)
	return n, err
}
func (s *SQLStore) ReserveReply(ctx context.Context, id string, now time.Time, max int) (bool, error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return false, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var status string
	if err = conn.QueryRowContext(ctx, "SELECT status FROM trigger_watches WHERE id=?", id).Scan(&status); err != nil {
		return false, missing(err)
	}
	var n int
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM trigger_replies WHERE handoff_id=? AND julianday(at)>julianday(?)", id, stamp(now.Add(-time.Hour))).Scan(&n); err != nil {
		return false, err
	}
	if max < 1 || n >= max || status != "active" {
		return false, nil
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO trigger_replies(handoff_id,at) VALUES(?,?)", id, stamp(now)); err != nil {
		return false, err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err == nil, err
}
func (s *SQLStore) State(ctx context.Context, id string) (WatchState, error) {
	var v WatchState
	var changed string
	err := s.DB.QueryRowContext(ctx, "SELECT new_hits,digest,replies_sent,changed_at FROM trigger_watch_state WHERE watch_id=?", id).Scan(&v.NewHits, &v.Digest, &v.Replies, &changed)
	if err == nil {
		v.ChangedAt, err = time.Parse(time.RFC3339Nano, changed)
	}
	return v, missing(err)
}
func (s *SQLStore) Seen(ctx context.Context, id string) error {
	_, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE trigger_watch_state SET new_hits=0 WHERE watch_id=?", id)
	return err
}
func (s *SQLStore) SetDigest(ctx context.Context, id, text string) error {
	_, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE trigger_watch_state SET digest=? WHERE watch_id=?", text, id)
	return err
}

// Recover never replays a session. Even a previously running CLI may remain
// alive after an engine crash, so the owner must verify it before resuming.
func (s *SQLStore) Recover(ctx context.Context, now time.Time) (int, error) {
	records, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, r := range records {
		runs, err := s.Runs(ctx, r.ID)
		if err != nil {
			return count, err
		}
		for _, run := range runs {
			if run.State != "running" {
				continue
			}
			// Pause first so any surviving credentials/drafts become unusable immediately.
			if err = s.Update(ctx, r.ID, func(v *Record) error {
				if v.Status == Active {
					v.Status = Paused
				}
				return nil
			}); err != nil {
				return count, err
			}
			run.State = "cancelled"
			run.ExitCode = -1
			run.FinishedAt = now
			if err = s.PutRun(ctx, run); err != nil {
				return count, err
			}
			if _, err = s.DB.ExecContext(ctx, "UPDATE trigger_hits SET state='cancelled' WHERE id=?", run.HitID); err != nil {
				return count, err
			}
			count++
		}
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE trigger_hits SET state='cancelled' WHERE state='recorded'")
	return count, err
}
