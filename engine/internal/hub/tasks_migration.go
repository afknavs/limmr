package hub

// Tasks have their own version and tables; no derived efforts or watches are rewritten.
func (r *Repo) migrateTasks() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=9").Scan(&n); err != nil || n > 0 {
		return err
	}
	_, err = tx.Exec(`CREATE TABLE tasks(id TEXT PRIMARY KEY,data TEXT NOT NULL CHECK(json_valid(data)));
 CREATE INDEX tasks_effort ON tasks(json_extract(data,'$.effort_id'));
 CREATE TABLE task_events(id TEXT PRIMARY KEY,task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,at TEXT NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)));
 CREATE INDEX task_events_at ON task_events(task_id,julianday(at));
 INSERT INTO hub_schema VALUES(9);`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
