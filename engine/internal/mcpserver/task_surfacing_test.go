package mcpserver_test

import (
	"context"
	"encoding/json"
	"limmr/internal/api"
	"limmr/internal/health"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"limmr/internal/watches"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskSurfacingMCPHomeAndUpdates(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Example own AI", "custom", "local")
	testkit.Must(t, err)
	a.Preset = "own"
	testkit.Must(t, h.UpdateAgent(a))
	other, _, err := h.CreateAgent("Other own AI", "custom", "local")
	testkit.Must(t, err)
	other.Preset = "own"
	testkit.Must(t, h.UpdateAgent(other))
	ws, err := watches.New(h, p, watches.Options{})
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p, Watches: ws}
	svc := mc.TaskService()
	// Suggestions stay available to MCP, while Home contains only tasks.
	for i := 0; i < 4; i++ {
		_, err = svc.Suggest(nil, tasks.Create{Title: "Exam material", DifferentBecause: "Separate example for pagination", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}}, tasks.Suggestion{Signals: []string{"Exam material needs a plan"}, Confidence: 0.75, DetectedAt: testkit.Epoch, EffortIDs: []string{}})
		testkit.Must(t, err)
	}
	open, err := svc.Create(&a, tasks.Create{Title: "Current study plan", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	out, err := mc.Call(context.Background(), a.ID, "task", map[string]any{"action": "list", "status": "suggested"})
	testkit.Must(t, err)
	xs := out.(map[string]any)["tasks"].([]tasks.TaskSummary)
	if len(xs) != 4 {
		t.Fatal("MCP list suggested missing")
	}
	for _, agent := range []model.Agent{a, other} {
		updates, err := ws.GetUpdates(context.Background(), agent.ID, "", 0, 100)
		testkit.Must(t, err)
		n := 0
		for _, it := range updates.Items {
			if it.Kind == "task_suggested" {
				n++
				if it.TaskID == "" || it.Text == "" {
					t.Fatal("suggested update missing detail")
				}
			}
		}
		if n != 4 {
			t.Fatalf("own AI got %d suggestions", n)
		}
	}
	other.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	testkit.Must(t, h.UpdateAgent(other))
	updates, err := ws.GetUpdates(context.Background(), other.ID, "", 0, 100)
	testkit.Must(t, err)
	for _, it := range updates.Items {
		if it.Kind == "task_suggested" {
			t.Fatal("revoked chat leaked update")
		}
	}
	server := &api.Server{Hub: h, People: p, MCP: mc, Link: l, Health: &health.Service{Hub: h, Link: l}, Port: 18800, ControlToken: "invented-control", StatusToken: "invented-status"}
	handler := server.Handler()
	req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/home", nil)
	req.Header.Set("Authorization", "Bearer invented-control")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("Home status %d: %s", rr.Code, rr.Body.String())
	}
	var home struct {
		Tasks       []tasks.TaskSummary `json:"tasks"`
		RecentLines []tasks.RecentLine  `json:"recent_lines"`
	}
	testkit.Must(t, json.Unmarshal(rr.Body.Bytes(), &home))
	if len(home.Tasks) != 1 || home.Tasks[0].ID != open.ID {
		t.Fatal("Home order/limit incorrect")
	}
	for _, line := range home.RecentLines {
		for _, suggestion := range xs {
			if line.TargetID == suggestion.ID {
				t.Fatal("Home recent activity contains suggestion")
			}
		}
	}
	for _, action := range []string{"keep", "dismiss"} {
		args := map[string]any{"action": action, "id": xs[0].ID}
		if action == "keep" {
			args["owner_request"] = map[string]any{"text": "Please track this work"}
		}
		if action == "dismiss" {
			args["id"] = xs[1].ID
			args["reason"] = "An invented example"
		}
		out, err = mc.Call(context.Background(), a.ID, "task", args)
		testkit.Must(t, err)
		task := out.(tasks.Task)
		if action == "keep" && task.Status != "active" || action == "dismiss" && task.CloseReason != "not_a_task" {
			t.Fatal("MCP resolution missing")
		}
	}
	// Match live ingestion: current chat metadata accompanies the message.
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Group, Name: "Design Club", LastMessageTime: &testkit.Epoch}))
	// Finite work warns and creates nothing; the caller explicitly starts a task.
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "current", ChatJID: testkit.Group, Content: "Prepare the application", Timestamp: testkit.Epoch}))
	_, err = mc.Call(context.Background(), a.ID, "circles", map[string]any{"action": "propose", "name": "Application review", "kind": "project", "chats": []any{testkit.Group}, "people": []any{model.PersonID(testkit.Phone)}, "why": "Design Club is preparing an application"})
	if err == nil || !strings.Contains(err.Error(), "task_override_reason") {
		t.Fatal("missing finite-work warning", err)
	}
	out, err = mc.Call(context.Background(), a.ID, "task", map[string]any{"action": "create", "owner_request": map[string]any{"text": "Please track this work"}, "title": "Review the application", "goal": "Submit the reviewed application", "kind": "long", "chats": []any{map[string]any{"id": testkit.Group}}})
	testkit.Must(t, err)
	task := out.(tasks.Task)
	if task.Status != "active" || task.Origin != "agent" {
		t.Fatal("explicit task creation missing")
	}

}

func TestChecklistAliases(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p}
	created, err := mc.Call(context.Background(), a.ID, "task", map[string]any{"action": "create", "owner_request": map[string]any{"text": "Please track this work"}, "title": "Exam preparation", "goal": "Be ready for the exam", "kind": "long"})
	testkit.Must(t, err)
	id := created.(tasks.Task).ID
	listed, err := mc.TaskService().List(nil, "open", 100)
	testkit.Must(t, err)
	if len(listed) != 1 || listed[0].ID != id {
		t.Fatal("agent-created task without chats is missing from the open list")
	}
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"action": "create", "owner_request": map[string]any{"text": "Please track this work"}, "title": "Exam preparation", "goal": "Be ready for the exam", "kind": "long", "lifespan_days": float64(7)}, "lifespan_days"},
		{map[string]any{"action": "update", "id": id, "checklist": []any{}}, "checklist"},
	} {
		_, err = mc.Call(context.Background(), a.ID, "task", tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "example:") {
			t.Fatalf("missing actionable error for %s: %v", tc.want, err)
		}
	}
	for _, args := range []map[string]any{
		{"action": "checklist", "id": id, "op": "add", "items": []any{map[string]any{"text": "Read"}}},
		{"action": "checklist", "id": id, "op": "add", "text": "Practice"},
	} {
		_, err = mc.Call(context.Background(), a.ID, "task", args)
		testkit.Must(t, err)
	}
	task, err := mc.TaskService().Get(&a, id)
	testkit.Must(t, err)
	if len(task.Checklist) != 2 || task.Checklist[0].Text != "Read" || task.Checklist[1].Text != "Practice" {
		t.Fatalf("checklist aliases: %+v", task.Checklist)
	}
}
