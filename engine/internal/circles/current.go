package circles

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"time"
)

type DMHint struct {
	JID          string     `json:"jid"`
	Name         string     `json:"name"`
	Messages     int        `json:"messages"`
	LastActivity *time.Time `json:"last_activity"`
}
type GroupHint struct {
	JID          string     `json:"jid"`
	Name         string     `json:"name"`
	MemberCount  int        `json:"member_count"`
	LastActivity *time.Time `json:"last_activity"`
}
type CommunityHint struct {
	JID    string      `json:"jid"`
	Name   string      `json:"name"`
	Note   string      `json:"note"`
	Groups []GroupHint `json:"current_sub_groups"`
}
type SetupHints struct {
	RecentDMs    []DMHint        `json:"recent_dms"`
	RecentGroups []GroupHint     `json:"recent_groups"`
	Communities  []CommunityHint `json:"communities"`
}

func (s *Service) hints(a *model.Agent, chats []model.Chat) (SetupHints, map[string]bool, error) {
	v := SetupHints{RecentDMs: []DMHint{}, RecentGroups: []GroupHint{}, Communities: []CommunityHint{}}
	currentPeople := map[string]bool{}
	currentChats := map[string]bool{}
	groups, e := s.People.Groups(a, "")
	if e != nil {
		return v, nil, e
	}
	gs := map[string]model.Group{}
	for _, g := range groups {
		gs[g.JID] = g
	}
	for _, c := range chats {
		if !s.Hub.Current(c) {
			continue
		}
		currentChats[c.JID] = true
		if c.IsGroup {
			g := gs[c.JID]
			if !g.IsCommunity {
				v.RecentGroups = append(v.RecentGroups, GroupHint{c.JID, c.Name, g.MemberCount, c.LastMessageTime})
			}
		} else {
			n := 0
			if e = s.Hub.Messages.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=? AND julianday(timestamp)>=julianday(?) AND julianday(timestamp)<=julianday(?)", c.JID, s.Hub.CurrentSince(), s.Hub.Now()).Scan(&n); e != nil {
				return v, nil, e
			}
			v.RecentDMs = append(v.RecentDMs, DMHint{c.JID, c.Name, n, c.LastMessageTime})
		}
	}
	ps, e := s.People.People(a, "")
	if e != nil {
		return v, nil, e
	}
	for _, p := range ps {
		for _, j := range p.JIDs {
			if currentChats[j] {
				currentPeople[p.ID] = true
			}
		}
	}
	ms, e := hub.List[model.Membership](s.Hub.Repo, "memberships")
	if e != nil {
		return v, nil, e
	}
	for _, m := range ms {
		if currentChats[m.Group] {
			currentPeople[m.Person] = true
		}
	}
	// A permitted parent is a grouping hint, never a proposed life circle.
	for _, parent := range groups {
		if !parent.IsCommunity {
			continue
		}
		hint := CommunityHint{JID: parent.JID, Name: parent.Name, Note: "Grouping hints, not circles by themselves", Groups: []GroupHint{}}
		for _, g := range v.RecentGroups {
			if gs[g.JID].ParentJID == parent.JID {
				hint.Groups = append(hint.Groups, g)
			}
		}
		sort.Slice(hint.Groups, func(i, j int) bool { return hint.Groups[i].JID < hint.Groups[j].JID })
		if len(hint.Groups) > 0 {
			v.Communities = append(v.Communities, hint)
		}
	}
	sort.Slice(v.RecentDMs, func(i, j int) bool {
		if v.RecentDMs[i].Messages != v.RecentDMs[j].Messages {
			return v.RecentDMs[i].Messages > v.RecentDMs[j].Messages
		}
		return v.RecentDMs[i].JID < v.RecentDMs[j].JID
	})
	sort.Slice(v.RecentGroups, func(i, j int) bool {
		x, y := v.RecentGroups[i], v.RecentGroups[j]
		if !x.LastActivity.Equal(*y.LastActivity) {
			return x.LastActivity.After(*y.LastActivity)
		}
		return x.JID < y.JID
	})
	if len(v.RecentDMs) > 15 {
		v.RecentDMs = v.RecentDMs[:15]
	}
	if len(v.RecentGroups) > 20 {
		v.RecentGroups = v.RecentGroups[:20]
	}
	return v, currentPeople, nil
}
func (s *Service) currentAsk(c model.Circle, a model.OpenAsk) bool {
	if a.At.Before(s.Hub.CurrentSince()) {
		return false
	}
	chats, _, e := s.Hub.Visible(nil)
	if e != nil {
		return false
	}
	for _, chat := range chats {
		if (a.ChatJID == chat.JID || a.ChatJID == "" && model.Contains(JIDs(c), chat.JID)) && s.Hub.Current(chat) {
			return true
		}
	}
	return false
}
func (s *Service) Reset() error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	return s.Hub.ResetCircles()
}
