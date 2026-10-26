package api

import (
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/watches"
	"net/http"
)

func (s *Server) watchRoutes(add func(string, string, route)) {
	add("GET /v1/watches", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.MCP.Watches == nil {
			return map[string]any{"watches": []watches.Summary{}}, nil
		}
		xs, err := s.MCP.Watches.List(r.Context(), false)
		return map[string]any{"watches": xs}, err
	})
	add("GET /v1/watches/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.MCP.Watches == nil {
			return nil, model.ErrNotFound
		}
		return s.MCP.Watches.Detail(r.Context(), r.PathValue("id"))
	})
	add("POST /v1/watches/{id}/instructions", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.MCP.Watches == nil {
			return nil, model.ErrNotFound
		}
		var b struct {
			Text string `json:"text"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		if _, err := s.MCP.Watches.Record(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		if err := s.MCP.Watches.Instruction(r.Context(), r.PathValue("id"), b.Text); err != nil {
			return nil, err
		}
		return s.MCP.Watches.Detail(r.Context(), r.PathValue("id"))
	})

	statusRoute := func(status triggers.Status) route {
		return func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			if s.MCP.Watches == nil {
				return nil, model.ErrNotFound
			}
			if err := s.MCP.Watches.Status(r.Context(), r.PathValue("id"), status); err != nil {
				return nil, err
			}
			d, err := s.MCP.Watches.Detail(r.Context(), r.PathValue("id"))
			return d.Summary, err
		}
	}
	add("POST /v1/watches/{id}/pause", "admin", statusRoute(triggers.Paused))
	add("POST /v1/watches/{id}/resume", "admin", statusRoute(triggers.Active))
	add("POST /v1/watches/{id}/stop", "admin", statusRoute(triggers.Stopped))

	add("POST /v1/watches/{id}/seen", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.MCP.Watches == nil {
			return nil, model.ErrNotFound
		}
		if _, err := s.MCP.Watches.Record(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		if err := s.MCP.Watches.Store.Seen(r.Context(), r.PathValue("id")); err != nil {
			return nil, err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil, nil
	})
}
