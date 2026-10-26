package hub

import (
	"context"
	"errors"
	"limmr/internal/model"
	"testing"
	"time"
)

func waitPaused(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(s.CheckUpdating(), model.ErrUpdating) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("prepare did not close admission")
}
func TestUpdateGateDrainAndScopedAdmission(t *testing.T) {
	s := &Service{}
	scoped := &Service{parent: s}
	done, err := scoped.beginOutbound()
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan UpdatePause, 1)
	go func() { p, _ := s.updates.prepare(context.Background(), time.Second, time.Minute); out <- p }()
	waitPaused(t, s)
	if _, err := scoped.beginOutbound(); err != model.ErrUpdating {
		t.Fatal("scoped send admitted")
	}
	select {
	case <-out:
		t.Fatal("prepare returned before drain")
	default:
	}
	done()
	select {
	case p := <-out:
		if !p.Paused || p.ActiveSends != 0 {
			t.Fatal(p)
		}
	case <-time.After(time.Second):
		t.Fatal("drain hung")
	}
	if _, err := s.PrepareUpdate(context.Background()); err != model.ErrConflict {
		t.Fatal("second prepare changed lease")
	}
	if s.CheckUpdating() != model.ErrUpdating {
		t.Fatal("second prepare released pause")
	}
	scoped.ResumeUpdates()
	s.ResumeUpdates()
	done, err = s.beginOutbound()
	if err != nil {
		t.Fatal(err)
	}
	done()
}
func TestUpdateGateTimeoutAndCancellationUnblock(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{true: "cancel", false: "timeout"}[cancelled], func(t *testing.T) {
			s := &Service{}
			done, _ := s.beginOutbound()
			defer done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			p, err := s.updates.prepare(ctx, 10*time.Millisecond, time.Minute)
			if err != nil || p.Paused || p.ActiveSends != 1 || s.CheckUpdating() != nil {
				t.Fatal("failed prepare left gate closed", p, err)
			}
			next, err := s.beginOutbound()
			if err != nil {
				t.Fatal(err)
			}
			next()
		})
	}
}
func TestUpdateGateExpiresAndRestartClears(t *testing.T) {
	s := &Service{}
	p, err := s.updates.prepare(context.Background(), time.Second, 15*time.Millisecond)
	if err != nil || !p.Paused {
		t.Fatal(p, err)
	}
	restarted := &Service{}
	if restarted.CheckUpdating() != nil {
		t.Fatal("pause persisted across restart")
	}
	time.Sleep(25 * time.Millisecond)
	done, err := s.beginOutbound()
	if err != nil {
		t.Fatal("lease did not expire", err)
	}
	done()
	p, err = s.PrepareUpdate(context.Background())
	if err != nil || !p.Paused {
		t.Fatal("cannot prepare after expiry")
	}
}
func TestUpdateGateResumeDuringDrain(t *testing.T) {
	s := &Service{}
	done, _ := s.beginOutbound()
	defer done()
	out := make(chan UpdatePause, 1)
	go func() { p, _ := s.PrepareUpdate(context.Background()); out <- p }()
	waitPaused(t, s)
	s.ResumeUpdates()
	select {
	case p := <-out:
		if p.Paused || p.ActiveSends != 1 {
			t.Fatal(p)
		}
	case <-time.After(time.Second):
		t.Fatal("resume did not wake prepare")
	}
}
