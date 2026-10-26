// Package api serves the local control contract and credential boundary.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/migrate"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/remote"
	knowledgeSync "limmr/internal/sync"
	"limmr/internal/wa"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Knowledge                 *knowledgeSync.Service
	Avatars                   *people.AvatarCache
	Hub                       *hub.Service
	People                    *people.Service
	MCP                       *mcpserver.Service
	Link                      *wa.Link
	Health                    *health.Service
	Remote                    *remote.Service
	Migration                 *migrate.Service
	Port                      int
	ControlToken, StatusToken string
	Context                   context.Context
	Gate                      *sync.RWMutex
}
type principal struct {
	admin, status bool
	agent         *model.Agent
}
type route func(http.ResponseWriter, *http.Request, principal) (any, error)

func JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, e error) {
	status, code, message := 500, "internal_error", "The operation could not be completed."
	switch {
	case errors.Is(e, model.ErrUpdating):
		status, code, message = 503, "updating", model.ErrUpdating.Error()
	case errors.Is(e, model.ErrInvalid):
		status, code, message = 400, "invalid_request", "The request is invalid."
	case errors.Is(e, model.ErrForbidden):
		status, code, message = 403, "forbidden", "This credential cannot perform that operation."
	case errors.Is(e, model.ErrNotFound):
		status, code, message = 404, "not_found", "Not found."
	case errors.Is(e, model.ErrConflict):
		status, code, message = 409, "conflict", "The operation conflicts with the current state."
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return model.ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return model.ErrInvalid
	}
	return nil
}
func integer(r *http.Request, k string, def int) int {
	v := r.URL.Query().Get(k)
	if v == "" {
		return def
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 0 {
		return -1
	}
	return n
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	add := func(pattern, access string, f route) {
		f = s.u4Route(pattern, f)
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			p, _ := r.Context().Value(principalKey{}).(principal)
			if !p.admin {
				if access == "admin" || access == "status" && !p.status {
					failure(w, model.ErrForbidden)
					return
				}
				if access != "status" && p.agent == nil {
					failure(w, model.ErrForbidden)
					return
				}
			}
			if p.agent != nil {
				if e := s.Hub.CheckAccess(*p.agent); e != nil {
					failure(w, e)
					return
				}
			}
			v, e := f(w, r, p)
			if e != nil {
				failure(w, e)
				return
			}
			if v != nil {
				JSON(w, v)
			}
		})
	}
	s.syncRoutes(add)
	s.circleRoutes(add)
	s.effortRoutes(add)
	s.taskRoutes(add)
	s.peopleUnderstandingRoutes(add)
	s.y45Routes(add)
	s.statsRoutes(add)
	add("GET /v1/digest", "agent", s.digestRoute)
	s.u4Routes(add)
	s.onboardingRoutes(add)
	add("GET /v1/people/{id}/avatar", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.Avatars == nil {
			return nil, model.ErrNotFound
		}
		data, err := s.Avatars.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return nil, nil
	})
	s.watchRoutes(add)
	s.updateRoutes(add)
	add("GET /v1/status", "status", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		state := s.Link.State()
		linked := state.State == "ready" || state.State == "syncing"
		connected := linked
		if s.Health.Connection != nil {
			linked = s.Health.Connection.Linked()
			connected = s.Health.Connection.Connected()
		}
		name := ""
		if state.Account != nil {
			name = state.Account.Name
		}
		xs, e := s.Hub.Agents()
		if e != nil {
			return nil, e
		}
		agents := []map[string]any{}
		for _, a := range xs {
			if a.Enabled {
				agents = append(agents, map[string]any{"name": a.Name, "last_seen_at": a.LastSeenAt})
			}
		}
		return map[string]any{"engine_version": health.Version, "linked": linked, "connected": connected, "account_name": name, "pending_approvals": s.Hub.PendingCount(), "agents": agents, "health": health.Overall(s.Health.Checks(r.Context()))}, nil
	})
	add("GET /v1/link", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Link.State(), nil })
	add("POST /v1/link/start", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Method string `json:"method"`
			Phone  string `json:"phone"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		ctx := s.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if e := s.Link.Start(ctx, b.Method, b.Phone); e != nil {
			return nil, e
		}
		return map[string]string{"state": "connecting"}, nil
	})
	add("POST /v1/link/cancel", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		s.Link.Cancel()
		return map[string]bool{"ok": true}, nil
	})
	add("POST /v1/unlink", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Confirm bool `json:"confirm"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if e := s.Link.Unlink(r.Context(), b.Confirm); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, nil
	})
	add("GET /v1/link/events", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		ch, done := s.Link.Subscribe()
		defer done()
		return nil, stream(w, r, "link", ch)
	})
	add("GET /v1/agents", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.Hub.Agents()
		return map[string]any{"agents": xs}, e
	})
	add("POST /v1/agents", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			Transport string `json:"transport"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if b.Transport == "" {
			b.Transport = "local"
		}
		a, t, e := s.Hub.CreateAgent(b.Name, b.Kind, b.Transport)
		if e != nil {
			return nil, e
		}
		out := map[string]any{"agent": a}
		if t != "" {
			out["token"] = t
		}
		return out, nil
	})
	add("GET /v1/agents/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.Hub.Agent(r.PathValue("id"))
	})
	add("PATCH /v1/agents/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		a, e := s.Hub.Agent(r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		var b struct {
			Name        json.RawMessage    `json:"name"`
			Enabled     *bool              `json:"enabled"`
			Permissions *model.Permissions `json:"permissions"`
		}
		if e = decode(r, &b); e != nil {
			return nil, e
		}
		if b.Name != nil {
			var raw string
			if err := json.Unmarshal(b.Name, &raw); err != nil {
				return nil, model.ErrInvalid
			}
			name, err := hub.ValidAgentName(raw)
			if err != nil {
				return nil, err
			}
			a.Name = name
		}
		if b.Enabled != nil {
			a.Enabled = *b.Enabled
		}
		if b.Permissions != nil {
			a.Permissions = *b.Permissions
		}
		if e = s.Hub.UpdateAgent(a); e != nil {
			return nil, e
		}
		return s.Hub.Agent(a.ID)
	})
	add("PUT /v1/agents/{id}/permissions", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		a, e := s.Hub.Agent(r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		if e = decode(r, &a.Permissions); e != nil {
			return nil, e
		}
		if e = s.Hub.UpdateAgent(a); e != nil {
			return nil, e
		}
		return s.Hub.Agent(a.ID)
	})
	add("POST /v1/agents/{id}/rotate", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		t, e := s.Hub.Rotate(r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		return map[string]string{"token": t}, nil
	})
	add("POST /v1/agents/{id}/revoke", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		id := r.PathValue("id")
		a, e := s.Hub.Agent(id)
		if e != nil {
			return nil, e
		}
		a.Enabled = false
		if e = s.Hub.UpdateAgent(a); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.Hub.Revoke(id)
	})
	add("DELETE /v1/agents/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.Hub.DeleteAgent(r.PathValue("id"))
	})
	add("GET /v1/approvals", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.Hub.Approvals()
		if e != nil {
			return nil, e
		}
		out := []model.Approval{}
		for _, a := range xs {
			if state := r.URL.Query().Get("state"); state == "" || a.State == state {
				out = append(out, a)
			}
		}
		start, end, next, e := pageBounds(r, len(out), 50)
		if e != nil {
			return nil, e
		}
		return map[string]any{"approvals": out[start:end], "next_cursor": next}, nil
	})
	add("GET /v1/approvals/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if e := s.Hub.Expire(); e != nil {
			return nil, e
		}
		var a model.Approval
		e := s.Hub.Repo.Get("approvals", r.PathValue("id"), &a)
		return a, e
	})
	add("POST /v1/approvals/{id}/approve", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Text *string `json:"text"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.Hub.Decide(r.Context(), r.PathValue("id"), true, b.Text, "")
	})
	add("POST /v1/approvals/{id}/deny", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Reason string `json:"reason"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.Hub.Decide(r.Context(), r.PathValue("id"), false, nil, b.Reason)
	})
	add("GET /v1/activity", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		before, limit := integer(r, "before", 0), integer(r, "limit", 50)
		if before < 0 || limit < 1 {
			return nil, model.ErrInvalid
		}
		xs, e := s.Hub.Activity(p.agent, hub.ActivityFilter{Agent: r.URL.Query().Get("agent"), Kind: r.URL.Query().Get("kind"), Chat: r.URL.Query().Get("chat"), Before: int64(before), Limit: limit})
		if e != nil {
			return nil, e
		}
		var next *int64
		if len(xs) > 0 {
			v := xs[len(xs)-1].ID
			next = &v
		}
		return map[string]any{"items": xs, "next_before": next}, nil
	})
	add("GET /v1/activity/stream", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return nil, s.refreshStream(w, r)
	})
	add("GET /v1/chats", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadMessages {
			return nil, model.ErrForbidden
		}
		cs, _, e := s.Hub.Visible(p.agent)
		if e != nil {
			return nil, e
		}
		query := strings.ToLower(r.URL.Query().Get("query"))
		if query != "" {
			filtered := []model.Chat{}
			for _, c := range cs {
				if strings.Contains(strings.ToLower(c.Name), query) {
					filtered = append(filtered, c)
				}
			}
			cs = filtered
		}
		start, end, next, e := pageBounds(r, len(cs), 100)
		if e != nil {
			return nil, e
		}
		return map[string]any{"chats": cs[start:end], "next_cursor": next}, nil
	})
	add("GET /v1/messages", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadMessages {
			return nil, model.ErrForbidden
		}
		chat := r.URL.Query().Get("chat_jid")
		if chat != "" && !s.Hub.CanChat(p.agent, chat) {
			return nil, model.ErrNotFound
		}
		_, vs, e := s.Hub.Visible(p.agent)
		if e != nil {
			return nil, e
		}
		limit := integer(r, "limit", 50)
		if limit < 1 {
			return nil, model.ErrInvalid
		}
		ms, e := s.Hub.Messages.Messages(wa.Query{Chat: chat, ID: r.URL.Query().Get("message_id"), Before: r.URL.Query().Get("before"), After: r.URL.Query().Get("after"), Limit: limit, Offset: integer(r, "offset", 0)}, vs)
		return map[string]any{"messages": ms}, e
	})
	add("GET /v1/people", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if e := s.People.MigrateAgentNotes(); e != nil {
			return nil, e
		}
		if p.agent != nil && !p.agent.Permissions.ReadPeople {
			return nil, model.ErrForbidden
		}
		xs, e := s.People.PeopleAll(p.agent, r.URL.Query().Get("query"), r.URL.Query().Get("all") == "true" || r.URL.Query().Get("query") != "")
		if e != nil {
			return nil, e
		}
		limit := integer(r, "limit", 50)
		if limit < 1 {
			return nil, model.ErrInvalid
		}
		limit = min(limit, 100)
		start := 0
		cursor := r.URL.Query().Get("cursor")
		if cursor != "" {
			found := false
			for i, p := range xs {
				if p.ID == cursor {
					start = i + 1
					found = true
					break
				}
			}
			if !found {
				return nil, model.ErrInvalid
			}
		}
		for i := range xs {
			p := &xs[i]
			p.Name = shortField(p.Name, 80)
			p.Label = shortField(p.Label, 80)
			p.PushName = shortField(p.PushName, 80)
			p.Role = shortField(p.Role, 60)
			p.Notes = shortField(p.Notes, 120)
			p.HowKnown = shortField(p.HowKnown, 120)
			p.Circle = shortField(p.Circle, 64)
			p.Details = model.PersonDetails{}
			p.DetailSuggestions = nil
		}
		end := fitPage(xs, start, min(start+limit, len(xs)))
		next := ""
		if end < len(xs) {
			next = xs[end-1].ID
		}
		return map[string]any{"people": xs[start:end], "next_cursor": next}, nil
	})
	add("GET /v1/people/changes", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		xs, e := s.People.Changes()
		for i := range xs {
			xs[i].Before = nil
			xs[i].After = nil
		}
		if e != nil {
			return nil, e
		}
		start, end, next, e := pageBounds(r, len(xs), 50)
		if e != nil {
			return nil, e
		}
		return map[string]any{"changes": xs[start:end], "next_cursor": next}, nil
	})
	add("POST /v1/people/changes/{id}/revert", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.People.Revert(r.PathValue("id"))
	})
	add("GET /v1/people/{id}", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadPeople {
			return nil, model.ErrForbidden
		}
		return s.People.Profile(p.agent, r.PathValue("id"))
	})
	add("PATCH /v1/people/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b map[string]any
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.People.UpdateProfile(nil, r.PathValue("id"), b)
	})
	add("DELETE /v1/people/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return nil, s.People.Forget(r.PathValue("id"))
	})
	add("POST /v1/people/{id}/suggestions/{suggestion}/confirm", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.People.ReviewDetail(r.PathValue("id"), r.PathValue("suggestion"), true)
	})
	add("POST /v1/people/{id}/suggestions/{suggestion}/dismiss", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.People.ReviewDetail(r.PathValue("id"), r.PathValue("suggestion"), false)
	})
	add("POST /v1/facts/{id}/confirm", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return nil, s.People.ConfirmFact(r.PathValue("id"))
	})
	add("GET /v1/people/export", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		ps, e := s.People.People(nil, "")
		if e != nil {
			return nil, e
		}
		profiles := []people.Profile{}
		for _, person := range ps {
			pr, err := s.People.Profile(nil, person.ID)
			if err != nil {
				return nil, err
			}
			profiles = append(profiles, pr)
		}
		cs, e := s.MCP.CircleService().List(nil)
		return map[string]any{"people": profiles, "circles": cs}, e
	})
	add("POST /v1/facts", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Subject   string   `json:"subject"`
			SubjectID string   `json:"subject_id"`
			Text      string   `json:"text"`
			Evidence  []string `json:"evidence_message_ids"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.People.AddFact(nil, model.Fact{Subject: b.Subject, SubjectID: b.SubjectID, Text: b.Text, EvidenceMessageIDs: b.Evidence})
	})
	add("POST /v1/facts/{id}/revert", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.People.Revert(r.PathValue("id"))
	})
	add("POST /v1/relations", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			A    string `json:"a"`
			B    string `json:"b"`
			Kind string `json:"kind"`
			Note string `json:"note"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.People.AddRelation(nil, model.Relation{A: b.A, B: b.B, Kind: b.Kind, Note: b.Note})
	})
	add("GET /v1/groups", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadPeople {
			return nil, model.ErrForbidden
		}
		xs, e := s.People.Groups(p.agent, r.URL.Query().Get("query"))
		return map[string]any{"groups": xs}, e
	})
	add("GET /v1/groups/{id}", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadPeople {
			return nil, model.ErrForbidden
		}
		return s.groupDetail(p, r.PathValue("id"))
	})
	add("GET /v1/graph", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		depth, limit := integer(r, "depth", 1), integer(r, "limit", 300)
		if depth < 1 || limit < 1 {
			return nil, model.ErrInvalid
		}
		return s.People.Graph(p.agent, r.URL.Query().Get("focus"), depth, limit)
	})
	add("GET /v1/graph/export", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return s.People.Export(p.agent)
	})
	add("GET /v1/summaries", "agent", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if p.agent != nil && !p.agent.Permissions.ReadMessages {
			return nil, model.ErrForbidden
		}
		chat := r.URL.Query().Get("chat_jid")
		if !s.Hub.CanChat(p.agent, chat) {
			return nil, model.ErrNotFound
		}
		xs, e := hub.List[model.Summary](s.Hub.Repo, "summaries")
		if e != nil {
			return nil, e
		}
		out := []model.Summary{}
		for _, v := range xs {
			if v.ChatJID == chat {
				out = append(out, v)
			}
		}
		return map[string]any{"summaries": out}, nil
	})
	add("POST /v1/attachments", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Filename string `json:"filename"`
			Data     string `json:"data_base64"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		data, e := base64.StdEncoding.DecodeString(b.Data)
		if e != nil {
			return nil, model.ErrInvalid
		}
		return s.Hub.Attachment(data, b.Filename)
	})
	add("GET /v1/settings", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Hub.Settings(), nil })
	add("PUT /v1/settings", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b model.Settings
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if e := s.Hub.SetSettings(b); e != nil {
			return nil, e
		}
		return s.Hub.Settings(), nil
	})
	add("GET /v1/health", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		checks := s.Health.Checks(r.Context())
		return map[string]any{"checks": checks, "health": health.Overall(checks)}, nil
	})
	add("POST /v1/fix/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.Health.Fix(r.Context(), r.PathValue("id"))
	})
	add("GET /v1/diagnostics", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, e := io.WriteString(w, s.Health.Diagnostics(r.Context()))
		return nil, e
	})
	if s.Remote != nil {
		add("GET /v1/remote", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return s.Remote.Status(), nil })
		add("POST /v1/remote", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b remote.Config
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			if e := s.Remote.Configure(b); e != nil {
				return nil, e
			}
			return s.Remote.Status(), nil
		})
		add("POST /v1/remote/approve", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b struct {
				Code     string        `json:"code"`
				Access   *model.Access `json:"access"`
				FillIn   bool          `json:"fill_in"`
				Preset   string        `json:"preset"`
				AgentID  string        `json:"agent_id"`
				NewAgent *struct {
					Name string `json:"name"`
					Kind string `json:"kind"`
				} `json:"new_agent"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			return map[string]bool{"ok": true}, s.Remote.Approve(b.Code, b.AgentID, b.NewAgent, b.Access, b.FillIn, b.Preset)
		})
		add("POST /v1/remote/deny", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b struct {
				Code string `json:"code"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			return map[string]bool{"ok": true}, s.Remote.Deny(b.Code)
		})
		add("POST /v1/remote/revoke", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b struct {
				ClientID string `json:"client_id"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			return map[string]bool{"ok": true}, s.Remote.Revoke(b.ClientID)
		})
	}
	if s.Migration != nil {
		add("POST /v1/migrate/whatsapp-mcp", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			var b struct {
				DryRun *bool `json:"dry_run"`
			}
			if e := decode(r, &b); e != nil {
				return nil, e
			}
			if b.DryRun == nil {
				return nil, model.ErrInvalid
			}
			return s.Migration.Run(r.Context(), *b.DryRun)
		})
		add("POST /v1/migrate/undo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
			return s.Migration.Undo(r.Context())
		})
	}
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := r.Context().Value(principalKey{}).(principal)
		if p.agent == nil {
			failure(w, model.ErrForbidden)
			return
		}
		s.MCP.Handler(false).ServeHTTP(w, mcpserver.WithAgent(r, *p.agent))
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { failure(w, model.ErrNotFound) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != fmt.Sprintf("127.0.0.1:%d", s.Port) && r.Host != fmt.Sprintf("localhost:%d", s.Port) {
			failure(w, model.ErrForbidden)
			return
		}
		if _, ok := r.Header["Origin"]; ok {
			failure(w, model.ErrForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/sync/") && s.Remote != nil {
			s.Remote.SyncHandler().ServeHTTP(w, r)
			return
		}
		auths := r.Header.Values("Authorization")
		if len(auths) != 1 || !strings.HasPrefix(auths[0], "Bearer ") {
			unauthorized(w)
			return
		}
		token := strings.TrimPrefix(auths[0], "Bearer ")
		p := principal{}
		if hub.Equal(token, s.ControlToken) && s.ControlToken != "" {
			p.admin = true
			p.status = true
		} else if hub.Equal(token, s.StatusToken) && s.StatusToken != "" {
			p.status = true
		} else {
			a, e := s.Hub.Authenticate(token)
			if e != nil {
				var identity hub.RunIdentity
				a, identity, e = s.Hub.AuthenticateRun(token)
				if e == nil {
					r = r.WithContext(hub.WithHandoff(r.Context(), identity))
					// A run capability is MCP-only; it is never an agent control credential.
					if r.URL.Path != "/mcp" {
						failure(w, model.ErrForbidden)
						return
					}
				}
			}
			if e != nil {
				unauthorized(w)
				return
			}
			p.agent = &a
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		r.Body = http.MaxBytesReader(w, r.Body, 28<<20)
		if s.Gate != nil {
			if strings.HasPrefix(r.URL.Path, "/v1/migrate/") {
				s.Gate.Lock()
				defer s.Gate.Unlock()
			} else if r.URL.Path != "/v1/link/events" && r.URL.Path != "/v1/activity/stream" && r.URL.Path != "/v1/updates/stream" {
				s.Gate.RLock()
				defer s.Gate.RUnlock()
			}
		}
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}

type principalKey struct{}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(401)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "unauthorized", "message": "A valid bearer credential is required."}})
}
func stream[T any](w http.ResponseWriter, r *http.Request, event string, ch <-chan T) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return model.ErrConflict
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	flusher.Flush()
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return nil
		case item := <-ch:
			b, e := json.Marshal(item)
			if e != nil {
				return e
			}
			if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); e != nil {
				return nil
			}
			flusher.Flush()
		case <-timer.C:
			if _, e := io.WriteString(w, ": heartbeat\n\n"); e != nil {
				return nil
			}
			flusher.Flush()
		}
	}
}
