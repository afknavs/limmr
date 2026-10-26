package hub_test

import (
	"context"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/triggers"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var watchTime = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func openWatchStore(t *testing.T) (*hub.Repo, *triggers.SQLStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hub.db")
	r, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, &triggers.SQLStore{DB: r.DB}, path
}
func storedWatch(id string) triggers.Record {
	return triggers.Record{Watch: triggers.Watch{ID: id, Owner: "agent", Goal: "Exam updates", Scope: triggers.Scope{Chats: []string{"demo-chat"}}, Match: triggers.Match{Kind: triggers.Any}, Action: triggers.Digest, Status: triggers.Active, CreatedAt: watchTime, CreatedVia: triggers.Provenance{Agent: "agent", GrantText: "Watch exam updates", At: watchTime}, QuietWindow: time.Minute, MaxWindow: 5 * time.Minute}}
}
func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestSQLWatchMigrationPersistenceAndTransactionalUpdate(t *testing.T) {
	r, s, path := openWatchStore(t)
	ctx := context.Background()
	w := storedWatch("exam")
	require(t, s.Create(ctx, w))
	if err := s.Create(ctx, w); !errors.Is(err, triggers.ErrExists) {
		t.Fatal("duplicate watch accepted")
	}
	require(t, s.Update(ctx, w.ID, func(v *triggers.Record) error {
		v.Instructions = append(v.Instructions, triggers.Instruction{Text: "Only dates", At: watchTime})
		return nil
	}))
	for _, fn := range []func(*triggers.Record) error{func(v *triggers.Record) error { v.Goal = "changed"; return errors.New("cancel") }, func(v *triggers.Record) error { v.Instructions[0].Text = "overwrite"; return nil }, func(v *triggers.Record) error { v.CreatedVia.GrantText = "replace"; return nil }, func(v *triggers.Record) error { v.Status = "bogus"; return nil }} {
		if err := s.Update(ctx, w.ID, fn); err == nil {
			t.Fatal("invalid update committed")
		}
	}
	got, err := s.Get(ctx, w.ID)
	require(t, err)
	if got.Goal != w.Goal || got.Instructions[0].Text != "Only dates" {
		t.Fatal("transaction rollback failed")
	}
	require(t, s.SetDigest(ctx, w.ID, "The exam is Tuesday"))
	for i := 0; i < 25; i++ {
		h := triggers.Hit{ID: time.Duration(i).String(), WatchID: w.ID, At: watchTime.Add(time.Duration(i) * time.Second), Action: triggers.Digest, State: "recorded", Messages: []triggers.IncomingMessage{{ID: time.Duration(i).String(), Text: "Invented exam news"}}}
		require(t, s.PutHit(ctx, h))
		h.State = "completed"
		require(t, s.PutHit(ctx, h))
	}
	state, err := s.State(ctx, w.ID)
	require(t, err)
	if state.NewHits != 25 {
		t.Fatal("hit finalisation counted twice")
	}
	hits, err := s.Hits(ctx, w.ID, 20)
	require(t, err)
	if len(hits) != 20 || !hits[0].At.Equal(watchTime.Add(5*time.Second)) {
		t.Fatal("newest N history not returned oldest first")
	}
	require(t, r.Close())
	r2, err := hub.Open(path)
	require(t, err)
	defer r2.Close()
	s2 := &triggers.SQLStore{DB: r2.DB}
	state, err = s2.State(ctx, w.ID)
	require(t, err)
	if state.NewHits != 25 || state.Digest != "The exam is Tuesday" {
		t.Fatal("widget state did not survive reopen")
	}
	require(t, s2.Seen(ctx, w.ID))
	state, err = s2.State(ctx, w.ID)
	require(t, err)
	if state.NewHits != 0 {
		t.Fatal("seen not persisted")
	}
	require(t, s2.Delete(ctx, w.ID))
	if _, err = s2.Get(ctx, w.ID); !errors.Is(err, triggers.ErrNotFound) {
		t.Fatal("watch not deleted")
	}
	var n int
	require(t, r2.DB.QueryRow("SELECT count(*) FROM trigger_hits").Scan(&n))
	if n != 0 {
		t.Fatal("foreign-key cascade disabled")
	}
}
func TestSQLReplyReservationsAreAtomicAcrossConnections(t *testing.T) {
	r, s, _ := openWatchStore(t)
	r.DB.SetMaxOpenConns(4)
	ctx := context.Background()
	require(t, s.Create(ctx, storedWatch("budget")))
	var count atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.ReserveReply(ctx, "budget", watchTime, 12)
			if err != nil {
				errs <- err
			}
			if ok {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require(t, err)
	}
	if count.Load() != 12 {
		t.Fatal("atomic reservation exceeded budget")
	}
	n, err := s.ReplyCount(ctx, "budget", watchTime.Add(time.Hour))
	require(t, err)
	if n != 0 {
		t.Fatal("rolling-hour boundary incorrect")
	}
	// An offset timestamp must compare by instant rather than lexical order.
	_, err = r.DB.Exec("INSERT INTO trigger_replies(handoff_id,at) VALUES(?,?)", "budget", "2026-10-17T17:30:00+05:30")
	require(t, err)
	n, err = s.ReplyCount(ctx, "budget", watchTime.Add(30*time.Minute))
	require(t, err)
	if n != 13 {
		t.Fatal("mixed RFC3339 offsets corrupted budget")
	}
}
func TestSQLRecoveryPausesInterruptedRunsAndPreservesHistory(t *testing.T) {
	r, s, _ := openWatchStore(t)
	ctx := context.Background()
	w := storedWatch("session")
	expiry := watchTime.Add(72 * time.Hour)
	w.ExpiresAt = &expiry
	w.Action = triggers.WakeSession
	w.CreatedVia.SessionID = "session-42"
	w.Handoff = &triggers.HandoffConfig{Runner: triggers.RunnerConfig{Kind: "codex", SessionID: "session-42", Workdir: "/tmp/demo"}, SendGrant: triggers.SendGrant{ChatJID: "demo-chat"}, Limits: triggers.Limits{MaxRuns: 50, MaxRepliesPerHour: 12}}
	require(t, s.Create(ctx, w))
	require(t, s.PutHit(ctx, triggers.Hit{ID: "hit", WatchID: w.ID, At: watchTime, Action: triggers.WakeSession, State: "recorded", Messages: []triggers.IncomingMessage{}}))
	run := triggers.RunRecord{ID: "run", HandoffID: w.ID, HitID: "hit", StartedAt: watchTime, State: "running"}
	require(t, s.PutRun(ctx, run))
	other := run
	other.ID = "overlap"
	if err := s.PutRun(ctx, other); err == nil {
		t.Fatal("overlapping persisted session allowed")
	}
	n, err := s.Recover(ctx, watchTime.Add(time.Minute))
	require(t, err)
	if n != 1 {
		t.Fatal("interrupted run not recovered")
	}
	got, err := s.Get(ctx, w.ID)
	require(t, err)
	if got.Status != triggers.Paused {
		t.Fatal("interrupted handoff resumed automatically")
	}
	runs, err := s.Runs(ctx, w.ID)
	require(t, err)
	hits, err := s.Hits(ctx, w.ID, 20)
	require(t, err)
	if runs[0].State != "cancelled" || runs[0].ExitCode != -1 || hits[0].State != "cancelled" {
		t.Fatal("interrupted outcome not recorded")
	}
	var version int
	require(t, r.DB.QueryRow("SELECT count(*) FROM hub_schema WHERE version=3").Scan(&version))
	if version != 1 {
		t.Fatal("missing migration version")
	}
	n, err = s.Recover(ctx, watchTime.Add(2*time.Minute))
	require(t, err)
	if n != 0 {
		t.Fatal("recovery not idempotent")
	}
}

func TestRecoveryDoesNotReviveStoppedHandoff(t *testing.T) {
	_, s, _ := openWatchStore(t)
	ctx := context.Background()
	w := storedWatch("ended")
	require(t, s.Create(ctx, w))
	require(t, s.PutHit(ctx, triggers.Hit{ID: "interrupted-hit", WatchID: w.ID, At: watchTime, Action: triggers.Digest, State: "recorded"}))
	require(t, s.PutRun(ctx, triggers.RunRecord{ID: "interrupted-run", HandoffID: w.ID, HitID: "interrupted-hit", StartedAt: watchTime, State: "running"}))
	require(t, s.Update(ctx, w.ID, func(r *triggers.Record) error { r.Status = triggers.Stopped; return nil }))
	_, err := s.Recover(ctx, watchTime.Add(time.Minute))
	require(t, err)
	r, err := s.Get(ctx, w.ID)
	require(t, err)
	if r.Status != triggers.Stopped {
		t.Fatal("restart revived a final stopped watch")
	}
}
