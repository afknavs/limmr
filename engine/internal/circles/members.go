package circles

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
)

func (s *Service) EditMember(c *model.Circle, kind, method, id string, raw map[string]json.RawMessage) error {
	switch kind {
	case "chats":
		v := model.CircleChat{}
		index := -1
		for i, x := range c.Chats {
			if x.ChatJID == id {
				v = x
				index = i
			}
		}
		if method != "POST" && index < 0 {
			return model.ErrNotFound
		}
		if method == "DELETE" {
			c.Chats = append(c.Chats[:index], c.Chats[index+1:]...)
			for i := range c.Topics {
				js := []string{}
				for _, j := range c.Topics[i].Chats {
					if j != id {
						js = append(js, j)
					}
				}
				c.Topics[i].Chats = js
			}
			return nil
		}
		fields := []string{"role_in_circle"}
		if method == "POST" {
			fields = append(fields, "chat_jid")
		}
		if e := Patch(&v, raw, fields...); e != nil {
			return e
		}
		if method == "POST" {
			for _, x := range c.Chats {
				if x.ChatJID == v.ChatJID {
					return model.ErrConflict
				}
			}
			v.IncludedSince = s.Hub.Now().UTC()
			c.Chats = append(c.Chats, v)
		} else {
			c.Chats[index] = v
		}
	case "people":
		v := model.CirclePerson{}
		index := -1
		for i, x := range c.People {
			if x.PersonID == id {
				v = x
				index = i
			}
		}
		if method != "POST" && index < 0 {
			return model.ErrNotFound
		}
		if method == "DELETE" {
			c.People = append(c.People[:index], c.People[index+1:]...)
			for i := range c.Topics {
				ps := []string{}
				for _, p := range c.Topics[i].People {
					if p != id {
						ps = append(ps, p)
					}
				}
				c.Topics[i].People = ps
			}
			return nil
		}
		fields := []string{"role", "note"}
		if method == "POST" {
			fields = append(fields, "person_id", "source")
		}
		if e := Patch(&v, raw, fields...); e != nil {
			return e
		}
		if v.Source == "" {
			v.Source = "you"
		}
		if method == "POST" {
			for _, x := range c.People {
				if x.PersonID == v.PersonID {
					return model.ErrConflict
				}
			}
			c.People = append(c.People, v)
		} else {
			c.People[index] = v
		}
	case "topics":
		v := model.Topic{Status: "active", Chats: []string{}, People: []string{}, Keywords: []string{}}
		index := -1
		for i, x := range c.Topics {
			if x.ID == id {
				v = x
				index = i
			}
		}
		if method != "POST" && index < 0 {
			return model.ErrNotFound
		}
		if method == "DELETE" {
			c.Topics = append(c.Topics[:index], c.Topics[index+1:]...)
			return nil
		}
		if e := Patch(&v, raw, "title", "status", "chats", "people", "keywords"); e != nil {
			return e
		}
		v.LastActivityAt = s.Hub.Now().UTC()
		if method == "POST" {
			v.ID = dbutil.ID()
			c.Topics = append(c.Topics, v)
		} else {
			c.Topics[index] = v
		}
	default:
		return model.ErrInvalid
	}
	return nil
}
