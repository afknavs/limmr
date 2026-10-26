package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"limmr/internal/model"
	"limmr/internal/remote"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// A listener with no socket. HTTP requests below go through the actual handler;
// the lifecycle goroutine waits on this listener until shutdown.
type inertListener struct {
	done chan struct{}
	once sync.Once
}

func (l *inertListener) Accept() (net.Conn, error) { <-l.done; return nil, net.ErrClosed }
func (l *inertListener) Close() error              { l.once.Do(func() { close(l.done) }); return nil }
func (l *inertListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18801} }
func remoteRig(t *testing.T) (*rig, *remote.Service) {
	r := newRig(t)
	s := remote.New(r.h, r.mc, 18801)
	s.Listen = func(network, address string) (net.Listener, error) {
		if network != "tcp" || address != "127.0.0.1:18801" {
			t.Error("remote not bound to loopback")
		}
		return &inertListener{done: make(chan struct{})}, nil
	}
	must(t, s.Configure(remote.Config{Enabled: true, PublicURL: "https://limmr.example"}))
	t.Cleanup(func() { s.Close() })
	r.api.Remote = s
	return r, s
}
func remoteRequest(t *testing.T, s *remote.Service, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	isForm := false
	switch v := body.(type) {
	case url.Values:
		data = []byte(v.Encode())
		isForm = true
	case nil:
	default:
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, s.Configuration().PublicURL+path, bytes.NewReader(data))
	if isForm {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}
func readMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

type oauthFlow struct{ client, verifier, challenge, tx, code, grant, token, refresh, agent string }

func register(t *testing.T, s *remote.Service, name string) oauthFlow {
	t.Helper()
	w := remoteRequest(t, s, "POST", "/register", "", map[string]any{"client_name": name, "redirect_uris": []string{"https://client.example/callback"}, "token_endpoint_auth_method": "none"})
	status(t, w, 201)
	m := readMap(t, w)
	verifier := strings.Repeat("p", 64)
	sum := sha256.Sum256([]byte(verifier))
	return oauthFlow{client: m["client_id"].(string), verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}
func authorize(t *testing.T, s *remote.Service, f *oauthFlow) {
	t.Helper()
	q := url.Values{"response_type": {"code"}, "client_id": {f.client}, "redirect_uri": {"https://client.example/callback"}, "code_challenge_method": {"S256"}, "code_challenge": {f.challenge}, "resource": {s.Configuration().PublicURL + "/mcp"}, "state": {"fixture-state"}}
	w := remoteRequest(t, s, "GET", "/authorize?"+q.Encode(), "", nil)
	status(t, w, 303)
	u, e := url.Parse(w.Header().Get("Location"))
	must(t, e)
	f.tx = u.Query().Get("tx")
	st := s.Status()
	for _, p := range st.Pending {
		if p.Name != "" && p.State == "waiting" {
			f.code = p.Code
		}
	}
	if len(f.code) != 6 {
		t.Fatal("missing six digit code")
	}
}
func approve(t *testing.T, r *rig, s *remote.Service, f *oauthFlow) {
	t.Helper()
	w := r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, map[string]any{"code": f.code, "new_agent": map[string]string{"name": "ChatGPT", "kind": "chatgpt"}}, nil)
	status(t, w, 200)
	w = remoteRequest(t, s, "GET", "/pair/status?tx="+f.tx, "", nil)
	status(t, w, 200)
	m := readMap(t, w)
	u, e := url.Parse(m["redirect"].(string))
	must(t, e)
	if u.Query().Get("state") != "fixture-state" {
		t.Fatal("OAuth state lost")
	}
	f.grant = u.Query().Get("code")
	for _, c := range s.Status().Clients {
		if c.ID == f.client {
			f.agent = c.AgentID
		}
	}
	if f.agent == "" {
		t.Fatal("approval not bound to agent")
	}
}
func exchange(t *testing.T, s *remote.Service, f *oauthFlow) *httptest.ResponseRecorder {
	t.Helper()
	w := remoteRequest(t, s, "POST", "/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client}, "code": {f.grant}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {f.verifier}, "resource": {s.Configuration().PublicURL + "/mcp"}})
	if w.Code == 200 {
		m := readMap(t, w)
		f.token = m["access_token"].(string)
		f.refresh = m["refresh_token"].(string)
	}
	return w
}
func TestOAuthEndToEndAndRevocation(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT fixture")
	authorize(t, s, &f)
	page := remoteRequest(t, s, "GET", "/pair?tx="+f.tx, "", nil)
	status(t, page, 200)
	if !strings.Contains(page.Body.String(), f.code) || strings.Contains(page.Body.String(), "<form") {
		t.Fatal("pair page must display code without approval form")
	}
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	headers := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "Fake client", "version": "1"}}}
	status(t, remoteRequest(t, s, "POST", "/mcp", f.token, headers), 200)
	w := remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}})
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"send"`) || !strings.Contains(w.Body.String(), "download_media") || !strings.Contains(w.Body.String(), `"name":"read"`) {
		t.Fatal("remote default tool filtering")
	}
	w = remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "whoami", "arguments": map[string]any{}}})
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), f.agent) {
		t.Fatal("remote agent not bound")
	}
	must(t, s.Revoke(f.client))
	status(t, remoteRequest(t, s, "POST", "/mcp", f.token, headers), 401)
	if _, e := s.Verify(context.Background(), f.token, nil); e == nil {
		t.Fatal("revoked token accepted")
	}
	status(t, exchange(t, s, &f), 401)
}
func TestOAuthPKCEReplayResourceAndRefresh(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	saved := f.verifier
	f.verifier = strings.Repeat("wrong", 16)
	status(t, exchange(t, s, &f), 400)
	f.verifier = saved
	status(t, exchange(t, s, &f), 200)
	status(t, exchange(t, s, &f), 400)
	w := remoteRequest(t, s, "POST", "/token", "", url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client}, "refresh_token": {f.refresh}, "resource": {"https://evil.test/mcp"}})
	status(t, w, 400)
	oldRefresh := f.refresh
	w = remoteRequest(t, s, "POST", "/token", "", url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client}, "refresh_token": {f.refresh}})
	status(t, w, 200)
	m := readMap(t, w)
	if m["refresh_token"] == oldRefresh {
		t.Fatal("refresh did not rotate")
	}
	status(t, remoteRequest(t, s, "POST", "/token", "", url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client}, "refresh_token": {oldRefresh}}), 400)
	must(t, s.Configure(remote.Config{Enabled: true, PublicURL: "https://changed.example"}))
	if _, e := s.Verify(context.Background(), f.token, nil); e == nil {
		t.Fatal("old audience accepted after issuer change")
	}
}
func TestOAuthDenyAndExpiry(t *testing.T) {
	for _, kind := range []string{"deny", "expire"} {
		t.Run(kind, func(t *testing.T) {
			r, s := remoteRig(t)
			f := register(t, s, "Claude fixture")
			authorize(t, s, &f)
			if kind == "deny" {
				must(t, s.Deny(f.code))
				w := remoteRequest(t, s, "GET", "/pair/status?tx="+f.tx, "", nil)
				if !strings.Contains(w.Body.String(), "access_denied") {
					t.Fatal("denial callback lost")
				}
				if e := s.Approve(f.code, "", nil, nil, false); e != model.ErrNotFound {
					t.Fatal("denied code approved")
				}
			} else {
				now := time.Now()
				s.Now = func() time.Time { return now.Add(11 * time.Minute) }
				w := r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, map[string]any{"code": f.code, "agent_id": r.a.ID}, nil)
				status(t, w, 404)
				if len(s.Status().Pending) != 0 {
					t.Fatal("expired code pending")
				}
			}
		})
	}
}
func TestRemoteGuardsAndAdminIsolation(t *testing.T) {
	r, s := remoteRig(t)
	status(t, remoteRequest(t, s, "POST", "/mcp", "", map[string]any{}), 401)
	status(t, remoteRequest(t, s, "POST", "/mcp", r.token, map[string]any{}), 401)
	status(t, remoteRequest(t, s, "GET", "/v1/status", r.api.ControlToken, nil), 404)
	req := httptest.NewRequest("GET", "https://evil.test/.well-known/oauth-authorization-server", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	status(t, w, 403)
	status(t, r.request(t, "POST", "/v1/remote/deny", r.token, map[string]string{"code": "123456"}, nil), 403)
	for _, path := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		t.Run(path, func(t *testing.T) { status(t, remoteRequest(t, s, "GET", path, "", nil), 200) })
	}
}
func TestOAuthRegistrationAndURLValidation(t *testing.T) {
	for _, raw := range []string{"http://evil.test", "https://example.test/a/../b", "https://user:pass@example.test", "https://example.test?x=1", "javascript:alert(1)", "https://example.test#fragment"} {
		t.Run(raw, func(t *testing.T) {
			if _, e := remote.ValidURL(raw); !errors.Is(e, model.ErrInvalid) {
				t.Fatal("bad public URL accepted")
			}
		})
	}
	_, s := remoteRig(t)
	for _, uri := range []string{"javascript:alert(1)", "http://evil.test/callback", "file:///etc/passwd", "https://user:pass@example.test/callback", "https://example.test/callback#fragment"} {
		t.Run(uri, func(t *testing.T) {
			w := remoteRequest(t, s, "POST", "/register", "", map[string]any{"client_name": "Fixture", "redirect_uris": []string{uri}})
			status(t, w, 400)
		})
	}
}
func TestOAuthHashedStateAndRestart(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	rows, e := r.h.Repo.DB.Query("SELECT id,data FROM objects WHERE bucket IN ('oauth_tokens','oauth_clients')")
	must(t, e)
	for rows.Next() {
		var id, data string
		must(t, rows.Scan(&id, &data))
		if strings.Contains(id+data, f.token) || strings.Contains(id+data, f.refresh) {
			t.Fatal("raw credential persisted")
		}
	}
	must(t, rows.Err())
	rows.Close()
	fresh := remote.New(r.h, r.mc, 18801)
	defer fresh.Close()
	if _, e = fresh.Verify(context.Background(), f.token, nil); e != nil {
		t.Fatal("tokens did not persist across service recreation")
	}
	statusBody := encode(s.Status())
	if strings.Contains(statusBody, "secret_digest") {
		t.Fatal("private digest in status")
	}
}
func TestRemotePermissionsRecheckedAndDisabled(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	a, e := r.h.Agent(f.agent)
	must(t, e)
	a.Permissions.ReadMessages = false
	a.Permissions.Search = false
	a.Permissions.ReadPeople = false
	must(t, r.h.UpdateAgent(a))
	w := remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}})
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"name":"read"`) {
		t.Fatal("catalog changed with permissions")
	}
	w = remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "list_messages", "arguments": map[string]any{"chat_jid": chatA}}})
	if !strings.Contains(w.Body.String(), "is off for") {
		t.Fatal("revoked capability still callable")
	}
	a.Enabled = false
	must(t, r.h.UpdateAgent(a))
	status(t, remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}}), 401)
}

func TestAgentRevokeCannotBeUndoneByReenable(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT fixture")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	status(t, r.request(t, "POST", "/v1/agents/"+f.agent+"/revoke", r.api.ControlToken, nil, nil), 200)
	a, e := r.h.Agent(f.agent)
	must(t, e)
	a.Enabled = true
	must(t, r.h.UpdateAgent(a))
	status(t, remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}}), 401)
	status(t, exchange(t, s, &f), 401)
}
