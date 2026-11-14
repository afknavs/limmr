package health

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
	"testing"
	"time"
)

func TestU2ConnectionPhaseTimestampsAndUnknownRemote(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	s := &Service{Hub: h, Link: link, Home: h.Home, StartedAt: now}
	for _, phase := range []struct{ state, want string }{{"connecting", "connecting"}, {"ready", "connected"}, {"error", "logged_out"}} {
		now = now.Add(time.Second)
		link.Emit(wa.LinkEvent{State: phase.state, StateAt: now})
		got := s.ConnectionStatus()
		if got.LinkPhase != phase.want || !got.LinkPhaseAt.Equal(now) || !got.EngineStartedAt.Equal(testkit.Epoch) {
			t.Fatal("bad phase facts", got)
		}
		now = now.Add(time.Second)
		if !s.ConnectionStatus().LinkPhaseAt.Equal(got.LinkPhaseAt) {
			t.Fatal("phase timestamp reset on poll")
		}
	}
	s.RemoteCheck = func(ctx context.Context) []model.Check {
		if dst, ok := ctx.Value(discoveryResultKey{}).(**DiscoveryResult); ok {
			*dst = &DiscoveryResult{}
		}
		return []model.Check{{ID: "remote", State: "fail", FixID: "restart_remote"}}
	}
	for _, c := range s.storeChecks(context.Background()) {
		if c.ID == "remote" && (c.State != "checking" || c.FixID != "") {
			t.Fatal("unprobed remote was a warning", c)
		}
	}
}
