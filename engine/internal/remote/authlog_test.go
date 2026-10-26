package remote

import (
	"bytes"
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/testkit"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// Every attacker- or client-supplied value in these tests contains this marker.
// The log must never contain it. Failures report case names only, never log or body text.
const sentinel = "SENTINELx7Q"

var authlogBase = "https://limmr.example/limmr"

type authlogRig struct {
	s        *Service
	buf      *bytes.Buffer
	now      time.Time
	clientID string
}

const (
	validCode    = sentinel + "-valid-code"
	goodVerifier = sentinel + "-verifier-" + "abcdefghijklmnopqrstuvwxyz0123456789"
	validRefresh = sentinel + "-valid-refresh"
	staleRefresh = sentinel + "-stale-refresh"
	staleAccess  = sentinel + "-stale-access"
	redirect     = "https://client.example/callback"
)

// newAuthlogRig builds a remote service with one approved public client, a pending
// authorization code, a valid and an expired refresh token, and an expired access token.
func newAuthlogRig(t *testing.T, logged bool) *authlogRig {
	t.Helper()
	s := q1Service(t)
	r := &authlogRig{s: s, buf: &bytes.Buffer{}, now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), clientID: sentinel + "-approved-client"}
	s.Now = func() time.Time { return r.now }
	if logged {
		s.Logger = log.New(r.buf, "", 0)
	}
	testkit.Must(t, s.Configure(Config{Enabled: true, PublicURL: authlogBase}))
	a, _, err := s.Hub.CreateAgent("Example AI", "chatgpt", "remote")
	testkit.Must(t, err)
	at := r.now
	testkit.Must(t, s.Hub.Repo.Put("oauth_clients", r.clientID, Client{ID: r.clientID, Name: "Example AI", RedirectURIs: []string{redirect}, AuthMethod: "none", AgentID: a.ID, ApprovedAt: &at}))
	s.unapproved[sentinel+"-unapproved"] = Client{ID: sentinel + "-unapproved", AuthMethod: "none", ExpiresAt: r.now.Add(time.Hour)}
	s.grants[dbutil.Digest(validCode)] = grant{ClientID: r.clientID, AgentID: a.ID, RedirectURI: redirect, Challenge: shaChallenge(goodVerifier), Resource: authlogBase + "/mcp", ExpiresAt: r.now.Add(5 * time.Minute)}
	put := func(raw, kind string, expires time.Time) {
		testkit.Must(t, s.Hub.Repo.Put("oauth_tokens", dbutil.Digest(raw), tokenRecord{Kind: kind, ClientID: r.clientID, AgentID: a.ID, Resource: authlogBase + "/mcp", ExpiresAt: expires}))
	}
	put(validRefresh, "refresh", r.now.Add(24*time.Hour))
	put(staleRefresh, "refresh", r.now.Add(-time.Minute))
	put(staleAccess, "access", r.now.Add(-time.Minute))
	return r
}

type authlogCase struct {
	name   string
	req    func(clientID string) *http.Request
	status int
	line   string // expected fixed log line (without timestamp)
}

func tokenReq(form url.Values, edit ...func(*http.Request)) *http.Request {
	// Funnel strips /limmr before Limmr sees the request.
	r := httptest.NewRequest("POST", "https://limmr.example/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", "session="+sentinel+"-cookie")
	for _, f := range edit {
		f(r)
	}
	return r
}
func mcpReq(auth ...string) *http.Request {
	r := httptest.NewRequest("POST", "https://limmr.example/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"q":"`+sentinel+`-body"}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Cookie", "session="+sentinel+"-cookie")
	for _, a := range auth {
		r.Header.Add("Authorization", a)
	}
	return r
}

func authlogCases() []authlogCase {
	code := func(clientID string, edit func(url.Values)) url.Values {
		f := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {validCode}, "redirect_uri": {redirect}, "code_verifier": {goodVerifier}, "resource": {authlogBase + "/mcp"}}
		if edit != nil {
			edit(f)
		}
		return f
	}
	refresh := func(clientID string, edit func(url.Values)) url.Values {
		f := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {validRefresh}, "resource": {authlogBase + "/mcp"}}
		if edit != nil {
			edit(f)
		}
		return f
	}
	w := "WARN remote auth failure "
	return []authlogCase{
		{"form duplicate value", func(id string) *http.Request {
			f := refresh(id, nil)
			f.Add("refresh_token", sentinel+"-dup")
			return tokenReq(f)
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_request reason=form_invalid"},
		{"unknown client", func(string) *http.Request { return tokenReq(refresh(sentinel+"-client", nil)) }, 401,
			w + "endpoint=token grant=refresh_token error=invalid_client reason=client_unknown"},
		{"public client sent secret", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("client_secret", sentinel+"-secret") }))
		}, 401, w + "endpoint=token grant=refresh_token error=invalid_client reason=client_auth_mismatch"},
		{"public client used basic auth", func(id string) *http.Request {
			return tokenReq(refresh(id, nil), func(r *http.Request) { r.SetBasicAuth(id, sentinel+"-basic") })
		}, 401, w + "endpoint=token grant=refresh_token error=invalid_client reason=client_auth_mismatch"},
		{"unapproved client", func(string) *http.Request { return tokenReq(refresh(sentinel+"-unapproved", nil)) }, 400,
			w + "endpoint=token grant=refresh_token error=invalid_grant reason=client_unapproved"},
		{"wrong resource", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("resource", "https://"+sentinel+".example/mcp") }))
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_target reason=resource_mismatch"},
		{"unknown grant type", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("grant_type", sentinel+"-grant") }))
		}, 400, w + "endpoint=token grant=unknown error=unsupported_grant_type reason=grant_unsupported"},
		{"unknown code", func(id string) *http.Request {
			return tokenReq(code(id, func(f url.Values) { f.Set("code", sentinel+"-code") }))
		}, 400, w + "endpoint=token grant=authorization_code error=invalid_grant reason=code_unknown"},
		{"redirect mismatch", func(id string) *http.Request {
			return tokenReq(code(id, func(f url.Values) { f.Set("redirect_uri", "https://"+sentinel+".example/cb") }))
		}, 400, w + "endpoint=token grant=authorization_code error=invalid_grant reason=redirect_mismatch"},
		{"malformed verifier", func(id string) *http.Request {
			return tokenReq(code(id, func(f url.Values) { f.Set("code_verifier", sentinel) }))
		}, 400, w + "endpoint=token grant=authorization_code error=invalid_grant reason=verifier_invalid"},
		{"wrong verifier", func(id string) *http.Request {
			return tokenReq(code(id, func(f url.Values) { f.Set("code_verifier", sentinel+"-other-"+strings.Repeat("z", 40)) }))
		}, 400, w + "endpoint=token grant=authorization_code error=invalid_grant reason=verifier_mismatch"},
		{"unknown refresh", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("refresh_token", sentinel+"-refresh") }))
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_grant reason=refresh_unknown"},
		{"expired refresh", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("refresh_token", staleRefresh) }))
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_grant reason=refresh_expired"},
		{"access token used as refresh", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("refresh_token", staleAccess) }))
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_grant reason=refresh_kind"},
		{"bad scope", func(id string) *http.Request {
			return tokenReq(refresh(id, func(f url.Values) { f.Set("scope", sentinel) }))
		}, 400, w + "endpoint=token grant=refresh_token error=invalid_grant reason=scope_invalid"},
		{"origin rejected", func(id string) *http.Request {
			return tokenReq(refresh(id, nil), func(r *http.Request) { r.Header.Set("Origin", "https://"+sentinel+".example") })
		}, 403, w + "endpoint=token grant=none error=forbidden reason=origin_rejected"},
		{"host mismatch", func(id string) *http.Request {
			return tokenReq(refresh(id, nil), func(r *http.Request) { r.Host = sentinel + ".example" })
		}, 403, w + "endpoint=token grant=none error=forbidden reason=host_mismatch"},
		{"bearer missing", func(string) *http.Request { return mcpReq() }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_missing"},
		{"bearer malformed", func(string) *http.Request { return mcpReq("Basic " + sentinel) }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer unknown", func(string) *http.Request { return mcpReq("Bearer " + sentinel + "-bearer") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_unknown"},
		// The SDK splits on any whitespace and ignores scheme case, so these reach Verify.
		{"bearer accepted with tabs", func(string) *http.Request { return mcpReq("\tBearer\t" + sentinel + "-tab") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_unknown"},
		{"bearer accepted with many spaces", func(string) *http.Request { return mcpReq("Bearer     " + sentinel + "-spaces") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_unknown"},
		{"bearer accepted with leading whitespace", func(string) *http.Request { return mcpReq("   Bearer " + sentinel + "-lead") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_unknown"},
		{"bearer accepted with mixed case", func(string) *http.Request { return mcpReq("bEaReR " + sentinel + "-case") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_unknown"},
		// The SDK rejects these before Verify.
		{"bearer blank header", func(string) *http.Request { return mcpReq(" \t ") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_missing"},
		{"bearer scheme without token", func(string) *http.Request { return mcpReq("Bearer") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer scheme with only spaces", func(string) *http.Request { return mcpReq("Bearer   ") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer extra fields", func(string) *http.Request { return mcpReq("Bearer " + sentinel + "-a " + sentinel + "-b") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer wrong scheme", func(string) *http.Request { return mcpReq("Token " + sentinel + "-scheme") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer scheme glued to token", func(string) *http.Request { return mcpReq("Bearer" + sentinel + "-glued") }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=bearer_malformed"},
		{"bearer expired", func(string) *http.Request { return mcpReq("Bearer " + staleAccess) }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_expired"},
		{"bearer is a refresh token", func(string) *http.Request { return mcpReq("Bearer " + validRefresh) }, 401,
			w + "endpoint=mcp grant=none error=invalid_token reason=token_kind"},
		{"duplicate authorization", func(string) *http.Request {
			return mcpReq("Bearer "+sentinel+"-a", "Bearer "+sentinel+"-b")
		}, 403, w + "endpoint=mcp grant=none error=forbidden reason=duplicate_authorization"},
	}
}

func serve(s *Service, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// Each failure path logs exactly one fixed line, never a request value, and the response
// is byte-for-byte what an unlogged service returns.
func TestAuthFailureLogIsRedactedAndResponsesUnchanged(t *testing.T) {
	for _, c := range authlogCases() {
		t.Run(c.name, func(t *testing.T) {
			logged, plain := newAuthlogRig(t, true), newAuthlogRig(t, false)
			got := serve(logged.s, c.req(logged.clientID))
			want := serve(plain.s, c.req(plain.clientID))
			if got.Code != c.status || want.Code != c.status {
				t.Fatalf("%s: status %d (unlogged %d), want %d", c.name, got.Code, want.Code, c.status)
			}
			if !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
				t.Fatalf("%s: response body changed by logging", c.name)
			}
			for _, h := range []string{"Content-Type", "Www-Authenticate", "Cache-Control"} {
				if got.Header().Get(h) != want.Header().Get(h) {
					t.Fatalf("%s: response header %s changed by logging", c.name, h)
				}
			}
			out := logged.buf.String()
			if strings.Contains(out, sentinel) || strings.Contains(out, "example") || strings.Contains(out, "Bearer") {
				t.Fatalf("%s: log contains a request value", c.name)
			}
			if strings.TrimSpace(out) != c.line {
				t.Fatalf("%s: log line differs from the expected fixed line", c.name)
			}
		})
	}
}

// Successful exchanges and refreshes record only that credentials were issued.
func TestAuthIssuedLogIsRedacted(t *testing.T) {
	r := newAuthlogRig(t, true)
	codeForm := url.Values{"grant_type": {"authorization_code"}, "client_id": {r.clientID}, "code": {validCode}, "redirect_uri": {redirect}, "code_verifier": {goodVerifier}, "resource": {authlogBase + "/mcp"}}
	if serve(r.s, tokenReq(codeForm)).Code != 200 {
		t.Fatal("code exchange failed")
	}
	refreshForm := url.Values{"grant_type": {"refresh_token"}, "client_id": {r.clientID}, "refresh_token": {validRefresh}}
	w := serve(r.s, tokenReq(refreshForm))
	if w.Code != 200 {
		t.Fatal("refresh failed")
	}
	// Reusing the consumed refresh token is a normal invalid_grant.
	if serve(r.s, tokenReq(refreshForm)).Code != 400 {
		t.Fatal("consumed refresh accepted")
	}
	lines := strings.Split(strings.TrimSpace(r.buf.String()), "\n")
	want := []string{
		"INFO remote auth issued endpoint=token grant=authorization_code",
		"INFO remote auth issued endpoint=token grant=refresh_token",
		"WARN remote auth failure endpoint=token grant=refresh_token error=invalid_grant reason=refresh_unknown",
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d log lines, want %d", len(lines), len(want))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("log line %d differs from the expected fixed line", i)
		}
	}
	issued := w.Body.String()
	for _, l := range lines {
		if strings.Contains(l, sentinel) || strings.Contains(issued, l) {
			t.Fatal("log contains a request or response value")
		}
	}
}

// Repeated identical failures are bounded to one line per window with a suppressed count.
func TestAuthFailureLogIsRateLimited(t *testing.T) {
	r := newAuthlogRig(t, true)
	for i := 0; i < 5; i++ {
		serve(r.s, mcpReq())
	}
	if n := strings.Count(r.buf.String(), "\n"); n != 1 {
		t.Fatalf("got %d lines within one window, want 1", n)
	}
	r.now = r.now.Add(authLogWindow + time.Second)
	serve(r.s, mcpReq())
	lines := strings.Split(strings.TrimSpace(r.buf.String()), "\n")
	if len(lines) != 2 || lines[1] != "WARN remote auth failure endpoint=mcp grant=none error=invalid_token reason=bearer_missing suppressed=4" {
		t.Fatal("next window did not report the suppressed count")
	}
	// A different failure is not suppressed by the first one.
	serve(r.s, mcpReq("Bearer "+sentinel))
	if strings.Count(r.buf.String(), "\n") != 3 || strings.Contains(r.buf.String(), sentinel) {
		t.Fatal("distinct failure suppressed or leaked")
	}
}

// Only vocabulary values reach the log; anything else is written as "unlisted".
func TestAuthLogAllowlist(t *testing.T) {
	r := newAuthlogRig(t, true)
	r.s.authFailure(sentinel, sentinel, sentinel, sentinel)
	if strings.TrimSpace(r.buf.String()) != "WARN remote auth failure endpoint=unlisted grant=unlisted error=unlisted reason=unlisted" {
		t.Fatal("non-vocabulary value reached the log")
	}
	for raw, want := range map[string]string{"authorization_code": "authorization_code", "refresh_token": "refresh_token", "": "none", sentinel: "unknown", "Refresh_Token": "unknown"} {
		if grantKind(raw) != want {
			t.Fatal("grant type mapping changed")
		}
	}
	// A nil logger is silent and safe.
	plain := newAuthlogRig(t, false)
	plain.s.authFailure("token", "none", "invalid_request", "form_invalid")
}

// bearerLabel must agree with the SDK it mirrors: it is empty exactly when the SDK's
// RequireBearerToken hands the token to the verifier. Failures report the case index only.
func TestBearerLabelMatchesSDKParsing(t *testing.T) {
	headers := []struct {
		h    string
		want string
	}{
		{"Bearer " + sentinel, ""},
		{"\tBearer\t" + sentinel, ""},
		{"Bearer     " + sentinel, ""},
		{"   Bearer " + sentinel, ""},
		{"bEaReR " + sentinel, ""},
		{"BEARER " + sentinel + " \t", ""},
		{"Bearer " + sentinel, ""},
		{"", "bearer_missing"},
		{" \t ", "bearer_missing"},
		{"Bearer", "bearer_malformed"},
		{"Bearer   ", "bearer_malformed"},
		{"Bearer " + sentinel + " " + sentinel, "bearer_malformed"},
		{"Basic " + sentinel, "bearer_malformed"},
		{"Token " + sentinel, "bearer_malformed"},
		{"Bearer" + sentinel, "bearer_malformed"},
		{sentinel, "bearer_malformed"},
	}
	for i, c := range headers {
		called := false
		verifier := func(context.Context, string, *http.Request) (*auth.TokenInfo, error) {
			called = true
			return nil, auth.ErrInvalidToken
		}
		h := auth.RequireBearerToken(verifier, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		r := httptest.NewRequest("POST", "http://127.0.0.1/mcp", nil)
		if c.h != "" {
			r.Header.Set("Authorization", c.h)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		got := bearerLabel(c.h)
		if got != c.want {
			t.Fatalf("case %d: label differs from expected", i)
		}
		if (got == "") != called {
			t.Fatalf("case %d: label disagrees with SDK parsing", i)
		}
		if strings.Contains(got, sentinel) {
			t.Fatalf("case %d: label contains a request value", i)
		}
	}
}
