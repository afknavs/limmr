package people

import (
	"encoding/json"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// enrichAffinity uses only projected chats and circles. Reasons contain signal
// descriptions, never excerpts, keywords, private chat names or message text.
func (s *Service) enrichAffinity(a *model.Agent, ps []model.Person, ms []model.Membership) error {
	cs, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return err
	}
	briefs, err := hub.List[model.Brief](s.Hub.Repo, "briefs")
	if err != nil {
		return err
	}
	allowed := s.Hub.ChatAccess(a)
	groups := map[string]map[string]bool{}
	for _, m := range ms {
		if allowed(m.Group) {
			if groups[m.Person] == nil {
				groups[m.Person] = map[string]bool{}
			}
			groups[m.Person][m.Group] = true
		}
	}
	groupList, err := hub.List[model.Group](s.Hub.Repo, "groups")
	if err != nil {
		return err
	}
	groupByID := map[string]model.Group{}
	for _, g := range groupList {
		if allowed(g.JID) {
			groupByID[g.JID] = g
		}
	}
	chats, err := s.Hub.Messages.Chats()
	if err != nil {
		return err
	}
	lastGroup := map[string]*time.Time{}
	for _, c := range chats {
		if allowed(c.JID) {
			lastGroup[c.JID] = c.LastMessageTime
		}
	}
	groupCircles := map[string][]string{}
	affinityCircles := []model.Circle{}
	for _, c := range cs {
		if !c.Confirmed || c.Archived || (a != nil && !s.Hub.CircleAccess(a, c)) {
			continue
		}
		full := true
		for _, j := range c.Chats {
			if !allowed(j.ChatJID) {
				full = false
			}
		}
		if !full {
			continue
		}
		affinityCircles = append(affinityCircles, c)
		for _, j := range c.Chats {
			groupCircles[j.ChatJID] = append(groupCircles[j.ChatJID], c.ID)
		}
	}
	latest := map[string]model.Brief{}
	for _, b := range briefs {
		if b.Version > latest[b.CircleID].Version {
			latest[b.CircleID] = b
		}
	}
	// Bound content to 40 messages per visible DM, and 90 days. No network/model call.
	texts := map[string]string{}
	recent := map[string]bool{}
	jidPerson := map[string]string{}
	for _, p := range ps {
		for _, j := range p.JIDs {
			if allowed(j) && !strings.HasSuffix(j, "@g.us") {
				jidPerson[j] = p.ID
			}
		}
	}
	if len(jidPerson) > 0 && len(cs) > 0 {
		jids := []string{}
		for jid := range jidPerson {
			jids = append(jids, jid)
		}
		encoded, _ := json.Marshal(jids)
		rows, e := s.Hub.Messages.DB.Query(`SELECT chat_jid,text FROM (SELECT chat_jid,SUBSTR(COALESCE(content,''),1,300) AS text,ROW_NUMBER() OVER (PARTITION BY chat_jid ORDER BY timestamp DESC,id DESC) AS n FROM messages WHERE deleted_at IS NULL AND chat_jid IN (SELECT value FROM json_each(?)) AND julianday(timestamp) BETWEEN julianday(?) AND julianday(?)) WHERE n<=40`, string(encoded), s.Hub.Now().AddDate(0, 0, -90), s.Hub.Now())
		if e != nil {
			return e
		}
		for rows.Next() {
			var j, t string
			if e = rows.Scan(&j, &t); e != nil {
				rows.Close()
				return e
			}
			if id := jidPerson[j]; id != "" {
				recent[id] = true
				if len(texts[id]) < 12000 {
					texts[id] += " " + string([]rune(t)[:min(len([]rune(t)), 300)])
				}
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	inferences := map[string]model.RelationshipInference{}
	if a == nil {
		// The object key, not an inferred label, identifies the person.
		rows, e := s.Hub.Repo.DB.Query("SELECT id,data FROM objects WHERE bucket='relationship_inferences'")
		if e != nil {
			return e
		}
		for rows.Next() {
			var id, raw string
			var v model.RelationshipInference
			if e = rows.Scan(&id, &raw); e != nil {
				rows.Close()
				return e
			}
			// Inference is optional enrichment. Match the former per-person Get:
			// malformed inference objects are ignored rather than hiding people.
			if json.Unmarshal([]byte(raw), &v) == nil && v.Status == "inferred" {
				inferences[id] = v
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	circleWords := map[string]map[string]bool{}
	for _, c := range cs {
		brief := latest[c.ID]
		briefText := brief.Summary
		if a != nil {
			if len(brief.ChatJIDs) == 0 {
				briefText = ""
			}
			for _, j := range brief.ChatJIDs {
				if !allowed(j) {
					briefText = ""
					break
				}
			}
		}
		words := tokens(c.Name + " " + c.Description + " " + briefText)
		for _, topic := range c.Topics {
			for _, w := range topic.Keywords {
				for t := range tokens(w) {
					words[t] = true
				}
			}
		}
		circleWords[c.ID] = words
	}
	peersByCircleGroup := map[string]map[string][]string{}
	peerCounts := map[string]map[string]int{}
	for _, c := range affinityCircles {
		byGroup := map[string][]string{}
		counts := map[string]int{}
		for _, m := range c.People {
			counts[m.PersonID]++
			// Index one entry per person even if malformed stored data repeats a member.
			if counts[m.PersonID] > 1 {
				continue
			}
			for g := range groups[m.PersonID] {
				byGroup[g] = append(byGroup[g], m.PersonID)
			}
		}
		peersByCircleGroup[c.ID], peerCounts[c.ID] = byGroup, counts
	}
	for i := range ps {
		p := &ps[i]
		p.GroupsInCommon = []model.GroupInCommon{}
		for jid := range groups[p.ID] {
			if g, ok := groupByID[jid]; ok {
				ids := []string{}
				for _, cid := range groupCircles[jid] {
					for _, c := range affinityCircles {
						if c.ID != cid {
							continue
						}
						connected := p.Circle == c.ID || strings.EqualFold(p.Circle, c.Name)
						for _, m := range c.People {
							connected = connected || m.PersonID == p.ID
						}
						for _, chat := range c.Chats {
							connected = connected || groups[p.ID][chat.ChatJID]
						}
						if connected {
							ids = append(ids, cid)
						}
					}
				}
				if ids == nil {
					ids = []string{}
				}
				sort.Strings(ids)
				p.GroupsInCommon = append(p.GroupsInCommon, model.GroupInCommon{ID: jid, Name: g.Name, CircleIDs: ids, LastActiveAt: lastGroup[jid]})
			}
		}
		sort.Slice(p.GroupsInCommon, func(i, j int) bool {
			a, b := p.GroupsInCommon[i], p.GroupsInCommon[j]
			if a.LastActiveAt != nil && b.LastActiveAt != nil && !a.LastActiveAt.Equal(*b.LastActiveAt) {
				return a.LastActiveAt.After(*b.LastActiveAt)
			}
			if (a.LastActiveAt == nil) != (b.LastActiveAt == nil) {
				return a.LastActiveAt != nil
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.ID < b.ID
		})
		p.CircleAffinity = []model.CircleAffinity{}
		p.PrimaryCircleID = ""
		p.InferredRelationship = nil
		if a != nil {
			filtered := []model.DetailSuggestion{}
			for _, v := range p.DetailSuggestions {
				if v.Field != "how_known" && v.Field != "role" && s.factVisible(a, model.Fact{EvidenceChats: v.EvidenceChats}) {
					filtered = append(filtered, v)
				}
			}
			p.DetailSuggestions = filtered
		}
		if v, ok := inferences[p.ID]; ok {
			p.InferredRelationship = &v
		}
		dm := tokens(texts[p.ID])
		manual := ""
		for _, c := range affinityCircles {
			score := 0.0
			reasons := []string{}
			n := 0
			for _, j := range c.Chats {
				if groups[p.ID][j.ChatJID] && strings.HasSuffix(j.ChatJID, "@g.us") {
					n++
				}
			}
			if n > 0 {
				score = math.Min(.9, .48+float64(n-1)*.25)
				reasons = append(reasons, fmt.Sprintf("Member of %d shared groups in this circle", n))
			}
			for _, m := range c.People {
				if m.PersonID == p.ID {
					score = 1
					reasons = append(reasons, "Confirmed circle member")
					if m.Source == "you" || (m.Origin != nil && m.Origin.ByKind == "you") {
						manual = c.ID
					}
				}
			}
			if p.Circle == c.ID || strings.EqualFold(p.Circle, c.Name) {
				manual = c.ID
				score = 1
				reasons = append(reasons, "Placed by you")
			}
			// Only structural links create a connection. Topic/peer signals rank real links.
			if score == 0 {
				continue
			}
			words := circleWords[c.ID]
			overlap := 0
			for w := range words {
				if dm[w] {
					overlap++
				}
			}
			if overlap >= 2 {
				score += .22
				reasons = append(reasons, "Recent direct chats overlap with circle topics")
			}
			peers := 0
			seenPeers := map[string]bool{}
			for g := range groups[p.ID] {
				for _, id := range peersByCircleGroup[c.ID][g] {
					if id != p.ID && !seenPeers[id] {
						peers += peerCounts[c.ID][id]
						seenPeers[id] = true
					}
				}
			}
			if peers > 0 {
				score += math.Min(.12, float64(peers)*.04)
				reasons = append(reasons, "Shares groups with confirmed members")
			}
			if score > 0 && recent[p.ID] {
				score += .04
				reasons = append(reasons, "Recent direct contact")
			}
			if score > 0 {
				p.CircleAffinity = append(p.CircleAffinity, model.CircleAffinity{CircleID: c.ID, Score: math.Min(1, score), Reasons: reasons})
			}
		}
		sort.Slice(p.CircleAffinity, func(i, j int) bool {
			if p.CircleAffinity[i].Score == p.CircleAffinity[j].Score {
				return p.CircleAffinity[i].CircleID < p.CircleAffinity[j].CircleID
			}
			return p.CircleAffinity[i].Score > p.CircleAffinity[j].Score
		})
		if manual != "" {
			p.PrimaryCircleID = manual
		} else if len(p.CircleAffinity) > 0 && p.CircleAffinity[0].Score >= .65 {
			p.PrimaryCircleID = p.CircleAffinity[0].CircleID
		}
	}
	return nil
}
func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len(w) >= 4 {
			switch w {
			case "this", "that", "with", "from", "have", "your", "about", "they", "their", "them", "will", "were", "been":
				continue
			}
			out[w] = true
		}
	}
	return out
}
