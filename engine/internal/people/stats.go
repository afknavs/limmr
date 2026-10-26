package people

import (
	"database/sql"
	"limmr/internal/model"
	"strings"
)

// allStats scans message metadata once for graph ranking/nightly refresh, rather
// than rescanning every message once per person. No text is loaded.
func (s *Service) allStats(a *model.Agent, ps []model.Person, gs []model.Group, ms []model.Membership) (map[string]Stats, error) {
	// Statistics depend on identity/group selection and membership order, not
	// enriched labels, affinity reasons or details. Directory changes are covered
	// by the revision in cacheKey.
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	gids := make([]string, len(gs))
	for i, g := range gs {
		gids[i] = g.JID
	}
	membersKey := make([][2]string, len(ms))
	for i, m := range ms {
		membersKey[i] = [2]string{m.Group, m.Person}
	}
	key := s.cacheKey(a, "stats", ids, gids, membersKey)
	s.shared().cacheMu.Lock()
	cached, ok := s.shared().statsCache[key]
	s.shared().cacheMu.Unlock()
	if ok {
		return cloneStats(cached), nil
	}

	out := map[string]Stats{}
	visiblePeople := map[string]bool{}
	for _, p := range ps {
		out[p.ID] = Stats{SharedGroups: []string{}}
		visiblePeople[p.ID] = true
	}
	canonical, e := s.directory()
	if e != nil {
		return nil, e
	}
	byJID := map[string]string{}
	bySender := map[string]string{}
	for _, p := range canonical {
		if !visiblePeople[p.ID] {
			continue
		}
		for _, j := range p.JIDs {
			byJID[j] = p.ID
			bySender[j] = p.ID
			bySender[strings.SplitN(j, "@", 2)[0]] = p.ID
		}
	}
	groupSet := map[string]bool{}
	for _, g := range gs {
		groupSet[g.JID] = true
	}
	members := map[string]map[string]bool{}
	for _, m := range ms {
		if !visiblePeople[m.Person] || !groupSet[m.Group] {
			continue
		}
		st := out[m.Person]
		st.SharedGroups = append(st.SharedGroups, m.Group)
		out[m.Person] = st
		if members[m.Group] == nil {
			members[m.Group] = map[string]bool{}
		}
		members[m.Group][m.Person] = true
	}
	allowed := s.Hub.ChatAccess(a)
	rows, e := s.Hub.Messages.DB.Query("SELECT chat_jid,COALESCE(sender,''),timestamp FROM messages")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	now := s.Hub.Now()
	for rows.Next() {
		var chat, sender string
		var at sql.NullTime
		if e = rows.Scan(&chat, &sender, &at); e != nil {
			return nil, e
		}
		if !at.Valid || !allowed(chat) {
			continue
		}
		if id := byJID[chat]; id != "" {
			st := out[id]
			if st.LastDM == nil || at.Time.After(*st.LastDM) {
				copy := at.Time
				st.LastDM = &copy
			}
			if !at.Time.Before(now.AddDate(0, 0, -30)) {
				st.DM30++
			}
			if !at.Time.Before(now.AddDate(0, 0, -365)) {
				st.DM365++
			}
			out[id] = st
		} else if id := bySender[sender]; id != "" && members[chat][id] {
			st := out[id]
			st.GroupMessages++
			out[id] = st
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	s.shared().cacheMu.Lock()
	if len(s.shared().statsCache) > 16 {
		s.shared().statsCache = nil
	}
	if s.shared().statsCache == nil {
		s.shared().statsCache = map[string]map[string]Stats{}
	}
	s.shared().statsCache[key] = cloneStats(out)
	s.shared().cacheMu.Unlock()
	return out, nil
}
