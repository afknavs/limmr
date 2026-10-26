package api

import (
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"testing"
)

func TestPersonTaskFilterRunsBeforeGlobalLimit(t *testing.T) {
	h, people, link := testkit.Open(t)
	testkit.Seed(t, h, people)
	for i := 0; i < 501; i++ {
		task := tasks.Task{ID: fmt.Sprintf("a-%03d", i), Title: "Prepare workshop", Status: "active", Kind: "long", CreatedAt: testkit.Epoch, UpdatedAt: testkit.Epoch}
		if i == 500 {
			task.ID = "z-match"
			task.Chats = []tasks.Entity{{ID: testkit.Phone, Name: "Maya Rao"}}
			task.People = []tasks.Entity{{ID: "maya", Name: "Maya Rao"}}
		}
		_, e := h.Repo.DB.Exec(`INSERT INTO tasks VALUES(?,?)`, task.ID, dbutil.JSON(task))
		testkit.Must(t, e)
	}
	s := &Server{Hub: h, People: people, MCP: &mcpserver.Service{Hub: h, People: people, Link: link}, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	r := httptest.NewRequest("GET", "/v1/tasks?person=maya&limit=1", nil)
	r.Host = "127.0.0.1:18800"
	r.Header.Set("Authorization", "Bearer fixture-control")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var result struct {
		Tasks []tasks.TaskSummary `json:"tasks"`
	}
	testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &result))
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "z-match" {
		t.Fatal("person task beyond first 500 was omitted")
	}
}

func TestPersonTaskFilterRetainsChatAuthorization(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	task := tasks.Task{ID: "private-task", Title: "Prepare workshop", Status: "active", Kind: "long", People: []tasks.Entity{{ID: "maya", Name: "Maya Rao"}}, Chats: []tasks.Entity{{ID: testkit.Phone, Name: "Maya Rao"}}, CreatedAt: testkit.Epoch, UpdatedAt: testkit.Epoch}
	_, e := h.Repo.DB.Exec(`INSERT INTO tasks VALUES(?,?)`, task.ID, dbutil.JSON(task))
	testkit.Must(t, e)
	svc := tasks.New(h, nil)
	a := model.Agent{ID: "fixture-ai", Permissions: model.Defaults(true)}
	visible, e := svc.ListForPerson(&a, "open", "maya", 1)
	testkit.Must(t, e)
	if len(visible) != 1 {
		t.Fatal("fixture task must initially be visible")
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, h.SetSettings(settings))
	hidden, e := svc.ListForPerson(&a, "open", "maya", 1)
	testkit.Must(t, e)
	if len(hidden) != 0 {
		t.Fatal("person filter bypassed hidden chat authorization")
	}
	owner, e := svc.ListForPerson(nil, "open", "maya", 1)
	testkit.Must(t, e)
	if len(owner) != 1 {
		t.Fatal("agent projection removed owner task")
	}
}
