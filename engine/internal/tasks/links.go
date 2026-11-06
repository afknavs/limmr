package tasks

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/wa"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var links = regexp.MustCompile(`https?://[^\s<>"\x60]+`)

func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimRight(raw, ".,;!?)\"'"))
	if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		u.Host = u.Hostname()
	}
	if u.Path == "/" {
		u.Path = ""
	}
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
func labelURL(raw string) string {
	u, _ := url.Parse(raw)
	switch u.Hostname() {
	case "notebooklm.google.com":
		return "NotebookLM"
	case "docs.google.com":
		return "Google Docs"
	case "drive.google.com":
		return "Google Drive"
	case "github.com":
		return "GitHub"
	}
	return u.Hostname()
}
func appendItem(xs []Item, it Item) []Item {
	if it.Kind == "file" && it.MediaType == "" {
		switch strings.ToLower(lastExtension(it.Title)) {
		case "jpg", "jpeg", "png", "gif", "heic", "webp":
			it.MediaType = "image"
		case "mp4", "mov", "m4v", "webm":
			it.MediaType = "video"
		default:
			it.MediaType = "document"
		}
		if it.AIName == "" {
			it.AIName = assetName(it)
		}
	}
	for _, x := range xs {
		if it.ID != "" && x.ID == it.ID {
			// Enrich pre-existing scanned items without replacing on-device names.
			if x.MediaType == "" && it.MediaType != "" {
				it.AIName = x.AIName
				if it.AIName == "" {
					it.AIName = assetName(it)
				}
				it.OCRText = x.OCRText
				it.Description = x.Description
				it.NamedOnDevice = x.NamedOnDevice
				for i := range xs {
					if xs[i].ID == it.ID {
						xs[i] = it
						break
					}
				}
			}
			return xs
		}
	}

	if it.Kind == "link" {
		it.URL = normalizeURL(it.URL)
		it.Label = labelURL(it.URL)
		for _, x := range xs {
			if x.Kind == "link" && normalizeURL(x.URL) == it.URL {
				return xs
			}
		}
	}
	return append(xs, it)
}
func (s *Service) scan(r *record, refs []Ref) error {
	add := func(m model.Message) {
		if m.DeletedAt != nil {
			return
		}
		if m.MediaType != "" && m.MediaType != "reaction" {
			title := m.Filename
			if title == "" {
				title = strings.ToUpper(m.MediaType[:1]) + m.MediaType[1:]
			}
			it := Item{ID: "media_" + dbutil.Digest(m.ChatJID+"\x00"+m.ID), Kind: "file", Title: title, AttachmentID: "media:" + m.ID, Source: "chat", MessageRef: &Ref{ChatJID: m.ChatJID, MessageID: m.ID}, AddedBy: "chat", AddedAt: m.Timestamp, MediaType: m.MediaType, Caption: m.Content}
			it.Chat = "Chat"
			for _, ch := range r.Chats {
				if ch.ID == m.ChatJID {
					it.Chat = ch.Name
				}
			}
			it.Sender = "Someone in the chat"
			if m.IsFromMe {
				it.Sender = "You"
			} else {
				jid := m.Sender
				if jid == "" {
					jid = m.ChatJID
				}
				var raw string
				if s.Hub.Repo.DB.QueryRow("SELECT o.data FROM people_jids p JOIN objects o ON o.bucket='people' AND o.id=p.person_id WHERE p.jid=? LIMIT 1", jid).Scan(&raw) == nil {
					var person model.Person
					if json.Unmarshal([]byte(raw), &person) == nil && personName(person) != "" {
						it.Sender = personName(person)
					}
				}
			}
			_ = s.Hub.Messages.DB.QueryRow("SELECT coalesce(preview_width,0),coalesce(preview_height,0) FROM messages WHERE id=? AND chat_jid=?", m.ID, m.ChatJID).Scan(&it.Width, &it.Height)
			if it.Caption == "" {
				var context string
				_ = s.Hub.Messages.DB.QueryRow("SELECT coalesce(content,'') FROM messages WHERE chat_jid=? AND id<>? AND deleted_at IS NULL AND content<>'' AND julianday(timestamp)<=julianday(?) AND julianday(timestamp)>=julianday(?) ORDER BY julianday(timestamp) DESC LIMIT 1", m.ChatJID, m.ID, m.Timestamp, m.Timestamp.Add(-5*time.Minute)).Scan(&context)
				it.Caption = short(context, 500)
			}
			it.AIName = assetName(it)
			r.Items = appendItem(r.Items, it)
		}
		for _, raw := range links.FindAllString(m.Content, -1) {
			u := normalizeURL(raw)
			if u == "" {
				continue
			}
			it := Item{ID: "itm_" + dbutil.ID(), Kind: "link", Title: labelURL(u), URL: u, Source: "chat", MessageRef: &Ref{ChatJID: m.ChatJID, MessageID: m.ID}, AddedBy: "chat", AddedAt: m.Timestamp, Caption: m.Content}
			it.AIName = assetName(it)
			it.Sender = "Someone in the chat"
			if m.IsFromMe {
				it.Sender = "You"
			} else {
				var p model.Person
				if s.Hub.Repo.Get("people", model.PersonID(m.Sender), &p) == nil && personName(p) != "" {
					it.Sender = personName(p)
				}
			}
			it.Chat = "Chat"
			for _, ch := range r.Chats {
				if ch.ID == m.ChatJID {
					it.Chat = ch.Name
				}
			}
			r.Items = appendItem(r.Items, it)
		}
	}
	for _, ref := range refs {
		ms, err := s.Hub.Messages.Messages(wa.Query{Chat: ref.ChatJID, ID: ref.MessageID, Limit: 1}, []string{ref.ChatJID})
		if err != nil {
			return err
		}
		for _, m := range ms {
			add(m)
		}
	}
	for _, c := range r.Chats {
		if s.Hub.HiddenChat(c.ID) {
			continue
		}
		// A task is often kept after its poster was shared. Include a bounded
		// recent media window before creation rather than missing those images.
		rows, err := s.Hub.Messages.DB.Query("SELECT id FROM messages WHERE chat_jid=? AND deleted_at IS NULL AND media_type IN ('image','video','document') AND julianday(timestamp)<julianday(?) AND julianday(timestamp)>=julianday(?) ORDER BY julianday(timestamp) DESC LIMIT 24", c.ID, r.CreatedAt, r.CreatedAt.AddDate(0, 0, -30))
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			ms, err := s.Hub.Messages.Messages(wa.Query{Chat: c.ID, ID: id, Limit: 1}, []string{c.ID})
			if err != nil {
				return err
			}
			for _, m := range ms {
				add(m)
			}
		}
		for offset := 0; ; offset += 1000 {
			ms, err := s.Hub.Messages.Messages(wa.Query{Chat: c.ID, After: r.CreatedAt.Add(-time.Nanosecond).Format(time.RFC3339Nano), Offset: offset, Limit: 1000}, []string{c.ID})
			if err != nil {
				return err
			}
			for _, m := range ms {
				add(m)
			}
			if len(ms) < 1000 {
				break
			}
		}
	}
	s.chooseCover(&r.Task)
	return nil
}
