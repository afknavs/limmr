package hub

import "limmr/internal/triggers"

func (r *Repo) migrateWatches() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=3").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}
	if _, err = tx.Exec(triggers.Schema); err != nil {
		return err
	}
	if _, err = tx.Exec(`
 CREATE TABLE trigger_watch_state(watch_id TEXT PRIMARY KEY REFERENCES trigger_watches(id) ON DELETE CASCADE,new_hits INTEGER NOT NULL DEFAULT 0,digest TEXT NOT NULL DEFAULT '',replies_sent INTEGER NOT NULL DEFAULT 0,changed_at TEXT NOT NULL);
 CREATE TABLE trigger_run_credentials(digest TEXT PRIMARY KEY,agent_id TEXT NOT NULL,handoff_id TEXT NOT NULL REFERENCES trigger_watches(id) ON DELETE CASCADE,session_id TEXT NOT NULL,run_id TEXT NOT NULL REFERENCES trigger_runs(id) ON DELETE CASCADE);
 CREATE TABLE trigger_outbound(chat_jid TEXT NOT NULL,message_id TEXT NOT NULL,watch_id TEXT,run_id TEXT,at TEXT NOT NULL,PRIMARY KEY(chat_jid,message_id));
 INSERT INTO hub_schema VALUES(3);`); err != nil {
		return err
	}
	return tx.Commit()
}
