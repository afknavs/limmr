package api

import (
	"bytes"
	"encoding/json"
	"io"
	"limmr/internal/hub"
	"limmr/internal/model"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) u4Routes(add func(string, string, route)) {
	add("GET /v1/stats/history", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		days := 140
		if values, ok := r.URL.Query()["days"]; ok {
			if len(values) != 1 {
				return nil, model.ErrInvalid
			}
			n, e := strconv.Atoi(values[0])
			if e != nil {
				return nil, model.ErrInvalid
			}
			days = n
		}
		return s.Hub.StatsHistory(days)
	})
	add("POST /v1/circles/{id}/seen", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if _, e := s.MCP.CircleService().Circle(nil, r.PathValue("id")); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.Hub.Repo.Put("circle_seen", r.PathValue("id"), s.Hub.Now().UTC())
	})
	add("POST /v1/circles/{id}/asks/{ask_id}/draft", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			AgentID string `json:"agent_id"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		a, e := s.Hub.Agent(b.AgentID)
		if e != nil {
			return nil, e
		}
		if !a.Enabled {
			return nil, model.ErrForbidden
		}
		c := s.MCP.CircleService()
		brief, e := c.Latest(&a, r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		if brief == nil {
			return nil, model.ErrNotFound
		}
		for _, ask := range brief.OpenAsks {
			if ask.ID == r.PathValue("ask_id") {
				// An action suggestion is a request for the named AI, never a fabricated reply.
				return c.Suggest(a, model.Suggestion{CircleID: r.PathValue("id"), Kind: "action", ChatJID: ask.ChatJID, Why: "Draft a reply for review to " + ask.From + ": “" + ask.Text + "”"})
			}
		}
		return nil, model.ErrNotFound
	})
}

// Wrap registrations, leaving the parallel lanes' entity mutation implementations alone.
func (s *Server) u4Route(pattern string, next route) route {
	return func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var icon *hub.EntityIcon
		hasIcon := false
		bucket := ""
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if p.admin && (r.Method == "PATCH" || r.Method == "PUT") && len(parts) >= 3 && (parts[1] == "people" || parts[1] == "agents" || parts[1] == "circles" || parts[1] == "groups") && (len(parts) == 3 || len(parts) == 4 && parts[3] == "permissions") {
			bucket = parts[1]
			raw, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if e != nil || len(raw) >= 1<<20 {
				return nil, model.ErrInvalid
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || fields == nil {
				return nil, model.ErrInvalid
			}
			if data, ok := fields["icon"]; ok {
				hasIcon = true
				if string(data) != "null" {
					var i hub.EntityIcon
					if json.Unmarshal(data, &i) != nil || !i.Valid() {
						return nil, model.ErrInvalid
					}
					icon = &i
				}
				delete(fields, "icon")
			}
			if hasIcon && len(fields) == 0 && len(parts) == 3 {
				v, e := s.Hub.IconEntity(bucket, parts[2])
				if e != nil {
					return nil, e
				}
				if e = s.Hub.SetEntityIcon(bucket, parts[2], icon); e != nil {
					return nil, e
				}
				return s.Hub.WithEntityIcons(v)
			}
			raw, e = json.Marshal(fields)
			if e != nil {
				return nil, e
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		v, e := next(w, r, p)
		if e != nil {
			return nil, e
		}
		if hasIcon {
			if e = s.Hub.SetEntityIcon(bucket, parts[2], icon); e != nil {
				return nil, e
			}
		}
		if pattern == "GET /v1/home" && v != nil {
			if e = s.homeUpdates(v); e != nil {
				return nil, e
			}
		}
		if v == nil {
			return nil, nil
		}
		return s.Hub.WithEntityIcons(v)
	}
}
func (s *Server) homeUpdates(value any) error {
	v, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	cards := map[string]map[string]any{}
	for _, key := range []string{"circles", "today"} {
		raw, _ := json.Marshal(v[key])
		var xs []map[string]any
		if e := json.Unmarshal(raw, &xs); e != nil {
			return e
		}
		for _, x := range xs {
			c, ok := x["circle"].(map[string]any)
			if !ok {
				continue
			}
			id, _ := c["id"].(string)
			if old, ok := cards[id]; ok {
				for k, value := range old {
					if _, exists := x[k]; !exists {
						x[k] = value
					}
				}
			}
			x["pinned"] = c["pinned"]
			cards[id] = x
		}
	}
	out := []map[string]any{}
	for id, x := range cards {
		var seen time.Time
		if e := s.Hub.Repo.Get("circle_seen", id, &seen); e != nil && e != model.ErrNotFound {
			return e
		}
		if !seen.IsZero() {
			c, e := s.MCP.CircleService().Circle(nil, id)
			if e != nil {
				return e
			}
			n := 0
			for _, chat := range c.Chats {
				var count int
				if e = s.Hub.Messages.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE chat_jid=? AND julianday(timestamp)>julianday(?) AND julianday(timestamp)<=julianday(?)`, chat.ChatJID, seen, s.Hub.Now()).Scan(&count); e != nil {
					return e
				}
				n += count
			}
			x["new_messages"] = n
		}
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ap, _ := a["pinned"].(bool)
		bp, _ := b["pinned"].(bool)
		if ap != bp {
			return ap
		}
		aa, _ := a["open_asks"].(float64)
		ba, _ := b["open_asks"].(float64)
		if (aa > 0) != (ba > 0) {
			return aa > 0
		}
		ac := a["circle"].(map[string]any)
		bc := b["circle"].(map[string]any)
		at, _ := ac["updated_at"].(string)
		bt, _ := bc["updated_at"].(string)
		if at != bt {
			return at > bt
		}
		return ac["id"].(string) < bc["id"].(string)
	})
	v["circles"] = out
	return nil
}
