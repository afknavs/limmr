package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

// Pause only after SQLite has handed the badge reader its sole connection.
// This makes the writer/read/runtime overlap deterministic without sleeps.
type badgeBarrierDB struct {
	dbutil.Database
	once            sync.Once
	opened, release chan struct{}
}

func (d *badgeBarrierDB) Query(q string, args ...any) (*sql.Rows, error) {
	rows, err := d.Database.Query(q, args...)
	if err == nil && strings.Contains(q, "FROM task_cards ORDER BY id") {
		d.once.Do(func() { close(d.opened); <-d.release })
	}
	return rows, err
}

func TestPeopleBadgesReleaseHubConnectionWithRuntimeAndGate(t *testing.T) {
	s, agent, now := fixture(t)
	if s.Hub.Repo.DB.(*sql.DB).Stats().MaxOpenConnections != 1 {
		t.Fatal("regression requires the real single-connection hub")
	}
	// Legacy membership invokes Efforts.Get while projecting the first card.
	person := model.PersonID(testkit.Phone)
	r := record{Task: Task{ID: "legacy-work", Title: "Review poster", Goal: "Review poster", Kind: "long", Status: "active", Origin: "owner", EffortID: "eff_fixture", Chats: []Entity{{ID: testkit.Phone}}, People: []Entity{{ID: person}}, CreatedAt: *now, UpdatedAt: *now, LastActiveAt: *now, LifespanDays: 21, NeedsYou: true}}
	testkit.Must(t, func() error {
		_, err := s.Hub.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", r.ID, dbutil.JSON(r))
		return err
	}())
	message := model.Message{ID: "incoming-fixture", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "The poster review is tomorrow", Timestamp: now.Add(time.Second)}
	testkit.Must(t, s.Hub.Messages.PutMessage(message))
	barrier := &badgeBarrierDB{Database: s.Hub.Repo.DB, opened: make(chan struct{}), release: make(chan struct{})}
	s.Hub.Repo.DB = barrier
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtimeTick := make(chan struct{})
	var tickOnce sync.Once
	s.Hub.MayRunAutomations = func(context.Context) bool {
		tickOnce.Do(func() { close(runtimeTick) })
		return false // no sync lease needed; incoming processing still runs
	}
	runDone := make(chan error, 1)
	go func() { runDone <- s.Run(ctx) }()
	<-runtimeTick

	var gate sync.RWMutex
	badgeDone := make(chan error, 1)
	go func() {
		gate.RLock()
		defer gate.RUnlock()
		badges, err := s.NeedsYouPeople()
		if err == nil && !badges[person] {
			err = fmt.Errorf("missing owner attention badge")
		}
		badgeDone <- err
	}()
	<-barrier.opened
	writerDone := make(chan struct{})
	go func() { gate.Lock(); defer gate.Unlock(); close(writerDone) }()
	// Confirm the migrate-style writer is pending before later gated reads.
	for gate.TryRLock() {
		gate.RUnlock()
		runtime.Gosched()
	}
	reads := make(chan error, 3)
	go func() { gate.RLock(); defer gate.RUnlock(); _, err := s.GetForOwner(r.ID); reads <- err }()
	go func() { gate.RLock(); defer gate.RUnlock(); _, err := s.Get(agent, r.ID); reads <- err }()
	go func() { gate.RLock(); defer gate.RUnlock(); reads <- nil }() // main-loop heartbeat
	s.Enqueue(wa.StoredEvent{Message: message})
	close(barrier.release)
	// The short go-test timeout prints the actual blocked goroutines on failure.
	testkit.Must(t, <-badgeDone)
	<-writerDone
	for i := 0; i < 3; i++ {
		testkit.Must(t, <-reads)
	}
	for {
		got, err := s.raw(r.ID)
		testkit.Must(t, err)
		if got.LastActiveAt.Equal(message.Timestamp) {
			break
		}
		select {
		case err := <-runDone:
			t.Fatalf("runtime stopped before incoming message: %v", err)
		default:
			runtime.Gosched()
		}
	}
	cancel()
	testkit.Must(t, <-runDone)
	var opened string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT json_extract(data,'$.owner_opened_at') FROM tasks WHERE id=?", r.ID).Scan(&opened))
	if opened == "" || len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
		t.Fatal("missing owner open or unexpected send")
	}
}
