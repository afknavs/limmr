package remote

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func q1Service(t *testing.T) *Service {
	t.Helper()
	h, p, _ := testkit.Open(t)
	s := New(h, &mcpserver.Service{Hub: h, People: p}, 0)
	s.Listen = func(string, string) (net.Listener, error) { return &q1Listener{done: make(chan struct{})}, nil }
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestURLChangePreservesOAuthAndRestore(t *testing.T) {
	s := q1Service(t)
	// Exercise Configure without opening any public address (listener is loopback).
	old := Config{Enabled: true, PublicURL: "https://old.example/limmr"}
	testkit.Must(t, s.Configure(old))
	a, _, err := s.Hub.CreateAgent("Example AI", "chatgpt", "remote")
	testkit.Must(t, err)
	now := s.Now()
	c := Client{ID: "approved", Name: "Example AI", AgentID: a.ID, ApprovedAt: &now}
	testkit.Must(t, s.Hub.Repo.Put("oauth_clients", c.ID, c))
	// Tokens are generated only in memory; no credential is emitted on failure.
	raw := dbutil.ID() + dbutil.ID()
	for _, kind := range []string{"access", "refresh"} {
		key := dbutil.Digest(raw)
		if kind == "refresh" {
			key = dbutil.Digest(raw + "refresh")
		}
		testkit.Must(t, s.Hub.Repo.Put("oauth_tokens", key, tokenRecord{Kind: kind, ClientID: c.ID, AgentID: a.ID, Resource: old.PublicURL + "/mcp", ExpiresAt: now.Add(time.Hour)}))
	}
	if _, err := s.Verify(context.Background(), raw, nil); err != nil {
		t.Fatal("initial token rejected")
	}
	s.pending["pending"] = &Pending{ExpiresAt: now.Add(time.Hour)}
	s.grants["grant"] = grant{ExpiresAt: now.Add(time.Hour)}
	testkit.Must(t, s.Configure(Config{Enabled: true, PublicURL: "https://new.example/limmr"}))
	tokens, err := hub.List[tokenRecord](s.Hub.Repo, "oauth_tokens")
	testkit.Must(t, err)
	if len(tokens) != 2 || len(s.Status().Clients) != 1 {
		t.Fatal("URL change destroyed OAuth data")
	}
	if s.Status().ClientsOnOtherURL != 1 || s.Status().ClientsWithTokens != 1 {
		t.Fatal("token holders not counted distinctly")
	}
	if len(s.pending) != 0 || len(s.grants) != 0 {
		t.Fatal("URL change retained ephemeral grants")
	}
	if _, err := s.Verify(context.Background(), raw, nil); err == nil {
		t.Fatal("old resource token accepted on new URL")
	}
	testkit.Must(t, s.Configure(old))
	if _, err := s.Verify(context.Background(), raw, nil); err != nil {
		t.Fatal("restored resource token rejected")
	}
	if s.Status().ClientsOnOtherURL != 0 {
		t.Fatal("restored resource still mismatched")
	}
	testkit.Must(t, s.Revoke(c.ID))
	tokens, err = hub.List[tokenRecord](s.Hub.Repo, "oauth_tokens")
	testkit.Must(t, err)
	if len(tokens) != 0 || len(s.Status().Clients) != 0 {
		t.Fatal("explicit disconnect retained OAuth data")
	}
	if _, err := s.Verify(context.Background(), raw, nil); err == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestConfigureRollbackGuardsAreAtomic(t *testing.T) {
	s := q1Service(t)
	intended := Config{Enabled: true, PublicURL: "https://wizard.example"}
	other := Config{Enabled: false, PublicURL: "https://other.example"}
	testkit.Must(t, s.Configure(other))
	rollback := Config{OnlyIf: &intended, RequireNoClients: true}
	if s.Configure(rollback) != model.ErrConflict {
		t.Fatal("stale rollback accepted")
	}
	if s.Configuration().PublicURL != other.PublicURL {
		t.Fatal("stale rollback changed address")
	}
	testkit.Must(t, s.Configure(intended))
	now := s.Now()
	testkit.Must(t, s.Hub.Repo.Put("oauth_clients", "approved", Client{ID: "approved", ApprovedAt: &now}))
	if s.Configure(rollback) != model.ErrConflict || !s.Configuration().Enabled {
		t.Fatal("rollback disabled an approved connection")
	}
	testkit.Must(t, s.Revoke("approved"))
	testkit.Must(t, s.Configure(rollback))
	if s.Configuration().Enabled || s.Configuration().OnlyIf != nil || s.Configuration().RequireNoClients {
		t.Fatal("guard persisted or restore failed")
	}
}

func TestPairContinueFallbackAndIsolation(t *testing.T) {
	s := q1Service(t)
	s.config = Config{Enabled: true, PublicURL: "https://limmr.example/limmr"}
	now := s.Now()
	s.pending["waiting-tx"] = &Pending{Tx: "waiting-tx", Name: "Example AI", Code: "123456", State: "waiting", ExpiresAt: now.Add(time.Minute)}
	redirect := "https://client.example/callback?code=invented-grant&state=exact%2Bstate"
	s.pending["approved-tx"] = &Pending{Tx: "approved-tx", Code: "654321", State: "approved", Redirect: redirect, ExpiresAt: now.Add(time.Minute)}
	s.pending["expired-tx"] = &Pending{Tx: "expired-tx", Code: "456789", State: "approved", Redirect: redirect, ExpiresAt: now.Add(-time.Second)}
	for _, tx := range []string{"waiting-tx", "approved-tx", "expired-tx", "unknown-tx", "unknown-tx&tx=approved-tx", ""} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://limmr.example/pair/continue?tx="+tx, nil))
		switch tx {
		case "approved-tx":
			if w.Code != 303 || w.Header().Get("Location") != redirect {
				t.Fatal("continue did not redirect to exact stored callback")
			}
		case "waiting-tx":
			body := w.Body.String()
			if w.Code != 200 || !strings.Contains(body, "123456") || !strings.Contains(body, "I’ve approved it, continue") || !strings.Contains(body, "<noscript>") || !strings.Contains(body, `http-equiv="refresh" content="5;url=https://limmr.example/limmr/pair/continue?tx=waiting-tx"`) || !strings.Contains(body, "/limmr/pair/status?tx=waiting-tx") {
				t.Fatal("waiting fallback missing")
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'nonce-") || !strings.Contains(body, `<script nonce="`) {
				t.Fatal("nonce CSP lost")
			}
		default:
			if w.Code != 410 || !strings.Contains(w.Body.String(), "expired") {
				t.Fatal("unknown transaction not expired")
			}
		}
		if tx != "approved-tx" && (w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), redirect) || strings.Contains(w.Body.String(), "654321")) {
			t.Fatal("transaction leaked another approval")
		}
	}
}

// No socket is bound; requests exercise the real handlers via httptest.
type q1Listener struct {
	done chan struct{}
	once sync.Once
}

func (l *q1Listener) Accept() (net.Conn, error) { <-l.done; return nil, net.ErrClosed }
func (l *q1Listener) Close() error              { l.once.Do(func() { close(l.done) }); return nil }
func (l *q1Listener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }
