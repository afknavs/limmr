package people

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"time"
)

func (s *Service) enrichGroups(a *model.Agent, gs []model.Group) error {
	cs, e := hub.List[model.Circle](s.Hub.Repo, "circles")
	if e != nil {
		return e
	}
	chats, e := s.Hub.Messages.Chats()
	if e != nil {
		return e
	}
	last := map[string]*time.Time{}
	for _, c := range chats {
		last[c.JID] = c.LastMessageTime
	}
	counts := map[string]int{}
	rows, e := s.Hub.Messages.DB.Query(`SELECT chat_jid,COUNT(*) FROM messages WHERE deleted_at IS NULL AND julianday(timestamp) BETWEEN julianday(?) AND julianday(?) GROUP BY chat_jid`, s.Hub.Now().AddDate(0, 0, -30), s.Hub.Now())
	if e != nil {
		return e
	}
	for rows.Next() {
		var j string
		var n int
		if e = rows.Scan(&j, &n); e != nil {
			rows.Close()
			return e
		}
		counts[j] = n
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	// Task storage belongs to the task lane; this is a read-only projection.
	taskIDs := map[string][]string{}
	rows, e = s.Hub.Repo.DB.Query("SELECT data FROM tasks WHERE json_extract(data,'$.status') IN ('active','waiting')")
	if e != nil {
		return e
	}
	for rows.Next() {
		var raw string
		var t struct {
			ID    string `json:"id"`
			Chats []struct {
				ID string `json:"id"`
			} `json:"chats"`
		}
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal([]byte(raw), &t); e != nil {
			rows.Close()
			return e
		}
		for _, c := range t.Chats {
			taskIDs[c.ID] = append(taskIDs[c.ID], t.ID)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	agents, e := s.Hub.Agents()
	if e != nil {
		return e
	}
	for i := range gs {
		g := &gs[i]
		g.LastActivity = last[g.JID]
		g.Messages30 = counts[g.JID]
		g.CircleIDs = []string{}
		g.TaskIDs = []string{}
		g.AgentAccess = []model.GroupAccess{}
		g.Photo = false
		var entry avatarEntry
		if s.Hub.Repo.Get("avatar_cache", g.JID, &entry) == nil {
			g.Photo = entry.Available
		}
		for _, c := range cs {
			if !c.Confirmed || c.Archived || a != nil && !s.Hub.CircleAccess(a, c) {
				continue
			}
			for _, j := range c.Chats {
				if j.ChatJID == g.JID {
					g.CircleIDs = append(g.CircleIDs, c.ID)
				}
			}
		}
		if a == nil {
			g.TaskIDs = append(g.TaskIDs, taskIDs[g.JID]...)
			for _, agent := range agents {
				if agent.Permissions.Sync {
					continue // a dot's sync credential is not an AI that reads chats
				}
				read := agent.Enabled && agent.Permissions.ReadMessages && s.Hub.CanChat(&agent, g.JID)
				rule, e := s.Hub.AgentSendPolicy(agent, g.JID)
				if e != nil {
					return e
				}
				send := "never"
				if read && agent.Permissions.RequestSend {
					send = rule
					if send != "free" && send != "never" {
						send = "approval"
					}
				}
				g.AgentAccess = append(g.AgentAccess, model.GroupAccess{AgentID: agent.ID, Name: agent.Name, Read: read, Send: send})
			}
		}
	}
	sort.SliceStable(gs, func(i, j int) bool {
		x, y := gs[i].LastActivity, gs[j].LastActivity
		if x == nil {
			return false
		}
		if y == nil {
			return true
		}
		if x.Equal(*y) {
			return gs[i].JID < gs[j].JID
		}
		return x.After(*y)
	})
	return nil
}
