package circles

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/wa"
	"sort"
	"time"
)

type Page struct {
	AgentID, ContextID, Scope string
	Since, Until              time.Time
	LastTime                  time.Time
	LastChat, LastID          string
}
type RoleMessage struct {
	Message model.Message `json:"message"`
	Role    string        `json:"role,omitempty"`
}
type ChatMessages struct {
	ChatJID  string        `json:"chat_jid"`
	Messages []RoleMessage `json:"messages"`
}
type CatchUp struct {
	Chats       []ChatMessages `json:"chats"`
	Cursor      time.Time      `json:"cursor"`
	Next        string         `json:"next_page_token,omitempty"`
	NewMessages int            `json:"new_messages"`
}

func (s *Service) CatchUp(a model.Agent, id, since, token string, limit int) (CatchUp, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	out := CatchUp{Chats: []ChatMessages{}}
	c, e := s.Context(&a, id)
	if e != nil {
		return out, e
	}
	key := s.cursorKey(a, id, c)
	cursor := s.Cursor(a, id, c)
	out.Cursor = cursor
	p := Page{AgentID: a.ID, ContextID: id, Scope: key, Since: cursor, Until: s.Hub.Now().UTC()}
	if since != "" {
		p.Since, e = time.Parse(time.RFC3339Nano, since)
		if e != nil || p.Since.After(p.Until) {
			return out, model.ErrInvalid
		}
	}
	if since == "" && token == "" && p.Since.Before(s.Hub.CurrentSince()) {
		p.Since = s.Hub.CurrentSince()
		if cursor.IsZero() {
			p.Since = s.Hub.Now().AddDate(0, 0, -7)
		}
	}
	if token != "" {
		if e = s.Hub.Repo.Get("catch_pages", token, &p); e != nil {
			return out, e
		}
		if p.AgentID != a.ID || p.ContextID != id || p.Scope != key || s.Hub.Now().Sub(p.Until) > time.Hour {
			return out, model.ErrNotFound
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 50 {
		limit = 50
	}
	// Query one bounded ascending page using exact time tuples in Go. The SQL
	// range bounds trim history; no OFFSET means concurrent inserts cannot skip it.
	ms := []model.Message{}
	for _, j := range JIDs(c) {
		lower := p.Since
		if !p.LastTime.IsZero() {
			lower = p.LastTime
		}
		rows, e := s.Hub.Messages.DB.Query("SELECT id,timestamp,julianday(timestamp) FROM messages WHERE chat_jid=? AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?) ORDER BY julianday(timestamp),id", j, lower, p.Until)
		if e != nil {
			return out, e
		}
		type row struct {
			id string
			at time.Time
		}
		rs := []row{}
		var boundary float64
		for rows.Next() {
			var r row
			var instant float64
			if e = rows.Scan(&r.id, &r.at, &instant); e != nil {
				rows.Close()
				return out, e
			}
			if len(rs) > limit && instant != boundary {
				break
			}
			if !r.at.After(p.Since) || r.at.After(p.Until) {
				continue
			}
			if !p.LastTime.IsZero() && (r.at.Before(p.LastTime) || r.at.Equal(p.LastTime) && (j < p.LastChat || j == p.LastChat && r.id <= p.LastID)) {
				continue
			}
			rs = append(rs, r)
			if len(rs) == limit+1 {
				boundary = instant
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		sort.Slice(rs, func(i, j int) bool {
			if !rs[i].at.Equal(rs[j].at) {
				return rs[i].at.Before(rs[j].at)
			}
			return rs[i].id < rs[j].id
		})
		if len(rs) > limit+1 {
			rs = rs[:limit+1]
		}
		for _, r := range rs {
			xs, e := s.Hub.Messages.Messages(wa.Query{Chat: j, ID: r.id, Limit: 1}, []string{j})
			if e != nil {
				return out, e
			}
			ms = append(ms, xs...)
		}
	}
	sort.Slice(ms, func(i, j int) bool {
		if !ms[i].Timestamp.Equal(ms[j].Timestamp) {
			return ms[i].Timestamp.Before(ms[j].Timestamp)
		}
		if ms[i].ChatJID != ms[j].ChatJID {
			return ms[i].ChatJID < ms[j].ChatJID
		}
		return ms[i].ID < ms[j].ID
	})
	more := len(ms) > limit
	if more {
		ms = ms[:limit]
	}
	roles := map[string]string{}
	// Sender role lookup also supports group-only projections without disclosing
	// the sender's other JIDs in any profile output.
	rawPeople, e := hub.List[model.Person](s.Hub.Repo, "people")
	if e != nil {
		return out, e
	}
	for _, cp := range c.People {
		for _, person := range rawPeople {
			if cp.PersonID == person.ID {
				for _, jid := range person.JIDs {
					roles[jid] = cp.Role
				}
			}
		}
	}
	for _, m := range ms {
		idx := -1
		for i, g := range out.Chats {
			if g.ChatJID == m.ChatJID {
				idx = i
			}
		}
		if idx < 0 {
			out.Chats = append(out.Chats, ChatMessages{ChatJID: m.ChatJID, Messages: []RoleMessage{}})
			idx = len(out.Chats) - 1
		}
		role := roles[m.Sender]
		if m.IsFromMe {
			role = "you"
		}
		out.Chats[idx].Messages = append(out.Chats[idx].Messages, RoleMessage{model.CompactMessageTo(m, model.MaxCatchUpText), role})
	}
	out.NewMessages = len(ms)
	if more {
		last := ms[len(ms)-1]
		p.LastTime = last.Timestamp
		p.LastChat = last.ChatJID
		p.LastID = last.ID
		out.Next = dbutil.ID()
		e = s.Hub.Repo.Put("catch_pages", out.Next, p)
	} else {
		// Advance monotonically only after the final page. Explicit historical reads
		// cannot rewind an agent, and scope changes get independent cursor keys.
		if p.Until.After(cursor) {
			out.Cursor = p.Until
			e = s.Hub.Repo.Put("cursors", key, p.Until)
		}
	}
	if e == nil {
		_, _ = s.Hub.Repo.DB.Exec("DELETE FROM objects WHERE bucket='catch_pages' AND julianday(json_extract(data,'$.Until'))<julianday(?)", s.Hub.Now().Add(-time.Hour))
	}
	return out, e
}
func Decode[T any](v any) (T, error) {
	var out T
	b, e := json.Marshal(v)
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	if e != nil {
		return out, model.ErrInvalid
	}
	return out, nil
}
