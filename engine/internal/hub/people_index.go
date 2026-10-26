package hub

// Identity lookup is proportional to the JIDs in the event, not directory size.
// Triggers also cover migration/import SQL and atomic object batches.
func (r *Repo) indexPeople() error {
	_, e := r.DB.Exec(`CREATE TABLE IF NOT EXISTS people_jids(jid TEXT NOT NULL,person_id TEXT NOT NULL,PRIMARY KEY(jid,person_id));
 CREATE INDEX IF NOT EXISTS people_jids_person ON people_jids(person_id);
 INSERT OR IGNORE INTO people_jids SELECT j.value,o.id FROM objects o,json_each(o.data,'$.jids') j WHERE o.bucket='people' AND j.type='text';
 CREATE TRIGGER IF NOT EXISTS people_jids_insert AFTER INSERT ON objects WHEN NEW.bucket='people' BEGIN INSERT OR IGNORE INTO people_jids SELECT value,NEW.id FROM json_each(NEW.data,'$.jids') WHERE type='text'; END;
 CREATE TRIGGER IF NOT EXISTS people_jids_update AFTER UPDATE ON objects WHEN NEW.bucket='people' BEGIN DELETE FROM people_jids WHERE person_id=OLD.id; INSERT OR IGNORE INTO people_jids SELECT value,NEW.id FROM json_each(NEW.data,'$.jids') WHERE type='text'; END;
 CREATE TRIGGER IF NOT EXISTS people_jids_delete AFTER DELETE ON objects WHEN OLD.bucket='people' BEGIN DELETE FROM people_jids WHERE person_id=OLD.id; END;`)
	return e
}
