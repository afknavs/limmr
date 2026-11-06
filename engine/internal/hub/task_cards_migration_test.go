package hub_test

import (
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskCardsMigrationAndSyncWrites(t *testing.T) {
	r, err := hub.Open(filepath.Join(t.TempDir(), "hub.db"))
	require(t, err)
	defer r.Close()
	data := map[string]any{"id": "synthetic-task", "title": "Workshop", "status": "active", "items": []any{map[string]any{"text": "PRIVATE_BODY_CANARY"}}, "updates_sent": []any{map[string]any{"at": "2026-10-28T09:00:00Z", "text_preview": "PRIVATE_BODY_CANARY"}}}
	_, err = r.DB.Exec("INSERT INTO tasks VALUES(?,?)", "synthetic-task", dbutil.JSON(data))
	require(t, err)
	var card string
	require(t, r.DB.QueryRow("SELECT data FROM task_cards WHERE id='synthetic-task'").Scan(&card))
	_, err = r.DB.Exec("INSERT INTO tasks VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", "synthetic-task", dbutil.JSON(data))
	require(t, err)
	if strings.Contains(card, "PRIVATE_BODY_CANARY") || !strings.Contains(card, "2026-10-28T09:00:00Z") {
		t.Fatal("card contains bodies or lost last update")
	}
	// Drop only derived state, then reopen/backfill the existing task atomically.
	_, err = r.DB.Exec("DROP TRIGGER task_cards_insert; DROP TRIGGER task_cards_update; DROP TRIGGER task_cards_delete; DROP TABLE task_cards; DELETE FROM hub_schema WHERE version=14")
	require(t, err)
	require(t, r.Migrate(nil))
	require(t, r.Migrate(nil))
	require(t, r.DB.QueryRow("SELECT data FROM task_cards WHERE id='synthetic-task'").Scan(&card))
	_, err = r.DB.Exec("UPDATE tasks SET data=json_set(data,'$.title','Review notes') WHERE id='synthetic-task'")
	require(t, err)
	require(t, r.DB.QueryRow("SELECT data FROM task_cards WHERE id='synthetic-task'").Scan(&card))
	if !strings.Contains(card, "Review notes") {
		t.Fatal("direct/sync write did not refresh card")
	}
	_, err = r.DB.Exec("DELETE FROM tasks WHERE id='synthetic-task'")
	require(t, err)
	var n int
	require(t, r.DB.QueryRow("SELECT count(*) FROM task_cards").Scan(&n))
	if n != 0 {
		t.Fatal("deleted card retained")
	}
}
