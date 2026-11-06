package health

import (
	"context"
	"encoding/json"
	"io"
	"limmr/internal/model"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Restart history contains timestamps only. A repeated watchdog failure delays
// the next exit, rather than making launchd spin. Healthy work can continue.
type RestartHistory struct {
	Exits  []time.Time `json:"exits"`
	Starts []time.Time `json:"starts,omitempty"`
}

func restartHistory(home string, now time.Time) RestartHistory {
	var h RestartHistory
	b, _ := os.ReadFile(filepath.Join(home, "watchdog-restarts.json"))
	_ = json.Unmarshal(b, &h)
	kept := h.Exits[:0]
	for _, at := range h.Exits {
		if now.Sub(at) < 10*time.Minute && !at.After(now) {
			kept = append(kept, at)
		}
	}
	h.Exits = kept
	starts := h.Starts[:0]
	for _, at := range h.Starts {
		if now.Sub(at) < 10*time.Minute && !at.After(now) {
			starts = append(starts, at)
		}
	}
	h.Starts = starts
	return h
}

// Count every service launch, including recovery after an unexpected crash.
func RecordEngineStart(home string, now time.Time) {
	h := restartHistory(home, now)
	h.Starts = append(h.Starts, now)
	writeRestartHistory(home, h)
}
func writeRestartHistory(home string, h RestartHistory) {
	b, _ := json.Marshal(h)
	temporary := filepath.Join(home, "watchdog-restarts.json.tmp")
	if os.WriteFile(temporary, b, 0600) == nil {
		_ = os.Rename(temporary, filepath.Join(home, "watchdog-restarts.json"))
	}
}
func RestartBackoff(home string, now time.Time) (int, time.Time) {
	h := restartHistory(home, now)
	count := len(h.Exits)
	last := time.Time{}
	if count > 0 {
		last = h.Exits[count-1]
	}
	if len(h.Starts) > 0 {
		count = max(0, len(h.Starts)-1)
		last = h.Starts[len(h.Starts)-1]
	}
	if count < 4 {
		return count, time.Time{}
	}
	delay := time.Minute * time.Duration(2<<min(count-4, 2))
	return count, last.Add(delay)
}
func WatchdogCheck(home string) model.Check {
	count, _ := RestartBackoff(home, time.Now())
	c := model.Check{ID: "engine_restarts", Title: "Limmr’s engine", State: "ok", Detail: "Running steadily"}
	if count > 3 {
		c.State = "warn"
		c.Detail = "The engine restarted several times. Automatic restarts are taking a break."
	}
	return c
}

// Watchdog is independent of the app and the main loop. Ping must bypass all
// database work; Beat must be supplied by the actual main loop, not another timer.
type Watchdog struct {
	Home, URL, Token string
	Logger           *log.Logger
	Beat             func() time.Time
	Exit             func(int)
	Interval, Limit  time.Duration
}

func (w Watchdog) Run(ctx context.Context) {
	interval, limit := w.Interval, w.Limit
	if interval == 0 {
		interval = 5 * time.Second
	}
	if limit == 0 {
		limit = 60 * time.Second
	}
	client := &http.Client{Timeout: min(time.Second, limit), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastPing := time.Now()
	_, notBefore := RestartBackoff(w.Home, lastPing)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", w.URL+"/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer "+w.Token)
		response, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64))
			response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				lastPing = time.Now()
			}
		}
		now := time.Now()
		reason := ""
		if now.Sub(lastPing) >= limit {
			reason = "HTTP ping stopped responding"
		} else if now.Sub(w.Beat()) >= limit {
			reason = "main loop stopped responding"
		}
		if reason == "" || now.Before(notBefore) {
			continue
		}
		h := restartHistory(w.Home, now)
		h.Exits = append(h.Exits, now)
		writeRestartHistory(w.Home, h)
		if w.Logger != nil {
			w.Logger.Print("WARN engine watchdog: " + reason + "; restarting engine")
		}
		exit := w.Exit
		if exit == nil {
			exit = os.Exit
		}
		exit(75)
		return
	}
}
