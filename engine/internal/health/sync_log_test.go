package health_test

import (
	"context"
	"limmr/internal/api"
	"limmr/internal/health"
	"limmr/internal/mcpserver"
	"limmr/internal/testkit"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLargeSyncLogHealthAndStatusStayFastWithBusyConnection(t *testing.T) {
	h, p, l := testkit.Open(t)
	_, err := h.Repo.DB.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<100000) INSERT INTO sync_log(bucket,id,hlc,origin,op,data) SELECT 'people','synthetic',x,'fake','upsert',json_object('padding',hex(zeroblob(512))) FROM n`)
	testkit.Must(t, err)
	hs := &health.Service{Hub: h, Link: l, Home: h.Home}
	server := &api.Server{Hub: h, People: p, Link: l, Health: hs, MCP: &mcpserver.Service{Hub: h, People: p}, Port: 18800, ControlToken: "fake-control"}
	handler := server.Handler()
	// Occupy the only hub connection: neither HTTP endpoint may queue behind it.
	conn, err := h.Repo.DB.Conn(context.Background())
	testkit.Must(t, err)
	for _, path := range []string{"/v1/health", "/v1/status"} {
		start := time.Now()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
		r.Header.Set("Authorization", "Bearer fake-control")
		handler.ServeHTTP(w, r)
		elapsed := time.Since(start)
		if elapsed >= 500*time.Millisecond || w.Code != 200 {
			t.Fatalf("%s: %d in %s", path, w.Code, elapsed)
		}
		t.Logf("%s large log + occupied connection: %s", path, elapsed)
	}
	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		for _, c := range hs.Checks(context.Background()) {
			if c.ID == "sync_log" {
				found = true
				if c.State != "warn" || !strings.Contains(c.Detail, "Last checked") {
					t.Fatal("missing bloat warning/check time")
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background check never completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
