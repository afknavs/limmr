package startup

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func request(t *testing.T, s *State, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestHealthDoesNotWaitForBackfill(t *testing.T) {
	var logs bytes.Buffer
	s := New(18800, "synthetic-control", "synthetic-status", log.New(&logs, "", 0))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Step("migration.backfill", func() error { close(entered); <-release; return nil })
		s.Ready(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	}()
	<-entered
	w := request(t, s, "/v1/health", "synthetic-control")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"starting"`) || !strings.Contains(w.Body.String(), `"stage":"migration.backfill"`) {
		t.Fatal("health did not report backfill")
	}
	w = request(t, s, "/v1/home", "synthetic-control")
	if w.Code != 503 || w.Header().Get("Retry-After") != "1" || !strings.Contains(w.Body.String(), `"code":"starting"`) {
		t.Fatal("warm endpoint did not defer")
	}
	close(release)
	<-done
	if request(t, s, "/v1/home", "synthetic-control").Code != 204 {
		t.Fatal("ready API not published")
	}
	if !strings.Contains(logs.String(), "duration_ms=") || strings.Contains(logs.String(), "synthetic-control") {
		t.Fatal("unsafe or missing timing")
	}
}
func TestStartingSecurityAndProgress(t *testing.T) {
	s := New(18800, "synthetic-control", "synthetic-status", nil)
	before := request(t, s, "/v1/health", "synthetic-control").Body.String()
	s.Observe("warmup.people", 0)
	after := request(t, s, "/v1/health", "synthetic-control").Body.String()
	if before == after {
		t.Fatal("stage did not advance")
	}
	s.Observe("warmup.people", time.Second)
	if request(t, s, "/v1/health", "synthetic-control").Body.String() != after {
		t.Fatal("poll/completion fabricated progress")
	}
	if request(t, s, "/v1/health", "wrong").Code != 401 {
		t.Fatal("invalid health credential")
	}
	if request(t, s, "/v1/status", "synthetic-status").Code != 503 {
		t.Fatal("status credential")
	}
	r := httptest.NewRequest("GET", "http://evil.example/v1/health", nil)
	r.Header.Set("Authorization", "Bearer synthetic-control")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("host check")
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/health", nil)
	r.Header.Set("Origin", "")
	r.Header.Set("Authorization", "Bearer synthetic-control")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("origin check")
	}
}

// A socket proof is optional only when the lane sandbox denies loopback bind.
func TestLoopbackReadiness(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	s := New(port, "synthetic-control", "synthetic-status", nil)
	server := &http.Server{Handler: s, ReadHeaderTimeout: time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = server.Serve(ln) }()
	defer func() { _ = server.Close(); <-done }()
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/v1/health", port), nil)
	req.Header.Set("Authorization", "Bearer synthetic-control")
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("starting health not reachable")
	}
}
