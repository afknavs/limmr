package integration_test

import (
	"context"
	"limmr/internal/tasks"
	"testing"
)

func TestOct29TaskGetBriefAndUpdatesIncludeOwnerNotes(t *testing.T) {
	r := newRig(t)
	ws, _, _ := setupWatches(t, r)
	v := newTask(t, r)
	status(t, r.request(t, "POST", "/v1/tasks/"+v.ID+"/notes", r.api.ControlToken, map[string]any{"text": "Venue changed to North Hall. Involve Maya."}, nil), 200)
	for _, action := range []string{"get", "brief"} {
		out, err := r.call(t, "task", map[string]any{"action": action, "id": v.ID})
		must(t, err)
		if len(out.(tasks.Task).OwnerNotes) != 1 {
			t.Fatal("owner notes missing in", action)
		}
	}
	updates, err := ws.GetUpdates(context.Background(), r.a.ID, "", 0, 100)
	must(t, err)
	found := false
	for _, item := range updates.Items {
		if item.Kind == "task_owner_note" {
			found = true
			if item.Text != "Owner notes" || len(item.OwnerNotes) != 1 {
				t.Fatal("incomplete owner notes update")
			}
		}
	}
	if !found {
		t.Fatal("no durable note update")
	}
	_, err = r.call(t, "task", map[string]any{"action": "update", "id": v.ID, "owner_notes": []any{}})
	if err == nil {
		t.Fatal("agent edited owner notes")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(settings))
	updates, err = ws.GetUpdates(context.Background(), r.a.ID, "", 0, 100)
	must(t, err)
	for _, item := range updates.Items {
		if item.TaskID == v.ID {
			t.Fatal("hidden note update leaked")
		}
	}
}
