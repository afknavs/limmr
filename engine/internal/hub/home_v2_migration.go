package hub

// Circle fields live in the transactional objects store, like the rest of the
// circle model. Backfill them atomically without rewriting briefs or drafts.
func (r *Repo) migrateHomeV2() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=4").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return tx.Commit()
	}
	if _, err = tx.Exec(`UPDATE objects SET data=json_set(data,'$.pinned',json('false')) WHERE bucket='circles' AND json_type(data,'$.pinned') IS NULL;
 INSERT INTO hub_schema VALUES(4);`); err != nil {
		return err
	}
	return tx.Commit()
}
