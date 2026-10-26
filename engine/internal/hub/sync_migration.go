package hub

import (
	"fmt"
	"limmr/internal/dbutil"
)

// SyncObjectPredicate is shared by capture, bootstrap and protocol validation.
// Existing memberships with group/admin are WhatsApp directory rows, not placements.
func SyncObjectPredicate(row string) string {
	return fmt.Sprintf(`(%[1]s.bucket IN ('people','person_aliases','circles','briefs','facts','relations','entity_icons','task_dismissals') OR (%[1]s.bucket='memberships' AND json_type(%[1]s.data,'$.group') IS NULL) OR (%[1]s.bucket='settings' AND %[1]s.id IN ('owner_profile','automation_lease')))`, row)
}

func (r *Repo) migrateSync() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS sync_state(id INTEGER PRIMARY KEY CHECK(id=1),engine_id TEXT NOT NULL,clock INTEGER NOT NULL DEFAULT 0,remote_hlc INTEGER,remote_origin TEXT,wall_ms INTEGER,suppress INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS sync_log(seq INTEGER PRIMARY KEY AUTOINCREMENT,bucket TEXT NOT NULL,id TEXT NOT NULL,hlc INTEGER NOT NULL,origin TEXT NOT NULL,op TEXT NOT NULL CHECK(op IN ('upsert','tombstone')),data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_meta(bucket TEXT,id TEXT,hlc INTEGER NOT NULL,origin TEXT NOT NULL,op TEXT NOT NULL,PRIMARY KEY(bucket,id));
 CREATE TABLE IF NOT EXISTS sync_deleted(bucket TEXT,id TEXT,data TEXT NOT NULL,deleted_at INTEGER NOT NULL,PRIMARY KEY(bucket,id));
 CREATE TABLE IF NOT EXISTS sync_items(task_id TEXT,list TEXT,id TEXT,hlc INTEGER NOT NULL,origin TEXT NOT NULL,removed INTEGER NOT NULL,data TEXT NOT NULL,PRIMARY KEY(task_id,list,id));
 CREATE TABLE IF NOT EXISTS sync_briefs(circle TEXT,source TEXT,data TEXT NOT NULL,hlc INTEGER NOT NULL,origin TEXT NOT NULL,PRIMARY KEY(circle,source));
 CREATE TABLE IF NOT EXISTS sync_reviews(id TEXT PRIMARY KEY,data TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_slot_claims(claim_id TEXT PRIMARY KEY,slot TEXT NOT NULL,hlc INTEGER NOT NULL,origin TEXT NOT NULL,state TEXT NOT NULL,reason TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO sync_state(id,engine_id) VALUES(1,?); INSERT OR IGNORE INTO objects SELECT 'settings','engine',json_object('engine_id',engine_id) FROM sync_state WHERE id=1`, dbutil.ID())
	if err != nil {
		return err
	}
	for _, table := range []string{"objects", "tasks", "task_events"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			kind := "upsert"
			if op == "DELETE" {
				row = "OLD"
				kind = "tombstone"
			}
			bucket := "'" + table + "'"
			cond := "1"
			data := row + ".data"
			if table == "objects" {
				bucket = row + ".bucket"
				cond = SyncObjectPredicate(row)
			}
			if table == "task_events" {
				data = fmt.Sprintf(`json_object('id',%[1]s.id,'task_id',%[1]s.task_id,'at',%[1]s.at,'kind',%[1]s.kind,'text',%[1]s.text,'payload',json(%[1]s.payload))`, row)
			}
			body := fmt.Sprintf(`UPDATE sync_state SET clock=max(clock+1,coalesce(remote_hlc,wall_ms,CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))) WHERE id=1;
 INSERT INTO sync_log(bucket,id,hlc,origin,op,data) SELECT %s,%s.id,coalesce(remote_hlc,clock),coalesce(remote_origin,engine_id),'%s',%s FROM sync_state WHERE id=1;
 INSERT INTO sync_meta SELECT %s,%s.id,coalesce(remote_hlc,clock),coalesce(remote_origin,engine_id),'%s' FROM sync_state WHERE id=1 ON CONFLICT(bucket,id) DO UPDATE SET hlc=excluded.hlc,origin=excluded.origin,op=excluded.op;`, bucket, row, kind, data, bucket, row, kind)
			if op == "DELETE" {
				body += fmt.Sprintf(`INSERT OR REPLACE INTO sync_deleted SELECT %s,%s.id,%s,clock FROM sync_state WHERE id=1;`, bucket, row, data)
			} else {
				body += fmt.Sprintf(`DELETE FROM sync_deleted WHERE bucket=%s AND id=%s.id;`, bucket, row)
			}
			if table == "objects" && op != "DELETE" {
				body += `INSERT OR IGNORE INTO sync_briefs SELECT json_extract(NEW.data,'$.circle_id'),coalesce(remote_origin,engine_id)||':'||coalesce(remote_hlc,clock),NEW.data,coalesce(remote_hlc,clock),coalesce(remote_origin,engine_id) FROM sync_state WHERE id=1 AND NEW.bucket='briefs' AND json_extract(NEW.data,'$.circle_id') IS NOT NULL;`
			}
			if table == "tasks" && op != "DELETE" {
				// Do not recapture peer materialization as local item edits.
				for _, list := range []string{"checklist", "items", "updates_sent"} {
					itemID := `coalesce(json_extract(j.value,'$.id'),json_extract(j.value,'$.chat_jid')||':'||json_extract(j.value,'$.message_id'))`
					body += fmt.Sprintf(`INSERT INTO sync_items SELECT NEW.id,'%[1]s',%[2]s,s.clock,s.engine_id,0,j.value FROM json_each(NEW.data,'$.%[1]s') j,sync_state s WHERE s.id=1 AND s.remote_hlc IS NULL AND %[2]s IS NOT NULL AND NOT EXISTS(SELECT 1 FROM sync_items i WHERE i.task_id=NEW.id AND i.list='%[1]s' AND i.id=%[2]s AND i.data=j.value AND i.removed=0) ON CONFLICT(task_id,list,id) DO UPDATE SET hlc=excluded.hlc,origin=excluded.origin,removed=0,data=excluded.data;
 UPDATE sync_items SET removed=1,hlc=(SELECT clock FROM sync_state),origin=(SELECT engine_id FROM sync_state) WHERE task_id=NEW.id AND list='%[1]s' AND removed=0 AND (SELECT remote_hlc FROM sync_state) IS NULL AND id NOT IN (SELECT %[2]s FROM json_each(NEW.data,'$.%[1]s') j WHERE %[2]s IS NOT NULL);`, list, itemID)
				}
			}
			_, err = tx.Exec(fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS sync_%s_%s AFTER %s ON %s WHEN (SELECT suppress FROM sync_state)=0 AND %s BEGIN %s END`, table, op, op, table, cond, body))
			if err != nil {
				return err
			}
		}
	}
	// Relation removal is a normal synced field edit; existing readers hide it.
	_, err = tx.Exec(`CREATE TRIGGER IF NOT EXISTS sync_relation_remove BEFORE DELETE ON objects WHEN OLD.bucket='relations' AND coalesce(json_extract(OLD.data,'$.status'),'')<>'removed' BEGIN UPDATE objects SET data=json_set(data,'$.status','removed') WHERE bucket=OLD.bucket AND id=OLD.id; SELECT RAISE(IGNORE); END;
 CREATE TRIGGER IF NOT EXISTS sync_relation_keep BEFORE DELETE ON objects WHEN OLD.bucket='relations' AND json_extract(OLD.data,'$.status')='removed' BEGIN SELECT RAISE(IGNORE); END;`)
	if err != nil {
		return err
	}
	// Suggestion rows are local projections. Edits/decisions for a claimed draft travel
	// as task events rather than expanding the synced object buckets.
	_, err = tx.Exec(`CREATE TRIGGER IF NOT EXISTS sync_suggestion_update AFTER UPDATE ON objects
 WHEN NEW.bucket='suggestions' AND NEW.data<>OLD.data AND (SELECT suppress FROM sync_state)=0
 AND EXISTS(SELECT 1 FROM task_events WHERE kind='slot_claim' AND json_extract(payload,'$.suggestion.id')=NEW.id)
 BEGIN
 INSERT INTO task_events(id,task_id,at,kind,text,payload)
 SELECT NEW.id||':revision:'||s.engine_id||':'||(s.clock+1),json_extract(NEW.data,'$.task_id'),strftime('%Y-%m-%dT%H:%M:%fZ','now'),'slot_suggestion','Scheduled draft updated',
 json_object('slot',json_extract(e.payload,'$.slot'),'suggestion',json(NEW.data))
 FROM task_events e,sync_state s WHERE e.kind='slot_claim' AND json_extract(e.payload,'$.suggestion.id')=NEW.id AND s.id=1 LIMIT 1;
 END;`)
	if err != nil {
		return err
	}
	// Bootstrap schema-9 knowledge only once, with the same capture rules as future writes.
	var n int
	if err = tx.QueryRow(`SELECT count(*) FROM hub_schema WHERE version=11`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		_, err = tx.Exec(`UPDATE objects AS o SET data=data WHERE ` + SyncObjectPredicate("o") + `; UPDATE tasks SET data=data; UPDATE task_events SET payload=payload; INSERT INTO hub_schema VALUES(11);`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
