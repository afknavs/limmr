package api

import (
	"encoding/json"
	"limmr/internal/model"
	"net/http"
)

func (s *Server) onboardingRoutes(add func(string, string, route)) {
	add("POST /v1/agents/{id}/preset", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Preset string `json:"preset"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.Hub.SwitchPreset(r.PathValue("id"), b.Preset)
	})
	add("GET /v1/owner", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.Hub.OwnerProfile(), nil
	})
	add("PUT /v1/owner", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b map[string]json.RawMessage
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		v, e := s.Hub.UpdateOwnerProfile(nil, b)
		if e == nil {
			e = s.Hub.Record(model.Activity{AgentID: "you", Tool: "update_owner_profile", Kind: "people_write", Count: 1})
		}
		return v, e
	})
	add("POST /v1/owner/suggestions/{id}/{decision}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		decision := r.PathValue("decision")
		if decision != "confirm" && decision != "dismiss" {
			return nil, model.ErrInvalid
		}
		v, e := s.Hub.ReviewOwnerSuggestion(r.PathValue("id"), decision == "confirm")
		if e == nil {
			e = s.Hub.Record(model.Activity{AgentID: "you", Tool: "review_owner_profile", Kind: "people_write", Count: 1})
		}
		return v, e
	})
	add("POST /v1/circle-additions/{id}/{decision}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		d := r.PathValue("decision")
		if d != "confirm" && d != "dismiss" {
			return nil, model.ErrInvalid
		}
		return s.MCP.CircleService().ReviewAddition(r.PathValue("id"), d == "confirm")
	})
	add("GET /v1/circles/changes", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.MCP.CircleService().AutoChanges()
		return map[string]any{"changes": xs}, e
	})
	add("POST /v1/circles/changes/undo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			ChangeIDs []string `json:"change_ids"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.MCP.CircleService().UndoAutoGroup(b.ChangeIDs)
	})
	add("POST /v1/circle-changes/{id}/undo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.MCP.CircleService().UndoAuto(r.PathValue("id"))
	})

	add("PATCH /v1/agents/{id}/access", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var raw map[string]json.RawMessage
		if e := decode(r, &raw); e != nil || raw == nil {
			return nil, model.ErrInvalid
		}
		return s.Hub.PatchAccess(r.PathValue("id"), raw)
	})
	add("GET /v1/setup/status", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		linked := false
		if s.Link != nil {
			v := s.Link.State().State
			linked = v == "ready" || v == "syncing"
		}
		return s.MCP.CircleService().Setup(nil, linked)
	})
	add("POST /v1/agents/this-mac/enable", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Hub.EnableThisMac() })
	review := func(accept bool) route {
		return func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b struct {
				IDs []string `json:"ids"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			xs, e := s.MCP.CircleService().ReviewProposals(b.IDs, accept)
			return map[string]any{"ok": e == nil, "circles": xs}, e
		}
	}
	add("POST /v1/circles/proposals/accept", "admin", review(true))
	add("POST /v1/circles/proposals/dismiss", "admin", review(false))
	add("PUT /v1/circles/{id}/brief", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Summary        string             `json:"summary"`
			WhatChanged    string             `json:"what_changed"`
			OpenAsks       []model.OpenAsk    `json:"open_asks"`
			OpenAsksForYou []model.OpenAsk    `json:"open_asks_for_you"`
			Upcoming       []model.Upcoming   `json:"upcoming"`
			PeopleNotes    []model.PeopleNote `json:"people_notes"`
			Evidence       []string           `json:"evidence_message_ids"`
			CoversUntil    json.RawMessage    `json:"covers_until"`
			WrittenBy      string             `json:"written_by"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		v := model.Brief{Summary: b.Summary, WhatChanged: b.WhatChanged, OpenAsks: b.OpenAsks, Upcoming: b.Upcoming, PeopleNotes: b.PeopleNotes, Evidence: b.Evidence, WrittenBy: b.WrittenBy}
		if b.OpenAsksForYou != nil {
			if b.OpenAsks != nil {
				return nil, model.ErrInvalid
			}
			v.OpenAsks = b.OpenAsksForYou
		}
		if b.CoversUntil != nil && json.Unmarshal(b.CoversUntil, &v.CoversUntil) != nil {
			return nil, model.ErrInvalid
		}
		return s.MCP.CircleService().WriteAdminBrief(r.PathValue("id"), v)
	})
	add("POST /v1/suggestions", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			TaskID     string   `json:"task_id"`
			CircleID   string   `json:"circle_id"`
			Kind       string   `json:"kind"`
			ChatJID    string   `json:"chat_jid"`
			DraftText  string   `json:"draft_text"`
			Why        string   `json:"why"`
			Voice      *string  `json:"voice"`
			Variations []string `json:"variations"`
			CreatedBy  string   `json:"created_by"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if b.CreatedBy != "this-mac" {
			return nil, model.ErrInvalid
		}
		a, e := s.Hub.Agent("this-mac")
		if e != nil || !a.Enabled || a.Kind != "on_device" {
			return nil, model.ErrForbidden
		}
		if b.TaskID != "" {
			if b.Kind != "nudge" {
				return nil, model.ErrInvalid
			}
			return s.MCP.TaskService().OnDeviceNudge(b.TaskID, b.DraftText)
		}
		v, e := s.MCP.CircleService().Suggest(a, model.Suggestion{CircleID: b.CircleID, Kind: b.Kind, ChatJID: b.ChatJID, DraftText: b.DraftText, Why: b.Why, Variations: b.Variations})
		if e != nil {
			return nil, e
		}
		e = s.Hub.Record(model.Activity{AgentID: a.ID, Tool: "suggest", Kind: "people_write", ChatJIDs: []string{b.ChatJID}, Count: 1})
		return v, e
	})
}
