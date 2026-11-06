package hub

// Version 14 keeps Home reads independent of the size of task workspaces.
// Triggers cover local edits, automation and paired sync in the same transaction.
func (r *Repo) migrateTaskCards() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const projection = `json_set(json_remove(NEW.data,'$.owner_notes','$.items','$.updates_sent','$.summary','$.latest','$.history','$.draft_check_in','$.draft_nudge','$.checklist'),'$.updates_sent',CASE WHEN json_array_length(NEW.data,'$.updates_sent')>0 THEN json_array(json_object('at',json_extract(NEW.data,'$.updates_sent[#-1].at'))) ELSE json('[]') END)`
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS task_cards(id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,data TEXT NOT NULL CHECK(json_valid(data)));
 CREATE TRIGGER IF NOT EXISTS task_cards_insert AFTER INSERT ON tasks BEGIN UPDATE task_cards SET data=` + projection + ` WHERE id=NEW.id; INSERT INTO task_cards SELECT NEW.id,` + projection + ` WHERE NOT EXISTS(SELECT 1 FROM task_cards WHERE id=NEW.id); END;
 CREATE TRIGGER IF NOT EXISTS task_cards_update AFTER UPDATE OF data ON tasks BEGIN UPDATE task_cards SET data=` + projection + ` WHERE id=NEW.id; INSERT INTO task_cards SELECT NEW.id,` + projection + ` WHERE NOT EXISTS(SELECT 1 FROM task_cards WHERE id=NEW.id); END;
 CREATE TRIGGER IF NOT EXISTS task_cards_delete AFTER DELETE ON tasks BEGIN DELETE FROM task_cards WHERE id=OLD.id; END;`)
	if err != nil {
		return err
	}
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=14").Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		_, err = tx.Exec(`INSERT OR REPLACE INTO task_cards SELECT id,json_set(json_remove(data,'$.owner_notes','$.items','$.updates_sent','$.summary','$.latest','$.history','$.draft_check_in','$.draft_nudge','$.checklist'),'$.updates_sent',CASE WHEN json_array_length(data,'$.updates_sent')>0 THEN json_array(json_object('at',json_extract(data,'$.updates_sent[#-1].at'))) ELSE json('[]') END) FROM tasks; INSERT INTO hub_schema VALUES(14);`)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
