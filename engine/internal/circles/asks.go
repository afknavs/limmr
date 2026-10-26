package circles

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"strings"
)

func AskID(circle, text, chat string) string {
	b, _ := json.Marshal([]string{circle, text, chat})
	return "ask-" + dbutil.Digest(string(b))[:24]
}
func (s *Service) normalizeAsks(c model.Circle, b *model.Brief) error {
	chats, _, err := s.Hub.Visible(nil)
	if err != nil {
		return err
	}
	titles := map[string]string{}
	for _, chat := range chats {
		titles[chat.JID] = chat.Name
	}
	for i := range b.OpenAsks {
		a := &b.OpenAsks[i]
		if strings.TrimSpace(a.Text) == "" || len(a.Text) > 65536 {
			return model.ErrInvalid
		}
		if a.ChatJID != "" && !model.Contains(b.ChatJIDs, a.ChatJID) {
			return model.ErrNotFound
		}
		a.ID = AskID(c.ID, a.Text, a.ChatJID)
		a.Chat = titles[a.ChatJID]
		if a.At.IsZero() {
			a.At = b.WrittenAt
		}
	}
	return nil
}
func (s *Service) presentAsks(c model.Circle, b *model.Brief) error {
	if err := s.normalizeAsks(c, b); err != nil {
		return err
	}
	out := []model.OpenAsk{}
	for _, a := range b.OpenAsks {
		var version int
		err := s.Hub.Repo.Get("done_asks", c.ID+":"+a.ID, &version)
		if err != nil && err != model.ErrNotFound {
			return err
		}
		if b.Version > version {
			out = append(out, a)
		}
	}
	b.OpenAsks = out
	return nil
}
func (s *Service) DoneAsk(circle, id string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	b, err := s.Latest(nil, circle)
	if err != nil {
		return err
	}
	if b == nil {
		return model.ErrNotFound
	}
	for _, a := range b.OpenAsks {
		if a.ID == id {
			return s.Hub.Repo.Put("done_asks", circle+":"+id, b.Version)
		}
	}
	return model.ErrNotFound
}
