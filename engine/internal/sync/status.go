package sync

import (
	"context"
	"encoding/json"
	"limmr/internal/dbutil"
)

func (s *Service) Status() (map[string]any, error) {
	var p Peer
	_ = s.Repo.Get("settings", "sync_peer", &p)
	var lease Lease
	_ = s.Repo.Get("settings", "automation_lease", &lease)
	reviews, e := s.Reviews()
	if e != nil {
		return nil, e
	}
	pending := []Review{}
	for _, r := range reviews {
		if r.State == "pending" {
			pending = append(pending, r)
		}
	}
	var out int
	if e = s.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log WHERE seq>?`, p.LastPushedSeq).Scan(&out); e != nil {
		return nil, e
	}
	return map[string]any{"paired": p.EngineID != "", "automations_paused": false, "automation_policy": "single_holder", "paused": p.Paused, "engine_id": s.EngineID, "peer": map[string]any{"engine_id": p.EngineID, "name": "Your dot", "last_sync_at": p.LastSyncAt, "last_error": p.LastError}, "lease": map[string]any{"holder": lease.EngineID, "renewed_at": lease.RenewedAt, "this_mac": s.HoldsLease()}, "pending": map[string]int{"out": out, "in": 0}, "items_in": p.Applied, "items_out": p.Sent, "merge_reviews": pending}, nil
}

// Owner resolution writes ordinary synced knowledge in the same transaction as
// the local review decision. Both source records remain available.
func (s *Service) ResolveReview(ctx context.Context, id, action string) error {
	if action != "merge" && action != "keep_both" {
		return ErrInvalid
	}
	tx, e := s.Repo.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw string
	if e = tx.QueryRow(`SELECT data FROM sync_reviews WHERE id=?`, id).Scan(&raw); e != nil {
		return e
	}
	var r Review
	if json.Unmarshal([]byte(raw), &r) != nil || r.State != "pending" || len(r.IDs) != 2 {
		return ErrInvalid
	}
	if action == "merge" {
		values := []map[string]any{}
		for _, key := range r.IDs {
			if r.Bucket == "tasks" {
				e = tx.QueryRow(`SELECT data FROM tasks WHERE id=?`, key).Scan(&raw)
			} else if r.Bucket == "circles" {
				e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='circles' AND id=?`, key).Scan(&raw)
			} else {
				return ErrInvalid
			}
			if e != nil {
				return e
			}
			var v map[string]any
			if json.Unmarshal([]byte(raw), &v) != nil {
				return ErrInvalid
			}
			values = append(values, v)
		}
		fill(values[0], values[1])
		values[0]["updated_at"] = s.Now()
		values[1]["updated_at"] = s.Now()
		if r.Bucket == "tasks" {
			values[1]["status"] = "dropped"
			values[1]["close_reason"] = "merged"
			values[1]["closed_at"] = s.Now()
		} else {
			values[1]["status"] = "archived"
			values[1]["archived"] = true
		}
		for i, key := range r.IDs {
			if r.Bucket == "tasks" {
				_, e = tx.Exec(`UPDATE tasks SET data=? WHERE id=?`, dbutil.JSON(values[i]), key)
			} else {
				_, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='circles' AND id=?`, dbutil.JSON(values[i]), key)
			}
			if e != nil {
				return e
			}
		}
	}
	r.State = action
	if _, e = tx.Exec(`UPDATE sync_reviews SET data=? WHERE id=?`, dbutil.JSON(r), id); e != nil {
		return e
	}
	return tx.Commit()
}
