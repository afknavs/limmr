package tasks

import (
	"database/sql"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"time"
)

// Receipts are created by the receiving engine, only after sync has committed a
// due event. They travel back through task_events; queued never means delivered.
// This runs even on a peer that doesn't hold the automation lease.
func (s *Service) ReceiveCheckIns() error {
	// Note events replicate independently of task rows. Once committed locally,
	// own AIs receive a private cursor update with the current merged notes.
	if _, err := s.Hub.Repo.DB.Exec(`INSERT INTO agent_updates(agent,at,kind,ref)
      SELECT a.id,e.at,'task_owner_note',e.id FROM task_events e
      JOIN sync_meta m ON m.bucket='task_events' AND m.id=e.id
      JOIN sync_state st ON st.id=1
      JOIN tasks t ON t.id=e.task_id
      JOIN objects a ON a.bucket='agents' AND (json_extract(a.data,'$.preset')='own' OR a.id=json_extract(t.data,'$.created_by.agent_id'))
      WHERE e.kind='task_owner_note' AND m.origin<>st.engine_id
      AND NOT EXISTS(SELECT 1 FROM agent_updates u WHERE u.agent=a.id AND u.ref=e.id AND u.kind='task_owner_note')`); err != nil {
		return err
	}
	rows, err := s.Hub.Repo.DB.Query(`SELECT e.id,e.task_id FROM task_events e JOIN sync_meta m ON m.bucket='task_events' AND m.id=e.id JOIN sync_state st ON st.id=1 WHERE e.kind='task_due' AND m.origin<>st.engine_id AND NOT EXISTS(SELECT 1 FROM task_events ack WHERE ack.id=e.id||':received:'||st.engine_id)`)
	if err != nil {
		return err
	}
	type due struct{ id, task string }
	xs := []due{}
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.id, &d.task); err != nil {
			break
		}
		xs = append(xs, d)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range xs {
		_, err = s.mutate(nil, d.task, func(r *record, tx *sql.Tx) error {
			var engine string
			if err := tx.QueryRow("SELECT engine_id FROM sync_state WHERE id=1").Scan(&engine); err != nil {
				return err
			}
			id := d.id + ":received:" + engine
			var n int
			if err := tx.QueryRow("SELECT count(*) FROM task_events WHERE id=?", id).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
			suggestion := model.Suggestion{ID: id, TaskID: r.ID, Kind: "check_in", Why: "Check-in received from your paired Limmr", CreatedAt: s.now(), ExpiresAt: s.now().AddDate(0, 0, 90), State: "pending", CreatedBy: r.CreatedBy.AgentID}
			if len(r.Chats) > 0 {
				suggestion.ChatJID = r.Chats[0].ID
			}
			// Synced slot claims may have already materialized this exact
			// due slot as a suggestion. Keep its identity and do not notify twice.
			var existing int
			if err := tx.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.kind')='check_in' AND (id=? OR substr(id,1,length(?)+1)=?||':')`, d.id, d.id, d.id).Scan(&existing); err != nil {
				return err
			}
			if existing == 0 {
				if _, err := tx.Exec("INSERT OR IGNORE INTO objects VALUES('suggestions',?,?)", id, dbutil.JSON(suggestion)); err != nil {
					return err
				}
			}
			if _, err := tx.Exec("INSERT OR IGNORE INTO task_events VALUES(?,?,?,?,?,?)", id, r.ID, s.now().UTC().Format(time.RFC3339Nano), "check_in_received", "Sent to your dot", dbutil.JSON(map[string]string{"event_id": d.id, "receiver": engine})); err != nil {
				return err
			}
			// Peer agent identities are local. Own AIs with access see this on get_updates.
			_, err := tx.Exec(`INSERT INTO agent_updates(agent,at,kind,ref) SELECT id,?,'task_due',? FROM objects WHERE bucket='agents' AND json_extract(data,'$.preset')='own'`, s.now().UTC().Format(time.RFC3339Nano), d.id)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
