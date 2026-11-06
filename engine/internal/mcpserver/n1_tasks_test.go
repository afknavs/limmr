package mcpserver_test

import (
	"context"
	"encoding/json"
	"limmr/internal/api"
	"limmr/internal/health"
	"limmr/internal/mcpserver"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"testing"
)

func TestN1AgentTaskWithoutChatsOrCircleOnHomeAndAllTasks(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p}
	out, err := mc.Call(context.Background(), a.ID, "task", map[string]any{"action": "create", "title": "Exam preparation", "kind": "long"})
	testkit.Must(t, err)
	task := out.(tasks.Task)
	if len(task.Chats) != 0 || len(task.CircleIDs) != 0 {
		t.Fatal("fixture must have no chats or circles")
	}
	server := &api.Server{Hub: h, People: p, MCP: mc, Link: l, Health: &health.Service{Hub: h, Link: l}, Port: 18800, ControlToken: "invented-control", StatusToken: "invented-status"}
	handler := server.Handler()
	for _, path := range []string{"/v1/home", "/v1/tasks?status=all&limit=500"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
		req.Header.Set("Authorization", "Bearer invented-control")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s: %d", path, rr.Code)
		}
		var response struct {
			Tasks []tasks.TaskSummary `json:"tasks"`
		}
		testkit.Must(t, json.Unmarshal(rr.Body.Bytes(), &response))
		if len(response.Tasks) != 1 || response.Tasks[0].ID != task.ID {
			t.Fatalf("task missing in %s", path)
		}
	}
	var activity int
	testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM activity WHERE json_extract(data,'$.tool')='task'").Scan(&activity))
	if activity != 1 {
		t.Fatal("creation must publish the activity event that refreshes mounted Home")
	}
}
