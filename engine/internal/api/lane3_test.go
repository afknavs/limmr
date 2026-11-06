package api

import (
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLane3SummaryUndoAndPictureOwnerRoutes(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	for i := 0; i < 5; i++ {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprint("summary-", i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Synthetic venue planning", Timestamp: testkit.Epoch}))
	}
	testkit.Must(t, h.Repo.Put("circles", "workshop", model.Circle{ID: "workshop", Name: "Workshop", Confirmed: true}))
	server := &Server{Hub: h, People: p, MCP: &mcpserver.Service{Hub: h, People: p, Link: link}, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	handler := server.Handler()
	request := func(method, path, body, token string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	path := "/v1/people/" + id + "/summary"
	if code := request("PUT", path, `{"text":"Discussed venue options.","based_on_until":"2026-10-16T12:00:00Z"}`, "fixture-control"); code != 200 {
		t.Fatalf("summary save: %d", code)
	}
	undo := `{"written_at":"2026-10-16T12:00:00Z"}`
	if code := request("POST", path+"/undo", undo, "fixture-status"); code != 403 {
		t.Fatalf("status credential undid summary: %d", code)
	}
	if code := request("POST", path+"/undo", undo, "fixture-control"); code != 200 {
		t.Fatalf("owner undo: %d", code)
	}
	if code := request("PATCH", "/v1/circles/workshop", `{"icon":{"kind":"monogram","value":"WS"}}`, "fixture-control"); code != 200 {
		t.Fatalf("picture save: %d", code)
	}
	var icon hub.EntityIcon
	testkit.Must(t, h.Repo.Get("entity_icons", "circles/workshop", &icon))
	if icon.Kind != "monogram" || icon.Value != "WS" {
		t.Fatal("picture choice not persisted in synced bucket")
	}
	if code := request("PATCH", "/v1/circles/workshop", `{"icon":{"kind":"monogram","value":"<script>"}}`, "fixture-control"); code != 400 {
		t.Fatalf("invalid monogram accepted: %d", code)
	}
}
