package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchdogExitsNonzero(t *testing.T) {
	if os.Getenv("LIMMR_WATCHDOG_TEST") == "1" {
		Watchdog{Home: os.Getenv("LIMMR_HOME"), URL: "http://127.0.0.1:1", Beat: time.Now, Interval: time.Millisecond, Limit: 20 * time.Millisecond}.Run(context.Background())
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWatchdogExitsNonzero$")
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "LIMMR_WATCHDOG_TEST=1", "LIMMR_HOME="+home)
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 75 {
		t.Fatalf("watchdog exit: %v", err)
	}
	assertStuckStacks(t, home)
}
func TestWatchdogMainStallAndBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	home := t.TempDir()
	now := time.Now()
	exits := 0
	Watchdog{Home: home, URL: server.URL, Beat: func() time.Time { return now.Add(-time.Minute) }, Interval: time.Millisecond, Limit: 10 * time.Millisecond, Exit: func(code int) {
		if code != 75 {
			t.Error(code)
		}
		assertStuckStacks(t, home) // must already be on disk before restart
		exits++
	}}.Run(context.Background())
	if exits != 1 {
		t.Fatal("missing stall exit")
	}
	h := RestartHistory{Exits: []time.Time{now, now, now, now}}
	b, _ := json.Marshal(h)
	_ = os.WriteFile(filepath.Join(home, "watchdog-restarts.json"), b, 0600)
	count, until := RestartBackoff(home, time.Now())
	if count != 4 || until.Sub(now) != 2*time.Minute {
		t.Fatal("missing backoff")
	}
	if WatchdogCheck(home).State != "warn" {
		t.Fatal("missing health warning")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	Watchdog{Home: home, URL: server.URL, Beat: func() time.Time { return now.Add(-time.Minute) }, Interval: time.Millisecond, Limit: 10 * time.Millisecond, Exit: func(int) { t.Error("restarted during backoff") }}.Run(ctx)
	if count, _ := RestartBackoff(home, now.Add(11*time.Minute)); count != 0 {
		t.Fatal("history did not expire")
	}
}

func assertStuckStacks(t *testing.T, home string) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(home, "logs", "engine-stuck-*.txt"))
	if err != nil || len(names) != 1 {
		t.Fatalf("missing stack dump: %v, %v", names, err)
	}
	b, err := os.ReadFile(names[0])
	if err != nil || !strings.Contains(string(b), "limmr/internal/health.Watchdog.Run") || !strings.Contains(string(b), "goroutine ") {
		t.Fatalf("incomplete watchdog stack dump: %v", err)
	}
}

func stackCaptureWait(ready chan<- struct{}, release <-chan struct{}) {
	ready <- struct{}{}
	<-release
}

func TestStuckStacksCaptureAllAndKeepNewestThree(t *testing.T) {
	home := t.TempDir()
	ready, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	// Exceed the initial stack buffer and prove every blocked worker is captured.
	const workers = 300
	for i := 0; i < workers; i++ {
		go stackCaptureWait(ready, release)
	}
	for i := 0; i < workers; i++ {
		<-ready
	}
	now := time.Date(2026, 11, 13, 13, 0, 0, 0, time.FixedZone("fixture", 2*60*60))
	for i := 0; i < 5; i++ {
		if err := writeStuckStacks(home, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	names, err := filepath.Glob(filepath.Join(home, "logs", "engine-stuck-*.txt"))
	if err != nil || len(names) != 3 {
		t.Fatalf("retention: %v, %v", names, err)
	}
	for i, name := range names {
		want := "engine-stuck-" + now.Add(time.Duration(i+2)*time.Second).UTC().Format("20060102T150405.000000000Z") + ".txt"
		if filepath.Base(name) != want {
			t.Fatalf("retained wrong dump: %s", name)
		}
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("dump mode: %v", info.Mode())
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) <= 64<<10 || strings.Count(string(b), "limmr/internal/health.stackCaptureWait(") != workers {
			t.Fatalf("truncated dump: %d bytes, %d workers", len(b), strings.Count(string(b), "limmr/internal/health.stackCaptureWait("))
		}
	}
}

func TestWatchdogRestartsWhenStackWriteFails(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "logs"), []byte("fixture obstruction"), 0600); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	Watchdog{Home: home, URL: "http://127.0.0.1:1", Beat: time.Now, Interval: time.Millisecond, Limit: 20 * time.Millisecond, Exit: func(code int) { exitCode = code }}.Run(context.Background())
	if exitCode != 75 {
		t.Fatalf("diagnostic failure prevented restart: %d", exitCode)
	}
}

func TestAllEngineStartsCountTowardBackoff(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	for i := 0; i < 5; i++ {
		RecordEngineStart(home, now.Add(time.Duration(i)*time.Second))
	}
	count, until := RestartBackoff(home, now.Add(5*time.Second))
	if count != 4 || !until.After(now.Add(time.Minute)) {
		t.Fatal("crash restarts missing from backoff")
	}
}
