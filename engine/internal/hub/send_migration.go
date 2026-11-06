package hub

import "strings"

// Schema 18 belongs to n1-files; 17 is reserved for the tasks lane.
func (r *Repo) migrateSendCoordination() error {
	tx, e := r.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM hub_schema WHERE version=18`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return nil
	}
	// Extend the existing capture triggers without changing any task trigger.
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		name := "sync_objects_" + op
		var source string
		if e = tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, name).Scan(&source); e != nil {
			return e
		}
		if !strings.Contains(source, "'send_queue'") {
			source = strings.Replace(source, "'task_dismissals'", "'task_dismissals','send_queue','send_files','send_acks','send_control'", 1)
		}
		if _, e = tx.Exec("DROP TRIGGER " + name); e != nil {
			return e
		}
		if _, e = tx.Exec(source); e != nil {
			return e
		}
	}
	_, e = tx.Exec(`INSERT INTO hub_schema VALUES(18)`)
	if e != nil {
		return e
	}
	return tx.Commit()
}
