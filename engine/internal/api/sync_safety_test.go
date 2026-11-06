package api

import (
	"context"
	"limmr/internal/mcpserver"
	syncer "limmr/internal/sync"
	"limmr/internal/testkit"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPairedLeaseControlOnlyHolderCanAct(t *testing.T) {
	h, people, link := testkit.Open(t)
	now := time.Now()
	k, e := syncer.New(h.Repo, h.Home, func() time.Time { return now })
	testkit.Must(t, e)
	s := &Server{Hub: h, People: people, MCP: &mcpserver.Service{Hub: h, People: people, Link: link}, Link: link, Knowledge: k, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	handler := s.Handler()
	post := func(body string) int {
		r := httptest.NewRequest("POST", "/v1/sync/lease", strings.NewReader(body))
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer fixture-control")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	// A non-holder may request a handoff; the request itself grants no authority.
	testkit.Must(t, k.Pair("fixture-peer", "fixture-peer"))
	for _, body := range []string{`{"take":true}`, `{"take":false}`} {
		want := 409
		if body == `{"take":true}` {
			want = 200
		}
		if code := post(body); code != want {
			t.Fatalf("non-holder %s status=%d, want %d", body, code, want)
		}
		if k.MayRunAutomations(context.Background()) {
			t.Fatal("non-holder gained automations")
		}
	}
	status, e := k.Status()
	testkit.Must(t, e)
	if status["automations_paused"] != false || status["lease"].(map[string]any)["this_mac"] != false {
		t.Fatalf("status misreports holder: %v", status["lease"])
	}
	// As holder: keeping is a no-op, giving hands off and stops at once.
	testkit.Must(t, k.Unpair())
	if k.MayRunAutomations(context.Background()) {
		t.Fatal("unpaired non-holder bypassed recovery")
	}
	if code := post(`{"take":true,"previous_holder_stopped":true}`); code != 409 {
		t.Fatalf("early recovery status=%d", code)
	}
	now = now.Add(time.Hour)
	if code := post(`{"take":true,"previous_holder_stopped":true}`); code != 200 {
		t.Fatalf("owner recovery status=%d", code)
	}
	testkit.Must(t, k.Pair("fixture-peer", k.EngineID))
	if code := post(`{"take":true}`); code != 200 || !k.MayRunAutomations(context.Background()) {
		t.Fatalf("holder keep status=%d", code)
	}
	if code := post(`{"take":false}`); code != 200 || k.MayRunAutomations(context.Background()) {
		t.Fatalf("holder give status=%d", code)
	}
	if code := post(`{"take":true}`); code != 200 || k.MayRunAutomations(context.Background()) {
		t.Fatalf("former holder took back without the peer, status=%d", code)
	}
}
