package integration_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"limmr/internal/tasks"
)

func TestR3TestKindMCPAndSilentHome(t *testing.T) {
	r := newRig(t)
	out, err := r.call(t, "task", map[string]any{"action": "create", "title": "Scratch poster", "goal": "Try the poster workflow", "kind": "test", "owner_request": map[string]any{"text": "Please track this scratch task"}})
	must(t, err)
	task := out.(tasks.Task)
	if task.Kind != "test" || task.Status != "active" {
		t.Fatal("MCP did not accept test kind", task)
	}
	now := epoch.Add(24 * time.Hour)
	r.h.Now = func() time.Time { return now }
	before, err := r.mc.TaskService().Recently(nil, "")
	must(t, err)
	counts := func() []int {
		result := []int{}
		for _, query := range []string{"SELECT count(*) FROM activity", "SELECT count(*) FROM agent_updates", "SELECT count(*) FROM objects WHERE bucket IN ('suggestions','drafts','approvals')"} {
			var n int
			must(t, r.h.Repo.DB.QueryRow(query).Scan(&n))
			result = append(result, n)
		}
		return result
	}
	baseline := counts()
	stream, cancel := r.h.Subscribe()
	defer cancel()
	must(t, r.mc.TaskService().Tick(context.Background()))
	if len(stream) != 0 || len(r.fake.Sent) != 0 || !reflect.DeepEqual(baseline, counts()) {
		t.Fatal("automatic ending produced attention or notify/send")
	}
	after, err := r.mc.TaskService().Recently(nil, "")
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("automatic Recently line", after)
	}
	resp := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, resp, 200)
	var home struct {
		Tasks    []tasks.TaskSummary
		Recently []string
	}
	must(t, json.Unmarshal(resp.Body.Bytes(), &home))
	if len(home.Tasks) != 0 || !reflect.DeepEqual(home.Recently, before) {
		t.Fatal("automatic Home line", resp.Body.String())
	}
	got, err := r.mc.TaskService().Get(nil, task.ID)
	must(t, err)
	if got.Status != "dropped" || got.CloseReason != "obsolete" || got.NeedsYou {
		t.Fatal(got)
	}
	// The explicit close-reason validation also accepts obsolete through MCP update.
	out, err = r.call(t, "task", map[string]any{"action": "create", "title": "Another scratch task", "goal": "Try a second workflow", "kind": "short", "owner_request": map[string]any{"text": "Track another scratch task"}})
	must(t, err)
	second := out.(tasks.Task)
	out, err = r.call(t, "task", map[string]any{"action": "update", "id": second.ID, "kind": "test", "status": "dropped", "close_reason": "obsolete"})
	must(t, err)
	updated := out.(tasks.Task)
	if updated.Kind != "test" || updated.CloseReason != "obsolete" {
		t.Fatal(updated)
	}
}
