package integration_test

import (
	"context"
	"encoding/json"
	"limmr/internal/efforts"
	"limmr/internal/watches"
	"reflect"
	"testing"
	"time"
)

func TestP9HomeBriefUpdatesDuringWarmup(t *testing.T) {
	r := newRig(t)
	setupWatches(t, r)
	c := circleFixture(t, r, "university", "confirm", false, chatA)
	checkHome := func(ready bool) []efforts.CircleUpdate {
		t.Helper()
		start := time.Now()
		w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var v struct {
			Ready   bool                   `json:"efforts_ready"`
			Updates []efforts.CircleUpdate `json:"circle_updates"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &v))
		if v.Ready != ready {
			t.Fatalf("home readiness=%v want=%v", v.Ready, ready)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("home blocked: %s", elapsed)
		}
		return v.Updates
	}
	if len(checkHome(false)) != 0 {
		t.Fatal("cold updates exposed")
	}
	start := time.Now()
	brief, err := r.call(t, "brief", map[string]any{"action": "get", "id": c.ID})
	must(t, err)
	if brief.(map[string]any)["efforts_ready"] != false {
		t.Fatal("cold brief inferred efforts")
	}
	v, err := r.call(t, "get_updates", map[string]any{"wait_seconds": 0})
	must(t, err)
	if v.(watches.Updates).EffortsReady {
		t.Fatal("cold updates inferred efforts")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("hints blocked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.mc.EffortService().RunWarmup(ctx, nil, nil) }()
	deadline := time.Now().Add(3 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		_, ready, err = r.mc.EffortService().ReadyCircleUpdates()
		must(t, err)
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !ready {
		t.Fatal("background warmup did not complete")
	}
	want, err := r.mc.EffortService().CircleUpdates()
	must(t, err)
	if got := checkHome(true); !reflect.DeepEqual(got, want) {
		t.Fatal("warm Home differs from synchronous effort result")
	}
	// Both paths see the same completed naming state. Rewarm after each audited
	// call because get_updates/brief intentionally record a new activity row.
	brief, err = r.call(t, "brief", map[string]any{"action": "get", "id": c.ID})
	must(t, err)
	if brief.(map[string]any)["efforts_ready"] != true {
		t.Fatal("warm brief unavailable")
	}
	_, err = r.mc.EffortService().List(nil, efforts.Filter{})
	must(t, err)
	v, err = r.call(t, "get_updates", map[string]any{"wait_seconds": 0})
	must(t, err)
	if !v.(watches.Updates).EffortsReady {
		t.Fatal("warm updates unavailable")
	}
}
