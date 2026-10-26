package api

import (
	"encoding/json"
	"fmt"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestY45GroupControlAccessRecentMessagesAndIcons(t *testing.T) {
	h, people, link := testkit.Open(t)
	testkit.Seed(t, h, people)
	mcp := &mcpserver.Service{Hub: h, People: people, Link: link}
	s := &Server{Hub: h, People: people, MCP: mcp, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	handler := s.Handler()
	a, credential, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/v1/groups/" + testkit.Group
	body := fmt.Sprintf(`{"agent_id":%q,"read":false,"send":"never"}`, a.ID)
	for _, token := range []string{"fixture-status", credential} {
		if w := request("PATCH", path+"/access", body, token); w.Code != http.StatusForbidden {
			t.Fatalf("non-owner access mutation returned %d", w.Code)
		}
	}
	w := request("PATCH", path+"/access", body, "fixture-control")
	if w.Code != 200 {
		t.Fatalf("owner access returned %d", w.Code)
	}
	updated, e := h.Agent(a.ID)
	testkit.Must(t, e)
	if h.CanChat(&updated, testkit.Group) || !h.CanChat(&updated, testkit.Phone) {
		t.Fatal("read edit changed unrelated scope")
	}
	rule, e := h.AgentSendPolicy(updated, testkit.Group)
	testkit.Must(t, e)
	if rule != "never" {
		t.Fatal("send edit was not enforced")
	}
	testkit.Must(t, h.Repo.Put("people", "non-member", model.Person{ID: "non-member", JIDs: []string{"15550199999@s.whatsapp.net"}, Name: "+15550199999", PushName: "Riya"}))
	for i := 0; i < 25; i++ {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprint("group-", i), ChatJID: testkit.Group, Sender: "15550199999@s.whatsapp.net", Content: "Material samples", Timestamp: testkit.Epoch.Add(-time.Duration(i) * time.Minute)}))
	}
	w = request("GET", path, "", "fixture-control")
	if w.Code != 200 {
		t.Fatalf("group detail %d", w.Code)
	}
	var detail struct {
		Recent  []model.Message     `json:"recent"`
		Group   model.Group         `json:"group"`
		Senders []map[string]string `json:"senders"`
	}
	testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &detail))
	if len(detail.Recent) != 20 || detail.Group.Messages30 != 25 || len(detail.Group.AgentAccess) != 1 {
		t.Fatal("group detail enrichment missing")
	}
	// A saved person whose name is just their number falls back to their WhatsApp name.
	if len(detail.Senders) != 1 || detail.Senders[0]["name"] != "Riya" {
		t.Fatalf("sender names = %v", detail.Senders)
	}
	w = request("PATCH", path, `{"icon":{"kind":"symbol","value":"person.3"}}`, "fixture-control")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"icon"`) {
		t.Fatal("group icon patch missing")
	}
	w = request("GET", path, "", credential)
	if w.Code != 404 {
		t.Fatal("revoked group still visible")
	}
}
func TestY45PersonTaskFilterAndSummaryMCP(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	mcp := &mcpserver.Service{Hub: h, People: p, Link: link}
	s := &Server{Hub: h, People: p, MCP: mcp, Link: link, Port: 18800, ControlToken: "fixture-control"}
	handler := s.Handler()
	id := model.PersonID(testkit.Phone)
	svc := mcp.TaskService()
	_, e := svc.Create(nil, tasks.Create{Kind: "long", Title: "Material review", People: []tasks.Entity{{ID: id, Name: "Maya Rao"}}})
	testkit.Must(t, e)
	_, e = svc.Create(nil, tasks.Create{Kind: "long", Title: "Other work"})
	testkit.Must(t, e)
	r := httptest.NewRequest("GET", "/v1/tasks?person="+id, nil)
	r.Host = "127.0.0.1:18800"
	r.Header.Set("Authorization", "Bearer fixture-control")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Material review") || strings.Contains(w.Body.String(), "Other work") {
		t.Fatal("task filter failed")
	}
	for i := 0; i < 5; i++ {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprint("recent", i), ChatJID: testkit.Phone, Content: "Material review", Timestamp: testkit.Epoch.Add(-time.Duration(i) * time.Hour)}))
	}
	a, _, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	_, e = mcp.Call(r.Context(), a.ID, "remember", mcpserver.Args{"kind": "summary", "subject": "person", "subject_id": id, "text": "You discussed material samples. The review is next."})
	testkit.Must(t, e)
	_, e = mcp.Call(r.Context(), a.ID, "remember", mcpserver.Args{"kind": "fact", "subject": "person", "subject_id": id, "text": "Maya is female", "evidence_message_ids": []any{"recent0"}})
	testkit.Must(t, e)
	pr, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if pr.Person.Details.Pronouns != "" || len(pr.Person.DetailSuggestions) != 1 || pr.Person.DetailSuggestions[0].Value != "she/her" || len(pr.Person.DetailSuggestions[0].EvidenceMessageIDs) != 1 {
		t.Fatal("remember gender did not become an evidenced suggestion")
	}
}
