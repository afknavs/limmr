package api

import (
	"database/sql"
	"io"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/wa"
	"net/http"
	"strings"
	"time"
)

func (s *Server) y45Routes(add func(string, string, route)) {
	add("GET /v1/settings/people/contacts", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		v := map[string]any{"enabled": false}
		e := s.Hub.Repo.Get("people_settings", "contacts", &v)
		if e == model.ErrNotFound {
			e = nil
		}
		return v, e
	})
	add("PUT /v1/settings/people/contacts", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var v struct {
			Enabled        bool       `json:"enabled"`
			LastImportedAt *time.Time `json:"last_imported_at"`
		}
		if e := decode(r, &v); e != nil {
			return nil, e
		}
		return v, s.Hub.Repo.Put("people_settings", "contacts", v)
	})
	add("PUT /v1/people/{id}/photo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		data, e := io.ReadAll(io.LimitReader(r.Body, people.MaxAvatarBytes+1))
		if e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.People.SetPhoto(r.PathValue("id"), data)
	})
	add("DELETE /v1/people/{id}/photo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		return map[string]bool{"ok": true}, s.People.SetPhoto(r.PathValue("id"), nil)
	})
	add("POST /v1/people/contacts-import", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Matches []people.ContactMatch `json:"matches"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return map[string]bool{"ok": true}, s.People.ImportContacts(b.Matches)
	})
	add("GET /v1/people/{id}/summary", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		profile, e := s.People.Profile(nil, r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		var v people.PersonSummary
		err := s.Hub.Repo.Get("person_summaries", profile.Person.ID, &v)
		if err == model.ErrNotFound || profile.Stats.DM30 < 5 {
			return map[string]any{"summary": nil}, nil
		}
		return map[string]any{"summary": v}, err
	})
	add("PUT /v1/people/{id}/summary", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			Text         string    `json:"text"`
			BasedOnUntil time.Time `json:"based_on_until"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		return s.People.SavePersonSummary(nil, r.PathValue("id"), b.Text, b.BasedOnUntil)
	})
	add("PATCH /v1/groups/{id}", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) { return nil, model.ErrInvalid }) // icon-only patches handled by u4Route
	add("GET /v1/groups/{id}/photo", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		if s.Avatars == nil {
			return nil, model.ErrNotFound
		}
		data, e := s.Avatars.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			return nil, e
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, e = w.Write(data)
		return nil, e
	})
	add("PATCH /v1/groups/{id}/access", "admin", func(w http.ResponseWriter, r *http.Request, p principal) (any, error) {
		var b struct {
			AgentID string  `json:"agent_id"`
			Read    *bool   `json:"read"`
			Send    *string `json:"send"`
		}
		if e := decode(r, &b); e != nil {
			return nil, e
		}
		if b.Read == nil && b.Send == nil {
			return nil, model.ErrInvalid
		}
		var g model.Group
		if e := s.Hub.Repo.Get("groups", r.PathValue("id"), &g); e != nil {
			return nil, e
		}
		a, e := s.Hub.Agent(b.AgentID)
		if e != nil {
			return nil, e
		}
		xs := []hub.Mutation{}
		if b.Read != nil {
			scope := &a.Permissions.Chats
			keep := []string{}
			for _, j := range scope.JIDs {
				if j != g.JID {
					keep = append(keep, j)
				}
			}
			scope.JIDs = keep
			if scope.Mode == "only" && *b.Read || scope.Mode != "only" && !*b.Read {
				scope.JIDs = append(scope.JIDs, g.JID)
			}
			if scope.Mode == "all" && !*b.Read {
				scope.Mode = "all_except"
			}
			xs = append(xs, hub.Mutation{Bucket: "agents", ID: a.ID, Value: a})
		}
		if b.Send != nil {
			if !model.Contains([]string{"free", "approval", "never"}, *b.Send) {
				return nil, model.ErrInvalid
			}
			xs = append(xs, hub.Mutation{Bucket: "group_send_rules", ID: a.ID + "/" + g.JID, Value: *b.Send})
		}
		if e = s.Hub.Repo.Apply(xs...); e != nil {
			return nil, e
		}
		return s.People.Group(nil, g.JID)
	})
}
func (s *Server) groupDetail(p principal, id string) (any, error) {
	value, e := s.People.Group(p.agent, id)
	if e != nil {
		return nil, e
	}
	v := value.(map[string]any)
	recent := []model.Message{}
	if p.agent == nil || p.agent.Permissions.ReadMessages {
		_, visible, e := s.Hub.Visible(p.agent)
		if e != nil {
			return nil, e
		}
		recent, e = s.Hub.Messages.Messages(wa.Query{Chat: id, Limit: 20}, visible)
		if e != nil {
			return nil, e
		}
	}
	v["recent"] = recent
	if p.agent == nil {
		// The app's people list is paged, so resolve senders here: members first,
		// then any saved person, then their WhatsApp name.
		names := map[string]string{}
		for _, m := range recent {
			if m.IsFromMe || m.Sender == "" {
				continue
			}
			if _, done := names[m.Sender]; done {
				continue
			}
			var label, name, push sql.NullString
			if s.Hub.Repo.DB.QueryRow(`SELECT json_extract(o.data,'$.label'),json_extract(o.data,'$.name'),json_extract(o.data,'$.push_name') FROM objects o, json_each(o.data,'$.jids') j WHERE o.bucket='people' AND j.value=? LIMIT 1`, m.Sender).Scan(&label, &name, &push) != nil {
				continue
			}
			for _, x := range []sql.NullString{label, name, push} {
				if x.Valid && strings.TrimSpace(x.String) != "" && !strings.HasPrefix(x.String, "+") {
					names[m.Sender] = x.String
					break
				}
			}
		}
		// A list, not an object: JSON key decoding strategies may rewrite map keys.
		list := []map[string]string{}
		for jid, name := range names {
			list = append(list, map[string]string{"jid": jid, "name": name})
		}
		v["senders"] = list
	}
	xs, e := hub.List[model.Summary](s.Hub.Repo, "summaries")
	if e != nil {
		return nil, e
	}
	var latest *model.Summary
	if p.agent == nil || p.agent.Permissions.ReadMessages {
		for _, x := range xs {
			if x.ChatJID == id && (latest == nil || x.CreatedAt.After(latest.CreatedAt)) {
				copy := x
				latest = &copy
			}
		}
	}
	v["summary"] = latest
	return v, nil
}
