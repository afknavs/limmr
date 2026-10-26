package triggers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Watch)
	}{
		{"empty_scope", func(w *Watch) { w.Scope = Scope{} }},
		{"empty_scope_selector", func(w *Watch) { w.Scope = Scope{Chats: []string{""}} }},
		{"missing_grant", func(w *Watch) { w.CreatedVia.GrantText = "" }},
		{"missing_owner", func(w *Watch) { w.Owner = "" }},
		{"invalid_regex", func(w *Watch) { w.Match = Match{Kind: Regex, Regex: "["} }},
		{"blank_keyword", func(w *Watch) { w.Match = Match{Kind: Keywords, Keywords: []string{" "}} }},
		{"combining_only_keyword", func(w *Watch) { w.Match = Match{Kind: Keywords, Keywords: []string{"\u0301"}} }},
		{"invalid_action", func(w *Watch) { w.Action = "unknown" }},
		{"wake_without_handoff", func(w *Watch) { w.Action = WakeSession }},
		{"unbounded_debounce", func(w *Watch) { w.MaxWindow = 6 * time.Minute }},
		{"negative_debounce", func(w *Watch) { w.QuietWindow = -time.Second }},
		{"invalid_id", func(w *Watch) { w.ID = "../elsewhere" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			w := watch()
			tt.change(&w)
			if _, err := f.e.CreateWatch(testCtx, w); err == nil {
				t.Fatal("invalid watch accepted")
			}
		})
	}
}
func TestHandoffValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Handoff)
	}{
		{"missing_chat", func(h *Handoff) { h.SendGrant.ChatJID = "" }},
		{"wrong_session", func(h *Handoff) { h.CreatedVia.SessionID = "another-session" }},
		{"session_flag", func(h *Handoff) { h.Runner.SessionID = "--latest" }},
		{"relative_workdir", func(h *Handoff) { h.Runner.Workdir = "relative" }},
		{"unknown_runner", func(h *Handoff) { h.Runner.Kind = "shell" }},
		{"negative_runs", func(h *Handoff) { h.Limits.MaxRuns = -1 }},
		{"negative_replies", func(h *Handoff) { h.Limits.MaxRepliesPerHour = -1 }},
		{"invalid_timezone", func(h *Handoff) { h.Limits.QuietHours.Timezone = "Missing/Place" }},
		{"invalid_quiet_time", func(h *Handoff) { h.Limits.QuietHours.Start = "25:00" }},
		{"empty_stop", func(h *Handoff) { h.StopWords = []string{""} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			h := handoff()
			tt.change(&h)
			if _, err := f.e.StartHandoff(testCtx, h); err == nil {
				t.Fatal("invalid handoff accepted")
			}
		})
	}
}
func TestHandoffDefaults(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.Limits = Limits{}
	r, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	if r.ExpiresAt.Sub(f.c.Now()) != 72*time.Hour || r.Handoff.Limits.MaxRepliesPerHour != 12 || r.Handoff.Limits.MaxRuns != 50 || r.QuietWindow != time.Minute || r.MaxWindow != 5*time.Minute || r.Handoff.Limits.QuietHours.Start != "23:00" {
		t.Fatalf("unexpected defaults: %+v", r)
	}
}
func TestQuietHourBoundaries(t *testing.T) {
	for _, tt := range []struct {
		start, end, zone, at string
		want                 bool
	}{
		{"23:00", "08:00", "UTC", "2026-10-17T22:59:00Z", false},
		{"23:00", "08:00", "UTC", "2026-10-17T23:00:00Z", true},
		{"23:00", "08:00", "UTC", "2026-10-18T07:59:00Z", true},
		{"23:00", "08:00", "UTC", "2026-10-18T08:00:00Z", false},
		{"09:00", "17:00", "UTC", "2026-10-17T12:00:00Z", true},
		{"09:00", "17:00", "UTC", "2026-10-17T18:00:00Z", false},
		{"08:00", "08:00", "UTC", "2026-10-17T12:00:00Z", true},
		{"23:00", "08:00", "Asia/Kolkata", "2026-10-17T18:00:00Z", true},
	} {
		t.Run(tt.at+tt.start+tt.zone, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tt.at)
			must(t, err)
			got, err := inQuiet(QuietHours{Start: tt.start, End: tt.end, Timezone: tt.zone}, at)
			must(t, err)
			if got != tt.want {
				t.Fatalf("quiet=%v want %v", got, tt.want)
			}
		})
	}
}
func TestSendPolicyUsesTrustedClock(t *testing.T) {
	f := setup(t)
	f.c.advance(12 * time.Hour)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	d, err := f.e.ReserveSend(testCtx, "handoff", "design-club", "as_agent", f.c.Now().Add(12*time.Hour))
	must(t, err)
	if d.Mode != "park" || d.Reason != "quiet_hours" {
		t.Fatal("future caller timestamp evaded quiet hours")
	}
	count, err := f.s.ReplyCount(testCtx, "handoff", f.c.Now())
	must(t, err)
	if count != 0 {
		t.Fatal("parked send consumed budget")
	}
}
func TestStoreAppendOnly(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	must(t, f.e.AppendInstruction(testCtx, "watch", "original"))
	if err = f.s.Update(testCtx, "watch", func(r *Record) error { r.Instructions[0].Text = "rewritten"; return nil }); err == nil {
		t.Fatal("rewrote instruction")
	}
	if err = f.s.Update(testCtx, "watch", func(r *Record) error { r.CreatedVia.GrantText = "changed"; return nil }); err == nil {
		t.Fatal("rewrote grant")
	}
}
func TestMissingDependenciesAndCancelledCalls(t *testing.T) {
	if _, err := NewEngine(Options{}); err == nil {
		t.Fatal("missing store accepted")
	}
	e, err := NewEngine(Options{Store: NewMemoryStore()})
	must(t, err)
	defer e.Close()
	if _, err = e.CreateWatch(testCtx, watch()); err == nil {
		t.Fatal("missing notifier accepted")
	}
	f := setup(t)
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	if _, err = f.e.CreateWatch(ctx, watch()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
