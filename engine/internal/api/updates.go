package api

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/watches"
	"net/http"
	"strings"
	"time"
)

func (s *Server) updateRoutes(add func(string, string, route)) {
	add("GET /v1/updates/capabilities", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"can_prepare": true}, nil
	})
	add("POST /v1/updates/prepare", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.Hub.PrepareUpdate(r.Context())
	})
	add("POST /v1/updates/resume", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		s.Hub.ResumeUpdates()
		return map[string]bool{"paused": false}, nil
	})
	add("GET /v1/waiting", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs := []watches.Waiting{}
		var err error
		if s.MCP.Watches != nil {
			xs, err = s.MCP.Watches.Waiting(r.Context())
		}
		return map[string]any{"waiting": xs}, err
	})
	add("GET /v1/updates/stream", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		// Control and status credentials are deliberately not agent identities.
		if p.agent == nil {
			return nil, model.ErrForbidden
		}
		if s.MCP.Watches == nil {
			return nil, model.ErrConflict
		}
		return nil, s.updateStream(w, r, p.agent.ID, 15*time.Second)
	})
}

func (s *Server) updateStream(w http.ResponseWriter, r *http.Request, agent string, heartbeat time.Duration) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return model.ErrConflict
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor == "" {
		cursor = r.Header.Get("Last-Event-ID")
	}
	out, err := s.MCP.Watches.GetUpdates(r.Context(), agent, cursor, 0, 100)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	flusher.Flush()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	beat := time.NewTicker(heartbeat)
	defer beat.Stop()
	send := func() bool {
		for i, item := range out.Items {
			// A page cursor covers the whole page. Emit it only with the final item,
			// so a broken connection replays rather than drops an incomplete page.
			b, e := json.Marshal(item)
			if e != nil {
				return false
			}
			id := ""
			if i == len(out.Items)-1 {
				id = "id: " + out.Cursor + "\n"
			}
			if _, e = fmt.Fprintf(w, "%sevent: update\ndata: %s\n\n", id, b); e != nil {
				return false
			}
			flusher.Flush()
		}
		cursor = out.Cursor
		return true
	}
	if !send() {
		return nil
	}
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-beat.C:
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return nil
			}
			flusher.Flush()
		case <-tick.C:
			current, e := s.Hub.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if e != nil || current.ID != agent {
				return nil
			}
			out, err = s.MCP.Watches.GetUpdates(r.Context(), agent, cursor, 0, 100)
			if err != nil {
				return nil
			} // Revocation/expiry closes the stream without private errors.
			if !send() {
				return nil
			}
		}
	}
}
