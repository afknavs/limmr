package api

import (
	"encoding/json"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/watches"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) circleRoutes(add func(string, string, route)) {
	c := s.MCP.CircleService()
	wrap := func(f func(*http.Request) (any, error)) route {
		return func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return f(r) }
	}
	add("GET /v1/home", "admin", wrap(func(r *http.Request) (any, error) {
		v, err := c.Home()
		if err != nil {
			return nil, err
		}
		xs := []watches.Summary{}
		if s.MCP.Watches != nil {
			xs, err = s.MCP.Watches.List(r.Context(), true)
			if err != nil {
				return nil, err
			}
		}
		out := v.(map[string]any)
		out["watches"] = xs
		taskSummaries, err := s.MCP.TaskService().List(nil, "open", 6)
		if err != nil {
			return nil, err
		}
		suggested, err := s.MCP.TaskService().List(nil, "suggested", 3)
		if err != nil {
			return nil, err
		}
		out["tasks"] = append(taskSummaries, suggested...)
		waiting := []watches.Waiting{}
		if s.MCP.Watches != nil {
			waiting, err = s.MCP.Watches.Waiting(r.Context())
			if err != nil {
				return nil, err
			}
		}
		out["waiting"] = waiting
		updates, ready, err := s.MCP.EffortService().ReadyCircleUpdates()
		if err != nil {
			return nil, err
		}
		out["circle_updates"] = updates
		out["efforts_ready"] = ready
		return out, nil
	}))
	add("GET /v1/map", "admin", wrap(func(r *http.Request) (any, error) {
		if focus := r.URL.Query().Get("focus"); focus != "" {
			return c.MapFocus(focus)
		}
		since := time.Time{}
		if raw, ok := r.URL.Query()["since"]; ok {
			if len(raw) != 1 {
				return nil, model.ErrInvalid
			}
			if strings.HasPrefix(raw[0], "p1.") {
				return c.MapDelta(raw[0])
			}
			var e error
			since, e = time.Parse(time.RFC3339Nano, raw[0])
			if e != nil {
				return nil, model.ErrInvalid
			}
		}
		return c.MapSince(since)
	}))
	add("GET /v1/presence", "admin", wrap(func(r *http.Request) (any, error) { return c.Presence() }))
	add("POST /v1/circles/reset", "admin", wrap(func(r *http.Request) (any, error) {
		var b struct {
			Confirm bool `json:"confirm"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if !b.Confirm {
			return nil, model.ErrInvalid
		}
		return map[string]bool{"ok": true}, c.Reset()
	}))
	add("GET /v1/circles", "admin", wrap(func(r *http.Request) (any, error) { xs, e := c.List(nil); return map[string]any{"circles": xs}, e }))
	add("POST /v1/circles", "admin", wrap(func(r *http.Request) (any, error) {
		var v model.Circle
		if e := decode(r, &v); e != nil {
			return nil, e
		}
		return c.Create(nil, v, "Created by you.")
	}))
	add("GET /v1/circles/{id}", "admin", wrap(func(r *http.Request) (any, error) { return c.Circle(nil, r.PathValue("id")) }))

	add("PATCH /v1/circles/{id}", "admin", wrap(func(r *http.Request) (any, error) {
		var raw map[string]json.RawMessage
		if e := decode(r, &raw); e != nil {
			return nil, e
		}
		return c.Mutate(r.PathValue("id"), func(v *model.Circle) error {
			return circles.Patch(v, raw, "archived", "name", "kind", "symbol", "description", "send_rule", "pinned")
		})
	}))
	add("DELETE /v1/circles/{id}", "admin", wrap(func(r *http.Request) (any, error) { return map[string]bool{"ok": true}, c.Delete(r.PathValue("id")) }))
	add("POST /v1/circles/{id}/confirm", "admin", wrap(func(r *http.Request) (any, error) {
		var b struct{}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return c.Confirm(r.PathValue("id"))
	}))
	add("POST /v1/circles/{id}/asks/{ask_id}/done", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if e := c.DoneAsk(r.PathValue("id"), r.PathValue("ask_id")); e != nil {
			return nil, e
		}
		w.WriteHeader(http.StatusNoContent)
		return nil, nil
	})
	add("GET /v1/circles/{id}/briefs", "admin", wrap(func(r *http.Request) (any, error) {
		xs, e := c.Briefs(nil, r.PathValue("id"))
		return map[string]any{"briefs": xs}, e
	}))
	add("POST /v1/circles/{id}/briefs/{version}/revert", "admin", wrap(func(r *http.Request) (any, error) {
		var b struct{}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		v, e := strconv.Atoi(r.PathValue("version"))
		if e != nil || v < 1 {
			return nil, model.ErrInvalid
		}
		return c.Revert(r.PathValue("id"), v)
	}))
	for _, kind := range []string{"chats", "people", "topics"} {
		for _, method := range []string{"POST", "PATCH", "DELETE"} {
			path := " /v1/circles/{id}/" + kind
			if method != "POST" {
				path += "/{member}"
			}
			add(method+path, "admin", wrap(func(r *http.Request) (any, error) {
				raw := map[string]json.RawMessage{}
				if method != "DELETE" {
					if e := decode(r, &raw); e != nil {
						return nil, e
					}
				}
				return c.Mutate(r.PathValue("id"), func(v *model.Circle) error { return c.EditMember(v, kind, method, r.PathValue("member"), raw) })
			}))
		}
	}
	add("GET /v1/suggestions", "admin", wrap(func(r *http.Request) (any, error) {
		xs, e := s.Hub.Suggestions()
		return map[string]any{"suggestions": xs}, e
	}))
	add("POST /v1/suggestions/{id}/send", "admin", wrap(func(r *http.Request) (any, error) {
		var b hub.SuggestionSend
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.Hub.SendSuggestionOptions(r.Context(), r.PathValue("id"), b)
	}))
	add("POST /v1/suggestions/{id}/dismiss", "admin", wrap(func(r *http.Request) (any, error) {
		var b struct{}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.Hub.DismissSuggestion(r.PathValue("id"))
	}))
	add("GET /v1/settings/briefs", "admin", wrap(func(r *http.Request) (any, error) { return c.Settings(), nil }))
	add("PUT /v1/settings/briefs", "admin", wrap(func(r *http.Request) (any, error) {
		var b model.BriefSettings
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return b, c.SetSettings(b)
	}))
	add("GET /v1/refresh-plan", "admin", wrap(func(r *http.Request) (any, error) { return c.Plan() }))
}
