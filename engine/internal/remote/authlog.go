package remote

import (
	"strings"
	"sync"
	"time"
)

// Remote auth failures are logged with fixed vocabulary only. Request values
// (tokens, codes, verifiers, secrets, client IDs, headers, URLs, bodies) never
// reach the log: every field is checked against an allowlist, and anything else
// is written as "unlisted".

var authEndpoints = set("token", "mcp", "other")
var authGrants = set("authorization_code", "refresh_token", "none", "unknown")
var authCodes = set("invalid_request", "invalid_client", "invalid_grant", "invalid_target", "unsupported_grant_type", "server_error", "invalid_token", "forbidden", "not_found")
var authReasons = set(
	// outer guards
	"remote_disabled", "host_mismatch", "duplicate_authorization", "origin_rejected",
	// token endpoint
	"form_invalid", "client_unknown", "client_auth_mismatch", "client_unapproved", "agent_inactive", "resource_mismatch",
	"code_unknown", "code_client_mismatch", "code_agent_mismatch", "redirect_mismatch", "code_resource_mismatch", "verifier_invalid", "verifier_mismatch",
	"refresh_unknown", "refresh_kind", "refresh_client_mismatch", "refresh_agent_mismatch", "refresh_resource_mismatch", "refresh_expired", "scope_invalid", "refresh_consumed",
	"grant_unsupported", "storage",
	// bearer
	"bearer_missing", "bearer_malformed", "token_unknown", "token_kind", "token_expired", "token_resource_mismatch", "client_agent_mismatch", "agent_not_remote", "agent_missing",
)

// authLogWindow bounds repeated identical lines; the next line reports how many were suppressed.
const authLogWindow = time.Minute

type authLogger struct {
	mu         sync.Mutex
	last       map[string]time.Time
	suppressed map[string]int
}

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}
func allow(m map[string]bool, v string) string {
	if m[v] {
		return v
	}
	return "unlisted"
}

// grantKind maps the client-supplied grant_type to a fixed enum.
func grantKind(raw string) string {
	switch raw {
	case "authorization_code", "refresh_token":
		return raw
	case "":
		return "none"
	}
	return "unknown"
}

// bearerLabel mirrors go-sdk auth.verify (strings.Fields, exactly two fields,
// case-insensitive "bearer") to label headers the SDK will reject before Verify.
// It returns "" when the SDK would pass the token on to Verify. Logging only.
func bearerLabel(header string) string {
	fields := strings.Fields(header)
	switch {
	case len(fields) == 0:
		return "bearer_missing"
	case len(fields) != 2 || strings.ToLower(fields[0]) != "bearer":
		return "bearer_malformed"
	}
	return ""
}

func endpointKind(path string) string {
	switch path {
	case "/token":
		return "token"
	case "/mcp":
		return "mcp"
	}
	return "other"
}

func (s *Service) authFailure(endpoint, grant, code, reason string) {
	s.authLine("WARN remote auth failure", endpoint, grant, code, reason)
}

// authIssued records that the token endpoint issued credentials, so a reconnect
// shows whether requests reached Limmr at all. It carries no request values.
func (s *Service) authIssued(grant string) {
	s.authLine("INFO remote auth issued", "token", grant, "", "")
}

func (s *Service) authLine(prefix, endpoint, grant, code, reason string) {
	if s.Logger == nil {
		return
	}
	line := prefix + " endpoint=" + allow(authEndpoints, endpoint) + " grant=" + allow(authGrants, grant)
	if code != "" {
		line += " error=" + allow(authCodes, code)
	}
	if reason != "" {
		line += " reason=" + allow(authReasons, reason)
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	l := &s.authLog
	l.mu.Lock()
	if l.last == nil {
		l.last, l.suppressed = map[string]time.Time{}, map[string]int{}
	}
	if t, ok := l.last[line]; ok && now.Sub(t) < authLogWindow {
		l.suppressed[line]++
		l.mu.Unlock()
		return
	}
	n := l.suppressed[line]
	l.last[line], l.suppressed[line] = now, 0
	l.mu.Unlock()
	if n > 0 {
		s.Logger.Printf("%s suppressed=%d", line, n)
		return
	}
	s.Logger.Print(line)
}
