package efforts

import (
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
)

// Generic actions, reply glue and counterpart identities never connect jobs.
// Domain nouns and named third parties remain useful inside one direct chat.
var directNoise = words("also quick latest update updates follow following followup regarding about official links link email mail speak said says ask asked tell told first next another same revised revision revisions approval decision proposal project plan report outline slides review brief pricing demo version opening ready requires need needs changes feedback details discussed discussion account instead doesnt does don dont use using access canary synthetic")

func (q qualityRules) directTerms(m model.Message, ts map[string]bool) map[string]bool {
	out := map[string]bool{}
	for t := range ts {
		if !q.dmIdentity[m.ChatJID][t] && !q.ownerNames[t] && !genericCore[t] && !directNoise[t] && (!topicHeads[t] || directDomains[t] != "") {
			out[t] = true
		}
	}
	// Directory names were removed from grouping tokens. A third party can carry
	// a goal within a DM; the owner/counterpart's own name cannot join their jobs.
	for _, w := range rawWords(m.Content) {
		if q.names[w] && !q.dmIdentity[m.ChatJID][w] && !q.ownerNames[w] {
			for t := range terms(w) {
				if !directNoise[t] && !genericCore[t] {
					out[t] = true
				}
			}
		}
	}
	return out
}
func directFollowup(m model.Message) bool {
	text := automationText(m.Content)
	for _, prefix := range []string{"quick update", "latest", "update:", "following up", "follow up", "on that"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

var directDomains = map[string]string{
	"workshop": "education", "symposium": "education", "club": "education", "department": "education", "hod": "education", "seminar": "education",
	"house": "architecture", "walkthrough": "architecture", "render": "architecture", "renders": "architecture", "rendering": "architecture", "photoreal": "architecture", "3d": "architecture", "architectural": "architecture", "interior": "architecture",
	"pitch": "client", "client": "client",
	"assignment": "coursework", "essay": "coursework", "laboratory": "coursework", "exam": "coursework",
	"website": "software", "software": "software", "release": "software",
}

func clearlyDisjoint(a, b map[string]bool) bool {
	ad, bd := map[string]bool{}, map[string]bool{}
	for t := range a {
		if d := directDomains[t]; d != "" {
			ad[d] = true
		}
	}
	for t := range b {
		if d := directDomains[t]; d != "" {
			bd[d] = true
		}
	}
	return len(ad) > 0 && len(bd) > 0 && !overlap(ad, bd)
}
func directChat(c *cluster) string {
	chat := c.ms[0].ChatJID
	if strings.HasSuffix(chat, "@g.us") {
		return ""
	}
	for _, m := range c.ms {
		if m.ChatJID != chat {
			return ""
		}
	}
	return chat
}
func directGap(a, b *cluster) time.Duration {
	if a.ms[len(a.ms)-1].Timestamp.Before(b.ms[0].Timestamp) {
		return b.ms[0].Timestamp.Sub(a.ms[len(a.ms)-1].Timestamp)
	}
	if b.ms[len(b.ms)-1].Timestamp.Before(a.ms[0].Timestamp) {
		return a.ms[0].Timestamp.Sub(b.ms[len(b.ms)-1].Timestamp)
	}
	return 0
}

// The broader 21-day continuity rule belongs only to a single counterpart.
// DM goal vocabulary can grow, while the stricter cross-chat lexical core
// remains frozen and cross-chat artefacts still require participant continuity.
func mergeDirectFragments(cs []*cluster) []*cluster {
	out := []*cluster{}
	byChat := map[string][]int{}
	copied := map[int]bool{}
	for _, c := range cs {
		chat := directChat(c)
		best, score := -1, -1
		gap := time.Duration(1<<63 - 1)
		if chat != "" && c.dmWork {
			for _, i := range byChat[chat] {
				d := out[i]
				if !d.dmWork || len(d.ms)+len(c.ms) > maxEffortMessages {
					continue
				}
				distance := directGap(c, d)
				first, last := d.ms[0].Timestamp, d.ms[len(d.ms)-1].Timestamp
				if c.ms[0].Timestamp.Before(first) {
					first = c.ms[0].Timestamp
				}
				if c.ms[len(c.ms)-1].Timestamp.After(last) {
					last = c.ms[len(c.ms)-1].Timestamp
				}
				if distance > 21*24*time.Hour || last.Sub(first) > 90*24*time.Hour {
					continue
				}
				shared, distinct := 0, false
				for t := range c.dmTerms {
					if d.dmTerms[t] {
						shared++
						distinct = distinct || directDomains[t] == "" && !topicHeads[t]
					}
				}
				artifact := overlap(c.artefacts, d.artefacts)
				if !artifact && clearlyDisjoint(c.dmTerms, d.dmTerms) && !distinct {
					continue
				}
				if shared == 0 && !artifact && !c.dmFollow && !d.dmFollow {
					continue
				}
				if accountArgument(c.ms) || accountArgument(d.ms) {
					continue
				}
				n := shared
				if artifact {
					n += 10
				}
				if n > score || n == score && distance < gap {
					best, score, gap = i, n, distance
				}
			}
		}
		if best < 0 {
			out = append(out, c)
			if chat != "" {
				byChat[chat] = append(byChat[chat], len(out)-1)
			}
		} else {
			d := out[best]
			if !copied[best] {
				clone := *d
				clone.ms = append([]model.Message{}, d.ms...)
				clone.dmTerms = cloneTerms(d.dmTerms)
				clone.artefacts = cloneTerms(d.artefacts)
				d = &clone
				out[best] = d
				copied[best] = true
			}
			d.ms = append(d.ms, c.ms...)
			for t := range c.dmTerms {
				d.dmTerms[t] = true
			}
			for a := range c.artefacts {
				d.artefacts[a] = true
			}
			d.dmFollow = d.dmFollow || c.dmFollow
			sort.Slice(d.ms, func(i, j int) bool {
				if d.ms[i].Timestamp.Equal(d.ms[j].Timestamp) {
					return key(ref(d.ms[i])) < key(ref(d.ms[j]))
				}
				return d.ms[i].Timestamp.Before(d.ms[j].Timestamp)
			})
		}
	}
	return out
}
