package perf_test

import (
	"context"
	"encoding/json"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"sync/atomic"
	"testing"
	"time"
)

// Hold the background worker at its first cooperative checkpoint. Home must
// finish while inference is still in flight, including cold people/circle work.
func TestP9P8HomeDuringWarmup(t *testing.T) {
	home := p8Fixture(t)
	for sample := 0; sample < 3; sample++ {
		func() {
			r := p8Open(t, home)
			defer r.close()
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			var calls atomic.Int32
			go func() {
				defer close(done)
				r.mc.EffortService().RunWarmup(ctx, func() bool {
					if calls.Add(1) == 2 {
						close(entered)
						<-release
					}
					return false
				}, nil)
			}()
			defer func() { close(release); cancel(); <-done }()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("warmup did not reach checkpoint")
			}
			response := make(chan []byte, 1)
			start := time.Now()
			go func() { response <- r.call(t, 0) }()
			var raw []byte
			select {
			case raw = <-response:
			case <-time.After(time.Second):
				t.Fatal("Home waited for blocked warmup")
			}
			elapsed := time.Since(start)
			var value struct {
				Ready   bool                   `json:"efforts_ready"`
				Updates []efforts.CircleUpdate `json:"circle_updates"`
			}
			testkit.Must(t, json.Unmarshal(raw, &value))
			if value.Ready || len(value.Updates) != 0 {
				t.Fatal("incomplete effort data exposed")
			}
			t.Logf("sample=%d cold_home_during_warmup_ms=%.3f", sample, float64(elapsed)/float64(time.Millisecond))
		}()
	}
}

// Baseline-compatible P8 incremental harness. Reopen stores and compute outside
// the timer, then append one chronological message and measure the exact route.
func TestP9P8IncrementalTimings(t *testing.T) {
	home := p8Fixture(t)
	for sample := 0; sample < 3; sample++ {
		r := p8Open(t, home)
		r.call(t, 4)
		m := model.Message{ID: "zz-p9-arrival", ChatJID: "100000@g.us", Sender: "15550000001@s.whatsapp.net", Content: "Synthetic design planning garden sketches project", Timestamp: testkit.Epoch}
		testkit.Must(t, r.h.Messages.PutMessage(m))
		start := time.Now()
		r.call(t, 4)
		t.Logf("sample=%d incremental_ms=%.3f", sample, float64(time.Since(start))/float64(time.Millisecond))
		_, err := r.h.Messages.DB.Exec("DELETE FROM messages WHERE id=?", m.ID)
		testkit.Must(t, err)
		r.close()
	}
}
