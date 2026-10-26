package triggers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var testCtx = context.Background()

type fakeClock struct {
	mu    sync.Mutex
	at    time.Time
	ticks chan time.Time
}

func newClock() *fakeClock {
	return &fakeClock{at: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC), ticks: make(chan time.Time, 10)}
}
func (c *fakeClock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *fakeClock) After(time.Duration) <-chan time.Time { return c.ticks }
func (c *fakeClock) advance(d time.Duration)              { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type fakeNotifier struct {
	mu     sync.Mutex
	events []Notification
	err    error
}

func (n *fakeNotifier) Notify(_ context.Context, event Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
	return n.err
}
func (n *fakeNotifier) count(kind string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	v := 0
	for _, e := range n.events {
		if e.Kind == kind {
			v++
		}
	}
	return v
}

type fakeSuggester struct {
	mu   sync.Mutex
	hits []Hit
}

func (s *fakeSuggester) Suggest(_ context.Context, _ Record, h Hit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits = append(s.hits, h)
	return nil
}

type fakeRunner struct {
	started  chan RunRequest
	release  chan struct{}
	finished chan struct{}
	result   RunResult
	err      error
}

func newRunner() *fakeRunner {
	return &fakeRunner{started: make(chan RunRequest, 10), release: make(chan struct{}, 10), finished: make(chan struct{}, 10)}
}
func (r *fakeRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	r.started <- req
	defer func() { r.finished <- struct{}{} }()
	select {
	case <-ctx.Done():
		return RunResult{ExitCode: -1}, ctx.Err()
	case <-r.release:
		return r.result, r.err
	}
}

type fixture struct {
	e  *Engine
	s  *MemoryStore
	c  *fakeClock
	n  *fakeNotifier
	sg *fakeSuggester
	r  *fakeRunner
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{s: NewMemoryStore(), c: newClock(), n: &fakeNotifier{}, sg: &fakeSuggester{}, r: newRunner()}
	var err error
	f.e, err = NewEngine(Options{Store: f.s, Clock: f.c, Notifier: f.n, Suggester: f.sg, Runner: f.r, OwnerName: func() string { return "Alex Rivera" }})
	must(t, err)
	t.Cleanup(func() { must(t, f.e.Close()) })
	return f
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func watch() Watch {
	return Watch{ID: "watch", Owner: "owner", Goal: "Watch Design Club", Scope: Scope{Chats: []string{"design-club"}}, Match: Match{Kind: Any}, Action: Notify, CreatedVia: Provenance{Agent: "agent", SessionID: "session-1", GrantText: "Watch Design Club for the demo"}}
}
func handoff() Handoff {
	w := watch()
	w.ID = "handoff"
	return Handoff{Watch: w, HandoffConfig: HandoffConfig{Runner: RunnerConfig{Kind: "codex", SessionID: "session-1", Workdir: "/tmp/project"}, SendGrant: SendGrant{ChatJID: "design-club"}, Limits: Limits{QuietHours: &QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"}}}}
}
func msg(text string) IncomingMessage {
	return IncomingMessage{ID: "message", ChatJID: "design-club", Sender: "maya", SenderPersonID: "maya", Text: text}
}
func (f *fixture) flush(t *testing.T, d time.Duration) {
	t.Helper()
	f.c.advance(d)
	must(t, f.e.FlushDue(testCtx))
}
func start(t *testing.T, r *fakeRunner) RunRequest {
	t.Helper()
	select {
	case req := <-r.started:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
		return RunRequest{}
	}
}
func waitIdle(t *testing.T, f *fixture, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.e.mu.Lock()
		idle := f.e.running[id] == nil
		f.e.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("runner did not finish")
}

func TestDebounceBatching(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("first")))
	f.flush(t, 40*time.Second)
	must(t, f.e.OnMessage(testCtx, msg("second")))
	f.flush(t, 59*time.Second)
	if f.n.count("hit") != 0 {
		t.Fatal("dispatched before quiet window")
	}
	f.flush(t, time.Second)
	hits, err := f.s.Hits(testCtx, "watch", 0)
	must(t, err)
	if len(hits) != 1 || len(hits[0].Messages) != 2 || f.n.count("hit") != 1 {
		t.Fatalf("bad batched hit: %+v", hits)
	}
}
func TestDebounceMaximumWindow(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	for i := 0; i < 6; i++ {
		must(t, f.e.OnMessage(testCtx, msg("burst")))
		f.flush(t, 50*time.Second)
	}
	hits, err := f.s.Hits(testCtx, "watch", 0)
	must(t, err)
	if len(hits) != 1 || len(hits[0].Messages) != 6 {
		t.Fatalf("five minute cap: %+v", hits)
	}
}
func TestLoopPrevention(t *testing.T) {
	f := setup(t)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	for _, m := range []IncomingMessage{func() IncomingMessage { m := msg("stop"); m.FromMe = true; return m }(), func() IncomingMessage { m := msg("stop"); m.FromLimmr = true; return m }()} {
		must(t, f.e.OnMessage(testCtx, m))
	}
	f.flush(t, time.Minute)
	r, err := f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Active {
		t.Fatal("self-sent stop must be ignored")
	}
	hits, err := f.s.Hits(testCtx, "handoff", 0)
	must(t, err)
	if len(hits) != 0 {
		t.Fatal("self-sent message was queued")
	}
}
func TestQueuedFollowupAndInstructions(t *testing.T) {
	f := setup(t)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("first change")))
	f.flush(t, time.Minute)
	req := start(t, f.r)
	if !strings.Contains(req.Prompt, "first run") || !strings.Contains(req.Prompt, "maya") {
		t.Fatal("missing introduction or sender")
	}
	must(t, f.e.OnMessage(testCtx, msg("second change")))
	f.flush(t, time.Minute)
	select {
	case <-f.r.started:
		t.Fatal("overlapping run")
	default:
	}
	must(t, f.e.AppendInstruction(testCtx, "handoff", "Keep the navigation simple"))
	f.r.release <- struct{}{}
	waitIdle(t, f, "handoff")
	f.flush(t, 0)
	req = start(t, f.r)
	if strings.Contains(req.Prompt, "This is the first run") || !strings.Contains(req.Prompt, "Keep the navigation simple") || !strings.Contains(req.Prompt, "second change") {
		t.Fatal("followup omitted latest instruction/message or repeated introduction")
	}
	f.r.release <- struct{}{}
	waitIdle(t, f, "handoff")
	hits, err := f.s.Hits(testCtx, "handoff", 0)
	must(t, err)
	if len(hits) != 2 {
		t.Fatal("expected two batches")
	}
}
func TestQuietHoursParkButWake(t *testing.T) {
	f := setup(t)
	f.c.advance(12 * time.Hour)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	d, err := f.e.SendPolicy(testCtx, "handoff", f.c.Now())
	must(t, err)
	if d.Mode != "park" || d.Reason != "quiet_hours" {
		t.Fatalf("%+v", d)
	}
	must(t, f.e.OnMessage(testCtx, msg("please update")))
	f.flush(t, time.Minute)
	start(t, f.r)
	f.r.release <- struct{}{}
	waitIdle(t, f, "handoff")
}
func TestReplyBudgetAndGrant(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.Limits.MaxRepliesPerHour = 2
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	for _, input := range [][2]string{{"elsewhere", "as_agent"}} {
		d, err := f.e.ReserveSend(testCtx, "handoff", input[0], input[1], f.c.Now())
		must(t, err)
		if d.Mode != "deny" {
			t.Fatal("grant widened")
		}
	}
	for i := 0; i < 3; i++ {
		d, err := f.e.ReserveSend(testCtx, "handoff", "design-club", "as_agent", f.c.Now())
		must(t, err)
		if i < 2 && d.Mode != "allow" || i == 2 && d.Mode != "park" {
			t.Fatalf("reservation %d: %+v", i, d)
		}
	}
	f.c.advance(time.Hour)
	d, err := f.e.SendPolicy(testCtx, "handoff", f.c.Now())
	must(t, err)
	if d.Mode != "allow" {
		t.Fatal("rolling budget did not reset at boundary")
	}
}
func TestAtomicReplyBudget(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.Limits.MaxRepliesPerHour = 3
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	var wg sync.WaitGroup
	results := make(chan SendDecision, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := f.e.ReserveSend(testCtx, "handoff", "design-club", "as_agent", f.c.Now())
			if err != nil {
				t.Error(err)
			}
			results <- d
		}()
	}
	wg.Wait()
	close(results)
	allowed := 0
	for d := range results {
		if d.Mode == "allow" {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("allowed %d", allowed)
	}
}
func TestExpiry(t *testing.T) {
	f := setup(t)
	h := handoff()
	at := f.c.Now().Add(time.Minute)
	h.ExpiresAt = &at
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("pending")))
	f.flush(t, time.Minute)
	f.flush(t, time.Minute)
	r, err := f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Expired || f.n.count("expired") != 1 {
		t.Fatal("expiry must be recorded and notified once")
	}
	d, err := f.e.SendPolicy(testCtx, "handoff", at.Add(-time.Hour))
	must(t, err)
	if d.Mode != "deny" {
		t.Fatal("old timestamp evaded expiry")
	}
}
func TestExpiryCancelsRun(t *testing.T) {
	f := setup(t)
	h := handoff()
	at := f.c.Now().Add(2 * time.Minute)
	h.ExpiresAt = &at
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("change")))
	f.flush(t, time.Minute)
	start(t, f.r)
	f.flush(t, time.Minute)
	waitIdle(t, f, "handoff")
	runs, err := f.s.Runs(testCtx, "handoff")
	must(t, err)
	if len(runs) != 1 || runs[0].State != "cancelled" {
		t.Fatalf("%+v", runs)
	}
}
func TestMaxRuns(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.Limits.MaxRuns = 1
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("change")))
	f.flush(t, time.Minute)
	start(t, f.r)
	d, err := f.e.SendPolicy(testCtx, "handoff", f.c.Now())
	must(t, err)
	if d.Mode != "allow" {
		t.Fatal("last allowed run cannot reply")
	}
	must(t, f.e.OnMessage(testCtx, msg("next")))
	f.r.release <- struct{}{}
	waitIdle(t, f, "handoff")
	f.flush(t, time.Minute)
	r, err := f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Expired || f.n.count("expired") != 1 {
		t.Fatal("run budget not enforced")
	}
	runs, err := f.s.Runs(testCtx, "handoff")
	must(t, err)
	if len(runs) != 1 {
		t.Fatal("ran beyond max")
	}
}
func TestStopWordsPauseBeforeMatch(t *testing.T) {
	for _, text := range []string{"STOP!", "Please talk to Alex", "stóp"} {
		t.Run(text, func(t *testing.T) {
			f := setup(t)
			h := handoff()
			h.Match = Match{Kind: Keywords, Keywords: []string{"demo"}}
			_, err := f.e.StartHandoff(testCtx, h)
			must(t, err)
			must(t, f.e.OnMessage(testCtx, msg(text)))
			f.flush(t, time.Minute)
			r, err := f.s.Get(testCtx, "handoff")
			must(t, err)
			if r.Status != Paused || f.n.count("paused") != 1 {
				t.Fatal("stop must override match")
			}
		})
	}
}
func TestCustomStopWordsAndBoundaries(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.StopWords = []string{"pause please"}
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("unstoppable")))
	r, err := f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Active {
		t.Fatal("substring matched")
	}
	must(t, f.e.OnMessage(testCtx, msg("Pause please!")))
	r, err = f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Paused {
		t.Fatal("custom phrase ignored")
	}
}
func TestStopCancelsRunAndClearsQueue(t *testing.T) {
	f := setup(t)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("change")))
	f.flush(t, time.Minute)
	start(t, f.r)
	must(t, f.e.OnMessage(testCtx, msg("second")))
	must(t, f.e.OnMessage(testCtx, msg("stop")))
	waitIdle(t, f, "handoff")
	must(t, f.e.SetStatus(testCtx, "handoff", Active))
	f.flush(t, time.Minute)
	select {
	case <-f.r.started:
		t.Fatal("paused batch resurrected")
	default:
	}
}
func TestHandoffGrantNarrowsWake(t *testing.T) {
	f := setup(t)
	h := handoff()
	h.Scope = Scope{Circles: []string{"project"}}
	_, err := f.e.StartHandoff(testCtx, h)
	must(t, err)
	m := msg("stop")
	m.ChatJID = "other-chat"
	m.CircleIDs = []string{"project"}
	must(t, f.e.OnMessage(testCtx, m))
	f.flush(t, time.Minute)
	r, err := f.s.Get(testCtx, "handoff")
	must(t, err)
	if r.Status != Active {
		t.Fatal("stop from outside grant")
	}
	runs, err := f.s.Runs(testCtx, "handoff")
	must(t, err)
	if len(runs) != 0 {
		t.Fatal("woke for unrelated chat")
	}
}
func TestDigestRollingAndSuggestion(t *testing.T) {
	f := setup(t)
	w := watch()
	w.Action = Digest
	_, err := f.e.CreateWatch(testCtx, w)
	must(t, err)
	for i := 0; i < 52; i++ {
		must(t, f.e.OnMessage(testCtx, msg("news")))
		f.flush(t, time.Minute)
	}
	hits, err := f.e.Digest(testCtx, "watch", 0)
	must(t, err)
	if len(hits) != 50 {
		t.Fatalf("digest length %d", len(hits))
	}
	if f.n.count("hit") != 0 {
		t.Fatal("digest should not notify every hit")
	}
	w.ID = "suggest"
	w.Action = ParkSuggestion
	_, err = f.e.CreateWatch(testCtx, w)
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("draft something")))
	f.flush(t, time.Minute)
	f.sg.mu.Lock()
	defer f.sg.mu.Unlock()
	if len(f.sg.hits) != 1 {
		t.Fatal("suggestion not parked")
	}
}
func TestAppendOnlyInstructionsAndPatch(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	must(t, f.e.AppendInstruction(testCtx, "watch", "First"))
	f.c.advance(time.Minute)
	must(t, f.e.AppendInstruction(testCtx, "watch", "Second"))
	goal := "Updated goal"
	must(t, f.e.UpdateWatch(testCtx, "watch", WatchPatch{Goal: &goal}))
	r, err := f.s.Get(testCtx, "watch")
	must(t, err)
	if len(r.Instructions) != 2 || r.Instructions[0].Text != "First" || !r.Instructions[1].At.After(r.Instructions[0].At) {
		t.Fatal("lost append-only history")
	}
	if r.CreatedVia.GrantText != watch().CreatedVia.GrantText {
		t.Fatal("changed provenance")
	}
}
func TestPauseResumeStopAndEscalate(t *testing.T) {
	f := setup(t)
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	must(t, f.e.Escalate(testCtx, "handoff"))
	d, err := f.e.SendPolicy(testCtx, "handoff", f.c.Now())
	must(t, err)
	if d.Mode != "deny" || f.n.count("paused") != 1 {
		t.Fatal("escalation did not pause")
	}
	must(t, f.e.SetStatus(testCtx, "handoff", Active))
	must(t, f.e.SetStatus(testCtx, "handoff", Stopped))
	if f.e.SetStatus(testCtx, "handoff", Active) == nil {
		t.Fatal("stopped handoff resumed")
	}
}
func TestRunFailureRecorded(t *testing.T) {
	f := setup(t)
	f.r.result = RunResult{ExitCode: 7, TimedOut: true, LogPath: "/tmp/private/run.log"}
	f.r.err = errors.New("fake failure")
	_, err := f.e.StartHandoff(testCtx, handoff())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("change")))
	f.flush(t, time.Minute)
	start(t, f.r)
	f.r.release <- struct{}{}
	waitIdle(t, f, "handoff")
	runs, err := f.s.Runs(testCtx, "handoff")
	must(t, err)
	if len(runs) != 1 || runs[0].ExitCode != 7 || !runs[0].TimedOut || runs[0].State != "failed" {
		t.Fatalf("%+v", runs)
	}
	must(t, f.e.Close())
	if f.n.count("run_failed") != 1 {
		t.Fatal("run failure not notified")
	}
}
func TestRunLoopAndShutdown(t *testing.T) {
	f := setup(t)
	_, err := f.e.CreateWatch(testCtx, watch())
	must(t, err)
	must(t, f.e.OnMessage(testCtx, msg("hello")))
	f.c.advance(time.Minute)
	ctx, cancel := context.WithCancel(testCtx)
	done := make(chan error, 1)
	go func() { done <- f.e.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for f.n.count("hit") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.n.count("hit") != 1 {
		t.Fatal("scheduler did not flush")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	must(t, f.e.Close())
	if !errors.Is(f.e.OnMessage(testCtx, msg("late")), ErrClosed) {
		t.Fatal("closed engine accepted message")
	}
}
