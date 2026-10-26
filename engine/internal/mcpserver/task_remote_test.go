package mcpserver_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/remote"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
)

// Exercise registration, PKCE, bearer authentication and the actual streamable
// MCP handler, without a listener, external requests, live stores or real tokens.
func TestRemoteOAuthTaskList(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	mc := &mcpserver.Service{Hub: h, People: p}
	const base = "https://limmr.example"
	testkit.Must(t, h.Repo.Put("remote_settings", "main", remote.Config{Enabled: true, PublicURL: base}))
	rs := remote.New(h, mc, 18801)
	handler := rs.Handler()
	request := func(method, path, bearer string, body any) map[string]any {
		t.Helper()
		var data []byte
		contentType := "application/json"
		if form, ok := body.(url.Values); ok {
			data = []byte(form.Encode())
			contentType = "application/x-www-form-urlencoded"
		} else if body != nil {
			var err error
			data, err = json.Marshal(body)
			testkit.Must(t, err)
		}
		req := httptest.NewRequest(method, base+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "application/json, text/event-stream")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 && w.Code != 201 && w.Code != 303 {
			t.Fatalf("%s %s: HTTP %d", method, req.URL.Path, w.Code)
		}
		out := map[string]any{}
		if w.Body.Len() > 0 {
			testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &out))
		}
		return out
	}
	registration := request("POST", "/register", "", map[string]any{
		"client_name": "ChatGPT fixture", "redirect_uris": []string{"https://client.example/callback"}, "token_endpoint_auth_method": "none",
	})
	clientID := registration["client_id"].(string)
	verifier := strings.Repeat("p", 64)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://client.example/callback"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "resource": {base + "/mcp"}}
	// Authorize redirects to the local approval page; never follow external URLs.
	req := httptest.NewRequest("GET", base+"/authorize?"+query.Encode(), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 303 {
		t.Fatalf("authorize: HTTP %d", w.Code)
	}
	approval, err := url.Parse(w.Header().Get("Location"))
	testkit.Must(t, err)
	pending := rs.Status().Pending
	if len(pending) != 1 {
		t.Fatal("approval missing")
	}
	a, _, err := h.CreateAgent("ChatGPT fixture", "chatgpt", "remote")
	testkit.Must(t, err)
	testkit.Must(t, rs.Approve(pending[0].Code, a.ID, nil, nil, false, "own"))
	callback := request("GET", "/pair/status?tx="+approval.Query().Get("tx"), "", nil)
	redirect, err := url.Parse(callback["redirect"].(string))
	testkit.Must(t, err)
	tokens := request("POST", "/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID},
		"code": {redirect.Query().Get("code")}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {base + "/mcp"}})
	bearer := tokens["access_token"].(string)
	rpc := func(method string, params any) map[string]any {
		t.Helper()
		out := request("POST", "/mcp", bearer, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		if out["error"] != nil {
			t.Fatal("MCP returned a protocol error")
		}
		return out["result"].(map[string]any)
	}
	rpc("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "ChatGPT fixture", "version": "1"}})
	rpc("tools/list", map[string]any{})
	task, err := mc.TaskService().Create(nil, tasks.Create{Title: "Review the study guide", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	suggested, err := mc.TaskService().Suggest(nil, tasks.Create{Title: "Print exam material", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Group}}}, tasks.Suggestion{Signals: []string{"Exam material is due"}, Confidence: 0.8, DetectedAt: testkit.Epoch})
	testkit.Must(t, err)
	for _, tc := range []struct {
		name      string
		args      map[string]any
		wantID    string
		wantError bool
	}{
		{"get", map[string]any{"action": "get", "id": task.ID}, task.ID, false},
		{"default list", map[string]any{"action": "list"}, task.ID, false},
		{"numeric limit", map[string]any{"action": "list", "limit": 1}, task.ID, false},
		{"string limit", map[string]any{"action": "list", "limit": "1"}, task.ID, false},
		{"suggested", map[string]any{"action": "list", "status": "suggested"}, suggested.ID, false},
		{"bad limit", map[string]any{"action": "list", "limit": "invalid"}, "", true},
		{"fractional limit", map[string]any{"action": "list", "limit": 1.5}, "", true},
		{"oversized limit", map[string]any{"action": "list", "limit": "501"}, "", true},
		{"negative limit", map[string]any{"action": "list", "limit": "-1"}, "", true},
		{"null limit", map[string]any{"action": "list", "limit": nil}, "", true},
		{"bad status", map[string]any{"action": "list", "status": "invalid"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM activity WHERE json_extract(data,'$.tool')='task'").Scan(&before))
			out := rpc("tools/call", map[string]any{"name": "task", "arguments": tc.args})
			isError, _ := out["isError"].(bool)
			if isError != tc.wantError {
				t.Fatalf("tool error=%v, want %v", isError, tc.wantError)
			}
			if !isError {
				encoded, err := json.Marshal(out["structuredContent"])
				testkit.Must(t, err)
				if !strings.Contains(string(encoded), tc.wantID) {
					t.Fatal("structured content missing task")
				}
				text, err := json.Marshal(out["content"])
				testkit.Must(t, err)
				if !strings.Contains(string(text), tc.wantID) {
					t.Fatal("text content missing task")
				}
			}
			var after int
			testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM activity WHERE json_extract(data,'$.tool')='task'").Scan(&after))
			if after != before+1 {
				t.Fatal("task call missing from Activity")
			}
			var activity model.Activity
			var raw string
			testkit.Must(t, h.Repo.DB.QueryRow("SELECT data FROM activity ORDER BY id DESC LIMIT 1").Scan(&raw))
			testkit.Must(t, json.Unmarshal([]byte(raw), &activity))
			if !isError && activity.Count != 1 {
				t.Fatal("task read missing audit count")
			}
			if isError && activity.Kind != "denied" {
				t.Fatal("rejected call missing denied audit")
			}
			if activity.AgentID != a.ID {
				t.Fatal("Activity lost OAuth identity")
			}
		})
	}
}
