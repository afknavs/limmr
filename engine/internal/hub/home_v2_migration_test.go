package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"path/filepath"
	"testing"
)

func TestHomeV2MigrationDefaultsAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	r, e := hub.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Put("circles", "old", map[string]any{"id": "old", "kind": "family", "confirmed": true}); e != nil {
		t.Fatal(e)
	}
	if e = r.Put("circles", "new", model.Circle{ID: "new", Kind: "family", Pinned: true}); e != nil {
		t.Fatal(e)
	}
	if _, e = r.DB.Exec("DELETE FROM hub_schema WHERE version=4"); e != nil {
		t.Fatal(e)
	}
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		r, e = hub.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		var old, current model.Circle
		if e = r.Get("circles", "old", &old); e != nil {
			t.Fatal(e)
		}
		if e = r.Get("circles", "new", &current); e != nil {
			t.Fatal(e)
		}
		if old.Pinned || !current.Pinned {
			t.Fatal("migration lost defaults or overrides")
		}
		var pinType string
		if e = r.DB.QueryRow("SELECT json_type(data,'$.pinned') FROM objects WHERE bucket='circles' AND id='old'").Scan(&pinType); e != nil {
			t.Fatal(e)
		}
		if pinType != "false" {
			t.Fatal("missing stored fields")
		}
		if e = r.Close(); e != nil {
			t.Fatal(e)
		}
	}
}
