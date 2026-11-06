package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchdogExitsNonzero(t *testing.T) {
	if os.Getenv("LIMMR_WATCHDOG_TEST") == "1" {
		Watchdog{Home: os.Getenv("LIMMR_HOME"), URL: "http://127.0.0.1:1", Beat: time.Now, Interval: time.Millisecond, Limit: 20 * time.Millisecond}.Run(context.Background())
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWatchdogExitsNonzero$")
	cmd.Env = append(os.Environ(), "LIMMR_WATCHDOG_TEST=1", "LIMMR_HOME="+t.TempDir())
	err := cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 75 {
		t.Fatalf("watchdog exit: %v", err)
	}
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
