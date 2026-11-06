package hub

import (
	"database/sql"
	"strings"
)

// Schema 16: owner notes use the same item-level sync merge as task files.
func (r *Repo) migrateTaskPreviews() error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow("SELECT count(*) FROM hub_schema WHERE version=16").Scan(&n); err != nil || n > 0 {
		return err
	}
	for _, op := range []string{"INSERT", "UPDATE"} {
		name := "sync_tasks_" + op
		var body string
		if err = tx.QueryRow("SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?", name).Scan(&body); err != nil {
			return err
		}
		// Fresh stores already have this stanza; older stores need a copy of items'.
		if !strings.Contains(body, "'owner_notes'") {
			start := strings.Index(body, "INSERT INTO sync_items SELECT NEW.id,'items'")
			if start < 0 {
				return sql.ErrNoRows
			}
			stop := strings.Index(body[start:], "UPDATE sync_items SET removed=1")
			if start < 0 || stop < 0 {
				return err
			}
			stop += start
			finish := strings.Index(body[stop:], ";") + stop + 1
			stanza := strings.ReplaceAll(body[start:finish], "'items'", "'owner_notes'")
			stanza = strings.ReplaceAll(stanza, "$.items", "$.owner_notes")
			body = strings.TrimSuffix(body, " END") + " " + stanza + " END"
			if _, err = tx.Exec("DROP TRIGGER " + name); err != nil {
				return err
			}
			if _, err = tx.Exec(body); err != nil {
				return err
			}
		}
	}
	// Replace Home triggers to omit potentially long owner notes from list reads.
	for _, op := range []string{"insert", "update"} {
		name := "task_cards_" + op
		var body string
		if err = tx.QueryRow("SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&body); err != nil {
			return err
		}
		body = strings.ReplaceAll(body, "'$.items'", "'$.owner_notes','$.items'")
		if _, err = tx.Exec("DROP TRIGGER " + name); err != nil {
			return err
		}
		if _, err = tx.Exec(body); err != nil {
			return err
		}
	}
	_, err = tx.Exec("UPDATE tasks SET data=data; INSERT INTO hub_schema VALUES(16)")
	if err != nil {
		return err
	}
	return tx.Commit()
}
