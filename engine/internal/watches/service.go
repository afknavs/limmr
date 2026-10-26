// Package watches adapts the trigger runtime to hub persistence, incoming events,
// Activity notifications, Suggestions and authenticated session runners.
package watches

import (
	"context"
	"errors"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type Service struct {
	Hub       *hub.Service
	People    *people.Service
	Engine    *triggers.Engine
	Store     *triggers.SQLStore
	queue     chan wa.StoredEvent
	dropped   atomic.Uint64
	mu        sync.Mutex
	failure   error
	recovered map[string]bool
}
type Options struct {
	Runner triggers.Runner
	Clock  triggers.Clock
	MCPURL string
}

func New(h *hub.Service, p *people.Service, o Options) (*Service, error) {
	s := &Service{Hub: h, People: p, Store: &triggers.SQLStore{DB: h.Repo.DB}, queue: make(chan wa.StoredEvent, 512), recovered: map[string]bool{}}
	s.Store.Event = s.event
	records, err := s.Store.List(context.Background())
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		runs, err := s.Store.Runs(context.Background(), r.ID)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.State == "running" && (r.Status == triggers.Active || r.Status == triggers.Paused) {
				s.recovered[r.ID] = true
			}
		}
	}
	_, err = s.Store.Recover(context.Background(), h.Now())
	if err != nil {
		return nil, err
	}
	// Credentials are single-run capabilities, never reused after restart.
	if _, err = h.Repo.DB.Exec("DELETE FROM trigger_run_credentials"); err != nil {
		return nil, err
	}
	runner := o.Runner
	if runner == nil {
		logdir := filepath.Join(h.Home, "logs", "watches")
		runner = triggers.SessionRunner{Codex: triggers.CodexRunner{Options: triggers.ProcessOptions{LogDir: logdir}}, Claude: triggers.ClaudeRunner{Options: triggers.ProcessOptions{LogDir: logdir}}}
	}
	e, err := triggers.NewEngine(triggers.Options{Store: s.Store, Notifier: s, Suggester: s, Runner: boundRunner{s: s, next: runner, url: o.MCPURL}, Clock: o.Clock, AllowMessage: s.allowMessage, OwnerName: h.OwnerName})
	if err != nil {
		return nil, err
	}
	s.Engine = e
	h.Watches = e
	h.WatchStore = s.Store
	return s, nil
}
func (s *Service) event(ctx context.Context, kind string, r triggers.Record, run *triggers.RunRecord) error {
	if kind == "paused" || kind == "stopped" || kind == "expired" {
		// Policy already denies these states; eagerly invalidate queued drafts too.
		tx, err := s.Hub.Repo.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `UPDATE objects SET data=json_set(data,'$.state','cancelled') WHERE bucket='drafts' AND json_extract(data,'$.handoff_id')=? AND json_extract(data,'$.state') IN ('draft','suggested')`, r.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.state')='pending' AND json_extract(data,'$.draft_id') IN (SELECT id FROM objects WHERE bucket='drafts' AND json_extract(data,'$.handoff_id')=?)`, r.ID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if kind == "resumed" || kind == "stopped" || kind == "expired" {
		s.mu.Lock()
		delete(s.recovered, r.ID)
		s.mu.Unlock()
	}
	act := model.Activity{AgentID: r.CreatedVia.Agent, Tool: "watch." + kind, Kind: "watch." + kind, Note: r.ID, ChatJIDs: r.Scope.Chats}
	if run != nil {
		act.Note = fmt.Sprintf("watch=%s run=%s hit=%s state=%s exit_code=%d", r.ID, run.ID, run.HitID, run.State, run.ExitCode)
		var replies int
		if err := s.Hub.Repo.DB.QueryRowContext(ctx, "SELECT count(*) FROM trigger_outbound WHERE run_id=?", run.ID).Scan(&replies); err != nil {
			return err
		}
		act.Note += fmt.Sprintf(" replies_sent=%d", replies)
		hits, err := s.Store.Hits(ctx, r.ID, 0)
		if err != nil {
			return err
		}
		for _, h := range hits {
			if h.ID == run.HitID {
				for _, m := range h.Messages {
					act.MessageIDs = append(act.MessageIDs, m.ID)
					if !model.Contains(act.ChatJIDs, m.ChatJID) {
						act.ChatJIDs = append(act.ChatJIDs, m.ChatJID)
					}
				}
			}
		}
	}
	return s.Hub.Record(act)
}
func (s *Service) Notify(ctx context.Context, n triggers.Notification) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a := model.Activity{Tool: "watch." + n.Kind, Kind: "watch." + n.Kind, Note: n.WatchID}
	if n.Hit != nil {
		for _, m := range n.Hit.Messages {
			a.MessageIDs = append(a.MessageIDs, m.ID)
			if !model.Contains(a.ChatJIDs, m.ChatJID) {
				a.ChatJIDs = append(a.ChatJIDs, m.ChatJID)
			}
		}
		a.Count = len(n.Hit.Messages)
	}
	return s.Hub.Record(a)
}
func (s *Service) Suggest(ctx context.Context, r triggers.Record, h triggers.Hit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	chat := ""
	if len(h.Messages) > 0 {
		chat = h.Messages[len(h.Messages)-1].ChatJID
	}
	circle := s.CircleFor(r, chat)
	x := model.Suggestion{ID: dbutil.ID(), CircleID: circle, Kind: "action", ChatJID: chat, Why: r.Goal, CreatedBy: r.CreatedVia.Agent, CreatedAt: s.Hub.Now().UTC(), ExpiresAt: s.Hub.Now().UTC().Add(72 * time.Hour), State: "pending"}
	a, err := s.Hub.Agent(r.CreatedVia.Agent)
	if err != nil || !a.Enabled || !a.Permissions.ReadMessages || !s.scopeAllowed(a, r) || !s.Hub.CanChat(&a, chat) {
		return model.ErrForbidden
	}
	if circle != "" {
		_, err = (&circles.Service{Hub: s.Hub, People: s.People}).Suggest(a, x)
		return err
	}
	// Uncircled chats use the same unscoped Home suggestion fallback as drafts.
	return s.Hub.Repo.Put("suggestions", x.ID, x)
}
func (s *Service) CircleFor(r triggers.Record, chat string) string {

	cs, _ := hub.List[model.Circle](s.Hub.Repo, "circles")
	a, e := s.Hub.Agent(r.CreatedVia.Agent)
	for _, preferScoped := range []bool{true, false} {
		for _, c := range cs {
			if !c.Confirmed || e == nil && !s.Hub.CircleAccess(&a, c) || preferScoped && !model.Contains(r.Scope.Circles, c.ID) {
				continue
			}
			for _, j := range c.Chats {
				if j.ChatJID == chat {
					return c.ID
				}
			}
		}
	}

	return ""
}

// Enqueue does no database work and never waits for matching or a runner.
func (s *Service) Enqueue(m wa.StoredEvent) {
	select {
	case s.queue <- m:
	default:
		s.dropped.Add(1)
	}
}
func (s *Service) Handle(ctx context.Context, m wa.StoredEvent) error {
	if m.Message.IsFromMe {
		return nil
	}
	var n int
	if err := s.Hub.Repo.DB.QueryRowContext(ctx, "SELECT count(*) FROM trigger_outbound WHERE chat_jid=? AND message_id=?", m.Message.ChatJID, m.Message.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return err
	}
	person := model.PersonID(m.Message.Sender)
	for _, p := range ps {
		if model.Contains(p.JIDs, m.Message.Sender) {
			person = p.ID
			break
		}
	}
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return err
	}
	ids := []string{}
	for _, c := range cs {
		if !c.Confirmed {
			continue
		}
		for _, j := range c.Chats {
			if j.ChatJID == m.Message.ChatJID {
				ids = append(ids, c.ID)
				break
			}
		}
	}
	// Current permission checks also apply when each debounce batch becomes due.
	return s.Engine.OnMessage(ctx, triggers.IncomingMessage{ID: m.Message.ID, ChatJID: m.Message.ChatJID, Sender: m.Message.Sender, SenderPersonID: person, CircleIDs: ids, Text: m.Message.Content, At: m.Message.Timestamp, MentionsMe: m.MentionsMe})
}
func (s *Service) Run(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- s.Engine.Run(ctx) }()
	defer s.Engine.Close()
	for {
		select {
		case <-ctx.Done():
			return <-done
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				s.setError(err)
			}
			return err
		case m := <-s.queue:
			if err := s.Handle(ctx, m); err != nil {
				s.setError(err)
			}
		}
	}
}
func (s *Service) setError(err error) {
	s.mu.Lock()
	s.failure = errors.Join(s.failure, err)
	s.mu.Unlock()
}
func (s *Service) Check() model.Check {
	c := model.Check{ID: "watches", Title: "Watches", State: "ok", Detail: "Watching for updates"}
	s.mu.Lock()
	err := s.failure
	recovered := len(s.recovered)
	s.mu.Unlock()
	if err != nil || s.Engine.Err() != nil {
		c.State = "warn"
		c.Detail = "Degraded: watch processing failed. Restart the engine after checking data-folder access."
	} else if s.dropped.Load() > 0 {
		c.State = "warn"
		c.Detail = "Degraded: incoming watch queue overflowed; some updates were skipped."
	} else if recovered > 0 {
		c.State = "warn"
		c.Detail = "Degraded: interrupted handoffs were paused. Verify their sessions have stopped before resuming."
	}
	return c
}
func (s *Service) Record(ctx context.Context, id string) (triggers.Record, error) {
	r, err := s.Store.Get(ctx, id)
	if errors.Is(err, triggers.ErrNotFound) {
		err = model.ErrNotFound
	}
	return r, err
}
func (s *Service) Instruction(ctx context.Context, id, text string) error {
	if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 500 {
		return model.ErrInvalid
	}
	return s.Engine.AppendInstruction(ctx, id, text)
}
func (s *Service) Status(ctx context.Context, id string, status triggers.Status) error {
	if _, err := s.Record(ctx, id); err != nil {
		return err
	}
	if err := s.Engine.SetStatus(ctx, id, status); err != nil {
		return model.ErrConflict
	}
	return nil
}

type Hit struct {
	At        time.Time `json:"at"`
	Chat      string    `json:"chat"`
	Sender    string    `json:"sender"`
	Text      string    `json:"text"`
	MessageID string    `json:"message_id"`
}
type RunnerSummary struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}
type Summary struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Goal      string          `json:"goal"`
	Where     string          `json:"where"`
	Action    triggers.Action `json:"action"`
	Status    triggers.Status `json:"status"`
	CreatedBy string          `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt *time.Time      `json:"expires_at"`
	NewHits   int             `json:"new_hits"`
	Latest    *Hit            `json:"latest"`
	Runner    *RunnerSummary  `json:"runner"`
}
type Stats struct {
	Runs      int        `json:"runs"`
	Replies   int        `json:"replies"`
	LastRunAt *time.Time `json:"last_run_at"`
}
type Detail struct {
	Summary
	Digest       string                 `json:"digest"`
	Instructions []triggers.Instruction `json:"instructions"`
	Hits         []Hit                  `json:"hits"`
	Stats        Stats                  `json:"stats"`
}

func short(s string) string {
	r := []rune(s)
	if len(r) > 32 {
		return string(r[:32])
	}
	return s
}
func (s *Service) Detail(ctx context.Context, id string) (Detail, error) {
	r, err := s.Record(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	state, err := s.Store.State(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Summary: Summary{ID: r.ID, Kind: "watch", Title: short(r.Title), Goal: r.Goal, Action: r.Action, Status: r.Status, CreatedBy: "You", CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, NewHits: state.NewHits}, Digest: state.Digest, Instructions: append([]triggers.Instruction{}, r.Instructions...), Hits: []Hit{}}
	if d.Title == "" {
		d.Title = short(r.Goal)
	}
	if a, e := s.Hub.Agent(r.CreatedVia.Agent); e == nil {
		d.CreatedBy = a.Name
	}
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return d, err
	}
	chats, err := s.Hub.Messages.Chats()
	if err != nil {
		return d, err
	}
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return d, err
	}
	labels := map[string]string{}
	for _, c := range cs {
		labels[c.ID] = c.Name
	}
	for _, c := range chats {
		labels[c.JID] = c.Name
	}
	for _, p := range ps {
		labels[p.ID] = p.Name
		for _, j := range p.JIDs {
			labels[j] = p.Name
		}
	}
	label := func(id string) string {
		if v := labels[id]; v != "" {
			return v
		}
		return "Chat"
	}
	places := []string{}
	for _, ids := range [][]string{r.Scope.Circles, r.Scope.Chats, r.Scope.People} {
		for _, id := range ids {
			places = append(places, label(id))
		}
	}
	d.Where = strings.Join(places, ", ")
	if r.Action == triggers.Feed {
		d.Kind = "feed"
		d.Where = "Everything it can read"
	}
	if r.Handoff != nil {
		d.Kind = "handoff"
		name := "Codex"
		if r.Handoff.Runner.Kind == "claude_code" {
			name = "Claude Code"
		}
		d.Runner = &RunnerSummary{Kind: r.Handoff.Runner.Kind, Label: name + " · " + filepath.Base(r.Handoff.Runner.Workdir)}
	}
	hits, err := s.Store.Hits(ctx, id, 20)
	if err != nil {
		return d, err
	}

	for _, batch := range hits {
		for _, m := range batch.Messages {
			sender := label(m.SenderPersonID)
			if labels[m.SenderPersonID] == "" {
				sender = label(m.Sender)
			}
			d.Hits = append(d.Hits, Hit{At: m.At, Chat: label(m.ChatJID), Sender: sender, Text: m.Text, MessageID: m.ID})
		}
	}
	sort.SliceStable(d.Hits, func(i, j int) bool { return d.Hits[i].At.After(d.Hits[j].At) })
	if len(d.Hits) > 20 {
		d.Hits = d.Hits[:20]
	}

	if len(d.Hits) > 0 {
		h := d.Hits[0]
		d.Latest = &h
	}
	runs, err := s.Store.Runs(ctx, id)
	if err != nil {
		return d, err
	}
	d.Stats = Stats{Runs: len(runs), Replies: state.Replies}
	if len(runs) > 0 {
		at := runs[len(runs)-1].StartedAt
		d.Stats.LastRunAt = &at
	}
	return d, nil
}
func (s *Service) List(ctx context.Context, home bool) ([]Summary, error) {
	rs, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	changed := map[string]time.Time{}
	for _, r := range rs {
		st, err := s.Store.State(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		changed[r.ID] = st.ChangedAt
	}
	sort.Slice(rs, func(i, j int) bool {
		a := rs[i].Status == triggers.Active || rs[i].Status == triggers.Paused
		b := rs[j].Status == triggers.Active || rs[j].Status == triggers.Paused
		if a != b {
			return a
		}
		if !a && !changed[rs[i].ID].Equal(changed[rs[j].ID]) {
			return changed[rs[i].ID].After(changed[rs[j].ID])
		}
		if rs[i].CreatedAt.Equal(rs[j].CreatedAt) {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].CreatedAt.After(rs[j].CreatedAt)
	})
	out := []Summary{}
	ended := 0
	for _, r := range rs {
		if r.Status != triggers.Active && r.Status != triggers.Paused {
			if home || ended >= 5 {
				continue
			}
			ended++
		}
		d, err := s.Detail(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, d.Summary)
	}
	return out, nil
}

func (s *Service) allowMessage(ctx context.Context, r triggers.Record, m triggers.IncomingMessage) (bool, error) {
	a, err := s.Hub.Agent(r.CreatedVia.Agent)
	if err != nil {
		return false, nil
	}
	return a.Enabled && a.Permissions.ReadMessages && s.scopeAllowed(a, r) && s.Hub.CanChat(&a, m.ChatJID), nil
}
