package hub

import (
	"context"
	"database/sql"
	"time"
)

// CompactSyncBatch scans at most limit sequence numbers and deletes only rows
// with a later version of the same key. Sequence numbers are never renumbered;
// cursors in gaps remain valid and newest tombstones survive forever.
// Keep the first task upsert as a parent anchor: its events can precede the
// latest task edit by several pages and must never arrive without their task.
// Briefs are immutable source editions (merge.brief), not LWW: retain them.
// Lease epochs retain their authority chain so an offline peer can verify every
// handover even when the latest recorded holder skips more than one epoch.
func (r *Repo) CompactSyncBatch(ctx context.Context, after int64, limit int) (next int64, deleted int64, err error) {
	return compactSyncBatch(ctx, r.DB, after, limit)
}

type syncBatchDB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func compactSyncBatch(ctx context.Context, db syncBatchDB, after int64, limit int) (next int64, deleted int64, err error) {
	if limit < 1 || limit > 256 {
		limit = 256
	}
	rows, err := db.QueryContext(ctx, `SELECT seq FROM sync_log WHERE seq>? ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return after, 0, err
	}
	next = after
	for rows.Next() {
		if err = rows.Scan(&next); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || next == after {
		return next, 0, err
	}
	result, err := db.ExecContext(ctx, `DELETE FROM sync_log WHERE bucket<>'briefs' AND NOT (bucket='settings' AND id='automation_lease') AND seq>? AND seq<=? AND NOT (bucket='tasks' AND op='upsert' AND NOT EXISTS(SELECT 1 FROM sync_log parent WHERE parent.bucket='tasks' AND parent.id=sync_log.id AND parent.op='upsert' AND parent.seq<sync_log.seq)) AND EXISTS(SELECT 1 FROM sync_log newer WHERE newer.bucket=sync_log.bucket AND newer.id=sync_log.id AND newer.seq>sync_log.seq)`, after, next)
	if err != nil {
		return next, 0, err
	}
	deleted, err = result.RowsAffected()
	return
}

// The cursor commits only after the corresponding delete. Replaying a batch
// after a crash is safe; completed batches are never scanned again on restart.
func (r *Repo) migrateSyncStorage() error {
	_, err := r.DB.Exec(`CREATE TABLE IF NOT EXISTS sync_storage(id INTEGER PRIMARY KEY CHECK(id=1),cursor INTEGER NOT NULL DEFAULT 0,complete INTEGER NOT NULL DEFAULT 0);
 INSERT OR IGNORE INTO sync_storage(id) VALUES(1); INSERT OR IGNORE INTO hub_schema VALUES(13);`)
	return err
}

func (r *Repo) compactSyncPage(ctx context.Context) (complete bool, err error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var after int64
	if err = tx.QueryRowContext(ctx, `SELECT cursor FROM sync_storage WHERE id=1`).Scan(&after); err != nil {
		return false, err
	}
	// Amortize WAL syncs while bounding each write transaction to 4096 rows or
	// 50 ms. Cursor and deletes now commit atomically, including after a crash.
	started := time.Now()
	for batch := 0; batch < 16; batch++ {
		next, _, e := compactSyncBatch(ctx, tx, after, 256)
		if e != nil {
			return false, e
		}
		complete = next == after
		after = next
		if complete || time.Since(started) >= 50*time.Millisecond {
			break
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_storage SET cursor=?,complete=? WHERE id=1`, after, complete); err != nil {
		return false, err
	}
	return complete, tx.Commit()
}
func (r *Repo) compactSync(ctx context.Context, yield time.Duration) error {
	for {
		complete, err := r.compactSyncPage(ctx)
		if err != nil {
			return err
		}
		if complete {
			return nil
		}
		if yield > 0 {
			timer := time.NewTimer(yield)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}

// The startup listener is already answering while this bounded work runs.
// Large stores resume from the durable cursor in serving maintenance or next boot.
func (r *Repo) PrepareSyncStorage(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := r.compactSync(bounded, 0); err != nil {
		return err
	}
	return r.reclaimSyncStorage(ctx)
}

func (r *Repo) reclaimSyncStorage(ctx context.Context) error {
	var complete, mode int
	if err := r.DB.QueryRowContext(ctx, `SELECT complete FROM sync_storage WHERE id=1`).Scan(&complete); err != nil {
		return err
	}
	if complete == 0 {
		return nil
	}
	if err := r.DB.QueryRowContext(ctx, `PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return err
	}
	// Switching from NONE requires one atomic VACUUM. Never repeat a full rebuild
	// after conversion. SQLite rolls it back on timeout; pages remain reusable.
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if mode != 2 {
		if _, err := r.DB.ExecContext(bounded, `PRAGMA auto_vacuum=INCREMENTAL; VACUUM`); err != nil {
			return err
		}
	} else {
		if _, err := r.DB.ExecContext(bounded, `PRAGMA incremental_vacuum(256)`); err != nil {
			return err
		}
	}
	_, err := r.DB.ExecContext(bounded, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func (r *Repo) MaintainSync(ctx context.Context) {
	for {
		if r.compactSync(ctx, 5*time.Millisecond) != nil {
			return
		}
		_ = r.reclaimSyncStorage(ctx)
		// Future passes revisit old keys after newer edits arrive. Preserve a partly
		// finished pass across interruption, including a failed space reclamation.
		_, _ = r.DB.ExecContext(ctx, `UPDATE sync_storage SET cursor=0,complete=0 WHERE id=1`)
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
