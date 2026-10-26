package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"limmr/internal/watches"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func updates(t *testing.T, r *rig, args map[string]any) watches.Updates {
	t.Helper()
	out, err := r.call(t, "get_updates", args)
	must(t, err)
	return out.(watches.Updates)
}
func incoming(t *testing.T, r *rig, s *watches.Service, id, chat, text string) {
	t.Helper()
	m := model.Message{ID: id, ChatJID: chat, Sender: chat, Content: text, Timestamp: epoch}
	must(t, r.m.PutMessage(m))
	must(t, s.Handle(context.Background(), wa.StoredEvent{Message: m}))
}
func standingFeed(t *testing.T, r *rig) watches.Detail {
	t.Helper()
	return createWatch(t, r, map[string]any{"action": "feed", "scope": map[string]any{"all": true}, "match": map[string]any{"kind": "any"}, "title": "My updates"})
}
func TestW1UpdatesCursorPagingAndIsolation(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	w := createWatch(t, r, nil)
	old := firstMessage(t, r, chatA)
	must(t, s.Store.PutHit(context.Background(), triggers.Hit{ID: "old", WatchID: w.ID, At: epoch.Add(-25 * time.Hour), Action: triggers.Notify, State: "recorded", Messages: []triggers.IncomingMessage{{ID: old.ID, ChatJID: chatA, Sender: chatA, Text: old.Content, At: epoch.Add(-25 * time.Hour)}}}))
	for i := 0; i < 105; i++ {
		incoming(t, r, s, fmt.Sprintf("u-%03d", i), chatA, "exam moved")
	}
	c.advance(time.Minute)
	must(t, s.Engine.FlushDue(context.Background()))
	page := updates(t, r, map[string]any{"limit": 100})
	if len(page.Items) != 100 || !page.More || page.Cursor == "" {
		t.Fatal("first page bound")
	}
	for _, item := range page.Items {
		if item.Kind != "watch_hit" || item.WatchID != w.ID || item.Chat != "Maya Rao" || item.Sender != "Maya Rao" {
			t.Fatal("watch projection")
		}
	}
	next := updates(t, r, map[string]any{"cursor": page.Cursor, "limit": 100})
	if len(next.Items) != 5 || next.More {
		t.Fatal("cursor skipped or repeated events")
	}
	seen := map[string]bool{}
	for _, item := range append(page.Items, next.Items...) {
		if seen[item.MessageID] {
			t.Fatal("duplicate page item")
		}
		seen[item.MessageID] = true
	}
	if len(updates(t, r, map[string]any{"cursor": next.Cursor}).Items) != 0 {
		t.Fatal("cursor replayed consumed events")
	}
	replay := updates(t, r, map[string]any{"cursor": page.Cursor})
	if len(replay.Items) != 5 {
		t.Fatal("cursor retries not stable")
	}
	other, _, err := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, err)
	if _, err = s.GetUpdates(context.Background(), other.ID, page.Cursor, 0, 100); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("cross-agent cursor accepted")
	}
	b := []byte(page.Cursor)
	if b[10] == 'a' {
		b[10] = 'b'
	} else {
		b[10] = 'a'
	}
	if _, err = s.GetUpdates(context.Background(), r.a.ID, string(b), 0, 100); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("tampered cursor accepted")
	}
	for _, args := range []map[string]any{{"wait_seconds": 26}, {"limit": 101}, {"limit": 0}, {"wait_seconds": -1}} {
		if _, err = r.call(t, "get_updates", args); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid bounds accepted")
		}
	}
	// Current text is looked up rather than returning the old hit copy.
	must(t, r.m.PutMessage(model.Message{ID: "u-000", ChatJID: chatA, Sender: chatA, Content: "Edited update", Timestamp: epoch}))
	if updates(t, r, map[string]any{"limit": 1}).Items[0].Text != "Edited update" {
		t.Fatal("stale hit text leaked")
	}
}
func TestW1FeedAccessExpiryPauseAndGrant(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	_, err := r.call(t, "create_watch", map[string]any{"goal": "Follow", "scope": map[string]any{"all": true}, "match": map[string]any{"kind": "any"}, "action": "feed", "grant_text": " "})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatal("missing grant allowed")
	}
	f := standingFeed(t, r)
	if f.Kind != "feed" || f.Where != "Everything it can read" {
		t.Fatal("feed summary")
	}
	if _, err = r.call(t, "create_watch", map[string]any{"goal": "Follow again", "scope": map[string]any{"all": true}, "match": map[string]any{"kind": "any"}, "action": "feed", "grant_text": "Follow everything"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("duplicate feed allowed")
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	incoming(t, r, s, "f-allowed", chatA, "one")
	incoming(t, r, s, "f-denied", chatB, "PRIVATE")
	c.advance(time.Second)
	must(t, s.Engine.FlushDue(context.Background()))
	page := updates(t, r, map[string]any{})
	if len(page.Items) != 1 || page.Items[0].Kind != "message" || strings.Contains(encode(page), "PRIVATE") {
		t.Fatal("feed access")
	}
	// A current hidden-chat change hides already queued hits too.
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(settings))
	if len(updates(t, r, map[string]any{}).Items) != 0 {
		t.Fatal("hidden backlog leaked")
	}
	settings.HiddenChats = []string{}
	must(t, r.h.SetSettings(settings))
	must(t, s.Status(context.Background(), f.ID, triggers.Paused))
	incoming(t, r, s, "f-paused", chatA, "paused")
	c.advance(5 * time.Second)
	must(t, s.Engine.FlushDue(context.Background()))
	if len(updates(t, r, map[string]any{}).Items) != 0 {
		t.Fatal("paused feed returned backlog")
	}
	must(t, s.Status(context.Background(), f.ID, triggers.Active))
	// New chats become eligible after a grant widens; no feed recreation required.
	p.Chats = model.Scope{Mode: "all"}
	r.agent(t, p)
	incoming(t, r, s, "f-new", chatB, "newly allowed")
	c.advance(time.Second)
	must(t, s.Engine.FlushDue(context.Background()))
	page = updates(t, r, map[string]any{"cursor": page.Cursor})
	if len(page.Items) != 1 || page.Items[0].ChatJID != chatB {
		t.Fatal("dynamic feed scope")
	}
	deadline := epoch.Add(time.Hour)
	must(t, s.Store.Update(context.Background(), f.ID, func(w *triggers.Record) error { w.ExpiresAt = &deadline; return nil }))
	r.h.Now = func() time.Time { return epoch.Add(2 * time.Hour) }
	if len(updates(t, r, map[string]any{}).Items) != 0 {
		t.Fatal("expired feed returned backlog")
	}
	// Access expiry also closes an already-started poll.
	p.Access.Until = &deadline
	r.agent(t, p)
	if _, err = s.GetUpdates(context.Background(), r.a.ID, "", 0, 100); err == nil {
		t.Fatal("expired access accepted")
	}
}
func TestW1LongPollArrivalTimeoutCancellationAndPermissionChange(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	standingFeed(t, r)
	first := updates(t, r, map[string]any{})
	type result struct {
		out watches.Updates
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := s.GetUpdates(context.Background(), r.a.ID, first.Cursor, 2, 100)
		done <- result{out, err}
	}()
	select {
	case <-done:
		t.Fatal("empty poll returned early")
	case <-time.After(150 * time.Millisecond):
	}
	incoming(t, r, s, "poll-one", chatA, "Arrived")
	c.advance(time.Second)
	must(t, s.Engine.FlushDue(context.Background()))
	var page watches.Updates
	select {
	case res := <-done:
		must(t, res.err)
		page = res.out
		if len(page.Items) != 1 {
			t.Fatal("poll missed arrival")
		}
	case <-time.After(time.Second):
		t.Fatal("poll did not wake")
	}
	start := time.Now()
	out, err := s.GetUpdates(context.Background(), r.a.ID, page.Cursor, 1, 100)
	must(t, err)
	if len(out.Items) != 0 || time.Since(start) < 900*time.Millisecond || time.Since(start) > 2*time.Second {
		t.Fatal("poll timeout not bounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.GetUpdates(ctx, r.a.ID, page.Cursor, 25, 100); err == nil {
		t.Fatal("cancellation ignored")
	}
	go func() {
		out, err := s.GetUpdates(context.Background(), r.a.ID, page.Cursor, 2, 100)
		done <- result{out, err}
	}()
	time.Sleep(150 * time.Millisecond)
	a, err := r.h.Agent(r.a.ID)
	must(t, err)
	a.Enabled = false
	must(t, r.h.UpdateAgent(a))
	select {
	case res := <-done:
		if res.err == nil {
			t.Fatal("revocation ignored")
		}
	case <-time.After(time.Second):
		t.Fatal("revocation did not wake poll")
	}
}
func TestW1OwnSendStatusAndNoFeedMessagesWithoutGrant(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	n1Circle(t, r, chatA, "free", true)
	incoming(t, r, s, "no-feed", chatA, "No standing grant")
	if len(updates(t, r, map[string]any{}).Items) != 0 {
		t.Fatal("message without feed grant")
	}
	out, err := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "hello", "voice": "as_agent", "signed": false})
	d := n1Draft(t, out, err)
	must(t, r.h.RecordReceipt(chatA, chatA, []string{d.MessageID}, "delivered", epoch.Add(time.Minute)))
	page := updates(t, r, map[string]any{})
	statuses := []string{}
	for _, item := range page.Items {
		if item.Kind != "send_status" || item.SendID != d.ID {
			t.Fatal("send projection")
		}
		statuses = append(statuses, item.Status)
	}
	if strings.Join(statuses, ",") != "queued,sent,delivered" {
		t.Fatalf("send transitions: %v", statuses)
	}
	other, _, err := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, err)
	otherPage, err := s.GetUpdates(context.Background(), other.ID, "", 0, 100)
	must(t, err)
	if len(otherPage.Items) != 0 {
		t.Fatal("other agent's send leaked")
	}
}
func TestW1WaitingContractAndHome(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	w := createWatch(t, r, nil)
	f := standingFeed(t, r)
	handoff := createHandoff(t, r, &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"})
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "GET", "/v1/waiting", token, nil, nil), 403)
	}
	response := r.request(t, "GET", "/v1/waiting", r.api.ControlToken, nil, nil)
	status(t, response, 200)
	var v struct {
		Waiting []watches.Waiting `json:"waiting"`
	}
	must(t, json.Unmarshal(response.Body.Bytes(), &v))
	if len(v.Waiting) != 3 {
		t.Fatal("missing waiting kinds")
	}
	kinds := map[string]bool{}
	for _, item := range v.Waiting {
		kinds[item.Kind] = true
		if item.Agent != r.a.Name || !strings.Contains(item.Sentence, r.a.Name) || item.Since.IsZero() || item.Latest != nil {
			t.Fatal("waiting contract")
		}
	}
	if !kinds["watch"] || !kinds["feed"] || !kinds["handoff"] {
		t.Fatal("waiting kinds")
	}
	home := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, home, 200)
	var hv struct {
		Waiting []watches.Waiting `json:"waiting"`
	}
	must(t, json.Unmarshal(home.Body.Bytes(), &hv))
	if encode(v.Waiting) != encode(hv.Waiting) {
		t.Fatal("Home waiting drift")
	}
	for _, id := range []string{w.ID, f.ID, handoff.ID} {
		must(t, s.Status(context.Background(), id, triggers.Paused))
	}
	response = r.request(t, "GET", "/v1/waiting", r.api.ControlToken, nil, nil)
	if response.Body.String() != "{\"waiting\":[]}\n" {
		t.Fatal("paused waiting not empty array")
	}
}

// A response recorder must not be read concurrently with the stream writer.
func TestW1SSEAuthHeartbeatAndItems(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	standingFeed(t, r)
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {"invalid", 401}, {r.api.ControlToken, 403}, {r.api.StatusToken, 403}} {
		status(t, r.request(t, "GET", "/v1/updates/stream", tc.token, nil, nil), tc.code)
	}
	incoming(t, r, s, "stream-one", chatA, "hello stream")
	c.advance(time.Second)
	must(t, s.Engine.FlushDue(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 15300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/updates/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+r.token)
	recorder := httptest.NewRecorder()
	r.api.Handler().ServeHTTP(recorder, req)
	body := recorder.Body.String()
	if recorder.Code != 200 || recorder.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, ": heartbeat\n\n") || !strings.Contains(body, "event: update\n") || !strings.Contains(body, "stream-one") || !strings.Contains(body, "id: ") {
		t.Fatal("stream/heartbeat contract")
	}
	if strings.Contains(body, r.token) {
		t.Fatal("credential leaked")
	}
	// Reconnect resumes after the final emitted page cursor.
	var cursor string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "id: ") {
			cursor = strings.TrimPrefix(line, "id: ")
		}
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel2()
	req = httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/updates/stream", nil).WithContext(ctx2)
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Last-Event-ID", cursor)
	recorder = httptest.NewRecorder()
	r.api.Handler().ServeHTTP(recorder, req)
	if strings.Contains(recorder.Body.String(), "stream-one") {
		t.Fatal("stream reconnect replayed completed page")
	}
	// Rotation invalidates an open stream on its next poll.
	ctx3, cancel3 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel3()
	req = httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/updates/stream", nil).WithContext(ctx3)
	req.Header.Set("Authorization", "Bearer "+r.token)
	done := make(chan struct{})
	go func() { r.api.Handler().ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	time.Sleep(150 * time.Millisecond)
	_, err := r.h.Rotate(r.a.ID)
	must(t, err)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rotation did not close stream")
	}
}

func TestW1UpdatesRunCapabilityCannotBroadenScope(t *testing.T) {
	r := newRig(t)
	s, c, requests := setupWatches(t, r)
	createHandoff(t, r, &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"})
	req := wake(t, s, c, requests)
	ctx := runContext(t, r, req)
	if _, err := s.GetUpdates(ctx, r.a.ID, "", 0, 100); err != model.ErrForbidden {
		t.Fatal("run capability read general updates")
	}
	// Waiting is read-only and must not change the AI's last-seen timestamp.
	a, err := r.h.Agent(r.a.ID)
	must(t, err)
	before := encode(a.LastSeenAt)
	r.h.Now = func() time.Time { return epoch.Add(time.Hour) }
	_, err = s.Waiting(context.Background())
	must(t, err)
	a, err = r.h.Agent(r.a.ID)
	must(t, err)
	if encode(a.LastSeenAt) != before {
		t.Fatal("widget read made agent appear active")
	}
}

func TestW1MigrationPreservesHistoryAndCursorRestart(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	h := createHandoff(t, r, nil)
	m := firstMessage(t, r, chatA)
	must(t, s.Store.PutHit(context.Background(), triggers.Hit{ID: "migration-hit", WatchID: h.ID, At: epoch, Action: triggers.WakeSession, State: "completed", Messages: []triggers.IncomingMessage{{ID: m.ID, ChatJID: chatA, Sender: chatA, Text: m.Content, At: epoch}}}))
	must(t, s.Store.PutRun(context.Background(), triggers.RunRecord{ID: "migration-run", HandoffID: h.ID, HitID: "migration-hit", StartedAt: epoch, FinishedAt: epoch.Add(time.Second), State: "completed"}))
	given, _, err := r.h.CreateAgent("Given AI", "custom", "local")
	must(t, err)
	// Reproduce the pre-W1 agent payload and absence of the W1 event journal.
	_, err = r.h.Repo.DB.Exec(`DROP TRIGGER w1_hit; DROP TRIGGER w1_send_insert; DROP TRIGGER w1_send_update; DROP TABLE agent_updates; DROP INDEX w1_one_feed; DELETE FROM objects WHERE bucket='updates'; DELETE FROM hub_schema WHERE version=6; UPDATE objects SET data=json_remove(data,'$.permissions.signature_mode') WHERE bucket='agents'; UPDATE objects SET data=json_set(data,'$.preset','given') WHERE bucket='agents' AND id=?`, given.ID)
	must(t, err)
	path := r.home + "/hub.db"
	must(t, r.h.Repo.Close())
	reopened, err := hub.Open(path)
	must(t, err)
	t.Cleanup(func() { reopened.Close() })
	r.h.Repo = reopened
	s.Store.DB = reopened.DB
	own, err := r.h.Agent(r.a.ID)
	must(t, err)
	g, err := r.h.Agent(given.ID)
	must(t, err)
	if own.Preset != "own" || g.Preset != "given" {
		t.Fatal("legacy signature migration")
	}
	runs, err := s.Store.Runs(context.Background(), h.ID)
	must(t, err)
	if len(runs) != 1 || runs[0].ID != "migration-run" {
		t.Fatal("migration lost handoff run")
	}
	rows, err := reopened.DB.Query("PRAGMA foreign_key_check")
	must(t, err)
	broken := rows.Next()
	rows.Close()
	if broken {
		t.Fatal("migration broke references")
	}
	var enabled int
	must(t, reopened.DB.QueryRow("PRAGMA foreign_keys").Scan(&enabled))
	if enabled != 1 {
		t.Fatal("migration left foreign keys off")
	}
	first := updates(t, r, map[string]any{})
	if len(first.Items) != 1 {
		t.Fatal("migration did not backfill hit history")
	}
	must(t, reopened.Close())
	finalRepo, err := hub.Open(path)
	must(t, err)
	t.Cleanup(func() { finalRepo.Close() })
	r.h.Repo = finalRepo
	s.Store.DB = finalRepo.DB
	if len(updates(t, r, map[string]any{"cursor": first.Cursor}).Items) != 0 {
		t.Fatal("cursor did not survive restart")
	}
	// The upgraded CHECK admits feed hits and its references still cascade.
	feed := standingFeed(t, r)
	must(t, s.Store.PutHit(context.Background(), triggers.Hit{ID: "feed-after-upgrade", WatchID: feed.ID, At: epoch, Action: triggers.Feed, State: "recorded", Messages: []triggers.IncomingMessage{{ID: m.ID, ChatJID: chatA, Sender: chatA, At: epoch}}}))
	must(t, s.Store.Delete(context.Background(), h.ID))
	var count int
	must(t, finalRepo.DB.QueryRow("SELECT count(*) FROM trigger_runs WHERE id='migration-run'").Scan(&count))
	if count != 0 {
		t.Fatal("foreign key cascade changed")
	}
}
