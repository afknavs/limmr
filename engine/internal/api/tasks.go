package api

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"net/http"
	"time"
)

func (s *Server) taskRoutes(add func(string, string, route)) {
	svc := s.MCP.TaskService()
	add("POST /v1/tasks/{id}/keep", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Keep(nil, r.PathValue("id"))
	})
	add("POST /v1/tasks/{id}/dismiss", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Dismiss(nil, r.PathValue("id"), "")
	})
	add("POST /v1/tasks/{id}/undo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Undo(r.PathValue("id"))
	})
	add("POST /v1/settings/accessory-cue/test", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		ctx := s.Context
		if ctx == nil {
			ctx = context.Background()
		}
		ok := svc.Accessory != nil && svc.Accessory.Test(ctx, s.Hub.Settings().AccessoryCue)
		return map[string]bool{"ok": ok, "running": ok}, nil
	})
	add("GET /v1/tasks/feed", "status", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var since time.Time
		var err error
		if raw := r.URL.Query().Get("since"); raw != "" {
			since, err = time.Parse(time.RFC3339, raw)
			if err != nil {
				return nil, model.ErrInvalid
			}
		}
		xs, err := svc.Feed(since)
		return map[string]any{"tasks": xs}, err
	})
	add("GET /v1/tasks", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, err := svc.ListForPerson(nil, r.URL.Query().Get("status"), r.URL.Query().Get("person"), integer(r, "limit", 100))
		return map[string]any{"tasks": xs}, err
	})
	add("GET /v1/tasks/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Get(nil, r.PathValue("id"))
	})
	add("POST /v1/tasks", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Create
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Create(nil, b)
	})
	add("PATCH /v1/tasks/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b map[string]any
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Update(nil, r.PathValue("id"), b)
	})
	add("POST /v1/tasks/{id}/items", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Item
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Attach(nil, r.PathValue("id"), b)
	})
	add("DELETE /v1/tasks/{id}/items/{item}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.RemoveItem(nil, r.PathValue("id"), r.PathValue("item"))
	})
	add("POST /v1/tasks/{id}/checklist", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b tasks.Step
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		return svc.Checklist(nil, r.PathValue("id"), "add", []tasks.Step{b})
	})
	add("PATCH /v1/tasks/{id}/checklist/{step}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Text  *string `json:"text"`
			Done  *bool   `json:"done"`
			Order *int    `json:"order"`
		}
		if err := decode(r, &b); err != nil {
			return nil, err
		}
		if b.Text == nil && b.Done == nil && b.Order == nil {
			return nil, model.ErrInvalid
		}
		return svc.UpdateStep(r.PathValue("id"), r.PathValue("step"), b.Text, b.Done, b.Order)
	})
	add("DELETE /v1/tasks/{id}/checklist/{step}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.Checklist(nil, r.PathValue("id"), "remove", []tasks.Step{{ID: r.PathValue("step")}})
	})
	add("POST /v1/tasks/{id}/nudge/skip", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return svc.SkipNudge(nil, r.PathValue("id"))
	})
}
