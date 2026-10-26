package integration_test

import (
	"context"
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"limmr/internal/watches"
	"strings"
	"sync"
	"testing"
	"time"
)

type watchClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *watchClock) Now() time.Time                         { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *watchClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (c *watchClock) advance(d time.Duration)                { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type watchRunner struct{ requests chan triggers.RunRequest }

func (r watchRunner) Run(ctx context.Context, req triggers.RunRequest) (triggers.RunResult, error) {
	select {
	case r.requests <- req:
	case <-ctx.Done():
		return triggers.RunResult{ExitCode: -1}, ctx.Err()
	}
	<-ctx.Done()
	return triggers.RunResult{ExitCode: -1}, ctx.Err()
}
func setupWatches(t *testing.T, r *rig) (*watches.Service, *watchClock, chan triggers.RunRequest) {
	t.Helper()
	c := &watchClock{at: epoch}
	requests := make(chan triggers.RunRequest, 8)
	s, err := watches.New(r.h, r.p, watches.Options{Runner: watchRunner{requests}, Clock: c, MCPURL: "http://127.0.0.1:18800/mcp"})
	must(t, err)
	r.mc.Watches = s
	r.api.Health.WatchesCheck = s.Check
	t.Cleanup(func() { s.Engine.Close() })
	return s, c, requests
}
func createWatch(t *testing.T, r *rig, extra map[string]any) watches.Detail {
	t.Helper()
	args := map[string]any{"goal": "Tell me about exam news", "grant_text": "Tell me about exam news", "scope": map[string]any{"chats": []string{chatA}}, "match": map[string]any{"kind": "keywords", "keywords": []string{"exam"}}, "action": "notify"}
	for k, v := range extra {
		args[k] = v
	}
	out, err := r.call(t, "create_watch", args)
	must(t, err)
	return out.(watches.Detail)
}
func createHandoff(t *testing.T, r *rig, q *triggers.QuietHours) watches.Detail {
	t.Helper()
	r.a.Kind = "codex"
	must(t, r.h.Repo.Put("agents", r.a.ID, r.a))
	circleFixture(t, r, "project", "free", false, chatA)
	args := map[string]any{"goal": "Help Maya refine the website", "grant_text": "Help Maya refine the website", "scope": map[string]any{"chats": []string{chatA}}, "runner": map[string]any{"kind": "codex", "session_id": "session-42", "workdir": r.home}, "chat_jid": chatA}
	if q != nil {
		args["limits"] = map[string]any{"quiet_hours": q}
	}
	out, err := r.call(t, "start_handoff", args)
	must(t, err)
	d := out.(watches.Detail)
	if d.ExpiresAt == nil || d.ExpiresAt.Sub(epoch) != 72*time.Hour {
		t.Fatal("missing default handoff expiry")
	}
	return d
}
func wake(t *testing.T, s *watches.Service, c *watchClock, requests chan triggers.RunRequest) triggers.RunRequest {
	t.Helper()
	must(t, s.Handle(context.Background(), wa.StoredEvent{Message: model.Message{ID: "wake-one", ChatJID: chatA, Sender: chatA, Content: "Please adjust the website", Timestamp: epoch}}))
	c.advance(time.Minute)
	must(t, s.Engine.FlushDue(context.Background()))
	select {
	case req := <-requests:
		return req
	case <-time.After(3 * time.Second):
		t.Fatal("fake runner not woken")
		return triggers.RunRequest{}
	}
}
func runContext(t *testing.T, r *rig, req triggers.RunRequest) context.Context {
	t.Helper()
	a, id, err := r.h.AuthenticateRun(req.MCPToken)
	must(t, err)
	if a.ID != r.a.ID || id.RunID != req.ID || id.SessionID != "session-42" {
		t.Fatal("run credential not bound to grant")
	}
	return hub.WithHandoff(context.Background(), id)
}
func TestWatchIncomingHitRoutesAndActivity(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	d := createWatch(t, r, nil)
	ch, unsub := r.h.Subscribe()
	defer unsub()
	m := model.Message{ID: "exam-one", ChatJID: chatA, Sender: chatA, Content: "Exam date moved", Timestamp: epoch}
	must(t, r.m.PutMessage(m))
	must(t, s.Handle(context.Background(), wa.StoredEvent{Message: m}))
	c.advance(time.Minute)
	must(t, s.Engine.FlushDue(context.Background()))
	detail := r.request(t, "GET", "/v1/watches/"+d.ID, r.api.ControlToken, nil, nil)
	status(t, detail, 200)
	var hit watches.Detail
	must(t, json.Unmarshal(detail.Body.Bytes(), &hit))
	if hit.NewHits != 1 || hit.Latest == nil || hit.Latest.Sender != "Maya Rao" || hit.Latest.MessageID != "exam-one" {
		t.Fatal("watch contract did not expose persisted/enriched hit")
	}
	select {
	case act := <-ch:
		if act.Kind != "watch.hit" || act.Note != d.ID {
			t.Fatal("missing watch notification Activity")
		}
	case <-time.After(time.Second):
		t.Fatal("missing Activity event")
	}
	home := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, home, 200)
	var state map[string]json.RawMessage
	must(t, json.Unmarshal(home.Body.Bytes(), &state))
	if !strings.Contains(string(state["watches"]), d.ID) {
		t.Fatal("Home omitted watches")
	}
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "GET", "/v1/watches", token, nil, nil), 403)
	}
	instructions := r.request(t, "POST", "/v1/watches/"+d.ID+"/instructions", r.api.ControlToken, map[string]any{"text": "Ignore practice exams"}, nil)
	status(t, instructions, 200)
	for _, text := range []string{" ", strings.Repeat("x", 501)} {
		status(t, r.request(t, "POST", "/v1/watches/"+d.ID+"/instructions", r.api.ControlToken, map[string]string{"text": text}, nil), 400)
	}
	status(t, r.request(t, "POST", "/v1/watches/"+d.ID+"/seen", r.api.ControlToken, nil, nil), 204)
	read, err := s.Detail(context.Background(), d.ID)
	must(t, err)
	if read.NewHits != 0 || len(read.Instructions) != 1 {
		t.Fatal("seen or instructions did not persist")
	}
	for _, path := range []string{"pause", "resume", "stop"} {
		status(t, r.request(t, "POST", "/v1/watches/"+d.ID+"/"+path, r.api.ControlToken, nil, nil), 200)
	}
	status(t, r.request(t, "POST", "/v1/watches/"+d.ID+"/resume", r.api.ControlToken, nil, nil), 409)
	if strings.Contains(detail.Body.String(), "workdir") || strings.Contains(detail.Body.String(), "log_path") || strings.Contains(detail.Body.String(), "grant_text") {
		t.Fatal("private runtime fields reached watch card")
	}
}
func TestWatchLoopPreventionAndPermissionRevocation(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	d := createWatch(t, r, nil)
	for _, m := range []model.Message{{ID: "from-me", ChatJID: chatA, Sender: chatA, Content: "exam", IsFromMe: true}, {ID: "limmr-echo", ChatJID: chatA, Sender: chatA, Content: "exam"}} {
		if m.ID == "limmr-echo" {
			_, err := r.h.Repo.DB.Exec("INSERT INTO trigger_outbound VALUES(?,?,?,?,?)", chatA, m.ID, "", "", epoch.Format(time.RFC3339))
			must(t, err)
		}
		must(t, s.Handle(context.Background(), wa.StoredEvent{Message: m}))
	}
	c.advance(time.Minute)
	must(t, s.Engine.FlushDue(context.Background()))
	detail, err := s.Detail(context.Background(), d.ID)
	must(t, err)
	if detail.NewHits != 0 {
		t.Fatal("self send produced a hit")
	}
	must(t, s.Handle(context.Background(), wa.StoredEvent{Message: model.Message{ID: "queued-before-revoke", ChatJID: chatA, Sender: chatA, Content: "exam", Timestamp: epoch}}))
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
	r.agent(t, p)
	c.advance(time.Minute)
	must(t, s.Engine.FlushDue(context.Background()))
	detail, err = s.Detail(context.Background(), d.ID)
	must(t, err)
	if detail.NewHits != 0 {
		t.Fatal("queued message bypassed revoked permission")
	}
	_, err = r.call(t, "update_watch", map[string]any{"id": d.ID, "digest": "hidden"})
	if err == nil {
		t.Fatal("hidden scope was editable")
	}
}
func TestHandoffSendBoundToChatVoiceAgentAndSession(t *testing.T) {
	r := newRig(t)
	s, c, requests := setupWatches(t, r)
	d := createHandoff(t, r, &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"})
	req := wake(t, s, c, requests)
	ctx := runContext(t, r, req)
	for _, v := range []struct{ chat, voice string }{{chatB, "as_agent"}} {
		_, err := r.h.DraftMessage(ctx, r.a, v.chat, "Invented reply", v.voice, true)
		if err == nil {
			t.Fatal("handoff widened its grant")
		}
	}
	forged := hub.WithHandoff(context.Background(), hub.RunIdentity{HandoffID: d.ID, RunID: req.ID, AgentID: r.a.ID, SessionID: "other-session"})
	if _, err := r.h.DraftMessage(forged, r.a, chatA, "Invented reply", "as_agent", true); err == nil {
		t.Fatal("wrong session was accepted")
	}
	sent, err := r.h.DraftMessage(ctx, r.a, chatA, "The new layout is ready", "as_agent", true, hub.SendOptions{OwnerInstruction: "Send the new layout to Maya."})
	must(t, err)
	if sent.State != "sent" || len(r.fake.Sent) != 1 || sent.FinalText != "The new layout is ready" {
		t.Fatal("scoped signed fake delivery failed")
	}
	detail, err := s.Detail(context.Background(), d.ID)
	must(t, err)
	if detail.Stats.Replies != 1 {
		t.Fatal("actual replies were not counted")
	}
	pending, err := r.h.DraftMessage(ctx, r.a, chatA, "Second draft", "as_agent", false)
	must(t, err)
	must(t, s.Status(context.Background(), d.ID, triggers.Paused))
	if _, _, err := r.h.AuthenticateRun(req.MCPToken); err == nil {
		t.Fatal("paused run still authenticated")
	}
	if _, err := r.h.SendDraft(ctx, r.a, pending.ID); err == nil {
		t.Fatal("paused handoff dispatched a pending draft")
	}
	var stored model.Draft
	must(t, r.h.Repo.Get("drafts", pending.ID, &stored))
	if stored.State != "cancelled" {
		t.Fatal("pending draft was not cancelled")
	}
}
func TestHandoffQuietHoursAndBudgetParkSuggestions(t *testing.T) {
	for _, kind := range []string{"quiet", "budget", "circle-confirm", "circle-never"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			s, c, requests := setupWatches(t, r)
			q := &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"}
			if kind == "quiet" {
				q = &triggers.QuietHours{Start: "00:00", End: "00:00", Timezone: "UTC"}
			}
			d := createHandoff(t, r, q)
			req := wake(t, s, c, requests)
			ctx := runContext(t, r, req)
			if kind == "budget" {
				for i := 0; i < 12; i++ {
					decision, err := s.Engine.ReserveSend(ctx, d.ID, chatA, "as_agent", c.Now())
					must(t, err)
					if decision.Mode != "allow" {
						t.Fatal("unexpected early budget refusal")
					}
				}
			}
			if kind == "circle-confirm" || kind == "circle-never" {
				rule := "confirm"
				if kind == "circle-never" {
					rule = "never"
				}
				circleFixture(t, r, "university", rule, false, chatA)
			}
			draft, err := r.h.DraftMessage(ctx, r.a, chatA, "Drafted update", "as_agent", true)
			if kind == "circle-never" {
				if err == nil {
					t.Fatal("handoff bypassed Never circle")
				}
				return
			}
			must(t, err)
			if draft.State != "suggested" || len(r.fake.Sent) != 0 {
				t.Fatal("handoff policy did not park reply")
			}
			xs, err := r.h.Suggestions()
			must(t, err)
			if len(xs) != 1 || xs[0].CircleID == "" || xs[0].DraftID != draft.ID {
				t.Fatal("parked draft missing from circle Suggestions")
			}
			if kind != "circle-confirm" {
				_, err = r.h.SendSuggestion(context.Background(), xs[0].ID, nil)
				if err == nil {
					t.Fatal("Mac path bypassed watch park policy")
				}
			}
		})
	}
}
func TestWatchMCPExplicitGrantOwnershipAndDigest(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	for _, text := range []string{"", " \n "} {
		_, err := r.call(t, "create_watch", map[string]any{"goal": "News", "grant_text": text, "scope": map[string]any{"chats": []string{chatA}}, "match": map[string]any{"kind": "any"}, "action": "notify"})
		if err == nil {
			t.Fatal("empty grant accepted")
		}
	}
	d := createWatch(t, r, map[string]any{"action": "digest"})
	_, err := r.call(t, "update_watch", map[string]any{"id": d.ID, "instruction": "Only new dates", "digest": "Next exam is Tuesday", "status": "paused"})
	must(t, err)
	detail, err := s.Detail(context.Background(), d.ID)
	must(t, err)
	if detail.Digest != "Next exam is Tuesday" || len(detail.Instructions) != 1 || detail.Status != triggers.Paused {
		t.Fatal("MCP update was not persisted")
	}
	for _, tool := range r.mc.Tools(r.a) {
		if strings.Contains(tool.Name, "watch") || strings.Contains(tool.Name, "handoff") {
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
				t.Fatal("watch action omitted confirmation annotations")
			}
		}
	}
	other, _, err := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, err)
	_, err = r.mc.Call(context.Background(), other.ID, "stop_watch", map[string]any{"id": d.ID})
	if err == nil {
		t.Fatal("another agent stopped watch")
	}
	must(t, s.Status(context.Background(), d.ID, triggers.Stopped))
	xs, err := s.List(context.Background(), true)
	must(t, err)
	if len(xs) != 0 {
		t.Fatal("Home included stopped watch")
	}
}
func TestWatchQueueDoesNotWaitForConsumer(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 4096; i++ {
			s.Enqueue(wa.StoredEvent{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch queue blocked message storage")
	}
	if s.Check().State != "warn" {
		t.Fatal("overflow was not exposed in health")
	}
}

func TestRunCredentialHTTPIdentityAndReadScope(t *testing.T) {
	r := newRig(t)
	s, c, requests := setupWatches(t, r)
	d := createHandoff(t, r, &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"})
	req := wake(t, s, c, requests)
	for _, path := range []string{"/v1/home", "/v1/watches", "/v1/people", "/v1/status"} {
		status(t, r.request(t, "GET", path, req.MCPToken, nil, nil), 403)
	}
	rpc := func(name string, args any, denied bool) map[string]any {
		t.Helper()
		w := r.request(t, "POST", "/mcp", req.MCPToken, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, map[string]string{"Accept": "application/json, text/event-stream"})
		status(t, w, 200)
		var body struct {
			Result struct {
				IsError           bool `json:"isError"`
				StructuredContent struct {
					Data map[string]any `json:"data"`
				} `json:"structuredContent"`
			} `json:"result"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &body))
		if body.Result.IsError != denied {
			t.Fatal("run MCP action had unexpected permission result")
		}
		return body.Result.StructuredContent.Data
	}
	who := rpc("whoami", map[string]any{}, false)
	perms := who["permissions"].(map[string]any)
	chats := perms["chats"].(map[string]any)
	jids := chats["jids"].([]any)
	if len(jids) != 1 || jids[0] != chatA {
		t.Fatal("run read scope was not narrowed to grant")
	}
	rpc("draft_message", map[string]any{"chat": chatB, "text": "Out of scope", "voice": "as_agent", "send_now": true}, true)
	rpc("draft_message", map[string]any{"chat": chatA, "text": "Signed fixture update", "voice": "as_agent", "send_now": true}, false)
	rpc("get_chat", map[string]any{"chat_jid": chatB}, true)
	must(t, r.h.Revoke(r.a.ID))
	status(t, r.request(t, "POST", "/mcp", req.MCPToken, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{}}, nil), 401)
	must(t, s.Status(context.Background(), d.ID, triggers.Stopped))
}

func TestParkSuggestionUsesWatchCircleAndDigestDoesNotNotify(t *testing.T) {
	for _, action := range []string{"park_suggestion", "digest"} {
		t.Run(action, func(t *testing.T) {
			r := newRig(t)
			s, c, _ := setupWatches(t, r)
			circle := circleFixture(t, r, "project", "free", false, chatA)
			d := createWatch(t, r, map[string]any{"action": action, "scope": map[string]any{"circles": []string{circle.ID}}})
			ch, unsub := r.h.Subscribe()
			defer unsub()
			must(t, s.Handle(context.Background(), wa.StoredEvent{Message: model.Message{ID: "circle-exam", ChatJID: chatA, Sender: chatA, Content: "Exam dates changed", Timestamp: epoch}}))
			c.advance(time.Minute)
			must(t, s.Engine.FlushDue(context.Background()))
			detail, err := s.Detail(context.Background(), d.ID)
			must(t, err)
			if detail.NewHits != 1 {
				t.Fatal("circle enrichment did not match")
			}
			if action == "park_suggestion" {
				xs, err := r.h.Suggestions()
				must(t, err)
				if len(xs) != 1 || xs[0].CircleID != circle.ID {
					t.Fatal("suggestion did not use watch's circle")
				}
			}
			for len(ch) > 0 {
				act := <-ch
				if act.Kind == "watch.hit" {
					t.Fatal("digest/suggestion caused a notify hit")
				}
			}
		})
	}
}

func TestWatchRecentTerminalLimitAndHitWindow(t *testing.T) {
	r := newRig(t)
	s, c, _ := setupWatches(t, r)
	ctx := context.Background()
	d := createWatch(t, r, nil)
	messages := []triggers.IncomingMessage{}
	for i := 0; i < 25; i++ {
		messages = append(messages, triggers.IncomingMessage{ID: time.Duration(i).String(), ChatJID: chatA, Sender: chatA, SenderPersonID: "maya", Text: "Exam date", At: epoch.Add(time.Duration(i) * time.Second)})
	}
	must(t, s.Store.PutHit(ctx, triggers.Hit{ID: "large-batch", WatchID: d.ID, At: c.Now(), Action: triggers.Notify, State: "completed", Messages: messages}))
	detail, err := s.Detail(ctx, d.ID)
	must(t, err)
	if len(detail.Hits) != 20 || detail.NewHits != 25 || detail.Hits[0].MessageID != time.Duration(24).String() {
		t.Fatal("detail did not retain newest twenty messages")
	}
	must(t, s.Status(ctx, d.ID, triggers.Stopped))
	for i := 0; i < 6; i++ {
		c.advance(time.Minute)
		d = createWatch(t, r, nil)
		must(t, s.Status(ctx, d.ID, triggers.Stopped))
	}
	xs, err := s.List(ctx, false)
	must(t, err)
	if len(xs) != 5 || xs[0].ID != d.ID {
		t.Fatal("terminal list did not prefer five most recently ended watches")
	}
}

func TestWatchBufferedLifecycleProcessesStoredMessages(t *testing.T) {
	r := newRig(t)
	s, err := watches.New(r.h, r.p, watches.Options{Runner: watchRunner{requests: make(chan triggers.RunRequest, 1)}, MCPURL: "http://127.0.0.1:18800/mcp"})
	must(t, err)
	r.mc.Watches = s
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	d, err := s.Engine.CreateWatch(context.Background(), triggers.Watch{Owner: r.a.ID, Goal: "Exam dates", Scope: triggers.Scope{Chats: []string{chatA}}, Match: triggers.Match{Kind: triggers.Any}, Action: triggers.Notify, CreatedVia: triggers.Provenance{Agent: r.a.ID, GrantText: "Watch exam dates"}, QuietWindow: time.Millisecond, MaxWindow: time.Minute})
	must(t, err)
	m := model.Message{ID: "buffered-exam", ChatJID: chatA, Sender: chatA, Content: "Exam date is Tuesday", Timestamp: epoch}
	must(t, r.m.PutMessage(m))
	s.Enqueue(wa.StoredEvent{Message: m})
	deadline := time.Now().Add(4 * time.Second)
	for {
		detail, err := s.Detail(context.Background(), d.ID)
		must(t, err)
		if detail.NewHits == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("buffered lifecycle did not produce a hit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	status(t, r.request(t, "GET", "/v1/watches/"+d.ID, r.api.ControlToken, nil, nil), 200)
}

func TestWatchStartupRecoveryAppearsInHealthUntilExplicitResume(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	store := &triggers.SQLStore{DB: r.h.Repo.DB}
	expiry := epoch.Add(72 * time.Hour)
	w := triggers.Record{Watch: triggers.Watch{ID: "interrupted", Owner: r.a.ID, Goal: "Help refine the website", Scope: triggers.Scope{Chats: []string{chatA}}, Match: triggers.Match{Kind: triggers.Any}, Action: triggers.WakeSession, Status: triggers.Active, CreatedAt: epoch, ExpiresAt: &expiry, CreatedVia: triggers.Provenance{Agent: r.a.ID, SessionID: "session-42", GrantText: "Help refine the website", At: epoch}, QuietWindow: time.Minute, MaxWindow: 5 * time.Minute}, Handoff: &triggers.HandoffConfig{Runner: triggers.RunnerConfig{Kind: "codex", SessionID: "session-42", Workdir: r.home}, SendGrant: triggers.SendGrant{ChatJID: chatA}, Limits: triggers.Limits{MaxRuns: 50, MaxRepliesPerHour: 12}}}
	must(t, store.Create(ctx, w))
	must(t, store.PutHit(ctx, triggers.Hit{ID: "crash-hit", WatchID: w.ID, At: epoch, Action: triggers.WakeSession, State: "recorded"}))
	must(t, store.PutRun(ctx, triggers.RunRecord{ID: "crash-run", HandoffID: w.ID, HitID: "crash-hit", StartedAt: epoch, State: "running"}))
	s, _, requests := setupWatches(t, r)
	check := s.Check()
	if check.State != "warn" || !strings.Contains(check.Detail, "interrupted") {
		t.Fatal("recovery missing from watch health")
	}
	status(t, r.request(t, "GET", "/v1/health", r.api.ControlToken, nil, nil), 200)
	select {
	case <-requests:
		t.Fatal("startup replayed an interrupted session")
	default:
	}
	must(t, s.Status(ctx, w.ID, triggers.Active))
	if s.Check().State != "ok" {
		t.Fatal("explicit resume did not acknowledge recovery")
	}
}
