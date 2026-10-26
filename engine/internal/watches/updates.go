package watches

import (
	"context"
	"database/sql"
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"strings"
	"time"
)

type UpdateItem struct {
	TaskID   string `json:"task_id,omitempty"`
	Note     string `json:"note,omitempty"`
	Progress *struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress,omitempty"`
	Kind       string    `json:"kind"`
	At         time.Time `json:"at"`
	WatchID    string    `json:"watch_id,omitempty"`
	WatchTitle string    `json:"watch_title,omitempty"`
	Chat       string    `json:"chat"`
	ChatJID    string    `json:"chat_jid"`
	Sender     string    `json:"sender"`
	Text       string    `json:"text"`
	MessageID  string    `json:"message_id"`
	SendID     string    `json:"send_id,omitempty"`
	Status     string    `json:"status,omitempty"`
}
type Updates struct {
	EffortsReady     bool         `json:"efforts_ready"`
	EffortNamingHint string       `json:"effort_naming_hint,omitempty"`
	Cursor           string       `json:"cursor"`
	Items            []UpdateItem `json:"items"`
	More             bool         `json:"more"`
}
type updateRow struct {
	seq                   int64
	at, kind, ref, status string
	part                  int
}

func excerpt(s string) string {
	r := []rune(s)
	if len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}

func (s *Service) updateItem(ctx context.Context, a model.Agent, row updateRow) (*UpdateItem, error) {
	item := UpdateItem{}
	var err error
	item.At, err = time.Parse(time.RFC3339Nano, row.at)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(row.kind, "task_") {
		return s.taskUpdate(a, row, item)
	}
	if row.kind == "hit" {
		var id, raw string
		err = s.Hub.Repo.DB.QueryRowContext(ctx, "SELECT watch_id,messages_json FROM trigger_hits WHERE id=?", row.ref).Scan(&id, &raw)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		} // Retention can remove the source before its cursor.
		w, err := s.Record(ctx, id)
		if err != nil {
			return nil, err
		}
		if w.Owner != a.ID || w.CreatedVia.Agent != a.ID || !a.Permissions.ReadMessages || !s.scopeAllowed(a, w) {
			return nil, nil
		}
		if w.Action == triggers.Feed && (w.Status != triggers.Active || w.ExpiresAt != nil && !s.Hub.Now().Before(*w.ExpiresAt)) {
			return nil, nil
		}
		var ms []triggers.IncomingMessage
		if err = json.Unmarshal([]byte(raw), &ms); err != nil {
			return nil, err
		}
		if row.part >= len(ms) {
			return nil, nil
		}
		m := ms[row.part]
		if !s.Hub.CanChat(&a, m.ChatJID) {
			return nil, nil
		}
		item.Kind = "watch_hit"
		if w.Action == triggers.Feed {
			item.Kind = "message"
		}
		item.WatchID = w.ID
		item.WatchTitle = w.Title
		if item.WatchTitle == "" {
			item.WatchTitle = short(w.Goal)
		}
		item.ChatJID = m.ChatJID
		item.Sender = m.Sender
		item.MessageID = m.ID
		item.Text = excerpt(m.Text)
		// Resolve current message text so deleted/edited messages do not leak stale text.
		ms2, e := s.Hub.Messages.Messages(wa.Query{ID: m.ID, Chat: m.ChatJID, Limit: 1}, []string{m.ChatJID})
		if e != nil {
			return nil, e
		}
		if len(ms2) == 0 || ms2[0].DeletedAt != nil {
			return nil, nil
		}
		item.Text = excerpt(ms2[0].Content)
	} else {
		var d model.Draft
		if row.kind == "drafts" {
			err = s.Hub.Repo.Get("drafts", row.ref, &d)
		} else {
			var ap model.Approval
			err = s.Hub.Repo.Get("approvals", row.ref, &ap)
			d = model.Draft{AgentID: ap.AgentID, ChatJID: ap.ChatJID, FinalText: ap.Text, MessageID: ap.MessageID}
		}
		if err == model.ErrNotFound {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if d.AgentID != a.ID || !s.Hub.CanChat(&a, d.ChatJID) {
			return nil, nil
		}
		item.Kind = "send_status"
		item.SendID = row.ref
		item.Status = row.status
		item.ChatJID = d.ChatJID
		item.Sender = a.Name
		item.Text = excerpt(d.FinalText)
		item.MessageID = d.MessageID
	}
	item.Chat = "Chat"
	var name string
	err = s.Hub.Messages.DB.QueryRowContext(ctx, "SELECT COALESCE(name,'') FROM chats WHERE jid=?", item.ChatJID).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if name != "" {
		item.Chat = name
	}
	if item.Kind != "send_status" && a.Permissions.ReadPeople {
		err = s.Hub.Repo.DB.QueryRowContext(ctx, `SELECT COALESCE(NULLIF(json_extract(o.data,'$.label'),''),json_extract(o.data,'$.name')) FROM people_jids j JOIN objects o ON o.bucket='people' AND o.id=j.person_id WHERE j.jid=? ORDER BY o.id LIMIT 1`, item.Sender).Scan(&name)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil && name != "" {
			item.Sender = name
		}
	}

	return &item, nil
}

func (s *Service) updatesPage(ctx context.Context, a model.Agent, c hub.UpdateCursor, limit int) (Updates, hub.UpdateCursor, error) {
	out := Updates{Items: []UpdateItem{}}
	var high int64
	if err := s.Hub.Repo.DB.QueryRowContext(ctx, "SELECT COALESCE(max(seq),0) FROM agent_updates WHERE agent=?", a.ID).Scan(&high); err != nil {
		return out, c, err
	}
	for {
		rows, err := s.Hub.Repo.DB.QueryContext(ctx, "SELECT seq,at,kind,ref,part,status FROM agent_updates WHERE agent=? AND seq>? AND seq<=? AND julianday(at)>=julianday(?) ORDER BY seq LIMIT 128", a.ID, c.Seq, high, c.Since)
		if err != nil {
			return out, c, err
		}
		batch := []updateRow{}
		for rows.Next() {
			var row updateRow
			if err = rows.Scan(&row.seq, &row.at, &row.kind, &row.ref, &row.part, &row.status); err != nil {
				break
			}
			batch = append(batch, row)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return out, c, err
		}
		for _, row := range batch {
			at, e := time.Parse(time.RFC3339Nano, row.at)
			if e != nil {
				return out, c, e
			}
			var item *UpdateItem
			if !at.Before(c.Since) {
				item, err = s.updateItem(ctx, a, row)
				if err != nil {
					return out, c, err
				}
			}
			if item != nil {
				if len(out.Items) == limit {
					out.More = true
					return out, c, nil
				}
				out.Items = append(out.Items, *item)
			}
			c.Seq = row.seq
		}
		if len(batch) < 128 {
			if high > c.Seq {
				c.Seq = high
			}
			return out, c, nil
		}
	}
}

// GetUpdates uses bounded polling over indexed durable events, not an unbounded
// subscription. Every pass reloads current agent permissions, including expiry.
func (s *Service) GetUpdates(ctx context.Context, agent, cursor string, wait, limit int) (Updates, error) {
	if wait < 0 || wait > 25 || limit < 1 || limit > 100 {
		return Updates{}, model.ErrInvalid
	}
	c, err := s.Hub.ReadUpdateCursor(agent, cursor)
	if err != nil {
		return Updates{}, err
	}
	deadline := time.NewTimer(time.Duration(wait) * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		a, e := s.Hub.Agent(agent)
		if e != nil {
			return Updates{}, e
		}
		if !a.Enabled || a.ID == "this-mac" || a.Kind == "on_device" {
			return Updates{}, model.ErrForbidden
		}
		if e = s.Hub.CheckAccess(a); e != nil {
			return Updates{}, e
		}
		if _, bound := hub.HandoffIdentity(ctx); bound {
			return Updates{}, model.ErrForbidden
		}
		out, next, e := s.updatesPage(ctx, a, c, limit)
		if e != nil {
			return out, e
		}
		c = next
		out.Cursor, e = s.Hub.WriteUpdateCursor(c)
		if e != nil {
			return out, e
		}
		if len(out.Items) > 0 || wait == 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return Updates{}, ctx.Err()
		case <-deadline.C:
			return out, nil
		case <-tick.C:
		}
	}
}

// Task events use the same private cursor and recheck all current chat permissions.
func (s *Service) taskUpdate(a model.Agent, row updateRow, item UpdateItem) (*UpdateItem, error) {
	if !a.Permissions.ReadMessages {
		return nil, nil
	}
	var raw, payload, id string
	err := s.Hub.Repo.DB.QueryRow("SELECT t.id,t.data,e.payload FROM task_events e JOIN tasks t ON t.id=e.task_id WHERE e.id=?", row.ref).Scan(&id, &raw, &payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t struct {
		Chats []struct {
			ID string `json:"id"`
		} `json:"chats"`
		CreatedBy struct {
			AgentID string `json:"agent_id"`
		} `json:"created_by"`
	}
	if err = json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, err
	}
	if row.kind == "task_suggested" && a.Preset != "own" || row.kind != "task_suggested" && t.CreatedBy.AgentID != a.ID {
		return nil, nil
	}
	for _, ch := range t.Chats {
		if !s.Hub.CanChat(&a, ch.ID) {
			return nil, nil
		}
	}
	item.Kind = row.kind
	item.TaskID = id
	if err = json.Unmarshal([]byte(payload), &item); err != nil {
		return nil, err
	}
	return &item, nil
}
