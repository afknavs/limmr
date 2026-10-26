package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Accessory is inert until enabled. URL and TokenPath must be supplied explicitly;
// tests use an in-memory transport or a fake server and a temporary token file.
type Accessory struct {
	State          *hub.Repo
	loaded         bool
	URL, TokenPath string
	Client         *http.Client
	mu             sync.Mutex
	seen           map[string]bool
	last           time.Time
	own            bool
	running        bool
	generation     uint64
	after          func(time.Duration, func())
}

func (g *Accessory) request(ctx context.Context, method, path string, body any) (map[string]any, bool) {
	token, err := os.ReadFile(g.TokenPath)
	if err != nil {
		return nil, false
	}
	var b io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		b = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.URL+path, b)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	c := g.Client
	if c == nil {
		c = &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}
	out := map[string]any{}
	if json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out) != nil {
		return nil, false
	}
	return out, true
}
func quiet(now time.Time, q *model.TaskQuietHours) bool {
	start, end := "23:00", "08:00"
	loc := now.Location()
	if q != nil {
		start, end = q.Start, q.End
		if q.Timezone != "" {
			if l, err := time.LoadLocation(q.Timezone); err == nil {
				loc = l
			}
		}
	}
	clock := now.In(loc).Format("15:04")
	if start < end {
		return clock >= start && clock < end
	}
	return clock >= start || clock < end
}
func (g *Accessory) Sync(ctx context.Context, xs []record, c model.AccessoryCue, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	type saved struct {
		Seen map[string]bool
		Last time.Time
		Own  bool
	}
	if !g.loaded {
		if g.State != nil {
			var v saved
			if g.State.Get("task_runtime", "accessory", &v) == nil {
				g.seen = v.Seen
				g.last = v.Last
				g.own = v.Own
			}
		}
		g.loaded = true
	}
	if g.State != nil {
		defer func() { _ = g.State.Put("task_runtime", "accessory", saved{g.seen, g.last, g.own}) }()
	}
	if g.seen == nil {
		g.seen = map[string]bool{}
	}
	needs := map[string]bool{}
	title := ""
	for _, r := range xs {
		if r.NeedsYou {
			needs[r.ID] = true
			if !g.seen[r.ID] && title == "" {
				title = r.Title
			}
		}
	}
	if !c.Enabled || len(needs) == 0 {
		if g.own {
			state, ok := g.request(ctx, "GET", "/v1/signals", nil)
			g.running = ok
			if ok {
				if activeSource(state) == "limmr" {
					_, g.running = g.request(ctx, "DELETE", "/v1/signal", nil)
					g.own = !g.running
				} else {
					g.own = false
				}
			}
		}
		g.seen = map[string]bool{}
		return
	}
	if title != "" && !quiet(now, c.QuietHours) && (g.last.IsZero() || now.Sub(g.last) >= 10*time.Minute) {
		color := c.Color
		if color == "" {
			color = "#5e5ce6"
		}
		_, ok := g.request(ctx, "POST", "/v1/signal", map[string]any{"color": color, "pattern": "breathe", "count": 1, "hold": -1, "source": "limmr", "message": title + " needs you"})
		g.running = ok
		g.last = now
		if ok {
			g.own = true
			g.generation++
			g.seen = needs
		}
	}
	for id := range g.seen {
		if !needs[id] {
			delete(g.seen, id)
		}
	}
}
func (g *Accessory) Check(enabled bool) model.Check {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := model.Check{ID: "accessory_cue", Title: "Accessory cue", State: "ok", Detail: "Off"}
	if enabled {
		c.Detail = "Accessory cue: on"
		if !g.running {
			c.State = "warn"
			c.Detail = "Accessory not running"
		}
	}
	return c
}

// activeSource reads Accessory's real signals envelope, including active:null.
func activeSource(state map[string]any) string {
	active, _ := state["active"].(map[string]any)
	source, _ := active["source"].(string)
	return source
}

// Test bypasses Limmr's enabled/quiet/throttle settings without consuming its
// attention state. Cleanup belongs to the engine lifetime, not the HTTP request.
func (g *Accessory) Test(ctx context.Context, c model.AccessoryCue) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	color := c.Color
	if color == "" {
		color = "#5e5ce6"
	}
	posted, ok := g.request(ctx, "POST", "/v1/signal", map[string]any{"color": color, "pattern": "breathe", "count": 1, "hold": -1, "source": "limmr", "message": "Test light"})
	g.running = ok
	if !ok {
		return false
	}
	g.generation++
	generation := g.generation
	clear := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if ctx.Err() != nil || generation != g.generation {
			return
		}
		state, running := g.request(ctx, "GET", "/v1/signals", nil)
		g.running = running
		active, _ := state["active"].(map[string]any)
		if running && posted["id"] != nil && activeSource(state) == "limmr" && active["id"] == posted["id"] {
			_, g.running = g.request(ctx, "DELETE", "/v1/signal", nil)
		}
	}
	if g.after != nil {
		g.after(5*time.Second, clear)
	} else {
		time.AfterFunc(5*time.Second, clear)
	}
	return true
}
