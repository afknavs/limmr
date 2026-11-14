package health

import "time"

var processStartedAt = time.Now()

type ConnectionStatus struct {
	EngineStartedAt time.Time `json:"engine_started_at"`
	LinkPhase       string    `json:"link_phase"`
	LinkPhaseAt     time.Time `json:"link_phase_at"`
}

// ConnectionStatus records observed phase transitions independently of slower
// database and remote probes. This does not start, reconnect, or send anything.
func (s *Service) ConnectionStatus() ConnectionStatus {
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	start := s.StartedAt
	if start.IsZero() {
		start = processStartedAt
	}
	now := time.Now()
	if s.Hub != nil && s.Hub.Now != nil {
		now = s.Hub.Now()
	}
	state := s.Link.State()
	linked := state.State == "ready" || state.State == "syncing" || state.State == "connecting"
	connected := state.State == "ready" || state.State == "syncing"
	if s.Connection != nil {
		linked, connected = s.Connection.Linked(), s.Connection.Connected()
	}
	phase := "disconnected"
	switch {
	case !linked:
		phase = "logged_out"
	case connected:
		phase = "connected"
	case state.State == "connecting":
		phase = "connecting"
	}
	if s.linkPhase != phase {
		s.linkPhase, s.linkPhaseAt = phase, now
		if !state.StateAt.IsZero() {
			s.linkPhaseAt = state.StateAt
		}
	}
	return ConnectionStatus{start, s.linkPhase, s.linkPhaseAt}
}
