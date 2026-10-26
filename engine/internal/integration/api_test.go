package integration_test

import (
	"bytes"
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/demo"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalGuard(t *testing.T) {
	for _, test := range []struct {
		name, token string
		headers     map[string]string
		want        int
	}{{"no_token", "", nil, 401}, {"wrong_token", "wrong", nil, 401}, {"origin", "control", map[string]string{"Origin": "https://example.test"}, 403}, {"empty_origin", "control", map[string]string{"Origin": ""}, 403}, {"bad_host", "control", map[string]string{"Host": "evil.test:18800"}, 403}, {"wrong_port", "control", map[string]string{"Host": "localhost:18801"}, 403}, {"localhost", "control", map[string]string{"Host": "localhost:18800"}, 200}, {"exact_host", "control", nil, 200}} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t)
			token := test.token
			if token == "control" {
				token = r.api.ControlToken
			}
			w := r.request(t, "GET", "/v1/status", token, nil, test.headers)
			status(t, w, test.want)
		})
	}
}
func TestCredentialClasses(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/v1/health", "/v1/agents", "/v1/graph/export", "/v1/diagnostics", "/v1/activity"} {
		t.Run(path, func(t *testing.T) { w := r.request(t, "GET", path, r.api.StatusToken, nil, nil); status(t, w, 403) })
	}
	status(t, r.request(t, "GET", "/v1/status", r.api.StatusToken, nil, nil), 200)
	status(t, r.request(t, "GET", "/v1/status", r.token, nil, nil), 403)
	status(t, r.request(t, "GET", "/v1/agents", r.token, nil, nil), 403)
	status(t, r.request(t, "POST", "/mcp", r.api.ControlToken, map[string]any{}, nil), 403)
}
func TestAPIApprovals(t *testing.T) {
	r := newRig(t)
	ap, e := r.h.RequestSend(r.a, "message", chatA, "Draft", "as_agent", "", "")
	must(t, e)
	w := r.request(t, "GET", "/v1/approvals?state=pending", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), ap.ID) {
		t.Fatal("missing approval")
	}
	status(t, r.request(t, "POST", "/v1/approvals/"+ap.ID+"/approve", r.token, map[string]any{}, nil), 403)
	status(t, r.request(t, "POST", "/v1/approvals/"+ap.ID+"/approve", r.api.ControlToken, map[string]any{"text": "Edited exact"}, nil), 200)
	if len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != "Edited exact" {
		t.Fatal("API did not send approved edit")
	}
	status(t, r.request(t, "POST", "/v1/approvals/"+ap.ID+"/approve", r.api.ControlToken, map[string]any{}, nil), 409)
}
func TestAPIInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		method, path string
		body         any
	}{{"POST", "/v1/unlink", map[string]any{"confirm": false}}, {"POST", "/v1/link/start", map[string]any{"method": "terminal"}}, {"POST", "/v1/agents", map[string]any{"name": "AI", "kind": "unknown"}}, {"POST", "/v1/agents", map[string]any{"name": "AI", "kind": "custom", "unknown": true}}, {"GET", "/v1/activity?limit=-1", nil}, {"GET", "/v1/graph?limit=-1", nil}, {"PUT", "/v1/settings", model.Settings{RetentionDays: 0}}} {
		t.Run(test.path, func(t *testing.T) {
			r := newRig(t)
			status(t, r.request(t, test.method, test.path, r.api.ControlToken, test.body, nil), 400)
		})
	}
}
func TestMCPEndToEnd(t *testing.T) {
	r := newRig(t)
	initialize := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "Fixture client", "version": "1"}}}
	headers := map[string]string{"Accept": "application/json, text/event-stream", "X-Agent": "spoofed-agent"}
	w := r.request(t, "POST", "/mcp", r.token, initialize, headers)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "limmr") {
		t.Fatal("initialize failed")
	}
	w = r.request(t, "POST", "/mcp", r.token, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}, headers)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"send"`) {
		t.Fatal("missing local tools")
	}
	w = r.request(t, "POST", "/mcp", r.token, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "whoami", "arguments": map[string]any{}}}, headers)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "spoofed-agent") || !strings.Contains(w.Body.String(), r.a.ID) {
		t.Fatal("credential identity not preserved")
	}
	xs, e := r.h.Activity(&r.a, hub.ActivityFilter{})
	must(t, e)
	for _, method := range []string{"initialize", "tools/list", "whoami"} {
		found := false
		for _, a := range xs {
			if a.Tool == method {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing audit %s", method)
		}
	}
}
func TestEveryToolAuditedAndReadsStoreIDs(t *testing.T) {
	r := newRig(t)
	out, e := r.call(t, "list_messages", map[string]any{"chat_jid": chatA})
	must(t, e)
	xs, e := r.h.Activity(&r.a, hub.ActivityFilter{})
	must(t, e)
	if len(xs) != 1 || xs[0].Count != len(out.([]model.Message)) || len(xs[0].MessageIDs) != 3 || xs[0].Note != "" || strings.Contains(encode(xs), "CANARY_MESSAGE") {
		t.Fatal("read audit copied text or lost IDs")
	}
	before := len(xs)
	for _, name := range []string{"whoami", "get_person", "unknown_tool", "send_message"} {
		_, _ = r.call(t, name, map[string]any{})
	}
	xs, e = r.h.Activity(&r.a, hub.ActivityFilter{})
	must(t, e)
	if len(xs) != before+4 {
		t.Fatal("missing call audit")
	}
}
func TestActivityCursorAndRetention(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 4; i++ {
		must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "test", Kind: "read", ChatJIDs: []string{chatA}, Count: i}))
	}
	xs, e := r.h.Activity(&r.a, hub.ActivityFilter{Limit: 2})
	must(t, e)
	ys, e := r.h.Activity(&r.a, hub.ActivityFilter{Limit: 2, Before: xs[1].ID})
	must(t, e)
	if len(xs) != 2 || len(ys) != 2 || xs[1].ID <= ys[0].ID {
		t.Fatal("cursor repeats or skips rows")
	}
	old := epoch.AddDate(-1, 0, 0)
	must(t, r.h.Record(model.Activity{At: old, AgentID: r.a.ID, Tool: "old", Kind: "read"}))
	must(t, r.h.Retain())
	xs, e = r.h.Activity(&r.a, hub.ActivityFilter{})
	must(t, e)
	if len(xs) != 4 {
		t.Fatal("retention wrong")
	}
}

type sseWriter struct {
	headers http.Header
	events  chan string
	ready   chan struct{}
	once    sync.Once
}

func (w *sseWriter) Header() http.Header         { return w.headers }
func (w *sseWriter) WriteHeader(code int)        {}
func (w *sseWriter) Write(b []byte) (int, error) { w.events <- string(b); return len(b), nil }
func (w *sseWriter) Flush()                      { w.once.Do(func() { close(w.ready) }) }
func TestSSELinkAndActivity(t *testing.T) {
	for _, kind := range []string{"link", "activity"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			path := "/v1/link/events"
			if kind == "activity" {
				path = "/v1/activity/stream"
			}
			req := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+r.api.ControlToken)
			writer := &sseWriter{headers: http.Header{}, events: make(chan string, 4), ready: make(chan struct{})}
			done := make(chan struct{})
			go func() { r.api.Handler().ServeHTTP(writer, req); close(done) }()
			select {
			case <-writer.ready:
			case <-ctx.Done():
				t.Fatal("SSE not started")
			}
			if kind == "activity" {
				must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "whoami", Kind: "read"}))
			}
			select {
			case data := <-writer.events:
				if !strings.Contains(data, "event: "+kind) {
					t.Fatal("SSE missing event")
				}
			case <-ctx.Done():
				t.Fatal("SSE event timeout")
			}
			cancel()
			<-done
		})
	}
}
func TestDiagnosticsCanaries(t *testing.T) {
	r := newRig(t)
	r.a.Name = "CANARY_NAME"
	must(t, r.h.UpdateAgent(r.a))
	settings := r.h.Settings()
	settings.Owner = "CANARY_OWNER"
	must(t, r.h.SetSettings(settings))
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Kind: "sent", Tool: "approval", Note: "CANARY_SECRET CANARY_FINAL_TEXT", ChatJIDs: []string{chatA}}))
	w := r.request(t, "GET", "/v1/diagnostics", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	for _, canary := range []string{"CANARY", chatA, "15550100001", "Maya", "Rao", r.token, r.api.ControlToken, r.api.StatusToken, r.home} {
		if strings.Contains(w.Body.String(), canary) {
			t.Fatal("diagnostics exposed a seeded canary")
		}
	}
	if !strings.Contains(w.Body.String(), "chats=4") {
		t.Fatal("diagnostics missing counts")
	}
}
func TestPrivateFilesAndPersistence(t *testing.T) {
	r := newRig(t)
	path := filepath.Join(r.home, "control-token")
	a, e := dbutil.Credential(path)
	must(t, e)
	b, e := dbutil.Credential(path)
	must(t, e)
	if a != b {
		t.Fatal("credential not persistent")
	}
	for _, name := range []string{"control-token", "messages.db", "hub.db"} {
		st, e := os.Stat(filepath.Join(r.home, name))
		must(t, e)
		if st.Mode().Perm() != 0600 {
			t.Fatal("private file permissions")
		}
	}
	repo, e := hub.Open(filepath.Join(r.home, "hub.db"))
	must(t, e)
	defer repo.Close()
	reopened := hub.New(repo, r.m, r.home)
	agent, e := reopened.Authenticate(r.token)
	must(t, e)
	if agent.ID != r.a.ID {
		t.Fatal("lost identity on restart")
	}
}
func TestLogBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.log")
	must(t, os.WriteFile(path, append(bytes.Repeat([]byte("a"), 1024), []byte("last-line")...), 0600))
	must(t, health.TrimLog(path, 512, 128))
	b, e := os.ReadFile(path)
	must(t, e)
	if len(b) != 128 || !bytes.HasSuffix(b, []byte("last-line")) {
		t.Fatal("log tail lost")
	}
}
func TestDemoContract(t *testing.T) {
	r := newRig(t)
	_, e := r.h.Repo.DB.Exec("DELETE FROM objects WHERE bucket != 'settings';DELETE FROM credentials;DELETE FROM activity")
	must(t, e)
	must(t, demo.Seed(r.h, r.p, r.link))
	ps, e := r.p.People(nil, "")
	must(t, e)
	gs, e := r.p.Groups(nil, "")
	must(t, e)
	agents, e := r.h.Agents()
	must(t, e)
	if len(ps) != 13 || len(gs) != 13 || len(agents) != 5 || r.h.PendingCount() != 0 {
		t.Fatalf("demo counts %d/%d/%d/%d", len(ps), len(gs), len(agents), r.h.PendingCount())
	}
	r.api.Health.Demo = true
	checks := r.api.Health.Checks(context.Background())
	warn := 0
	for _, c := range checks {
		if c.State == "warn" {
			warn++
		}
	}
	if warn != 1 {
		t.Fatalf("expected one demo warning, got %d", warn)
	}
	must(t, demo.Seed(r.h, r.p, r.link))
	if r.h.PendingCount() != 0 {
		t.Fatal("demo reseeded duplicate approvals")
	}
}
func TestMessageStoreUpsertAndFTS(t *testing.T) {
	r := newRig(t)
	later := epoch.Add(time.Hour)
	earlier := epoch.Add(-time.Hour)
	must(t, r.m.PutChat(model.Chat{JID: chatA, Name: "Maya Rao", LastMessageTime: &later}))
	must(t, r.m.PutChat(model.Chat{JID: chatA, Name: "", LastMessageTime: &earlier}))
	cs, e := r.m.Chats()
	must(t, e)
	for _, c := range cs {
		if c.JID == chatA && (c.Name != "Maya Rao" || !c.LastMessageTime.Equal(later)) {
			t.Fatal("name or clock regressed")
		}
	}
	msg := firstMessage(t, r, chatA)
	msg.Content = "uniqueorchid"
	must(t, r.m.PutMessage(msg))
	out, e := r.call(t, "search_messages", map[string]any{"query": "uniqueorchid"})
	must(t, e)
	if len(out.([]model.Message)) != 1 {
		t.Fatal("FTS update not indexed")
	}
	msg.Content = "uniqueviolet"
	must(t, r.m.PutMessage(msg))
	out, e = r.call(t, "search_messages", map[string]any{"query": "uniqueorchid"})
	must(t, e)
	if len(out.([]model.Message)) != 0 {
		t.Fatal("stale FTS content")
	}
	must(t, r.m.Rebuild())
	var schema string
	must(t, r.m.DB.QueryRow("SELECT sql FROM sqlite_master WHERE name='messages_fts'").Scan(&schema))
	if !strings.Contains(schema, "fts5") {
		t.Fatal("not FTS5")
	}
}
func TestContextRequiresChatAndOrdersNeighbours(t *testing.T) {
	r := newRig(t)
	target := firstMessage(t, r, chatA)
	ms, e := r.m.Context(chatA, target.ID, 2, []string{chatA})
	must(t, e)
	if len(ms) != 3 {
		t.Fatal("missing nearby context")
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].Timestamp.Before(ms[i-1].Timestamp) {
			t.Fatal("context not chronological")
		}
	}
	if _, e = r.m.Context(chatB, target.ID, 2, []string{chatB}); e != model.ErrNotFound {
		t.Fatal("message ID crossed chat")
	}
}
