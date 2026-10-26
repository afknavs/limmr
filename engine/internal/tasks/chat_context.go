package tasks

import (
	"context"
	"database/sql"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"strings"
	"unicode"
)

// Ignore phone-only directory entries, including formatted numbers and raw JIDs.
func personName(p model.Person) string {
	for _, name := range []string{p.Label, p.Name, p.PushName} {
		name = strings.TrimSpace(name)
		if name != "" && !strings.Contains(name, "@") && strings.ContainsFunc(name, unicode.IsLetter) {
			return name
		}
	}
	return ""
}

type chatContext struct {
	chats     map[string]string
	people    map[string]model.Person
	byJID     map[string]model.Person
	members   []model.Membership
	hidden    func(string) bool
	canonical func(string) string
}

func (s *Service) chatContext() (chatContext, error) {
	c := chatContext{chats: map[string]string{}, people: map[string]model.Person{}, byJID: map[string]model.Person{}, hidden: s.Hub.HiddenChatAccess()}
	aliases, err := s.Hub.Messages.Aliases()
	if err != nil {
		return c, err
	}
	c.canonical = func(j string) string {
		if v := aliases[j]; v != "" {
			return v
		}
		return j
	}
	ps, err := hub.List[model.Person](s.Hub.Repo, "people")
	if err != nil {
		return c, err
	}
	for _, p := range ps {
		c.people[p.ID] = p
		for _, j := range p.JIDs {
			c.byJID[c.canonical(j)] = p
		}
	}
	chats, err := s.Hub.Messages.Chats()
	if err != nil {
		return c, err
	}
	for _, chat := range chats {
		c.chats[chat.JID] = chat.Name
	}
	c.members, err = hub.List[model.Membership](s.Hub.Repo, "memberships")
	return c, err
}
func (c chatContext) hiddenPerson(p model.Person) bool {
	for _, j := range p.JIDs {
		if c.hidden(j) {
			return true
		}
	}
	return false
}
func (c chatContext) peopleFor(chats []Entity) []Entity {
	out := []Entity{}
	for _, ch := range chats {
		if c.hidden(ch.ID) {
			continue
		}
		ids := map[string]bool{}
		if strings.HasSuffix(ch.ID, "@g.us") {
			for _, m := range c.members {
				if m.Group == ch.ID && m.Person != "me" {
					if p, ok := c.people[m.Person]; ok && personName(p) != "" {
						ids[p.ID] = true
					}
				}
			}
			// Count known members before hiding people so privacy never makes a big group small.
			if len(ids) > 3 {
				continue
			}
		} else if p, ok := c.byJID[c.canonical(ch.ID)]; ok && p.ID != "me" && personName(p) != "" {
			ids[p.ID] = true
		}
		sorted := []string{}
		for id := range ids {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		for _, id := range sorted {
			p := c.people[id]
			if !c.hiddenPerson(p) {
				out = mergeEntities(out, []Entity{{ID: id, Name: personName(p)}})
			}
		}
	}
	return out
}
func (s *Service) latest(t Task) ([]LatestMessage, error) {
	out := []LatestMessage{}
	if s.Hub.Messages == nil {
		return out, nil
	}
	c, err := s.chatContext()
	if err != nil {
		return nil, err
	}
	// Filter hidden senders before LIMIT, including every alias and every JID of a hidden person.
	blocked := map[string]bool{}
	for _, p := range c.people {
		if c.hiddenPerson(p) {
			for _, j := range p.JIDs {
				blocked[c.canonical(j)] = true
			}
		}
	}
	aliases, err := s.Hub.Messages.Aliases()
	if err != nil {
		return nil, err
	}
	for alias, target := range aliases {
		if c.hidden(alias) || blocked[c.canonical(target)] {
			blocked[alias] = true
		}
	}
	for _, j := range s.Hub.Settings().HiddenChats {
		blocked[j] = true
		blocked[c.canonical(j)] = true
	}
	for _, ch := range t.Chats {
		if c.hidden(ch.ID) {
			continue
		}
		if p, ok := c.byJID[c.canonical(ch.ID)]; ok && c.hiddenPerson(p) {
			continue
		}
		args := []any{ch.ID}
		where := ""
		for j := range blocked {
			where += " AND (is_from_me=1 OR COALESCE(sender,'')<>?)"
			args = append(args, j)
		}
		rows, err := s.Hub.Messages.DB.Query("SELECT id,COALESCE(sender,''),timestamp,is_from_me,COALESCE(content,''),COALESCE(media_type,'') FROM messages WHERE chat_jid=? AND deleted_at IS NULL"+where+" ORDER BY julianday(timestamp) DESC,id DESC LIMIT 3", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m model.Message
			if err = rows.Scan(&m.ID, &m.Sender, &m.Timestamp, &m.IsFromMe, &m.Content, &m.MediaType); err != nil {
				break
			}
			sender := "Someone in the chat"
			jid := m.Sender
			if jid == "" && !strings.HasSuffix(ch.ID, "@g.us") {
				jid = ch.ID
			}
			if p, ok := c.byJID[c.canonical(jid)]; ok {
				if n := personName(p); n != "" {
					sender = n
				}
				if p.ID == "me" {
					sender = "You"
				}
			}
			if m.IsFromMe {
				sender = "You"
			}
			text := m.Content
			if strings.TrimSpace(text) == "" {
				text = map[string]string{"image": "Photo", "video": "Video", "audio": "Voice message", "ptt": "Voice message", "sticker": "Sticker", "document": "Document"}[m.MediaType]
				if text == "" {
					text = "Media message"
				}
			}
			chat := strings.TrimSpace(c.chats[ch.ID])
			if chat == "" {
				chat = strings.TrimSpace(ch.Name)
			}
			if p, ok := c.byJID[c.canonical(ch.ID)]; ok && personName(p) != "" {
				chat = personName(p)
			}
			if chat == "" || strings.Contains(chat, "@") || !strings.ContainsFunc(chat, unicode.IsLetter) {
				chat = "Chat"
			}
			out = append(out, LatestMessage{ChatJID: ch.ID, MessageID: m.ID, Chat: chat, Sender: sender, Timestamp: m.Timestamp, Text: text})
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.After(out[j].Timestamp)
		}
		if out[i].MessageID != out[j].MessageID {
			return out[i].MessageID > out[j].MessageID
		}
		return out[i].ChatJID > out[j].ChatJID
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out, nil
}

// Startup only; each row is rechecked inside its transaction. The event and links commit together.
func (s *Service) BackfillChatPeople(ctx context.Context) error {
	c, err := s.chatContext()
	if err != nil {
		return err
	}
	rs, err := s.records()
	if err != nil {
		return err
	}
	for _, r := range rs {
		if err := ctx.Err(); err != nil {
			return err
		}
		eligible := func(t Task) bool {
			return t.Origin == "agent" && t.Suggestion != nil && (t.Status == "active" || t.Status == "suggested") && len(t.Chats) > 0 && len(t.People) == 0
		}
		if !eligible(r.Task) {
			continue
		}
		ps := c.peopleFor(r.Chats)
		if len(ps) == 0 {
			continue
		}
		_, err = s.mutate(nil, r.ID, func(r *record, tx *sql.Tx) error {
			if !eligible(r.Task) {
				return nil
			}
			r.People = ps
			r.UpdatedAt = s.now()
			return s.event(tx, *r, "chat_people_backfill", "people_linked", "Linked people from the chat", nil)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
