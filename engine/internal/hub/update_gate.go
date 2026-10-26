package hub

import (
	"context"
	"limmr/internal/model"
	"sync"
	"time"
)

const UpdateDrainTimeout = 10 * time.Second
const UpdatePauseTTL = 5 * time.Minute

// Separate from the hub mutation lock: prepare and rejected send calls must never
// wait for a network send holding that lock. This gate is process-local, shared
// by scoped services, and deliberately never persisted across engine restarts.
type updateGate struct {
	mu         sync.Mutex
	until      time.Time
	active     int
	generation uint64
	changed    chan struct{}
}
type UpdatePause struct {
	Paused      bool `json:"paused"`
	ActiveSends int  `json:"active_sends"`
}

func (g *updateGate) signal() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}
func (g *updateGate) paused() bool { return time.Now().Before(g.until) }
func (s *Service) CheckUpdating() error {
	g := &s.shared().updates
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused() {
		return model.ErrUpdating
	}
	return nil
}
func (s *Service) beginOutbound() (func(), error) {
	g := &s.shared().updates
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused() {
		return nil, model.ErrUpdating
	}
	g.active++
	return func() { g.mu.Lock(); defer g.mu.Unlock(); g.active--; g.signal() }, nil
}
func (s *Service) PrepareUpdate(ctx context.Context) (UpdatePause, error) {
	return s.shared().updates.prepare(ctx, UpdateDrainTimeout, UpdatePauseTTL)
}
func (g *updateGate) prepare(ctx context.Context, drain, ttl time.Duration) (UpdatePause, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused() {
		return UpdatePause{ActiveSends: g.active}, model.ErrConflict
	}
	g.generation++
	generation := g.generation
	g.until = time.Now().Add(ttl)
	g.signal()
	timer := time.NewTimer(min(drain, ttl))
	defer timer.Stop()
	for {
		if ctx.Err() != nil || generation != g.generation || !g.paused() {
			break
		}
		if g.active == 0 {
			return UpdatePause{Paused: true}, nil
		}
		changed := g.changed
		g.mu.Unlock()
		timedOut := false
		select {
		case <-ctx.Done():
			timedOut = true
		case <-timer.C:
			timedOut = true
		case <-changed:
		}
		g.mu.Lock()
		if timedOut {
			break
		}
	}
	// A cancelled/older prepare cannot release a newer owner's pause.
	if generation == g.generation {
		g.until = time.Time{}
		g.signal()
	}
	return UpdatePause{ActiveSends: g.active}, nil
}
func (s *Service) ResumeUpdates() {
	g := &s.shared().updates
	g.mu.Lock()
	defer g.mu.Unlock()
	g.generation++
	g.until = time.Time{}
	g.signal()
}
