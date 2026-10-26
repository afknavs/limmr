package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Rebuild only the CHECK-constrained hits table. Foreign keys are restored on
// this same connection before Open returns; run/reply history is preserved.
func (r *Repo) migrateUpdates() error {
	ctx := context.Background()
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var n int
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM hub_schema WHERE version=6").Scan(&n); err != nil || n > 0 {
		return err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO objects VALUES('updates','cursor_key',json_quote(?))", hex.EncodeToString(key)); err != nil {
		return err
	}
	_, err = tx.Exec(`CREATE TABLE w1_hits (
 id TEXT PRIMARY KEY, watch_id TEXT NOT NULL REFERENCES trigger_watches(id) ON DELETE CASCADE,
 at TEXT NOT NULL, action TEXT NOT NULL CHECK(action IN ('feed','notify','digest','park_suggestion','wake_session')),
 state TEXT NOT NULL CHECK(state IN ('recorded','completed','failed','cancelled')),
 messages_json TEXT NOT NULL CHECK(json_valid(messages_json)));
 INSERT INTO w1_hits SELECT * FROM trigger_hits;
 DROP TABLE trigger_hits;
 ALTER TABLE w1_hits RENAME TO trigger_hits;
 CREATE INDEX trigger_hits_watch_at ON trigger_hits(watch_id,at,id);
 CREATE UNIQUE INDEX w1_one_feed ON trigger_watches(owner) WHERE status IN ('active','paused') AND json_extract(definition_json,'$.action')='feed';
 CREATE TABLE agent_updates(seq INTEGER PRIMARY KEY AUTOINCREMENT,agent TEXT NOT NULL,at TEXT NOT NULL,kind TEXT NOT NULL,ref TEXT NOT NULL,part INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT '');
 CREATE INDEX agent_updates_agent_seq ON agent_updates(agent,seq);
 CREATE INDEX agent_updates_agent_at ON agent_updates(agent,julianday(at),seq);
 CREATE TRIGGER w1_hit AFTER INSERT ON trigger_hits BEGIN
 INSERT INTO agent_updates(agent,at,kind,ref,part) SELECT w.owner,NEW.at,'hit',NEW.id,m.key FROM trigger_watches w,json_each(NEW.messages_json) m WHERE w.id=NEW.watch_id AND m.type='object';
 END;
 INSERT INTO agent_updates(agent,at,kind,ref,part) SELECT w.owner,h.at,'hit',h.id,m.key FROM trigger_hits h JOIN trigger_watches w ON w.id=h.watch_id,json_each(h.messages_json) m WHERE m.type='object' ORDER BY h.at,h.id,m.key;
 CREATE TRIGGER w1_send_insert AFTER INSERT ON objects WHEN NEW.bucket IN ('drafts','approvals') BEGIN
 INSERT INTO agent_updates(agent,at,kind,ref,status) VALUES(json_extract(NEW.data,'$.agent_id'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),NEW.bucket,NEW.id,COALESCE(NULLIF(json_extract(NEW.data,'$.status'),''),json_extract(NEW.data,'$.state')));
 END;
 CREATE TRIGGER w1_send_update AFTER UPDATE ON objects WHEN NEW.bucket IN ('drafts','approvals') AND (CASE WHEN json_extract(NEW.data,'$.state') IN ('failed','denied','expired','cancelled') THEN json_extract(NEW.data,'$.state') ELSE COALESCE(NULLIF(json_extract(NEW.data,'$.status'),''),json_extract(NEW.data,'$.state')) END) IS NOT (CASE WHEN json_extract(OLD.data,'$.state') IN ('failed','denied','expired','cancelled') THEN json_extract(OLD.data,'$.state') ELSE COALESCE(NULLIF(json_extract(OLD.data,'$.status'),''),json_extract(OLD.data,'$.state')) END) BEGIN
 INSERT INTO agent_updates(agent,at,kind,ref,status) VALUES(json_extract(NEW.data,'$.agent_id'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),NEW.bucket,NEW.id,CASE WHEN json_extract(NEW.data,'$.state') IN ('failed','denied','expired','cancelled') THEN json_extract(NEW.data,'$.state') ELSE COALESCE(NULLIF(json_extract(NEW.data,'$.status'),''),json_extract(NEW.data,'$.state')) END);
 END;
 INSERT INTO agent_updates(agent,at,kind,ref,status) SELECT json_extract(data,'$.agent_id'),COALESCE(json_extract(data,'$.read_at'),json_extract(data,'$.delivered_at'),json_extract(data,'$.sent_at'),json_extract(data,'$.created_at')),bucket,id,CASE WHEN json_extract(data,'$.state') IN ('failed','denied','expired','cancelled') THEN json_extract(data,'$.state') ELSE COALESCE(NULLIF(json_extract(data,'$.status'),''),json_extract(data,'$.state')) END FROM objects WHERE bucket IN ('drafts','approvals');
 INSERT INTO hub_schema VALUES(6);`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
