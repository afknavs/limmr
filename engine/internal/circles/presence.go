package circles

import (
	"encoding/json"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
)

type AgentPresence struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Target    string    `json:"target"`
	TargetIDs []string  `json:"target_ids"`
	Since     time.Time `json:"since"`
}

func presenceState(a model.Activity) string {
	if a.Kind == "failed" || a.Kind == "denied" {
		return ""
	}
	if (a.Tool == "brief" || a.Tool == "remember") && a.Kind == "summary" {
		return "writing_brief"
	}
	if model.Contains([]string{"read", "find", "get", "people", "brief", "circles"}, a.Tool) && model.Contains([]string{"read", "search", "people_read"}, a.Kind) {
		return "reading"
	}
	switch a.Tool {
	case "update_brief", "save_summary":
		return "writing_brief"
	case "send", "draft_message", "send_message", "send_file", "send_reaction", "suggest":
		return "drafting"
	case "catch_up", "get_brief", "get_circle", "list_messages", "search_messages", "get_message_context", "get_chat", "find_person", "get_person", "list_groups", "get_group", "people_near", "list_circles", "resolve_context":
		return "reading"
	}
	return ""
}
func (s *Service) Presence() (any, error) {
	agents, err := s.Hub.Agents()
	if err != nil {
		return nil, err
	}
	cs, err := s.List(nil)
	if err != nil {
		return nil, err
	}
	chats, _, err := s.Hub.Visible(nil)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	for _, c := range cs {
		titles[c.ID] = c.Name
	}
	for _, c := range chats {
		titles[c.JID] = c.Name
	}
	target := func(js []string) (string, []string) {
		ids := []string{}
		for _, c := range cs {
			for _, j := range js {
				if model.Contains(JIDs(c), j) {
					ids = append(ids, c.ID)
					break
				}
			}
		}
		if len(ids) == 0 {
			for _, j := range js {
				if titles[j] != "" {
					ids = append(ids, j)
				}
			}
		}
		sort.Strings(ids)
		labels := []string{}
		for _, id := range ids {
			labels = append(labels, titles[id])
		}
		return strings.Join(labels, ", "), ids
	}
	latest := map[string]model.Activity{}
	rows, err := s.Hub.Repo.DB.Query("SELECT data FROM activity WHERE julianday(at)>julianday(?) AND julianday(at)<=julianday(?) ORDER BY julianday(at) DESC,id DESC", s.Hub.Now().Add(-120*time.Second), s.Hub.Now())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var a model.Activity
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := latest[a.AgentID]; !ok && presenceState(a) != "" {
			latest[a.AgentID] = a
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	active := map[string]AgentPresence{}
	for _, a := range agents {
		if !a.Enabled {
			continue
		}
		if act, ok := latest[a.ID]; ok {
			label, ids := target(act.ChatJIDs)
			active[a.ID] = AgentPresence{a.ID, a.Name, presenceState(act), label, ids, act.At}
		}
	}
	// Running sessions take precedence and remain present beyond Activity's 120s.
	rows, err = s.Hub.Repo.DB.Query(`SELECT w.owner,w.definition_json,r.started_at FROM trigger_runs r JOIN trigger_watches w ON w.id=r.handoff_id WHERE r.state='running' AND w.status='active' AND (w.expires_at IS NULL OR julianday(w.expires_at)>julianday(?)) ORDER BY julianday(r.started_at),r.id`, s.Hub.Now())
	if err != nil {
		return nil, err
	}
	type definition struct {
		Scope struct {
			Chats   []string `json:"chats"`
			Circles []string `json:"circles"`
		} `json:"scope"`
	}
	for rows.Next() {
		var owner, raw, stamp string
		var d definition
		if err = rows.Scan(&owner, &raw, &stamp); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			rows.Close()
			return nil, err
		}
		at, e := time.Parse(time.RFC3339Nano, stamp)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if at.After(s.Hub.Now()) {
			continue
		}
		for _, a := range agents {
			if a.ID == owner && a.Enabled {
				label, ids := target(d.Scope.Chats)
				if len(d.Scope.Circles) > 0 {
					ids = []string{}
					labels := []string{}
					for _, id := range d.Scope.Circles {
						if titles[id] != "" {
							ids = append(ids, id)
							labels = append(labels, titles[id])
						}
					}
					label = strings.Join(labels, ", ")
				}
				active[a.ID] = AgentPresence{a.ID, a.Name, "handoff", label, ids, at}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []AgentPresence{}
	for _, a := range active {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return map[string]any{"agents": out}, nil
}
