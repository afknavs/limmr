package migrate

import (
	"database/sql"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/url"
	"time"
)

// ImportActivity commits a legacy history snapshot atomically. Stable row
// fingerprints make retries safe even if migration later rolls back or stops.
func ImportActivity(path string, repo *hub.Repo) error {
	u := url.URL{Scheme: "file", Path: path}
	source, err := sql.Open("sqlite3", u.String()+"?mode=ro")
	if err != nil {
		return err
	}
	defer source.Close()
	tx, err := repo.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = ReadActivity(source, func(at time.Time, agent, tool, chat, text, id string) error {
		key := dbutil.Digest(dbutil.JSON([]string{at.Format(time.RFC3339Nano), agent, tool, chat, text, id}))
		result, e := tx.Exec("INSERT OR IGNORE INTO objects VALUES('legacy_activity_imports',?,'true')", key)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return nil
		}
		activity := model.Activity{At: at, AgentID: "legacy-" + dbutil.Digest(agent)[:12], Tool: tool, Kind: "sent", ChatJIDs: []string{chat}, MessageIDs: []string{id}, Count: 1, Note: text}
		_, e = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", at, dbutil.JSON(activity))
		return e
	})
	if err != nil {
		return err
	}
	return tx.Commit()
}
