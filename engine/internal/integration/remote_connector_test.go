package integration_test

import (
	"encoding/json"
	"limmr/internal/remote"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Hosted connectors use public discovery and OAuth, never the local control credential.
// Both transports execute the real engine handlers with synthetic stores and no sockets.
func TestHostedConnectorFlow(t *testing.T) {
	for _, client := range []struct {
		name, kind, auth string
		fill             bool
	}{
		{"ChatGPT fixture", "chatgpt", "none", true},
		{"Claude fixture", "claude_ai", "client_secret_basic", false},
	} {
		t.Run(client.kind, func(t *testing.T) {
			r, s := remoteRig(t)
			base := "https://limmr.example:10000"
			must(t, s.Configure(remote.Config{Enabled: true, PublicURL: base}))
			discovery := remoteRequest(t, s, "GET", "/.well-known/oauth-authorization-server", "", nil)
			status(t, discovery, 200)
			metadata := readMap(t, discovery)
			if metadata["issuer"] != base || metadata["registration_endpoint"] != base+"/register" {
				t.Fatal("incorrect issuer or registration endpoint")
			}
			resource := readMap(t, remoteRequest(t, s, "GET", "/.well-known/oauth-protected-resource/mcp", "", nil))
			if resource["resource"] != base+"/mcp" {
				t.Fatal("incorrect resource")
			}
			challenge := remoteRequest(t, s, "GET", "/mcp", "", nil)
			status(t, challenge, 401)
			if !strings.Contains(challenge.Header().Get("WWW-Authenticate"), `resource_metadata="`+base+`/.well-known/oauth-protected-resource/mcp"`) {
				t.Fatal("missing protected resource challenge")
			}
			f := register(t, s, client.name)
			secret := ""
			if client.auth != "none" {
				registration := remoteRequest(t, s, "POST", "/register", "", map[string]any{
					"client_name": client.name, "redirect_uris": []string{"https://client.example/callback"},
					"token_endpoint_auth_method": client.auth, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
				})
				status(t, registration, 201)
				m := readMap(t, registration)
				f.client = m["client_id"].(string)
				secret = m["client_secret"].(string)
			}
			authorize(t, s, &f)
			page := remoteRequest(t, s, "GET", "/pair?tx="+f.tx, "", nil)
			status(t, page, 200)
			if !strings.Contains(page.Body.String(), f.code) {
				t.Fatal("pair page omitted the approval code")
			}
			until := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
			approval := map[string]any{"code": f.code, "new_agent": map[string]string{"name": client.name, "kind": client.kind}, "access": map[string]any{"circles": []string{}, "unsorted": true, "until": until}, "fill_in": client.fill}
			status(t, r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, approval, nil), 200)
			callback := readMap(t, remoteRequest(t, s, "GET", "/pair/status?tx="+f.tx, "", nil))
			u, e := url.Parse(callback["redirect"].(string))
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
			agent, e := r.h.Agent(f.agent)
			must(t, e)
			if agent.Permissions.Access.Circles == nil || !agent.Permissions.Access.Unsorted || agent.Permissions.Access.Until == nil {
				t.Fatal("approval lost the selected access")
			}
			if agent.Permissions.WritePeople != client.fill || agent.Permissions.SaveSummaries != client.fill || agent.Permissions.RequestSend || agent.Permissions.DownloadMedia {
				t.Fatal("approval capabilities differ from fill_in")
			}
			tokenRequest := func(form url.Values) *httptest.ResponseRecorder {
				req := httptest.NewRequest("POST", base+"/token", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if secret != "" {
					req.SetBasicAuth(f.client, secret)
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, req)
				return w
			}
			form := url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client}, "code": {f.grant}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {f.verifier}, "resource": {base + "/mcp"}}
			w := tokenRequest(form)
			status(t, w, 200)
			tokens := readMap(t, w)
			f.token = tokens["access_token"].(string)
			f.refresh = tokens["refresh_token"].(string)
			status(t, tokenRequest(form), 400)
			call := func(method string, params any) *httptest.ResponseRecorder {
				return remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
			}
			w = call("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": client.name, "version": "1"}})
			status(t, w, 200)
			if !strings.Contains(w.Body.String(), "protocolVersion") || strings.Contains(w.Body.String(), `"error"`) {
				t.Fatal("MCP initialization failed")
			}
			status(t, remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}), 202)
			w = call("tools/list", map[string]any{})
			status(t, w, 200)
			if !strings.Contains(w.Body.String(), `"name":"find"`) || !strings.Contains(w.Body.String(), `"name":"send"`) {
				t.Fatal("stable tools missing")
			}
			w = call("tools/call", map[string]any{"name": "list_chats", "arguments": map[string]any{}})
			status(t, w, 200)
			var result struct {
				Result struct {
					IsError bool `json:"isError"`
				}
				Error json.RawMessage
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &result))
			if result.Result.IsError || len(result.Error) > 0 || !strings.Contains(w.Body.String(), "Maya") {
				t.Fatal("remote message read failed")
			}
			refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client}, "refresh_token": {f.refresh}, "resource": {base + "/mcp"}}
			refresh.Set("resource", "https://other.example/mcp")
			status(t, tokenRequest(refresh), 400)
			refresh.Set("resource", base+"/mcp")
			w = tokenRequest(refresh)
			status(t, w, 200)
			rotated := readMap(t, w)
			if rotated["refresh_token"] == f.refresh || rotated["access_token"] == f.token {
				t.Fatal("refresh credentials did not rotate")
			}
			status(t, tokenRequest(refresh), 400)
			f.token = rotated["access_token"].(string)
			status(t, call("tools/list", map[string]any{}), 200)
			// The Settings Disconnect action must invalidate both old and refreshed credentials.
			status(t, r.request(t, "POST", "/v1/remote/revoke", r.api.ControlToken, map[string]string{"client_id": f.client}, nil), 200)
			status(t, call("tools/list", map[string]any{}), 401)
			refresh.Set("refresh_token", rotated["refresh_token"].(string))
			status(t, tokenRequest(refresh), 401)
			status(t, remoteRequest(t, s, "POST", "/mcp", tokens["access_token"].(string), map[string]any{}), 401)
			if len(r.fake.Sent) != 0 {
				t.Fatal("connector flow must never send")
			}
		})
	}
}

func TestRemoteRejectsWrongOrMissingAuthorizationResource(t *testing.T) {
	_, s := remoteRig(t)
	f := register(t, s, "ChatGPT fixture")
	for _, resource := range []string{"", "https://other.example/mcp", "https://limmr.example"} {
		q := url.Values{"response_type": {"code"}, "client_id": {f.client}, "redirect_uri": {"https://client.example/callback"}, "code_challenge_method": {"S256"}, "code_challenge": {f.challenge}, "resource": {resource}}
		status(t, remoteRequest(t, s, "GET", "/authorize?"+q.Encode(), "", nil), 400)
	}
	if len(s.Status().Pending) != 0 {
		t.Fatal("invalid resource created an approval")
	}
}

// Fault injection proves rotation is atomic even if consuming the old credential fails.
func TestRemoteRefreshStorageFailureRollsBackReplacement(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT fixture")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	var before int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='oauth_tokens'").Scan(&before))
	_, err := r.h.Repo.DB.Exec(`CREATE TRIGGER block_refresh_delete BEFORE DELETE ON objects WHEN OLD.bucket='oauth_tokens' BEGIN SELECT RAISE(ABORT,'synthetic storage failure'); END`)
	must(t, err)
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {f.client}, "refresh_token": {f.refresh}, "resource": {"https://limmr.example/mcp"}}
	status(t, remoteRequest(t, s, "POST", "/token", "", refresh), 500)
	var after int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='oauth_tokens'").Scan(&after))
	if before != after {
		t.Fatal("failed rotation persisted replacement credentials")
	}
	_, err = r.h.Repo.DB.Exec("DROP TRIGGER block_refresh_delete")
	must(t, err)
	status(t, remoteRequest(t, s, "POST", "/token", "", refresh), 200)
	status(t, remoteRequest(t, s, "POST", "/token", "", refresh), 400)
}

func TestRemoteRevocationStorageFailureIsReported(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT fixture")
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 200)
	_, err := r.h.Repo.DB.Exec(`CREATE TRIGGER block_client_delete BEFORE DELETE ON objects WHEN OLD.bucket='oauth_clients' BEGIN SELECT RAISE(ABORT,'synthetic storage failure'); END`)
	must(t, err)
	form := url.Values{"client_id": {f.client}, "token": {f.refresh}, "token_type_hint": {"refresh_token"}}
	status(t, remoteRequest(t, s, "POST", "/revoke", "", form), 500)
	_, err = r.h.Repo.DB.Exec("DROP TRIGGER block_client_delete")
	must(t, err)
	status(t, remoteRequest(t, s, "POST", "/revoke", "", form), 200)
	status(t, remoteRequest(t, s, "GET", "/mcp", f.token, nil), 401)
}
