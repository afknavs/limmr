package health_test

import (
	"context"
	"limmr/internal/health"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type connection struct {
	outdated bool
	calls    []string
}

func (*connection) Linked() bool          { return true }
func (*connection) Connected() bool       { return false }
func (*connection) SyncDone() bool        { return false }
func (c *connection) Outdated() bool      { return c.outdated }
func (*connection) PhoneSeen() *time.Time { return nil }
func (c *connection) Reconnect(context.Context) error {
	c.calls = append(c.calls, "reconnect")
	return nil
}
func (c *connection) RefreshGroups(context.Context) error {
	c.calls = append(c.calls, "groups")
	return nil
}
func (c *connection) RequestHistory(context.Context) error {
	c.calls = append(c.calls, "history")
	return nil
}
func TestHealthStatesFixesAndCanaryDiagnostics(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, token, e := h.CreateAgent("CANARY_NAME", "custom", "local")
	testkit.Must(t, e)
	settings := h.Settings()
	settings.Owner = "CANARY_OWNER"
	testkit.Must(t, h.SetSettings(settings))
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Note: "CANARY_TEXT"}))
	l.Emit(wa.LinkEvent{State: "ready", Account: &wa.Account{Name: "CANARY_ACCOUNT", PhoneMasked: "15550100001"}})
	c := &connection{outdated: true}
	s := &health.Service{Hub: h, Link: l, Home: h.Home, Connection: c}
	checks := s.Checks(context.Background())
	if health.Overall(checks) != "fail" {
		t.Fatal("outdated client not a failure")
	}
	found := false
	for _, ch := range checks {
		if ch.ID == "whatsapp" && strings.Contains(ch.Detail, "405") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing outdated client evidence")
	}
	report := s.Diagnostics(context.Background())
	for _, canary := range []string{"CANARY", "15550100001", testkit.Phone, token, h.Home} {
		if strings.Contains(report, canary) {
			t.Fatal("diagnostic exposed a canary")
		}
	}
	for _, fix := range []string{"reconnect", "refresh_groups", "history_sync", "rebuild_search", "vacuum", "trim_logs"} {
		testkit.Must(t, s.Fix(context.Background(), fix))
	}
	if len(c.calls) != 3 {
		t.Fatal("fix dispatch wrong")
	}
	if s.Fix(context.Background(), "unknown") != model.ErrInvalid {
		t.Fatal("unknown fix accepted")
	}
}
func TestBoundedLogRetainsTailAndInode(t *testing.T) {
	home := t.TempDir()
	l, e := health.OpenLog(home)
	testkit.Must(t, e)
	defer l.Close()
	path := filepath.Join(home, "logs", "engine.log")
	before, e := os.Stat(path)
	testkit.Must(t, e)
	_, e = l.Write([]byte(strings.Repeat("a", (20<<20)+1) + "final-line"))
	testkit.Must(t, e)
	_, e = l.Write([]byte("next-line"))
	testkit.Must(t, e)
	after, e := os.Stat(path)
	testkit.Must(t, e)
	b, e := os.ReadFile(path)
	testkit.Must(t, e)
	if !os.SameFile(before, after) || len(b) != (5<<20)+len("next-line") || !strings.HasSuffix(string(b), "final-linenext-line") {
		t.Fatal("bounded log lost tail/inode")
	}
}
