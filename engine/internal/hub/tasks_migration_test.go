package hub_test

import (
	"context"
	"limmr/internal/hub"
	"testing"
)

func TestTasksMigrationPreservesExistingEffortsAndWatches(t *testing.T) {
	r, store, path := openWatchStore(t)
	require(t, store.Create(context.Background(), storedWatch("existing-watch")))
	require(t, r.Put("effort_edits", "existing-effort", map[string]any{"title": "Study material", "by": "example"}))
	var watch, effort string
	require(t, r.DB.QueryRow("SELECT definition_json FROM trigger_watches WHERE id='existing-watch'").Scan(&watch))
	require(t, r.DB.QueryRow("SELECT data FROM objects WHERE bucket='effort_edits'").Scan(&effort))
	_, err := r.DB.Exec("DROP TRIGGER IF EXISTS sync_suggestion_update; DROP TABLE task_events; DROP TABLE tasks; DELETE FROM hub_schema WHERE version=9")
	require(t, err)
	migrated, err := hub.Open(path)
	require(t, err)
	defer migrated.Close()
	for i := 0; i < 2; i++ {
		require(t, migrated.Migrate(nil))
		var gotWatch, gotEffort string
		require(t, migrated.DB.QueryRow("SELECT definition_json FROM trigger_watches WHERE id='existing-watch'").Scan(&gotWatch))
		require(t, migrated.DB.QueryRow("SELECT data FROM objects WHERE bucket='effort_edits'").Scan(&gotEffort))
		if watch != gotWatch || effort != gotEffort {
			t.Fatal("tasks migration rewrote existing work")
		}
	}
	var version, count int
	require(t, migrated.DB.QueryRow("SELECT max(version) FROM hub_schema").Scan(&version))
	require(t, migrated.DB.QueryRow("SELECT count(*) FROM tasks").Scan(&count))
	if version != 11 || count != 0 {
		t.Fatal("tasks migration missing")
	}
}
