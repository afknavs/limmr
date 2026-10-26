package hub

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
)

// DigestCircles keeps projection and brief visibility in the existing circles
// service, without introducing a hub -> circles import cycle.
type DigestCircles interface {
	List(*model.Agent) ([]model.Circle, error)
	Latest(*model.Agent, string) (*model.Brief, error)
}

type DigestOptions struct{ Period, CircleID, Cursor string }
type Digest struct {
	WaitingOnYou  []DigestAsk          `json:"waiting_on_you"`
	Due           []DigestDue          `json:"due"`
	CircleUpdates []DigestCircleUpdate `json:"circle_updates"`
	AgentWork     []DigestAgentWork    `json:"agent_work"`
	Sends         []DigestSend         `json:"sends"`
	Cursor        string               `json:"cursor"`
}
type DigestAsk struct {
	CircleID     string    `json:"circle_id"`
	Who          string    `json:"who"`
	Ask          string    `json:"ask"`
	WaitingSince time.Time `json:"waiting_since"`
	ChatJID      string    `json:"chat_jid"`
	Chat         string    `json:"chat"`
}
type DigestDue struct {
	CircleID string `json:"circle_id"`
	Date     string `json:"date"`
	Text     string `json:"text"`
}
type DigestCircleUpdate struct {
	CircleID     string   `json:"circle_id"`
	Circle       string   `json:"circle"`
	NewMessages  int      `json:"new_messages"`
	ActivePeople []string `json:"active_people"`
	LatestLine   string   `json:"latest_line"`
}
type DigestAgentWork struct {
	AgentID  string       `json:"agent_id"`
	Agent    string       `json:"agent"`
	Messages int          `json:"messages"`
	Sessions int          `json:"sessions"`
	Tasks    []DigestTask `json:"tasks"`
}
type DigestTask struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Messages int       `json:"messages"`
	Chats    []string  `json:"chats"`
}
type DigestSend struct {
	AgentID string    `json:"agent_id"`
	Agent   string    `json:"agent"`
	ChatJID string    `json:"chat_jid"`
	To      string    `json:"to"`
	Text    string    `json:"text"`
	Status  string    `json:"status"`
	At      time.Time `json:"at"`
}

func digestText(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 160 {
		return string(r[:159]) + "…"
	}
	return string(r)
}

type digestCursor struct {
	Scope string    `json:"scope"`
	At    time.Time `json:"at"`
}

// GetDigest is local, on-demand aggregation only. No model, messenger, receipt,
// or catch-up cursor is invoked. Each call rebuilds from current access.
func (s *Service) GetDigest(a *model.Agent, o DigestOptions, circles DigestCircles) (Digest, error) {
	out := Digest{WaitingOnYou: []DigestAsk{}, Due: []DigestDue{}, CircleUpdates: []DigestCircleUpdate{}, AgentWork: []DigestAgentWork{}, Sends: []DigestSend{}}
	if a != nil && (!a.Enabled || !a.Permissions.ReadMessages || !a.Permissions.ReadPeople) {
		return out, model.ErrForbidden
	}
	if a != nil {
		if err := s.CheckAccess(*a); err != nil {
			return out, err
		}
	}
	if o.Period == "" {
		o.Period = "today"
	}
	if !model.Contains([]string{"today", "since_last", "week"}, o.Period) || o.Cursor != "" && o.Period != "since_last" {
		return out, model.ErrInvalid
	}
	now := s.Now().UTC()
	start := now.Truncate(24 * time.Hour)
	if o.Period == "week" {
		start = now.AddDate(0, 0, -7)
	}
	cs, err := circles.List(a)
	if err != nil {
		return out, err
	}
	allowed := s.ChatAccess(a)
	hidden := s.HiddenChatAccess()
	visible := map[string]bool{}
	selected := []model.Circle{}
	circleIDs := []string{}
	for _, c := range cs {
		if !c.Confirmed || c.Archived || o.CircleID != "" && c.ID != o.CircleID {
			continue
		}
		chats := []model.CircleChat{}
		for _, chat := range c.Chats {
			if allowed(chat.ChatJID) && !hidden(chat.ChatJID) {
				chats = append(chats, chat)
				visible[chat.ChatJID] = true
			}
		}
		if len(chats) > 0 {
			c.Chats = chats
			selected = append(selected, c)
			circleIDs = append(circleIDs, c.ID)
		}
	}
	if o.CircleID != "" && len(selected) == 0 {
		return out, model.ErrNotFound
	}
	chats, _, err := s.Visible(a)
	if err != nil {
		return out, err
	}
	names := map[string]string{}
	for _, chat := range chats {
		if !hidden(chat.JID) && (o.CircleID == "" || visible[chat.JID]) {
			visible[chat.JID] = true // Include permitted unsorted chats in activity.
			names[chat.JID] = digestText(chat.Name)
		}
	}
	ids := []string{}
	for jid := range visible {
		ids = append(ids, jid)
	}
	sort.Strings(ids)
	sort.Strings(circleIDs)
	principal := "owner"
	if a != nil {
		principal = "agent:" + a.ID + ":" + a.RunChat
	}
	// Bind cursors to the caller, circle selection AND current chat projection.
	// A scope change gets a fresh baseline instead of skipping newly visible data.
	scope := dbutil.Digest(dbutil.JSON([]any{principal, o.CircleID, ids, circleIDs}))
	exclusive := false
	if o.Period == "since_last" {
		var previous digestCursor
		if o.Cursor != "" {
			raw, e := base64.RawURLEncoding.DecodeString(o.Cursor)
			if e != nil || json.Unmarshal(raw, &previous) != nil || previous.Scope != scope || previous.At.IsZero() || previous.At.After(now) {
				return out, model.ErrInvalid
			}
		} else if e := s.Repo.Get("digest_cursors", scope, &previous); e != nil && e != model.ErrNotFound {
			return out, e
		}
		if !previous.At.IsZero() {
			start = previous.At
			exclusive = true
		}
	}
	inWindow := func(at time.Time) bool {
		return !at.After(now) && (!at.Before(start) && (!exclusive || at.After(start)))
	}
	for _, c := range selected {
		b, e := circles.Latest(a, c.ID)
		if e != nil {
			return out, e
		}
		if b != nil {
			// Owner projections normally include hidden chats. Drop whole briefs
			// mentioning any such chat; their free text cannot be safely split.
			full := len(b.ChatJIDs) > 0
			for _, jid := range b.ChatJIDs {
				full = full && visible[jid] && allowed(jid) && !hidden(jid)
			}
			if full {
				for _, ask := range b.OpenAsks {
					if inWindow(ask.At) && (ask.ChatJID == "" || visible[ask.ChatJID]) {
						out.WaitingOnYou = append(out.WaitingOnYou, DigestAsk{c.ID, digestText(ask.From), digestText(ask.Text), ask.At, ask.ChatJID, names[ask.ChatJID]})
					}
				}
				if inWindow(b.WrittenAt) {
					for _, due := range b.Upcoming {
						out.Due = append(out.Due, DigestDue{c.ID, digestText(due.Date), digestText(due.Text)})
					}
				}
			}
		}
		u := DigestCircleUpdate{CircleID: c.ID, Circle: digestText(c.Name), ActivePeople: []string{}}
		latest := time.Time{}
		people := map[string]int{}
		for _, chat := range c.Chats {
			// Aggregate in SQL, loading only one short meaningful preview per chat.
			rows, e := s.Messages.DB.Query(`SELECT COALESCE(sender,''),COUNT(*) FROM messages WHERE chat_jid=? AND deleted_at IS NULL AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?)`+digestLower(exclusive)+` GROUP BY sender ORDER BY sender`, chat.ChatJID, start, now, start)
			if e != nil {
				return out, e
			}
			for rows.Next() {
				var sender string
				var n int
				if e = rows.Scan(&sender, &n); e != nil {
					rows.Close()
					return out, e
				}
				u.NewMessages += n
				if sender != "" {
					people[sender] += n
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return out, e
			}
			var line string
			var at time.Time
			e = s.Messages.DB.QueryRow(`SELECT substr(content,1,161),timestamp FROM messages WHERE chat_jid=? AND deleted_at IS NULL AND length(trim(content))>0 AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?)`+digestLower(exclusive)+` ORDER BY julianday(timestamp) DESC,id DESC LIMIT 1`, chat.ChatJID, start, now, start).Scan(&line, &at)
			if e != nil && e != sql.ErrNoRows {
				return out, e
			}
			if e == nil && at.After(latest) {
				latest, u.LatestLine = at, digestText(line)
			}
		}
		for p := range people {
			u.ActivePeople = append(u.ActivePeople, p)
		}
		sort.Slice(u.ActivePeople, func(i, j int) bool {
			x, y := u.ActivePeople[i], u.ActivePeople[j]
			if people[x] == people[y] {
				return x < y
			}
			return people[x] > people[y]
		})
		u.ActivePeople = u.ActivePeople[:min(len(u.ActivePeople), 8)]
		for i := range u.ActivePeople {
			u.ActivePeople[i] = digestText(u.ActivePeople[i])
		}
		if u.NewMessages > 0 {
			out.CircleUpdates = append(out.CircleUpdates, u)
		}
	}
	// Use the same ownership restriction as my_activity and the U3 digest.
	rows, err := s.Repo.DB.Query("SELECT id,data FROM activity WHERE julianday(at)>=julianday(?) AND julianday(at)<=julianday(?) ORDER BY id DESC", start, now)
	if err != nil {
		return out, err
	}
	events := []model.Activity{}
	for rows.Next() {
		var v model.Activity
		var raw string
		var id int64
		if err = rows.Scan(&id, &raw); err == nil {
			err = decode(raw, &v)
		}
		if err != nil {
			rows.Close()
			return out, err
		}
		if !inWindow(v.At) || a != nil && v.AgentID != a.ID || len(v.ChatJIDs) == 0 {
			continue
		}
		ok := true
		for _, jid := range v.ChatJIDs {
			ok = ok && visible[jid]
		}
		if ok {
			v.ID = id
			events = append(events, v)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	agents, err := s.Agents()
	if err != nil {
		return out, err
	}
	agentNames := map[string]string{}
	for _, agent := range agents {
		agentNames[agent.ID] = digestText(agent.Name)
	}
	work := map[string]*DigestAgentWork{}
	for _, session := range GroupReadSessions(events) {
		w := work[session.AgentID]
		if w == nil {
			w = &DigestAgentWork{AgentID: session.AgentID, Agent: agentNames[session.AgentID], Tasks: []DigestTask{}}
			work[session.AgentID] = w
		}
		w.Messages += session.Messages
		w.Sessions++
		w.Tasks = append(w.Tasks, DigestTask{session.Start, session.End, session.Messages, session.Chats})
	}
	for _, w := range work {
		out.AgentWork = append(out.AgentWork, *w)
	}
	sort.Slice(out.AgentWork, func(i, j int) bool { return out.AgentWork[i].AgentID < out.AgentWork[j].AgentID })
	for _, v := range events {
		if v.Kind != "sent" {
			continue
		}
		for _, jid := range v.ChatJIDs {
			status := "sent"
			for _, id := range v.MessageIDs {
				var receipt sql.NullString
				e := s.Repo.DB.QueryRow(`SELECT json_extract(data,'$.status') FROM objects WHERE bucket IN ('drafts','approvals') AND json_extract(data,'$.chat_jid')=? AND json_extract(data,'$.message_id')=? ORDER BY bucket DESC LIMIT 1`, jid, id).Scan(&receipt)
				if e != nil && e != sql.ErrNoRows {
					return out, e
				}
				if receipt.Valid && receipt.String != "" {
					status = receipt.String
				}
			}
			out.Sends = append(out.Sends, DigestSend{v.AgentID, agentNames[v.AgentID], jid, names[jid], digestText(v.Note), digestText(status), v.At})
		}
	}
	current := digestCursor{scope, now}
	if err = s.Repo.Put("digest_cursors", scope, current); err != nil {
		return out, err
	}
	out.Cursor = base64.RawURLEncoding.EncodeToString([]byte(dbutil.JSON(current)))
	return out, nil
}

func digestLower(exclusive bool) string {
	if exclusive {
		return " AND julianday(timestamp)>julianday(?)"
	}
	return " AND julianday(timestamp)>=julianday(?)"
}
