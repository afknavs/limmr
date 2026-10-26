package api

import (
	"limmr/internal/model"
	"net/http"
)

func (s *Server) peopleUnderstandingRoutes(add func(string, string, route)) {
	add("GET /v1/settings/people", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		v := map[string]any{"enabled": nil}
		e := s.Hub.Repo.Get("people_settings", "inference", &v)
		if e == model.ErrNotFound {
			e = nil
		}
		return v, e
	})
	add("PUT /v1/settings/people", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var v struct {
			Enabled bool `json:"enabled"`
		}
		if e := decode(r, &v); e != nil {
			return nil, e
		}
		return v, s.Hub.Repo.Put("people_settings", "inference", v)
	})
	add("GET /v1/people/to-understand", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.People.PeopleToUnderstand(nil)
		return map[string]any{"people": xs}, e
	})
	add("PUT /v1/people/{id}/inference", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var v model.RelationshipInference
		if e := decode(r, &v); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.People.SaveInference(r.PathValue("id"), v)
	})
	add("POST /v1/people/{id}/inference/confirm", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.People.ReviewInference(r.PathValue("id"), true)
	})
	add("POST /v1/people/{id}/inference/dismiss", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.People.ReviewInference(r.PathValue("id"), false)
	})
	add("POST /v1/people/affinity/suggest", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.MCP.CircleService().SuggestAffinity()
	})
}
