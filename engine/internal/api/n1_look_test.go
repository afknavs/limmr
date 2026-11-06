package api

import (
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	knowledgeSync "limmr/internal/sync"
	"limmr/internal/testkit"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestN1OwnerConfirmedDotLinkAndHue(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Pip", "custom", "local")
	testkit.Must(t, err)
	testkit.Must(t, h.Repo.Put("settings", "sync_peer", knowledgeSync.Peer{EngineID: "invented-dot", AgentID: a.ID}))
	server := &Server{Hub: h, People: p, MCP: &mcpserver.Service{Hub: h, People: p, Link: link}, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	handler := server.Handler()
	request := func(path, body, token string) int {
		r := httptest.NewRequest("PATCH", path, strings.NewReader(body))
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	path := "/v1/agents/" + a.ID
	if request(path, `{"runs_on_peer":"invented-dot"}`, "fixture-status") != 403 {
		t.Fatal("non-owner linked an AI")
	}
	if request(path, `{"runs_on_peer":"unknown-dot"}`, "fixture-control") != 400 {
		t.Fatal("unpaired peer accepted")
	}
	if code := request(path, `{"runs_on_peer":"invented-dot"}`, "fixture-control"); code != 200 {
		t.Fatalf("owner link: %d", code)
	}
	read, err := h.Agent(a.ID)
	testkit.Must(t, err)
	if read.RunsOnPeer != "invented-dot" {
		t.Fatal("link not saved with agent")
	}
	if request(path, `{"name":"Pip studio"}`, "fixture-control") != 200 {
		t.Fatal("rename failed")
	}
	read, err = h.Agent(a.ID)
	testkit.Must(t, err)
	if read.RunsOnPeer != "invented-dot" {
		t.Fatal("rename removed link")
	}
	if request(path, `{"runs_on_peer":""}`, "fixture-control") != 200 {
		t.Fatal("unlink failed")
	}
	read, err = h.Agent(a.ID)
	testkit.Must(t, err)
	if read.RunsOnPeer != "" {
		t.Fatal("unlink not saved")
	}
	for _, hue := range []string{"blue", "orange", "purple", "green", "yellow", "teal", "pink", "indigo"} {
		icon := hub.EntityIcon{Kind: "symbol", Value: "graduationcap", Hue: hue}
		if !icon.Valid() {
			t.Fatal("palette hue rejected")
		}
	}
	if (hub.EntityIcon{Kind: "symbol", Value: "heart", Hue: "unsafe"}).Valid() {
		t.Fatal("unknown hue accepted")
	}
}
