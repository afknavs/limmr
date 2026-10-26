package efforts

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadyReadsNeverWaitForInference(t *testing.T) {
	s, _ := seed(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	s.yield = func() {
		select {
		case <-entered:
		default:
			close(entered)
			<-release
		}
	}
	go func() { _, err := s.List(nil, Filter{}); done <- err }()
	<-entered
	start := time.Now()
	rows, ready, err := s.ReadyCircleUpdates()
	testkit.Must(t, err)
	a := model.Agent{Permissions: model.Preset("own")}
	hint, hintReady := s.ReadyNamingHint(a)
	elapsed := time.Since(start)
	close(release)
	testkit.Must(t, <-done)
	s.yield = nil
	if ready || hintReady || len(rows) != 0 || hint != "" {
		t.Fatal("incomplete snapshot exposed")
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("ready reads waited on inference: %s", elapsed)
	}
	want, err := s.CircleUpdates()
	testkit.Must(t, err)
	got, ready, err := s.ReadyCircleUpdates()
	testkit.Must(t, err)
	if !ready || !reflect.DeepEqual(got, want) {
		t.Fatal("completed snapshot differs")
	}
	// Even while another computation holds its lock, the completed snapshot is readable.
	s.mu.Lock()
	_, ready, err = s.ReadyCircleUpdates()
	s.mu.Unlock()
	testkit.Must(t, err)
	if !ready {
		t.Fatal("ready snapshot depends on compute lock")
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Group, testkit.Phone}
	testkit.Must(t, s.Hub.SetSettings(settings))
	rows, ready, err = s.ReadyCircleUpdates()
	testkit.Must(t, err)
	if ready || len(rows) != 0 {
		t.Fatal("stale visibility projection served")
	}
}

func TestBackgroundWarmupDefersToSyncAndStops(t *testing.T) {
	s, _ := seed(t)
	var busy atomic.Bool
	busy.Store(true)
	var gate sync.RWMutex
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.RunWarmup(ctx, busy.Load, &gate) }()
	defer func() { cancel(); <-done }()
	// More than one worker tick: no snapshot may be computed during catch-up.
	time.Sleep(550 * time.Millisecond)
	if s.published.Load() != nil {
		t.Fatal("warmup ran during sync")
	}
	gate.Lock()
	busy.Store(false)
	time.Sleep(300 * time.Millisecond)
	blocked := s.published.Load() == nil
	gate.Unlock()
	if !blocked {
		t.Fatal("warmup overlapped exclusive store migration")
	}
	deadline := time.Now().Add(3 * time.Second)
	for s.published.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.published.Load() == nil {
		t.Fatal("warmup did not resume")
	}
	got, ready, err := s.ReadyCircleUpdates()
	testkit.Must(t, err)
	want, err := s.CircleUpdates()
	testkit.Must(t, err)
	if !ready || !reflect.DeepEqual(got, want) {
		t.Fatal("background result differs")
	}
}

func TestBackgroundDoesNotBorrowForegroundMessageConnection(t *testing.T) {
	s, _ := seed(t)
	// Occupy the complete foreground pool before warmup starts. Background
	// inference must still publish using its independent read-only connection.
	conn, err := s.Hub.Messages.DB.Conn(context.Background())
	testkit.Must(t, err)
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.RunWarmup(ctx, nil, nil) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for s.published.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.published.Load() == nil {
		t.Fatal("background borrowed the foreground pool")
	}
}
