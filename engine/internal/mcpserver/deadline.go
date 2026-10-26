package mcpserver

import (
	"context"
	"fmt"
	"limmr/internal/model"
	"log"
	"strings"
	"sync/atomic"
	"time"
)

const ToolTimeout = 20 * time.Second
const SlowToolThreshold = 2 * time.Second

type phaseKey struct{}
type callPhase struct{ value atomic.Value }

func phase(ctx context.Context, part string) {
	if p, ok := ctx.Value(phaseKey{}).(*callPhase); ok {
		p.value.Store(part)
	}
}

// Call bounds time spent waiting for locks, SQL, downloads and WhatsApp. Workers
// are capped; their scoped SQL/transactions and network calls share the deadline.
// A cancelled worker cannot resume later and commit a queued database mutation.
func (s *Service) Call(ctx context.Context, id, name string, args Args) (any, error) {
	started := time.Now()
	timeout := s.ToolCallTimeout
	if timeout <= 0 || timeout > ToolTimeout {
		timeout = ToolTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p := &callPhase{}
	p.value.Store("query")
	ctx = context.WithValue(ctx, phaseKey{}, p)
	safeName := "unknown"
	for _, t := range Catalog() {
		if t.Name == canonicalName(name) {
			safeName = t.Name
			break
		}
	}
	defer func() {
		if elapsed := time.Since(started); elapsed > SlowToolThreshold {
			logger := s.Logger
			if logger == nil {
				logger = log.Default()
			}
			logger.Printf("WARN slow MCP tool=%s duration_ms=%d part=%s", safeName, elapsed.Milliseconds(), p.value.Load().(string))
		}
	}()
	s.callOnce.Do(func() { s.callSlots = make(chan struct{}, 16) })
	select {
	case s.callSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, s.deadlineError(p.value.Load().(string))
	}
	type result struct {
		out any
		err error
	}
	done := make(chan result, 1)
	s.TaskService()
	h := s.Hub.WithContext(ctx)
	people := s.People.WithHub(h)
	scoped := &Service{Hub: h, People: people, Link: s.Link, Fetcher: s.Fetcher, RemoteMedia: s.RemoteMedia, Downloader: s.Downloader, Watches: s.Watches}
	scoped.taskOnce.Do(func() { scoped.taskService = s.TaskService().WithHub(h) })
	scoped.effortOnce.Do(func() { scoped.effortService = s.EffortService().WithHub(h) })
	scoped.circleOnce.Do(func() { scoped.circleService = s.CircleService().WithServices(h, people) })
	go func() {
		defer func() { <-s.callSlots }()
		out, err := scoped.call(ctx, id, name, args)
		// Domain validators deliberately use ErrInvalid without embedding values.
		// Retain their safe context and add the selected contract for correction.
		if err == model.ErrInvalid {
			for _, t := range append(Catalog(), LegacyCatalog()...) {
				if t.Name == name {
					err = invalidArgs(t, args)
					break
				}
			}
		}
		done <- result{out, err}
	}()
	select {
	case <-ctx.Done():
		return nil, s.deadlineError(p.value.Load().(string))
	case r := <-done:
		if ctx.Err() != nil {
			return nil, s.deadlineError(p.value.Load().(string))
		}
		return r.out, r.err
	}
}
func (s *Service) deadlineError(part string) error {
	advice := "The local query took too long. Retry with a smaller chat, circle or message limit; check Limmr Health if it repeats."
	if part == "bridge" {
		advice = "The bridge or attachment operation took too long. Check Limmr Health and connection, then retry."
		if s.Link == nil {
			advice = "WhatsApp is offline or unlinked. Open Limmr and reconnect, then check send_status before retrying a send."
		} else if _, connected, _ := s.Link.Connection(); !connected {
			advice = "WhatsApp is offline. Reconnect in Limmr, then check send_status before retrying a send."
		}
	}
	return fmt.Errorf("MCP deadline exceeded (%s). %s", strings.ToLower(part), advice)
}
