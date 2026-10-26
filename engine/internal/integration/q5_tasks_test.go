package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/demo"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Regenerate with LIMMR_UPDATE_TASK_FIXTURES=1. Only random object IDs and
// ID-dependent array order are canonicalized; field names, values, nulls and
// envelopes come from the actual handlers, never hand-authored JSON.
func TestQ5SharedTaskFixtures(t *testing.T) {
	t.Run("workspace", func(t *testing.T) { q5SharedTaskFixtures(t, false) })
	t.Run("surfacing", func(t *testing.T) { q5SharedTaskFixtures(t, true) })
}

func q5SharedTaskFixtures(t *testing.T, surfacing bool) {
	r := newRig(t)
	must(t, demo.Seed(r.h, r.p, r.link))
	svc := r.mc.TaskService()
	svc.Location = time.UTC
	if !surfacing {
		// Preserve the four-workspace Swift contract fixtures. Exercise the full
		// demo, including Y3 suggestions, in a separate set of handler responses.
		must(t, r.h.Repo.Put("demo_meta", "task_surfacing_v1", true))
	}
	must(t, demo.SeedTasks(svc))
	xs, err := svc.List(nil, "all", 100)
	must(t, err)
	names := map[string]string{}
	replacements := []string{}
	keys := map[string]string{
		"Maya's study plan":                  "study",
		"Book a dentist appointment":         "short",
		"Wait for the project syllabus":      "waiting",
		"Print the revision guide":           "guide",
		"Prepare the fundraiser application": "application",
		"Review the family CV":               "cv",
	}
	for _, x := range xs {
		key, ok := keys[x.Title]
		if !ok || names[key] != "" {
			t.Fatalf("unexpected or duplicate demo task: %q", x.Title)
		}
		names[key] = x.ID
	}
	want := 3
	if surfacing {
		want = 6
	}
	if len(names) != want || names["study"] == "" || names["short"] == "" || names["waiting"] == "" {
		t.Fatalf("demo task identities: got %d, want %d", len(names), want)
	}
	if surfacing {
		for _, key := range []string{"guide", "application", "cv"} {
			v, err := svc.Get(nil, names[key])
			must(t, err)
			if v.Suggestion == nil || len(v.Suggestion.Signals) == 0 || v.NeedsYou {
				t.Fatalf("%s lost its surfacing metadata", key)
			}
			if key == "cv" {
				if v.Status != "active" || v.Origin != "agent" || v.CreatedBy.AgentID != "agent-2" || v.AutoKeptAt == nil || v.UndoChangeID == "" {
					t.Fatal("auto-kept task lost its provenance or Undo")
				}
			} else if v.Status != "suggested" || v.Origin != "limmr" || v.AutoKeptAt != nil || v.UndoChangeID != "" {
				t.Fatalf("%s must remain an unkept Limmr suggestion", key)
			}
		}
	}
	es, err := r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if len(es) == 0 {
		t.Fatal("no seeded effort")
	}
	linked, err := svc.Create(nil, tasks.Create{Title: "Track festival review", Kind: "long", EffortID: es[0].ID})
	must(t, err)
	names["linked"] = linked.ID
	study, err := svc.Get(nil, names["study"])
	must(t, err)
	day := int(demo.Epoch.Weekday())
	status(t, r.request(t, "PATCH", "/v1/tasks/"+study.ID, r.api.ControlToken, map[string]any{
		"summary":  map[string]any{"text": "Maya has started revision."},
		"check_in": tasks.CheckIn{Rule: "weekly", Weekday: &day, AtTime: "12:00", To: demo.DM(4), Note: "Learning progress", NextAt: demo.Epoch},
	}, nil), 200)
	_, err = svc.Draft(nil, study.ID, "check_in", "Hi Maya, your revision notes are ready to review.")
	must(t, err)
	status(t, r.request(t, "POST", "/v1/tasks/"+study.ID+"/items", r.api.ControlToken, map[string]any{"kind": "note", "title": "Next session", "text": "Review memory exercises"}, nil), 200)
	must(t, svc.Tick(context.Background()))
	settings := r.h.Settings()
	settings.AccessoryCue = model.AccessoryCue{Color: "#5e5ce6", QuietHours: &model.TaskQuietHours{Start: "23:00", End: "08:00", Timezone: "Asia/Kolkata"}}
	must(t, r.h.SetSettings(settings))
	for key := range names {
		v, err := svc.Get(nil, names[key])
		must(t, err)
		replacements = append(replacements, v.ID, "tsk_demo_"+key)
		if v.UndoChangeID != "" {
			replacements = append(replacements, v.UndoChangeID, "chg_demo_"+key)
		}
		for i, step := range v.Checklist {
			replacements = append(replacements, step.ID, fmt.Sprintf("stp_%s_%d", key, i))
		}
		for i, item := range v.Items {
			replacements = append(replacements, item.ID, fmt.Sprintf("item_%s_%d", key, i))
		}
	}
	replacer := strings.NewReplacer(replacements...)
	routes := map[string]string{
		"detail": "/v1/tasks/" + names["study"], "waiting": "/v1/tasks/" + names["waiting"], "short": "/v1/tasks/" + names["short"],
		"list": "/v1/tasks", "home": "/v1/home", "suggestion": "/v1/suggestions", "settings": "/v1/settings", "feed": "/v1/tasks/feed", "effort": "/v1/efforts/" + linked.EffortID,
	}
	if surfacing {
		routes = map[string]string{
			"surfaced-home": "/v1/home", "surfaced-list": "/v1/tasks?status=all", "surfaced-feed": "/v1/tasks/feed",
			"suggested": "/v1/tasks?status=suggested", "suggested-detail": "/v1/tasks/" + names["guide"], "auto-kept": "/v1/tasks/" + names["cv"],
		}
	}
	for name, path := range routes {
		w := r.request(t, "GET", path, r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var value any
		must(t, json.Unmarshal([]byte(replacer.Replace(w.Body.String())), &value))
		obj, ok := value.(map[string]any)
		if !ok {
			t.Fatal("response envelope")
		}
		if name == "home" || name == "surfaced-home" {
			obj = map[string]any{"tasks": obj["tasks"], "queued": obj["queued"]}
			value = obj
		}
		// Keep only task suggestions in the shared Home/suggestion slices.
		for _, field := range []string{"queued", "suggestions"} {
			if values, ok := obj[field].([]any); ok {
				filtered := []any{}
				for _, v := range values {
					if v.(map[string]any)["task_id"] != nil {
						filtered = append(filtered, v)
					}
				}
				obj[field] = filtered
			}
		}
		if values, ok := obj["tasks"].([]any); ok {
			for _, v := range values {
				if events, ok := v.(map[string]any)["events"].([]any); ok {
					sort.Slice(events, func(i, j int) bool {
						a, _ := json.Marshal(events[i])
						b, _ := json.Marshal(events[j])
						return string(a) < string(b)
					})
				}
			}
		}
		for _, field := range []string{"tasks", "queued", "suggestions"} {
			if values, ok := obj[field].([]any); ok {
				sort.Slice(values, func(i, j int) bool {
					return values[i].(map[string]any)["id"].(string) < values[j].(map[string]any)["id"].(string)
				})
			}
		}
		raw, err := json.MarshalIndent(value, "", "  ")
		must(t, err)
		raw = append(raw, '\n')
		file := filepath.Join("../../../app/Tests/LimmrKitTests/Fixtures/tasks", name+".json")
		if os.Getenv("LIMMR_UPDATE_TASK_FIXTURES") == "1" {
			must(t, os.MkdirAll(filepath.Dir(file), 0755))
			must(t, os.WriteFile(file, raw, 0644))
		}
		committed, err := os.ReadFile(file)
		must(t, err)
		if !bytes.Equal(raw, committed) {
			t.Errorf("%s is stale; regenerate with LIMMR_UPDATE_TASK_FIXTURES=1", file)
		}
	}
}

func TestQ5TaskPatchClearsAndChecklistRoutes(t *testing.T) {
	r := newRig(t)
	v := newTask(t, r)
	day := int(epoch.Weekday())
	status(t, r.request(t, "PATCH", "/v1/tasks/"+v.ID, r.api.ControlToken, map[string]any{"check_in": tasks.CheckIn{Rule: "weekly", Weekday: &day, AtTime: "12:00", To: chatA, NextAt: epoch.Add(time.Hour)}, "waiting": tasks.Waiting{On: &v.People[0], What: "notes", Since: epoch.AddDate(0, 0, -4)}}, nil), 200)
	must(t, r.mc.TaskService().Tick(context.Background()))
	status(t, r.request(t, "POST", "/v1/tasks/"+v.ID+"/nudge/skip", r.api.ControlToken, nil, nil), 200)
	got, err := r.mc.TaskService().Get(nil, v.ID)
	must(t, err)
	if got.Waiting.NudgeState != "skipped" || got.NeedsYou {
		t.Fatal("skip did not settle attention")
	}
	w := r.request(t, "PATCH", "/v1/tasks/"+v.ID, r.api.ControlToken, map[string]any{"check_in": nil, "waiting": nil}, nil)
	status(t, w, 200)
	got = tasks.Task{}
	must(t, json.Unmarshal(w.Body.Bytes(), &got))
	if got.CheckIn != nil || got.Waiting != nil || got.Status != "active" {
		t.Fatal("null did not clear schedules/waiting")
	}
	status(t, r.request(t, "PATCH", "/v1/tasks/"+v.ID+"/checklist/"+v.Checklist[0].ID, r.api.ControlToken, map[string]any{"text": "Review notes", "order": 0, "done": true}, nil), 200)
	status(t, r.request(t, "DELETE", "/v1/tasks/"+v.ID+"/checklist/"+v.Checklist[0].ID, r.api.ControlToken, nil, nil), 200)
}

type q5Transport func(*http.Request) (*http.Response, error)

func (f q5Transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestQ5AccessoryTestControlOnlyAndDownResponse(t *testing.T) {
	r := newRig(t)
	path := "/v1/settings/accessory-cue/test"
	for _, token := range []string{r.api.StatusToken, r.token} {
		status(t, r.request(t, "POST", path, token, nil, nil), 403)
	}
	w := r.request(t, "POST", path, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"ok":false,"running":false}` {
		t.Fatal("unavailable response")
	}
	tokenPath := filepath.Join(t.TempDir(), "token")
	must(t, os.WriteFile(tokenPath, []byte("invented-credential"), 0600))
	posts := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.api.Context = ctx
	r.mc.TaskService().Accessory = &tasks.Accessory{URL: "http://fake-accessory.invalid", TokenPath: tokenPath, Client: &http.Client{Transport: q5Transport(func(req *http.Request) (*http.Response, error) {
		if req.Method != "POST" || req.URL.String() != "http://fake-accessory.invalid/v1/signal" {
			t.Fatal("unknown fake route")
		}
		posts++
		w := httptest.NewRecorder()
		w.WriteString(`{"ok":true,"id":1}`)
		return w.Result(), nil
	})}}
	w = r.request(t, "POST", path, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"ok":true,"running":true}` || posts != 1 {
		t.Fatal("test route did not fire")
	}
	r.mc.TaskService().Accessory.Client = &http.Client{Transport: q5Transport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "fake-accessory.invalid" {
			t.Fatal("unexpected endpoint")
		}
		return nil, errors.New("invented offline error")
	})}
	w = r.request(t, "POST", path, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"ok":false,"running":false}` {
		t.Fatal("offline Accessory response")
	}
}

func TestQ5HomeIncludesCheckInWithoutProse(t *testing.T) {
	r := newRig(t)
	v := newTask(t, r)
	_, err := r.mc.TaskService().Update(nil, v.ID, map[string]any{"check_in": tasks.CheckIn{Rule: "once", AtTime: "12:00", To: chatA, NextAt: epoch}})
	must(t, err)
	must(t, r.mc.TaskService().Tick(context.Background()))
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var home struct {
		Queued []model.Suggestion `json:"queued"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if len(home.Queued) != 1 || home.Queued[0].TaskID != v.ID || home.Queued[0].Kind != "check_in" || home.Queued[0].DraftText != "" {
		t.Fatal("Home dropped a task cue awaiting prose")
	}
}
