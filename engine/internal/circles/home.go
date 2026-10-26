package circles

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"time"
)

type RefreshItem struct {
	CircleID    string    `json:"circle_id"`
	Name        string    `json:"name"`
	NewMessages int       `json:"new_messages"`
	Since       time.Time `json:"since"`
}
type RefreshPlan struct {
	Circles    []RefreshItem `json:"circles"`
	Background bool          `json:"background"`
}

func (s *Service) newCount(jids []string, since time.Time) (int, error) {
	n := 0
	for _, j := range jids {
		var k int
		if e := s.Hub.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=? AND julianday(timestamp)>julianday(?) AND julianday(timestamp)<=julianday(?)", j, since, s.Hub.Now()).Scan(&k); e != nil {
			return 0, e
		}
		n += k
	}
	return n, nil
}
func (s *Service) Plan() (RefreshPlan, error) {
	out := RefreshPlan{Circles: []RefreshItem{}, Background: true}
	cs, e := s.List(nil)
	if e != nil {
		return out, e
	}
	for _, c := range cs {
		if c.Archived {
			continue
		}
		b, e := s.Latest(nil, c.ID)
		if e != nil {
			return out, e
		}
		since := s.Hub.CurrentSince()
		if b != nil {
			if b.CoversUntil.After(since) {
				since = b.CoversUntil
			}
		}
		n, e := s.newCount(JIDs(c), since)
		if e != nil {
			return out, e
		}
		if n > 0 {
			out.Circles = append(out.Circles, RefreshItem{c.ID, c.Name, n, since})
		}
	}
	return out, nil
}
func (s *Service) Settings() model.BriefSettings {
	v := model.BriefSettings{Time: "06:00"}
	_ = s.Hub.Repo.Get("brief_settings", "main", &v)
	return v
}
func (s *Service) SetSettings(v model.BriefSettings) error {
	if len(v.Time) != 5 {
		return model.ErrInvalid
	}
	if _, e := time.Parse("15:04", v.Time); e != nil {
		return model.ErrInvalid
	}
	if v.OnDeviceBriefs.Enabled {
		if _, e := s.Hub.EnableThisMac(); e != nil {
			return e
		}
	}
	return s.Hub.Repo.Put("brief_settings", "main", v)
}
func (s *Service) Home() (any, error) {
	cs, e := s.List(nil)
	if e != nil {
		return nil, e
	}
	today := []any{}
	needsReply := []any{}
	pinned, live := []map[string]any{}, []map[string]any{}
	for _, c := range cs {
		if c.Archived {
			continue
		}
		b, e := s.Latest(nil, c.ID)
		if e != nil {
			return nil, e
		}
		since := s.Hub.CurrentSince()
		asks, upcoming := 0, 0
		if b != nil {
			if b.CoversUntil.After(since) {
				since = b.CoversUntil
			}
			for _, a := range b.OpenAsks {
				if s.currentAsk(c, a) {
					asks++
				}
			}
			upcoming = len(b.Upcoming)
		}
		n, e := s.newCount(JIDs(c), since)
		if e != nil {
			return nil, e
		}
		busySince := s.Hub.Now().Add(-24 * time.Hour)
		if since.After(busySince) {
			busySince = since
		}
		recent, e := s.newCount(JIDs(c), busySince)
		if e != nil {
			return nil, e
		}
		why := ""
		if c.Pinned {
			why = "pinned"
		} else if asks > 0 {
			why = "open_ask"
		} else if recent >= 5 {
			why = "busy"
		}
		card := map[string]any{"circle": c, "brief": b, "new_messages": n, "open_asks": asks, "upcoming": upcoming, "pinned": c.Pinned, "why_shown": why}
		if c.Pinned {
			pinned = append(pinned, card)
		} else if why != "" {
			live = append(live, card)
		}
		if b != nil {
			for _, a := range b.OpenAsks {
				if !s.currentAsk(c, a) {
					continue
				}
				needsReply = append(needsReply, map[string]any{"id": a.ID, "circle_id": c.ID, "circle": c.Name, "text": a.Text, "chat": a.Chat, "chat_jid": a.ChatJID, "from": a.From, "at": a.At})
			}
		}
		if n > 0 || asks > 0 || upcoming > 0 {
			today = append(today, map[string]any{"circle": c, "brief": b, "new_messages": n, "open_asks": asks, "upcoming": upcoming})
		}
	}
	sg, e := s.Hub.Suggestions()
	if e != nil {
		return nil, e
	}
	ps, e := s.Proposals()
	if e != nil {
		return nil, e
	}
	as, e := s.Hub.Agents()
	if e != nil {
		return nil, e
	}
	integrations := []any{}
	for _, a := range as {
		if a.Enabled {
			integrations = append(integrations, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind, "last_seen_at": a.LastSeenAt, "access": a.Access, "access_summary": a.AccessSummary})
		}
	}
	queued := []model.Suggestion{}
	for _, x := range sg {
		if x.Kind == "reply" && x.Body != "" || x.Kind == "check_in" || x.Kind == "nudge" {
			queued = append(queued, x)
		}
	}
	// Ask-bearing circles precede busy-only circles, then count and stable ID.
	sort.SliceStable(live, func(i, j int) bool {
		ai, aj := live[i]["open_asks"].(int) > 0, live[j]["open_asks"].(int) > 0
		if ai != aj {
			return ai
		}
		ni, nj := live[i]["new_messages"].(int), live[j]["new_messages"].(int)
		if ni != nj {
			return ni > nj
		}
		return live[i]["circle"].(model.Circle).ID < live[j]["circle"].(model.Circle).ID
	})
	if len(live) > 3 {
		live = live[:3]
	}
	cards := append(pinned, live...)
	activity, e := s.Hub.Activity(nil, hub.ActivityFilter{Limit: 6})
	if e != nil {
		return nil, e
	}
	additions, e := s.Additions()
	if e != nil {
		return nil, e
	}
	changes, e := s.AutoChanges()
	if e != nil {
		return nil, e
	}
	more := max(0, len(changes)-3)
	if len(changes) > 3 {
		changes = changes[:3]
	}
	return map[string]any{"circle_additions": additions, "circle_changes": changes, "circle_changes_more": more, "today": today, "cards": today, "suggestions": sg, "proposals": ps, "integrations": integrations, "queued": queued, "needs_reply": needsReply, "circles": cards, "recent_activity": activity}, nil
}
