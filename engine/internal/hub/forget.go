package hub

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
)

// ForgetSyncedObject emits a fresh privacy marker even for a previously deleted
// row whose ordinary tombstone has already been acknowledged by the peer.
func ForgetSyncedObject(tx *sql.Tx, bucket, id string) error {
	if bucket == "relations" {
		if _, err := tx.Exec(`UPDATE objects SET data='{"forgotten":true,"status":"removed"}' WHERE bucket=? AND id=?`, bucket, id); err != nil {
			return err
		}
	} else {
		_, err := tx.Exec(`UPDATE sync_state SET clock=max(clock+1,coalesce(wall_ms,CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))) WHERE id=1;
 INSERT INTO sync_log(bucket,id,hlc,origin,op,data) SELECT ?,?,clock,engine_id,'tombstone','{"forgotten":true}' FROM sync_state WHERE id=1;
 INSERT INTO sync_meta SELECT ?,?,clock,engine_id,'tombstone' FROM sync_state WHERE id=1 ON CONFLICT(bucket,id) DO UPDATE SET hlc=excluded.hlc,origin=excluded.origin,op=excluded.op`, bucket, id, bucket, id)
		if err != nil {
			return err
		}
	}
	return ScrubForgotten(tx, bucket, id)
}

// ScrubForgotten removes retained payloads, keeping only the replication identity
// and deletion clock. Call in the same transaction as the forget or peer apply.
// Ordinary reversible deletions continue to retain their payloads.
func ScrubForgotten(tx *sql.Tx, bucket, id string) error {
	marker := `{"forgotten":true}`
	if bucket == "relations" {
		marker = `{"forgotten":true,"status":"removed"}`
	}
	if _, err := tx.Exec(`UPDATE sync_log SET data=? WHERE bucket=? AND id=?;
 UPDATE sync_deleted SET data=? WHERE bucket=? AND id=?`, marker, bucket, id, marker, bucket, id); err != nil {
		return err
	}
	if bucket != "people" {
		return nil
	}
	// Circle snapshots also retain owner-taught roles and topic placements. Scrub
	// historical copies as well as the current circle edits made by Forget.
	for _, table := range []string{"sync_log", "sync_deleted"} {
		rows, err := tx.Query(`SELECT rowid,data FROM ` + table + ` WHERE bucket='circles'`)
		if err != nil {
			return err
		}
		type edit struct {
			row int64
			raw string
		}
		edits := []edit{}
		for rows.Next() {
			var row int64
			var raw string
			if err = rows.Scan(&row, &raw); err != nil {
				break
			}
			var circle map[string]any
			if err = json.Unmarshal([]byte(raw), &circle); err != nil {
				break
			}
			changed := false
			if people, ok := circle["people"].([]any); ok {
				kept := []any{}
				for _, person := range people {
					p, _ := person.(map[string]any)
					if p["person_id"] == id {
						changed = true
					} else {
						kept = append(kept, person)
					}
				}
				circle["people"] = kept
			}
			if topics, ok := circle["topics"].([]any); ok {
				for _, topic := range topics {
					obj, _ := topic.(map[string]any)
					if people, ok := obj["people"].([]any); ok {
						kept := []any{}
						for _, person := range people {
							if person == id {
								changed = true
							} else {
								kept = append(kept, person)
							}
						}
						obj["people"] = kept
					}
				}
			}
			if changed {
				edits = append(edits, edit{row, dbutil.JSON(circle)})
			}
		}
		if e := rows.Err(); err == nil {
			err = e
		}
		rows.Close()
		if err != nil {
			return err
		}
		for _, edit := range edits {
			if _, err = tx.Exec(`UPDATE `+table+` SET data=? WHERE rowid=?`, edit.raw, edit.row); err != nil {
				return err
			}
		}
	}
	return nil
}
