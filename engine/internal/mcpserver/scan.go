package mcpserver

import (
	"encoding/json"
	"limmr/internal/model"
	"limmr/internal/wa"
	"sort"
	"strings"
)

type scanLine struct {
	Chat    string               `json:"chat"`
	Name    string               `json:"name"`
	Circle  string               `json:"circle,omitempty"`
	Last    string               `json:"last_activity,omitempty"`
	Count   int                  `json:"messages"`
	Senders []string             `json:"top_senders,omitempty"`
	Preview string               `json:"last_message,omitempty"`
	Author  *model.MessageAuthor `json:"author,omitempty"`
	Unread  bool                 `json:"unread_for_owner"`
}

func (s *Service) scan(a model.Agent, args Args) (any, error) {
	days := args.num("since_days", 30)
	if days < 1 || days > 3650 {
		return nil, model.ErrInvalid
	}
	limit := min(args.num("limit", 40), 40)
	if limit < 1 {
		return nil, model.ErrInvalid
	}
	since := s.Hub.Now().AddDate(0, 0, -days)
	cs, e := s.CircleService().List(&a)
	if e != nil {
		return nil, e
	}
	circles := map[string]string{}
	for _, c := range cs {
		if args.str("circle_id") != "" && c.ID != args.str("circle_id") {
			continue
		}
		for _, j := range c.Chats {
			circles[j.ChatJID] = c.Name
		}
	}
	chats, _, e := s.Hub.Visible(&a)
	if e != nil {
		return nil, e
	}
	ps, e := s.People.People(&a, "")
	if e != nil {
		return nil, e
	}
	names := map[string]string{}
	for _, p := range ps {
		n := p.Label
		if n == "" {
			n = p.Name
		}
		for _, j := range p.JIDs {
			names[j] = n
			names[strings.SplitN(j, "@", 2)[0]] = n
		}
	}
	out := []scanLine{}
	for _, c := range chats {
		if len(out) >= limit {
			break
		}
		if c.LastMessageTime == nil || c.LastMessageTime.Before(since) || !s.Hub.Current(c) || args.str("circle_id") != "" && circles[c.JID] == "" {
			continue
		}
		v := scanLine{Chat: c.JID, Name: trim(c.Name, 48), Circle: trim(circles[c.JID], 32), Last: c.LastMessageTime.UTC().Format("2006-01-02")}
		var count int
		e = s.Hub.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=? AND julianday(timestamp)>=julianday(?)", c.JID, since).Scan(&count)
		if e != nil {
			return nil, e
		}
		v.Count = count
		rows, e := s.Hub.Messages.DB.Query("SELECT COALESCE(sender,''),count(*) AS n FROM messages WHERE chat_jid=? AND julianday(timestamp)>=julianday(?) AND is_from_me=0 GROUP BY sender ORDER BY n DESC,sender LIMIT 3", c.JID, since)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var j string
			var n int
			if e = rows.Scan(&j, &n); e != nil {
				rows.Close()
				return nil, e
			}
			if names[j] != "" {
				v.Senders = append(v.Senders, trim(names[j], 32))
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		ms, e := s.Hub.Messages.Messages(wa.Query{Chat: c.JID, Limit: 1}, []string{c.JID})
		if e != nil {
			return nil, e
		}
		if len(ms) > 0 {
			m, err := s.Hub.Attribute(ms[0])
			if err != nil {
				return nil, err
			}
			v.Author = &m.Author
			v.Preview = trim(ms[0].Content, 80)
			if ms[0].MediaType != "" {
				v.Preview = "[" + ms[0].MediaType + "]"
			}
			v.Unread = !ms[0].IsFromMe && s.unread(c.JID, ms[0].ID)
		}
		candidate := append(out, v)
		b, _ := json.Marshal(candidate)
		escaped, _ := json.Marshal(string(b))
		if len(escaped)+200 >= 6000 {
			break
		}
		out = candidate
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out, nil
}
func trim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// No WhatsApp receipts are sent: history's owner unread count is cached locally.
func (s *Service) unread(chat, id string) bool {
	var n int
	_ = s.Hub.Messages.DB.QueryRow("SELECT COALESCE(unread_count,0) FROM chats WHERE jid=?", chat).Scan(&n)
	return n > 0
}
