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
	// Four suggestions; Home must include only three, following open tasks.
	for i := 0; i < 4; i++ {
		_, err = svc.Suggest(nil, tasks.Create{Title: "Exam material", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}}, tasks.Suggestion{Signals: []string{"Exam material needs a plan"}, Confidence: 0.75, DetectedAt: testkit.Epoch, EffortIDs: []string{}})
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
		Tasks []tasks.TaskSummary `json:"tasks"`
	}
	testkit.Must(t, json.Unmarshal(rr.Body.Bytes(), &home))
	if len(home.Tasks) != 4 || home.Tasks[0].ID != open.ID {
		t.Fatal("Home order/limit incorrect")
	}
	for _, action := range []string{"keep", "dismiss"} {
		args := map[string]any{"action": action, "id": xs[0].ID}
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
	// An undersized project is a task, with the why signal preserved.
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "current", ChatJID: testkit.Group, Content: "Prepare the application", Timestamp: testkit.Epoch}))
	out, err = mc.Call(context.Background(), a.ID, "circles", map[string]any{"action": "propose", "name": "Application review", "kind": "project", "chats": []any{testkit.Group}, "people": []any{model.PersonID(testkit.Phone)}, "why": "Design Club is preparing an application"})
	testkit.Must(t, err)
	id := out.(map[string]string)["converted_to_task"]
	if !strings.HasPrefix(id, "tsk_") {
		t.Fatal("project not converted")
	}
	task, err := svc.Get(&a, id)
	testkit.Must(t, err)
	if task.Status != "suggested" || task.Suggestion.Signals[0] != "Design Club is preparing an application" {
		t.Fatal("proposal signal missing")
	}
}
