package circles

import (
	"golang.org/x/text/unicode/norm"
	"limmr/internal/model"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

func normalize(x string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, norm.NFD.String(x))), " ")
}
func similarity(a, b string) float64 {
	a = normalize(a)
	b = normalize(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if strings.Contains(b, a) {
		return .95
	}
	best := 0.
	for _, q := range strings.Fields(a) {
		for _, w := range strings.Fields(b) {
			ar, br := []rune(q), []rune(w)
			prev := make([]int, len(br)+1)
			for j := range prev {
				prev[j] = j
			}
			for i, r := range ar {
				cur := make([]int, len(br)+1)
				cur[0] = i + 1
				for j, t := range br {
					cost := 1
					if r == t {
						cost = 0
					}
					cur[j+1] = min(cur[j]+1, prev[j+1]+1, prev[j]+cost)
				}
				prev = cur
			}
			score := 1 - float64(prev[len(br)])/float64(max(len(ar), len(br)))
			if score > best {
				best = score
			}
		}
	}
	return best * .9
}

type Candidate struct {
	Kind     string  `json:"kind"`
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	CircleID string  `json:"circle_id"`
	Score    float64 `json:"score"`
	Why      string  `json:"why"`
}
type Resolution struct {
	Candidates         []Candidate `json:"candidates"`
	NeedsClarification bool        `json:"needs_clarification"`
}

func (s *Service) Resolve(a model.Agent, query string) (Resolution, error) {
	out := Resolution{Candidates: []Candidate{}, NeedsClarification: true}
	if strings.TrimSpace(query) == "" {
		return out, model.ErrInvalid
	}
	if p, e := s.Hub.Owner(); e == nil && s.Hub.CanChat(&a, s.Hub.OwnerChat()) {
		q := normalize(query)
		if q == "me" || q == "i" || q == "myself" || q == "send it to me" || q == normalize(p.Name) || q == normalize(p.Label) {
			return Resolution{Candidates: []Candidate{{Kind: "person", ID: "me", Title: p.Name, Score: 1, Why: "owner"}}, NeedsClarification: false}, nil
		}
	}
	if id := s.Hub.OwnerPersonAlias(&a, query); id != "" {
		p, e := s.People.Profile(&a, id)
		if e != nil {
			return out, e
		}
		return Resolution{Candidates: []Candidate{{Kind: "person", ID: id, Title: p.Person.Name, Score: 1, Why: "owner profile relationship"}}, NeedsClarification: false}, nil
	}
	query = s.Hub.OwnerQuery(&a, query)
	cs, e := s.List(&a)
	if e != nil {
		return out, e
	}
	chats, _, e := s.Hub.Visible(&a)
	if e != nil {
		return out, e
	}
	ps, e := s.People.People(&a, "")
	if e != nil {
		return out, e
	}
	chatMap := map[string]model.Chat{}
	for _, c := range chats {
		chatMap[c.JID] = c
	}
	appendCandidate := func(kind, id, title, circle string, lex float64, at, cursor time.Time) {
		recency := 0.
		if !at.IsZero() {
			recency = math.Exp(-math.Max(0, s.Hub.Now().Sub(at).Hours()) / (24 * 7))
		}
		history := 0.
		if !cursor.IsZero() {
			history = math.Exp(-math.Max(0, s.Hub.Now().Sub(cursor).Hours()) / (24 * 14))
		}
		score := .8*lex + .13*recency + .07*history
		why := "name similarity"
		if (kind == "chat" || kind == "person") && (at.IsZero() || at.Before(s.Hub.CurrentSince())) {
			score *= .2
			why += "; outside current window"
		}
		if lex < .4 {
			why = "weak name match"
		}
		if recency > .5 {
			why += "; recent visible message activity"
		}
		if history > .5 {
			why += "; your own recent catch-up cursor"
		}
		out.Candidates = append(out.Candidates, Candidate{kind, id, title, circle, math.Round(score*1000) / 1000, why})
	}
	for _, c := range cs {
		at := time.Time{}
		for _, j := range c.Chats {
			v := chatMap[j.ChatJID]
			if v.LastMessageTime != nil && v.LastMessageTime.After(at) {
				at = *v.LastMessageTime
			}
		}
		cursor := s.Cursor(a, c.ID, c)
		lex := similarity(query, c.Name)
		if strings.Contains(normalize(query), "project") && c.Kind == "project" {
			lex = math.Max(lex, .65)
		}
		appendCandidate("circle", c.ID, c.Name, c.ID, lex, at, cursor)
		for _, t := range c.Topics {
			lex := similarity(query, t.Title+" "+strings.Join(t.Keywords, " "))
			if strings.Contains(normalize(query), "project") && c.Kind == "project" && t.Status == "active" {
				lex = math.Max(lex, .7)
			}
			topicAt := time.Time{}
			for _, j := range t.Chats {
				v := chatMap[j]
				if v.LastMessageTime != nil && v.LastMessageTime.After(topicAt) {
					topicAt = *v.LastMessageTime
				}
			}
			appendCandidate("topic", t.ID, t.Title, c.ID, lex, topicAt, cursor)
		}
	}
	for _, c := range chats {
		at := time.Time{}
		if c.LastMessageTime != nil {
			at = *c.LastMessageTime
		}
		appendCandidate("chat", c.JID, c.Name, "", similarity(query, c.Name), at, time.Time{})
	}
	for _, p := range ps {
		at := time.Time{}
		for _, j := range p.JIDs {
			c := chatMap[j]
			if c.LastMessageTime != nil && c.LastMessageTime.After(at) {
				at = *c.LastMessageTime
			}
		}
		appendCandidate("person", p.ID, p.Name, "", similarity(query, p.Name+" "+p.Label), at, time.Time{})
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		if out.Candidates[i].Score != out.Candidates[j].Score {
			return out.Candidates[i].Score > out.Candidates[j].Score
		}
		return out.Candidates[i].Kind+out.Candidates[i].ID < out.Candidates[j].Kind+out.Candidates[j].ID
	})
	if len(out.Candidates) > 0 {
		out.NeedsClarification = out.Candidates[0].Score < .6 || len(out.Candidates) > 1 && out.Candidates[0].Score-out.Candidates[1].Score <= .1000001
	}
	if len(out.Candidates) > 20 {
		out.Candidates = out.Candidates[:20]
	}
	return out, nil
}
