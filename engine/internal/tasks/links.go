package tasks

import (
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
	for _, x := range xs {
		if it.ID != "" && x.ID == it.ID {
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
		for _, raw := range links.FindAllString(m.Content, -1) {
			u := normalizeURL(raw)
			if u == "" {
				continue
			}
			it := Item{ID: "itm_" + dbutil.ID(), Kind: "link", Title: labelURL(u), URL: u, Source: "chat", MessageRef: &Ref{ChatJID: m.ChatJID, MessageID: m.ID}, AddedBy: "chat", AddedAt: s.now()}
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
	return nil
}
