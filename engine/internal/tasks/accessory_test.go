package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"limmr/internal/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAccessoryFakeHTTPServerQuietRateOwnershipAndDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("invented-test-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	posts, deletes, gets := 0, 0, 0
	source := ""
	down := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer invented-test-credential" {
			t.Fatal("wrong fake request path or credential")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/signal":
			posts++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, present := body["sound"]; present {
				t.Fatal("cue must omit sound")
			}
			if _, present := body["name"]; present {
				t.Fatal("cue must be custom")
			}
			if body["source"] != "limmr" || body["pattern"] != "breathe" || body["sound"] != nil || body["name"] != nil || body["hold"] != float64(-1) {
				t.Fatal("wrong cue payload")
			}
			source = "limmr"
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": posts, "targets": []string{"strip"}})
		case "GET /v1/signals":
			gets++
			var active any
			if source != "" {
				active = map[string]any{"source": source, "id": posts}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": active, "recent": []any{}, "definitions": map[string]any{}, "mute": nil})
		case "DELETE /v1/signal":
			deletes++
			source = ""
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cleared": true})
		default:
			t.Fatalf("unknown fake route: %s %s", r.Method, r.URL.Path)
		}
	})
	// An actual HTTP handler behind an injected transport needs no loopback bind.
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "fake-accessory.invalid" {
			t.Fatal("unexpected endpoint")
		}
		if down {
			return nil, errors.New("fake offline")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	g := &Accessory{URL: "http://fake-accessory.invalid", TokenPath: path, Client: client}
	ctx := context.Background()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	xs := []record{{Task: Task{ID: "a", Title: "Study plan", NeedsYou: true}}}
	g.Sync(ctx, xs, model.AccessoryCue{}, now)
	if posts != 0 {
		t.Fatal("off setting called Accessory")
	}
	config := model.AccessoryCue{Enabled: true}
	g.Sync(ctx, xs, config, now)
	g.Sync(ctx, xs, config, now.Add(time.Minute))
	if posts != 1 {
		t.Fatal("steady need cued more than once")
	}
	xs = append(xs, record{Task: Task{ID: "b", Title: "Appointment", NeedsYou: true}})
	g.Sync(ctx, xs, config, now.Add(9*time.Minute))
	if posts != 1 {
		t.Fatal("rate limit ignored")
	}
	g.Sync(ctx, xs, config, now.Add(10*time.Minute))
	if posts != 2 {
		t.Fatal("pending need did not cue after rate window")
	}
	source = "other-app"
	g.Sync(ctx, nil, config, now.Add(11*time.Minute))
	if deletes != 0 || gets != 1 {
		t.Fatal("cleared another app's cue")
	}
	g.Sync(ctx, xs, config, now.Add(21*time.Minute))
	g.Sync(ctx, nil, config, now.Add(22*time.Minute))
	if deletes != 1 {
		t.Fatal("did not clear own cue")
	}
	before := posts
	night := time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC)
	g.Sync(ctx, xs, config, night)
	if posts != before {
		t.Fatal("quiet hours ignored")
	}
	g.Sync(ctx, xs, config, night.Add(9*time.Hour))
	if posts != before+1 {
		t.Fatal("quiet need not deferred")
	}
	if c := g.Check(true); c.Detail != "Accessory cue: on" {
		t.Fatal("missing on health")
	}
	g.Sync(ctx, nil, config, night.Add(10*time.Hour))
	down = true
	g.Sync(ctx, xs, config, night.Add(11*time.Hour))
	if c := g.Check(true); c.Detail != "Accessory not running" {
		t.Fatal("down health missing")
	}
	// Error bodies and credentials never escape the client.
	g.Client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("private fake response")), Header: http.Header{}}, nil
	})}
	g.Sync(ctx, xs, config, night.Add(12*time.Hour))
}

func TestAccessoryRateAndSeenSurviveRestart(t *testing.T) {
	s, _, now := fixture(t)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("invented-test-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	posts := 0
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "fake-accessory.invalid" {
			t.Fatal("unexpected endpoint")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/signal" {
			t.Fatal("unknown fake route")
		}
		if r.Method == "POST" {
			posts++
		}
		w := httptest.NewRecorder()
		w.WriteString(`{"source":"limmr"}`)
		return w.Result(), nil
	})}
	makeClient := func() *Accessory {
		return &Accessory{State: s.Hub.Repo, URL: "http://fake-accessory.invalid", TokenPath: path, Client: client}
	}
	config := model.AccessoryCue{Enabled: true}
	xs := []record{{Task: Task{ID: "a", Title: "Study", NeedsYou: true}}}
	g := makeClient()
	g.Sync(context.Background(), xs, config, *now)
	g = makeClient()
	g.Sync(context.Background(), xs, config, now.Add(time.Minute))
	if posts != 1 {
		t.Fatal("restart repeated stable cue")
	}
	xs = append(xs, record{Task: Task{ID: "b", Title: "Book appointment", NeedsYou: true}})
	g.Sync(context.Background(), xs, config, now.Add(9*time.Minute))
	if posts != 1 {
		t.Fatal("restart lost throttle")
	}
	g.Sync(context.Background(), xs, config, now.Add(10*time.Minute))
	if posts != 2 {
		t.Fatal("pending new cue not delivered")
	}
}

func TestAccessoryTestSilentSavedColorAndDelayedOwnership(t *testing.T) {
	for _, replacement := range []string{"same", "other", "newer-limmr", "null", "down"} {
		t.Run(replacement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token")
			if err := os.WriteFile(path, []byte("invented-test-credential"), 0600); err != nil {
				t.Fatal(err)
			}
			var clear func()
			posts, deletes := 0, 0
			active := any(map[string]any{"source": "limmr", "id": 1})
			g := &Accessory{URL: "http://fake-accessory.invalid", TokenPath: path, after: func(delay time.Duration, f func()) {
				if delay != 5*time.Second {
					t.Fatal("wrong clear delay")
				}
				clear = f
			}}
			g.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "fake-accessory.invalid" {
					t.Fatal("unexpected endpoint")
				}
				w := httptest.NewRecorder()
				switch r.Method + " " + r.URL.Path {
				case "POST /v1/signal":
					posts++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if _, present := body["sound"]; present {
						t.Fatal("test must omit sound")
					}
					if _, present := body["name"]; present {
						t.Fatal("test must be custom")
					}
					if body["color"] != "#abcdef" || body["source"] != "limmr" || body["pattern"] != "breathe" {
						t.Fatal("wrong test cue")
					}
					w.WriteString(`{"ok":true,"id":1}`)
				case "GET /v1/signals":
					if replacement == "down" {
						return nil, errors.New("fake down")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"active": active})
				case "DELETE /v1/signal":
					deletes++
					w.WriteString(`{"ok":true}`)
				default:
					t.Fatalf("unknown fake route %s %s", r.Method, r.URL.Path)
				}
				return w.Result(), nil
			})}
			// Disabled, quiet and just rate limited still permit Test.
			g.last = time.Now()
			last := g.last
			if !g.Test(context.Background(), model.AccessoryCue{Color: "#abcdef", QuietHours: &model.TaskQuietHours{Start: "00:00", End: "23:59"}}) || posts != 1 || clear == nil {
				t.Fatal("test did not fire")
			}
			if g.last != last || g.own || len(g.seen) != 0 {
				t.Fatal("test consumed attention state")
			}
			switch replacement {
			case "other":
				active = map[string]any{"source": "other-app", "id": 2}
			case "newer-limmr":
				active = map[string]any{"source": "limmr", "id": 2}
			case "null":
				active = nil
			}
			clear()
			if (deletes == 1) != (replacement == "same") {
				t.Fatal("cleared a replacement cue or left own test cue")
			}
		})
	}
	g := &Accessory{URL: "http://fake-accessory.invalid", TokenPath: filepath.Join(t.TempDir(), "missing")}
	if g.Test(context.Background(), model.AccessoryCue{}) {
		t.Fatal("unavailable reported running")
	}
}
