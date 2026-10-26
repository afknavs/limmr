package hub

// Reuse the JSON object store; retain only refs and explicit metadata edits.
func (r *Repo) migrateEfforts() error {
	_, e := r.DB.Exec(`CREATE TRIGGER IF NOT EXISTS e1_edit_insert AFTER INSERT ON objects WHEN NEW.bucket='effort_edits' BEGIN UPDATE p1_revision SET value=value+1; END;
 CREATE TRIGGER IF NOT EXISTS e1_edit_update AFTER UPDATE ON objects WHEN NEW.bucket='effort_edits' BEGIN UPDATE p1_revision SET value=value+1; END;
 CREATE TRIGGER IF NOT EXISTS e1_edit_delete AFTER DELETE ON objects WHEN OLD.bucket='effort_edits' BEGIN UPDATE p1_revision SET value=value+1; END;`)
	return e
}
