package startup

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// State publishes the full API atomically, after all migrations and warmups.
// Its health path never takes a database lock, even during a long backfill.
type State struct {
	mu              sync.RWMutex
	stage           string
	progress        uint64
	handler         http.Handler
	logger          *log.Logger
	port            int
	control, status string
}

func New(port int, control, status string, logger *log.Logger) *State {
	return &State{stage: "stores", port: port, control: control, status: status, logger: logger}
}
func (s *State) Observe(stage string, elapsed time.Duration) {
	if elapsed == 0 {
		s.mu.Lock()
		s.stage = stage
		s.progress++
		s.mu.Unlock()
	} else if s.logger != nil {
		s.logger.Printf("INFO startup stage=%s duration_ms=%.3f", stage, float64(elapsed)/float64(time.Millisecond))
	}
}
func (s *State) Step(stage string, work func() error) error { return Measure(s.Observe, stage, work) }
func (s *State) Ready(handler http.Handler)                 { s.mu.Lock(); defer s.mu.Unlock(); s.handler = handler }
func (s *State) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler, stage, progress := s.handler, s.stage, s.progress
	s.mu.RUnlock()
	if handler != nil {
		handler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if (r.Host != fmt.Sprintf("127.0.0.1:%d", s.port) && r.Host != fmt.Sprintf("localhost:%d", s.port)) || hasOrigin(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	auth := r.Header.Values("Authorization")
	control, status := false, false
	if len(auth) == 1 && strings.HasPrefix(auth[0], "Bearer ") {
		token := strings.TrimPrefix(auth[0], "Bearer ")
		control = s.control != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.control)) == 1
		status = s.status != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.status)) == 1
	}
	// MCP credentials cannot be resolved safely while identity migrations are running.
	// A retryable 503 contains only fixed stage metadata; it grants no capabilities.
	if !control && !status && r.URL.Path != "/mcp" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == "GET" && control && r.URL.Path == "/v1/health" {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "starting", "stage": stage, "progress": progress})
		return
	}
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "starting", "stage": stage, "progress": progress, "error": map[string]string{"code": "starting", "message": "Limmr is starting. Please wait."}})
}

// Header presence matters even when its value is empty.
func hasOrigin(r *http.Request) bool { _, ok := r.Header["Origin"]; return ok }
