package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"limmr/internal/model"
	"limmr/internal/tasks"
)

func TestR2HintsOnTaskReadsNeverHome(t *testing.T) {
	r := newRig(t)
	v, err := r.mc.TaskService().Create(nil, tasks.Create{Title: "Science fair poster", Goal: "Science fair poster", Kind: "long", Chats: []tasks.Entity{{ID: chatA}}})
	must(t, err)
	now := epoch.Add(time.Minute)
	r.h.Now = func() time.Time { return now }
	must(t, r.m.PutMessage(model.Message{ID: "contact-complete", ChatJID: chatA, Sender: chatA, Content: "Submitted the poster", Timestamp: now}))
	must(t, r.mc.TaskService().Tick(context.Background()))
	for _, action := range []string{"list", "get"} {
		args := map[string]any{"action": action}
		if action == "get" {
			args["id"] = v.ID
		}
		out, err := r.call(t, "task", args)
		must(t, err)
		raw := encode(out)
		if !strings.Contains(raw, "finish_hint") || !strings.Contains(raw, "Ask the owner before closing it.") {
			t.Fatal("MCP hint missing", raw)
		}
	}
	for _, path := range []string{"/v1/tasks", "/v1/tasks/" + v.ID} {
		resp := r.request(t, "GET", path, r.api.ControlToken, nil, nil)
		status(t, resp, 200)
		if !strings.Contains(resp.Body.String(), "finish_hint") {
			t.Fatal("owner hint missing", resp.Body.String())
		}
	}
	resp := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, resp, 200)
	if strings.Contains(resp.Body.String(), "finish_hint") || strings.Contains(resp.Body.String(), "Ask the owner before closing it.") {
		t.Fatal("hint on Home")
	}
	var home struct{ Tasks []tasks.TaskSummary }
	must(t, json.Unmarshal(resp.Body.Bytes(), &home))
	for _, summary := range home.Tasks {
		if summary.ID == v.ID && summary.NeedsYou {
			t.Fatal("hint raised Needs you")
		}
	}
}
