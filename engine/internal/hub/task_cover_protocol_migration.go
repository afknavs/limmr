package hub

// Schema 17 invalidates earlier chat-picked covers before serving any routes.
// Owner picks/removals survive. The task service reconsiders local originals once.
func (r *Repo) migrateTaskCoverProtocol() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=17").Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err = tx.Exec(`UPDATE tasks SET data=json_set(json_remove(data,'$.cover'),'$.cover_review_pending',json('true')) WHERE json_extract(data,'$.cover.added_by')='chat' AND coalesce(json_extract(data,'$.cover_item_id'),'')='' AND coalesce(json_extract(data,'$.cover_removed'),0)=0; INSERT INTO hub_schema VALUES(17)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
