package triggers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Options struct {
	MayRunAutomations func(context.Context) bool
	Store             Store
	Notifier          Notifier
	Suggester         Suggester
	Runner            Runner
	Clock             Clock
	AllowMessage      func(context.Context, Record, IncomingMessage) (bool, error)
	OwnerName         func() string
}
type batch struct {
	messages    []IncomingMessage
	first, last time.Time
}
type Engine struct {
	MayRunAutomations func(context.Context) bool
	mu                sync.Mutex
	store             Store
	notifier          Notifier
	suggester         Suggester
	runner            Runner
	clock             Clock
	pending           map[string]*batch
	running           map[string]context.CancelFunc
	ctx               context.Context
	cancel            context.CancelFunc
	workers           sync.WaitGroup
	closed            bool
	asyncErr          error
	allowMessage      func(context.Context, Record, IncomingMessage) (bool, error)
	ownerName         func() string
}

func NewEngine(o Options) (*Engine, error) {
	if o.Store == nil {
		return nil, errors.New("trigger store is required")
	}
	if o.Clock == nil {
		o.Clock = RealClock{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{MayRunAutomations: o.MayRunAutomations, store: o.Store, allowMessage: o.AllowMessage, ownerName: o.OwnerName, notifier: o.Notifier, suggester: o.Suggester, runner: o.Runner, clock: o.Clock, pending: map[string]*batch{}, running: map[string]context.CancelFunc{}, ctx: ctx, cancel: cancel}, nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("trigger ID entropy unavailable")
	}
	return hex.EncodeToString(b[:])
}
func validate(r Record) error {
	w := r.Watch
	if !validSession(w.ID) {
		return errors.New("invalid trigger ID")
	}
	if strings.TrimSpace(w.Owner) == "" || strings.TrimSpace(w.Goal) == "" {
		return errors.New("owner and goal are required")
	}
	if w.Scope.All && (w.Action != Feed || len(w.Scope.Chats)+len(w.Scope.Circles)+len(w.Scope.People) != 0 || w.Match.Kind != Any) {
		return errors.New("feed requires only all scope and any match")
	}
	if w.Action == Feed && !w.Scope.All {
		return errors.New("feed requires all scope")
	}
	if !w.Scope.All && len(w.Scope.Chats)+len(w.Scope.Circles)+len(w.Scope.People) == 0 {
		return errors.New("explicit scope is required")
	}
	for _, ids := range [][]string{w.Scope.Chats, w.Scope.Circles, w.Scope.People} {
		for _, id := range ids {
			if strings.TrimSpace(id) == "" {
				return errors.New("empty scope selector")
			}
		}
	}
	if strings.TrimSpace(w.CreatedVia.Agent) == "" || strings.TrimSpace(w.CreatedVia.GrantText) == "" {
		return errors.New("explicit grant provenance is required")
	}
	if w.Status != Active && w.Status != Paused && w.Status != Stopped && w.Status != Expired {
		return errors.New("invalid trigger status")
	}
	if w.QuietWindow <= 0 || w.MaxWindow < w.QuietWindow || w.MaxWindow > 5*time.Minute {
		return errors.New("debounce must be positive and bounded by five minutes")
	}
	switch w.Match.Kind {
	case Any, MentionsMe:
	case Keywords:
		if len(w.Match.Keywords) == 0 {
			return errors.New("keywords required")
		}
		for _, k := range w.Match.Keywords {
			if strings.TrimSpace(fold(k)) == "" {
				return errors.New("empty keyword")
			}
		}
	case Regex:
		if w.Match.Regex == "" {
			return errors.New("regex required")
		}
		if _, err := regexp.Compile(w.Match.Regex); err != nil {
			return errors.New("invalid regex")
		}
	case FromPeople:
		if len(w.Match.People) == 0 {
			return errors.New("matching people required")
		}
		for _, p := range w.Match.People {
			if strings.TrimSpace(p) == "" {
				return errors.New("empty person selector")
			}
		}
	default:
		return errors.New("invalid match kind")
	}
	switch w.Action {
	case Feed, Notify, Digest, ParkSuggestion:
		if r.Handoff != nil {
			return errors.New("handoffs require wake_session")
		}
	case WakeSession:
		if r.Handoff == nil {
			return errors.New("wake_session requires a handoff")
		}
	default:
		return errors.New("invalid action")
	}
	if h := r.Handoff; h != nil {
		if h.Runner.Kind != "codex" && h.Runner.Kind != "claude_code" {
			return errors.New("invalid runner")
		}
		if !validSession(h.Runner.SessionID) || !filepath.IsAbs(h.Runner.Workdir) {
			return errors.New("valid session and absolute workdir required")
		}
		if strings.TrimSpace(h.SendGrant.ChatJID) == "" {
			return errors.New("handoff requires a chat grant")
		}
		if w.CreatedVia.SessionID != h.Runner.SessionID {
			return errors.New("grant must identify the resumed session")
		}
		if w.ExpiresAt == nil {
			return errors.New("handoff expiry required")
		}
		if h.Limits.MaxRepliesPerHour < 1 || h.Limits.MaxRuns < 1 {
			return errors.New("positive limits required")
		}
		for _, list := range [][]string{h.StopWords, h.Escalate} {
			for _, text := range list {
				if strings.TrimSpace(fold(text)) == "" {
					return errors.New("empty stop or escalation phrase")
				}
			}
		}
		if q := h.Limits.QuietHours; q != nil {
			if _, _, err := quietRange(*q); err != nil {
				return err
			}
		}
	}
	return nil
}
func validSession(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func (e *Engine) prepare(w Watch) Watch {
	now := e.clock.Now()
	if w.ID == "" {
		w.ID = newID()
	}
	w.Status = Active
	w.CreatedAt = now
	w.CreatedVia.At = now
	if w.QuietWindow == 0 {
		w.QuietWindow = time.Minute
	}
	if w.MaxWindow == 0 {
		w.MaxWindow = 5 * time.Minute
	}
	for i := range w.Instructions {
		w.Instructions[i].At = now
	}
	return w
}
func (e *Engine) CreateWatch(ctx context.Context, w Watch) (Record, error) {
	return e.create(ctx, Record{Watch: e.prepare(copyValue(w))})
}
func (e *Engine) StartHandoff(ctx context.Context, h Handoff) (Record, error) {
	h = copyValue(h)
	h.Watch = e.prepare(h.Watch)
	h.Action = WakeSession
	if h.ExpiresAt == nil {
		at := e.clock.Now().Add(72 * time.Hour)
		h.ExpiresAt = &at
	}
	if h.Limits.MaxRepliesPerHour == 0 {
		h.Limits.MaxRepliesPerHour = 12
	}
	if h.Limits.MaxRuns == 0 {
		h.Limits.MaxRuns = 50
	}
	if h.Limits.QuietHours == nil {
		h.Limits.QuietHours = &QuietHours{Start: "23:00", End: "08:00", Timezone: "Local"}
	}
	if len(h.StopWords) == 0 {
		h.StopWords = e.defaultStopWords()
	}
	if len(h.Escalate) == 0 {
		h.Escalate = []string{"money", "dates or commitments", "personal topics", "anything uncertain"}
	}
	return e.create(ctx, Record{Watch: h.Watch, Handoff: &h.HandoffConfig})
}

// defaultStopWords let a contact end a handoff by asking for the owner by first name.
func (e *Engine) defaultStopWords() []string {
	words := []string{"stop"}
	if e.ownerName != nil {
		if name := strings.Fields(e.ownerName()); len(name) > 0 {
			words = append(words, "talk to "+name[0])
		}
	}
	return words
}
func (e *Engine) create(ctx context.Context, r Record) (Record, error) {
	if err := validate(r); err != nil {
		return Record{}, err
	}
	if r.ExpiresAt != nil && !r.ExpiresAt.After(e.clock.Now()) {
		return Record{}, errors.New("expiry must be in the future")
	}
	if e.notifier == nil {
		return Record{}, errors.New("notifier required")
	}
	if r.Action == ParkSuggestion && e.suggester == nil {
		return Record{}, errors.New("suggester required")
	}
	if r.Action == WakeSession && (e.runner == nil || e.notifier == nil) {
		return Record{}, errors.New("runner and notifier required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return Record{}, ErrClosed
	}
	if err := e.store.Create(ctx, r); err != nil {
		return Record{}, err
	}
	return copyValue(r), nil
}

// WatchPatch cannot replace grant provenance, instructions, runner, or send grant.
// Permission changes to a handoff require a new explicit StartHandoff grant.
type WatchPatch struct {
	Goal      *string
	Scope     *Scope
	Match     *Match
	ExpiresAt *time.Time
}

func (e *Engine) UpdateWatch(ctx context.Context, id string, p WatchPatch) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	err := e.store.Update(ctx, id, func(r *Record) error {
		if p.Goal != nil {
			r.Goal = *p.Goal
		}
		if p.Scope != nil {
			r.Scope = copyValue(*p.Scope)
		}
		if p.Match != nil {
			r.Match = copyValue(*p.Match)
		}
		if p.ExpiresAt != nil {
			if !p.ExpiresAt.After(e.clock.Now()) {
				return errors.New("expiry must be in the future")
			}
			at := *p.ExpiresAt
			r.ExpiresAt = &at
		}
		return validate(*r)
	})
	if err == nil {
		delete(e.pending, id)
	}
	return err
}
func (e *Engine) AppendInstruction(ctx context.Context, id, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("instruction is empty")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return e.store.Update(ctx, id, func(r *Record) error {
		r.Instructions = append(r.Instructions, Instruction{Text: text, At: e.clock.Now()})
		return nil
	})
}
func (e *Engine) SetStatus(ctx context.Context, id string, status Status) error {
	if status != Active && status != Paused && status != Stopped {
		return errors.New("invalid requested status")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	err := e.store.Update(ctx, id, func(r *Record) error {
		if r.Status == Stopped || r.Status == Expired {
			return errors.New("terminal trigger cannot resume")
		}
		if status == Active && r.ExpiresAt != nil && !r.ExpiresAt.After(e.clock.Now()) {
			return errors.New("trigger has expired")
		}
		r.Status = status
		return nil
	})
	if err == nil && status != Active {
		delete(e.pending, id)
		if cancel := e.running[id]; cancel != nil {
			cancel()
		}
	}
	return err
}

// Watches returns the persistence snapshot for Home. Digest returns the most
// recent 50 batches by default; full hit history remains available in Store.
func (e *Engine) Watches(ctx context.Context) ([]Record, error) { return e.store.List(ctx) }
func (e *Engine) Digest(ctx context.Context, id string, limit int) ([]Hit, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	return e.store.Hits(ctx, id, limit)
}
func (e *Engine) notify(ctx context.Context, ns []Notification) error {
	var err error
	if e.notifier != nil {
		for _, n := range ns {
			err = errors.Join(err, e.notifier.Notify(ctx, n))
		}
	}
	return err
}
func (e *Engine) expireLocked(ctx context.Context, r Record, now time.Time) (bool, *Notification, error) {
	if r.Status == Expired || r.Status == Stopped {
		return true, nil, nil
	}
	expired := r.ExpiresAt != nil && !r.ExpiresAt.After(now)
	if !expired && r.Handoff != nil && e.running[r.ID] == nil {
		runs, err := e.store.Runs(ctx, r.ID)
		if err != nil {
			return false, nil, err
		}
		expired = len(runs) >= r.Handoff.Limits.MaxRuns
	}
	if !expired {
		return false, nil, nil
	}
	if err := e.store.Update(ctx, r.ID, func(v *Record) error { v.Status = Expired; return nil }); err != nil {
		return false, nil, err
	}
	delete(e.pending, r.ID)
	if cancel := e.running[r.ID]; cancel != nil {
		cancel()
	}
	return true, &Notification{WatchID: r.ID, Kind: "expired"}, nil
}
func (e *Engine) OnMessage(ctx context.Context, m IncomingMessage) error {
	if e.MayRunAutomations != nil && !e.MayRunAutomations(ctx) {
		return ctx.Err()
	}
	if m.FromMe || m.FromLimmr {
		return nil
	}
	if m.ChatJID == "" {
		return errors.New("message chat required")
	}
	m = copyValue(m)
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	now := e.clock.Now()
	if m.At.IsZero() {
		m.At = now
	}
	records, err := e.store.List(ctx)
	var ns []Notification
	if err == nil {
		for _, r := range records {
			done, n, x := e.expireLocked(ctx, r, now)
			if n != nil {
				ns = append(ns, *n)
			}
			if x != nil {
				err = errors.Join(err, x)
				continue
			}
			if done || r.Status != Active {
				continue
			}
			// The send grant also narrows wake scope, even if the watch spans a circle.
			if r.Handoff != nil && r.Handoff.SendGrant.ChatJID != m.ChatJID {
				continue
			}
			if e.allowMessage != nil {
				ok, x := e.allowMessage(ctx, r, m)
				if x != nil {
					err = errors.Join(err, x)
					continue
				}
				if !ok {
					continue
				}
			}
			if r.Handoff != nil && inScope(r.Scope, m) {
				stop := false
				for _, s := range r.Handoff.StopWords {
					if containsPhrase(m.Text, s) {
						stop = true
						break
					}
				}
				if stop {
					x = e.store.Update(ctx, r.ID, func(v *Record) error { v.Status = Paused; return nil })
					err = errors.Join(err, x)
					if x == nil {
						delete(e.pending, r.ID)
						if cancel := e.running[r.ID]; cancel != nil {
							cancel()
						}
						ns = append(ns, Notification{WatchID: r.ID, Kind: "paused"})
					}
					continue
				}
			}
			if !Matches(r.Watch, m) {
				continue
			}
			b := e.pending[r.ID]
			if b == nil {
				b = &batch{first: now}
				e.pending[r.ID] = b
			}
			b.last = now
			b.messages = append(b.messages, m)
		}
	}
	e.mu.Unlock()
	return errors.Join(err, e.notify(ctx, ns))
}

// Run services debounce and expiry at one-second resolution. Use a single Run
// loop per Engine. FlushDue exposes the same deterministic step to fake clocks.
func (e *Engine) Run(ctx context.Context) (runErr error) {
	defer func() { runErr = errors.Join(runErr, e.Close()) }()
	for {
		if err := e.FlushDue(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.ctx.Done():
			return ErrClosed
		case <-e.clock.After(time.Second):
		}
	}
}
func (e *Engine) FlushDue(ctx context.Context) error {
	if e.MayRunAutomations != nil && !e.MayRunAutomations(ctx) {
		return ctx.Err()
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	now := e.clock.Now()
	records, err := e.store.List(ctx)
	var ns []Notification
	type effect struct {
		r Record
		h Hit
	}
	var effects []effect
	if err == nil {
		for _, r := range records {
			done, n, x := e.expireLocked(ctx, r, now)
			if n != nil {
				ns = append(ns, *n)
			}
			if x != nil {
				err = errors.Join(err, x)
				continue
			}
			if done || r.Status != Active {
				continue
			}
			b := e.pending[r.ID]
			if b == nil || e.running[r.ID] != nil {
				continue
			}
			if now.Before(b.last.Add(r.QuietWindow)) && now.Before(b.first.Add(r.MaxWindow)) {
				continue
			}
			if e.allowMessage != nil {
				kept := b.messages[:0]
				for _, m := range b.messages {
					ok, x := e.allowMessage(ctx, r, m)
					if x != nil {
						err = errors.Join(err, x)
					}
					if x == nil && ok {
						kept = append(kept, m)
					}
				}
				b.messages = kept
				if len(kept) == 0 {
					delete(e.pending, r.ID)
					continue
				}
			}
			hit := Hit{ID: newID(), WatchID: r.ID, At: now, Messages: copyValue(b.messages), Action: r.Action, State: "recorded"}
			if x = e.store.PutHit(ctx, hit); x != nil {
				err = errors.Join(err, x)
				continue
			}
			if r.Handoff != nil {
				runs, x := e.store.Runs(ctx, r.ID)
				if x != nil {
					err = errors.Join(err, x)
					continue
				}
				run := RunRecord{ID: newID(), HandoffID: r.ID, HitID: hit.ID, StartedAt: now, State: "running"}
				if x = e.store.PutRun(ctx, run); x != nil {
					err = errors.Join(err, x)
					continue
				}
				runctx, cancel := context.WithCancel(e.ctx)
				e.running[r.ID] = cancel
				delete(e.pending, r.ID)
				e.workers.Add(1)
				req := RunRequest{ID: run.ID, HandoffID: r.ID, Runner: r.Handoff.Runner, Prompt: BuildPrompt(r, hit, len(runs) == 0)}
				go e.execute(runctx, cancel, req, run, hit)
			} else {
				delete(e.pending, r.ID)
				effects = append(effects, effect{r: r, h: hit})
			}
		}
	}
	e.mu.Unlock()
	err = errors.Join(err, e.notify(ctx, ns))
	for _, v := range effects {
		var x error
		switch v.r.Action {
		case Notify:
			if e.notifier == nil {
				x = errors.New("notifier unavailable")
			} else {
				x = e.notifier.Notify(ctx, Notification{WatchID: v.r.ID, Kind: "hit", Hit: &v.h})
			}
		case ParkSuggestion:
			if e.suggester == nil {
				x = errors.New("suggester unavailable")
			} else {
				x = e.suggester.Suggest(ctx, v.r, v.h)
			}
		}
		v.h.State = "completed"
		if x != nil {
			v.h.State = "failed"
		}
		err = errors.Join(err, x, e.store.PutHit(ctx, v.h))
	}
	return err
}
func (e *Engine) execute(ctx context.Context, cancel context.CancelFunc, req RunRequest, run RunRecord, hit Hit) {
	defer e.workers.Done()
	defer cancel()
	var result RunResult
	var runErr error
	if e.MayRunAutomations != nil && !e.MayRunAutomations(ctx) {
		cancel()
		runErr = context.Canceled
	} else {
		result, runErr = e.runner.Run(ctx, req)
	}
	run.FinishedAt = e.clock.Now()
	run.ExitCode = result.ExitCode
	run.TimedOut = result.TimedOut
	run.LogPath = result.LogPath
	run.State = "completed"
	hit.State = "completed"
	if runErr != nil || result.ExitCode != 0 || result.TimedOut {
		run.State = "failed"
		hit.State = "failed"
	}
	if ctx.Err() != nil {
		run.State = "cancelled"
		hit.State = "cancelled"
	}
	e.mu.Lock()
	delete(e.running, req.HandoffID)
	// Cancellation of a session must not cancel recording its final outcome.
	persist := context.Background()
	err := errors.Join(e.store.PutRun(persist, run), e.store.PutHit(persist, hit))
	var ns []Notification
	if run.State == "failed" {
		ns = append(ns, Notification{WatchID: req.HandoffID, Kind: "run_failed"})
	}
	r, x := e.store.Get(persist, req.HandoffID)
	err = errors.Join(err, x)
	if x == nil {
		_, n, x := e.expireLocked(persist, r, e.clock.Now())
		err = errors.Join(err, x)
		if n != nil {
			ns = append(ns, *n)
		}
	}
	e.asyncErr = errors.Join(e.asyncErr, err)
	e.mu.Unlock()
	if x = e.notify(persist, ns); x != nil {
		e.mu.Lock()
		e.asyncErr = errors.Join(e.asyncErr, x)
		e.mu.Unlock()
	}
}

// Err reports asynchronous persistence/notification failures without printing any
// process output. Run exits on synchronous errors so its owner can report health.
func (e *Engine) Err() error { e.mu.Lock(); defer e.mu.Unlock(); return e.asyncErr }
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	e.mu.Unlock()
	e.workers.Wait()
	return e.Err()
}

func quietRange(q QuietHours) (int, int, error) {
	start, x := time.Parse("15:04", q.Start)
	end, y := time.Parse("15:04", q.End)
	_, z := time.LoadLocation(q.Timezone)
	if x != nil || y != nil || z != nil {
		return 0, 0, errors.New("invalid quiet hours or timezone")
	}
	return start.Hour()*60 + start.Minute(), end.Hour()*60 + end.Minute(), nil
}
func inQuiet(q QuietHours, now time.Time) (bool, error) {
	s, end, err := quietRange(q)
	if err != nil {
		return false, err
	}
	loc, _ := time.LoadLocation(q.Timezone)
	at := now.In(loc)
	n := at.Hour()*60 + at.Minute()
	if s == end {
		return true, nil
	}
	if s < end {
		return n >= s && n < end, nil
	}
	return n >= s || n < end, nil
}

type SendDecision struct {
	Mode    string `json:"mode"`
	Reason  string `json:"reason"`
	ChatJID string `json:"chat_jid,omitempty"`
}

// SendPolicy is advisory. ReserveSend checks the grant and atomically consumes a
// reply slot. Neither bypasses circle rules, identity, or base agent permissions.
func (e *Engine) SendPolicy(ctx context.Context, id string, now time.Time) (SendDecision, error) {
	e.mu.Lock()
	d, ns, err := e.sendPolicyLocked(ctx, id, now)
	e.mu.Unlock()
	return d, errors.Join(err, e.notify(ctx, ns))
}
func (e *Engine) sendPolicyLocked(ctx context.Context, id string, now time.Time) (SendDecision, []Notification, error) {
	deny := SendDecision{Mode: "deny", Reason: "inactive"}
	if e.closed {
		return deny, nil, ErrClosed
	}
	r, err := e.store.Get(ctx, id)
	if err != nil {
		return deny, nil, err
	}
	if r.Handoff == nil {
		return SendDecision{Mode: "deny", Reason: "not_handoff"}, nil, nil
	}
	// Use the trusted clock for enforcement, including quiet hours and budgets.
	// The supplied timestamp is only a caller scheduling hint.
	now = e.clock.Now()
	done, n, err := e.expireLocked(ctx, r, now)
	var ns []Notification
	if n != nil {
		ns = append(ns, *n)
	}
	if err != nil || done || r.Status != Active {
		return deny, ns, err
	}
	h := r.Handoff
	d := SendDecision{Mode: "allow", Reason: "grant", ChatJID: h.SendGrant.ChatJID}
	if q := h.Limits.QuietHours; q != nil {
		quiet, x := inQuiet(*q, now)
		if x != nil {
			return deny, ns, x
		}
		if quiet {
			d.Mode = "park"
			d.Reason = "quiet_hours"
			return d, ns, nil
		}
	}
	nreply, err := e.store.ReplyCount(ctx, id, now)
	if err != nil {
		return deny, ns, err
	}
	if nreply >= h.Limits.MaxRepliesPerHour {
		d.Mode = "park"
		d.Reason = "reply_budget"
	}
	return d, ns, nil
}
func (e *Engine) ReserveSend(ctx context.Context, id, chat, _ string, now time.Time) (SendDecision, error) {
	e.mu.Lock()
	d, ns, err := e.sendPolicyLocked(ctx, id, now)
	if err == nil && d.Mode != "deny" && (chat != d.ChatJID) {
		d = SendDecision{Mode: "deny", Reason: "outside_grant"}
	}
	if err == nil && d.Mode == "allow" {
		r, x := e.store.Get(ctx, id)
		err = x
		if x == nil {
			now = e.clock.Now()
			ok, x := e.store.ReserveReply(ctx, id, now, r.Handoff.Limits.MaxRepliesPerHour)
			err = x
			if !ok {
				d.Mode = "park"
				d.Reason = "reply_budget"
			}
		}
	}
	e.mu.Unlock()
	if err != nil {
		d = SendDecision{Mode: "deny", Reason: "store_error"}
	}
	return d, errors.Join(err, e.notify(ctx, ns))
}

// Escalate pauses a handoff after the AI/MCP layer flags an escalation. Textual
// topic classification stays in the prompt; stop phrases are enforced locally.
func (e *Engine) Escalate(ctx context.Context, id string) error {
	if err := e.SetStatus(ctx, id, Paused); err != nil {
		return err
	}
	return e.notify(ctx, []Notification{{WatchID: id, Kind: "paused"}})
}
