package hub_test

import (
	"limmr/internal/hub"
	"path/filepath"
	"strings"
	"testing"
)

func TestOct29UpgradesExistingNoteCaptureAndKeepsHomeCompact(t *testing.T) {
	r, err := hub.Open(filepath.Join(t.TempDir(), "hub.db"))
	require(t, err)
	defer r.Close()
	// Reconstruct a pre-16 trigger from its deployed SQL, then reopen migration.
	for _, op := range []string{"INSERT", "UPDATE"} {
		name := "sync_tasks_" + op
		var body string
		require(t, r.DB.QueryRow("SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&body))
		start := strings.Index(body, "INSERT INTO sync_items SELECT NEW.id,'owner_notes'")
		stop := strings.Index(body[start:], "UPDATE sync_items SET removed=1") + start
		end := strings.Index(body[stop:], ";") + stop + 1
		body = body[:start] + body[end:]
		_, err = r.DB.Exec("DROP TRIGGER " + name)
		require(t, err)
		_, err = r.DB.Exec(body)
		require(t, err)
	}
	_, err = r.DB.Exec("DELETE FROM hub_schema WHERE version=16")
	require(t, err)
	require(t, r.Migrate(nil))
	require(t, r.Migrate(nil))
	_, err = r.DB.Exec(`INSERT INTO tasks VALUES('note-task','{"id":"note-task","owner_notes":[{"id":"note","text":"PRIVATE_NOTE_CANARY"}]}')`)
	require(t, err)
	var n int
	require(t, r.DB.QueryRow("SELECT count(*) FROM sync_items WHERE task_id='note-task' AND list='owner_notes'").Scan(&n))
	if n != 1 {
		t.Fatal("existing store not upgraded")
	}
	var card string
	require(t, r.DB.QueryRow("SELECT data FROM task_cards WHERE id='note-task'").Scan(&card))
	if strings.Contains(card, "PRIVATE_NOTE_CANARY") {
		t.Fatal("Home leaked note body")
	}
}
