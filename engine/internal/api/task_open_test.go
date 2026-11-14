package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/mcpserver"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
)

func TestOwnerTaskGETRecordsOpenOnlyAfterAuthorization(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	automatic := now.Add(-time.Hour)
	v := tasks.Task{ID: "invented-old", Title: "Review poster", Goal: "Review poster", Kind: "long", Status: "active", Origin: "agent", AutoKeptAt: &automatic, CreatedAt: automatic, UpdatedAt: automatic, LastActiveAt: automatic, LifespanDays: 14}
	_, err := h.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", v.ID, dbutil.JSON(v))
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p, Link: link}
	server := &Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18888, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	handler := server.Handler()
	request := func(path, token string) int {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18888"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	data := func() string {
		var raw string
		testkit.Must(t, h.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", v.ID).Scan(&raw))
		return raw
	}
	before := data()
	testkit.Must(t, func() error { _, e := mc.TaskService().Get(nil, v.ID); return e }())
	if request("/v1/tasks/"+v.ID, "fixture-status") == 200 || request("/v1/tasks/missing", "fixture-control") != 404 || request("/v1/tasks", "fixture-control") != 200 {
		t.Fatal("invalid authorization/list fixture")
	}
	if data() != before {
		t.Fatal("internal, list or refused read recorded an owner open")
	}
	plan, err := tasks.PlanProtocolPass(h.Repo.DB, now, time.UTC)
	testkit.Must(t, err)
	if plan.Tasks[0].Outcome != "Becomes a suggestion" {
		t.Fatal(plan)
	}
	if request("/v1/tasks/"+v.ID, "fixture-control") != 200 {
		t.Fatal("owner get failed")
	}
	var got map[string]json.RawMessage
	testkit.Must(t, json.Unmarshal([]byte(data()), &got))
	var opened string
	testkit.Must(t, json.Unmarshal(got["owner_opened_at"], &opened))
	if opened != now.Format(time.RFC3339Nano) {
		t.Fatal(opened)
	}
	delete(got, "owner_opened_at")
	var original map[string]json.RawMessage
	testkit.Must(t, json.Unmarshal([]byte(before), &original))
	if dbutil.JSON(got) != dbutil.JSON(original) {
		t.Fatal("open changed other fields")
	}
	plan, err = tasks.PlanProtocolPass(h.Repo.DB, now, time.UTC)
	testkit.Must(t, err)
	if plan.Tasks[0].Outcome != "Stays a task" || !strings.Contains(plan.Tasks[0].Actions[0].Reason, "you opened it") {
		t.Fatal(plan)
	}
	once := data()
	now = now.Add(time.Hour)
	if request("/v1/tasks/"+v.ID, "fixture-control") != 200 || once != data() {
		t.Fatal("repeat open rewrote first touch")
	}
	var n int
	testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM task_events").Scan(&n))
	if n != 0 {
		t.Fatal("open created history")
	}
}
