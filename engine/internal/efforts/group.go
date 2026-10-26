package efforts

import (
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"sort"
	"strings"
	"time"
	"unicode"
)

var stop = words("the a an and or of to in on at for with from is are was were be been it its this that these those i me my you your we our they their he she can could would should will please thanks thank hi hello hey okay ok yes no good morning today tomorrow done submitted see then have has had do did not just about how what when where who all up out as so but if there here also by us them let know send need want details outline revisions final ready review layout monday tuesday wednesday thursday friday saturday sunday due lol haha hahaha nice cool great weather sunshine")

func init() {
	for w := range words("yeah yep yup sure absolutely certainly definitely totally indeed hmm uh huh nah hai hain haan nahi nahin acha achha accha arre yaar bhai toh bhi bas ab aur kya ka ki ke ko se mein main mujhe tum aap hum apna kar karo karna ho hoga tha thi nahi matlab image photo video audio media omitted sticker reaction deleted attachment document jpg jpeg png webp mp4 pdf pptx docx xlsx txt zip shared message file attached received sent uploaded updated revised check checking finished completed agreed decided confirmed progressing") {
		stop[w] = true
	}
	// These distinguish purposes rather than conversational glue.
	delete(stop, "layout")
	delete(stop, "outline")
	delete(stop, "review")
}
func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}
func terms(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		switch w {
		case "deck", "presentation", "slides", "pptx":
			w = "slides"
		case "reports":
			w = "report"
		case "plans":
			w = "plan"
		}
		if len([]rune(w)) > 2 && !stop[w] && !allDigits(w) {
			out[w] = true
		}
	}
	return out
}
func key(r Ref) string        { return r.ChatJID + "\x00" + r.MessageID }
func ref(m model.Message) Ref { return Ref{m.ChatJID, m.ID} }

type cluster struct {
	ms               []model.Message
	ts               map[string]bool
	people           map[string]bool
	anchored         bool
	core             map[string]bool
	counts           map[string]int
	seedPeople       map[string]bool
	corePeople       map[string]bool
	bodyCounts       map[string]int
	chats            map[string]bool
	aligned          int
	artefacts        map[string]bool
	dmTerms          map[string]bool
	dmWork, dmFollow bool
}

// A deadline/artefact anchors a cross-chat vocabulary change. A shared work
// noun ("deadline", "slides") or the owner alone is never enough to merge it.
func workAnchor(m model.Message) bool {
	return (m.MediaType == "document" && goodFile(m.Filename)) || linkPattern.MatchString(m.Content) || messageDue(m) != nil
}

func relatedTopic(a, ap, b, bp map[string]bool, anchored bool) bool {
	if !anchored {
		return false
	}
	external := false
	for p := range ap {
		if bp[p] && !strings.HasPrefix(p, "chat:") && p != "me" && !strings.HasSuffix(p, "@g.us") {
			external = true
			break
		}
	}
	if !external {
		return false
	}
	workA, workB, distinctive := false, false, false
	for term := range a {
		workA = workA || topicHeads[term]
		if b[term] && !topicHeads[term] {
			distinctive = true
		}
	}
	for term := range b {
		workB = workB || topicHeads[term]
	}
	return distinctive && workA && workB
}

func participants(m model.Message) map[string]bool {
	p := map[string]bool{"chat:" + m.ChatJID: true}
	if !strings.HasSuffix(m.ChatJID, "@g.us") {
		p[m.ChatJID] = true
	}
	if !m.IsFromMe && m.Sender != "" {
		p[m.Sender] = true
	}
	return p
}
func overlap(a, b map[string]bool) bool {
	for x := range a {
		if b[x] {
			return true
		}
	}
	return false
}

// Inverted keyword and quote indexes avoid comparing every pair of messages.
// The history window is 90 days; unrelated chatter does not reset an effort.
type grouper struct {
	cs       []*cluster
	index    map[string][]int
	quotes   map[string]int
	lastChat map[string]int
	rules    qualityRules
	ambient  map[string]bool
	yield    func()
}

func newGrouper() *grouper {
	return &grouper{index: map[string][]int{}, quotes: map[string]int{}, lastChat: map[string]int{}, rules: qualityRules{names: map[string]bool{}}}
}
func group(ms []model.Message) []*cluster {
	g := newGrouper()
	g.ambient = ambientGroups(ms)
	g.add(ms)
	return g.cs
}
func (g *grouper) add(ms []model.Message) { g.addChecked(ms, false) }

// snapshot has already applied quality filtering while loading each message.
func (g *grouper) addFiltered(ms []model.Message) { g.addChecked(ms, true) }
func (g *grouper) addChecked(ms []model.Message, filtered bool) {
	cs, index, quotes, lastChat := g.cs, g.index, g.quotes, g.lastChat
	tokens := make([]map[string]bool, len(ms))
	for i, m := range ms {
		if i%64 == 0 && g.yield != nil {
			g.yield()
		}
		if !filtered && g.rules.reason(m) != "" {
			continue
		}
		tokens[i] = g.rules.groupTerms(m)
	}
	// Reuse candidate storage across messages. Index hits are deduplicated with
	// generation stamps rather than allocating a map for every incoming message.
	candidateMarks := make([]int, len(cs))
	candidates := []int{}
	generation := 0
	addCandidate := func(i int) {
		if candidateMarks[i] != generation {
			candidateMarks[i] = generation
			candidates = append(candidates, i)
		}
	}
	for messageIndex, m := range ms {
		if messageIndex%64 == 0 && g.yield != nil {
			g.yield()
		}
		ts := tokens[messageIndex]
		isFiller := filler(m)
		isFollowup := explicitFollowup(m)
		if ts == nil {
			continue
		}
		ps := participants(m)
		body := g.rules.bodyTerms(m)
		anchor := workAnchor(m)
		candidates = candidates[:0]
		generation++
		if !isFiller {
			for p := range ps {
				for t := range ts {
					for _, i := range index[p+"\x00"+t] {
						addCandidate(i)
					}
				}
			}
			for t := range ts {
				for _, i := range index["*\x00"+t] {
					addCandidate(i)
				}
			}
		}
		quoted, hasQuote := quotes[key(Ref{m.ChatJID, m.QuotedMessageID})]
		if m.QuotedMessageID != "" && hasQuote {
			addCandidate(quoted)
		}
		best, score := -1, -1.0
		bestCoreReply := false
		lower := strings.ToLower(strings.Trim(strings.TrimSpace(m.Content), ".!"))
		follow := lower == "done" || lower == "submitted" || lower == "see you then" || lower == "agreed" || lower == "confirmed"
		if i, ok := lastChat[m.ChatJID]; ok && follow {
			addCandidate(i)
		}
		for _, i := range candidates {
			c := cs[i]
			// No score exceeds two. Preserve the oldest-cluster tie break without
			// rescoring newer candidates once a perfect match is available.
			if score >= 2 && i > best || len(c.ms) >= maxEffortMessages ||
				(!c.chats[m.ChatJID] && len(c.chats) >= maxEffortChats) ||
				m.Timestamp.Before(c.ms[len(c.ms)-1].Timestamp) || m.Timestamp.Sub(c.ms[0].Timestamp) > 90*24*time.Hour {
				continue
			}
			s := -1.0
			if !isFiller && !(isFollowup && m.ChatJID != c.ms[0].ChatJID && !continuousMessage(c, m, ts)) {
				s = coreScore(c, ts, ps, m, anchor)
			}
			// Exact replies preserve short coordination without letting a reply to
			// a newly admitted, off-core member act as a bridge to another topic.
			quotedCore := false
			quotedContinuity := false
			quotedEvidence := false
			if hasQuote && m.QuotedMessageID != "" && i == quoted {
				for _, original := range c.ms {
					if original.ChatJID == m.ChatJID && original.ID == m.QuotedMessageID {
						ots := g.rules.groupTerms(original)
						quotedCore = coreScore(c, ots, participants(original), original, workAnchor(original)) >= 0 && (len(c.ms) < 16 || c.aligned*5 >= len(c.ms)*3)
						originalEvidence := continuousMessage(c, original, g.rules.bodyTerms(original)) || original.MediaType == "document" && goodFile(original.Filename) && distinctiveShared(ots, c.core)
						quotedEvidence = originalEvidence
						quotedContinuity = quotedCore && originalEvidence && (corePeopleOverlap(ps, c.corePeople) || m.IsFromMe && c.chats[m.ChatJID])
						break
					}
				}
			}
			if quotedCore && (!newTaskTopic(m, ts) || s >= 0) {
				s = max(s, 2)
			}
			if follow && lastChat[m.ChatJID] == i && m.Timestamp.Sub(c.ms[len(c.ms)-1].Timestamp) < 24*time.Hour {
				s = max(s, 1)
			}
			if s < 0 || (!withinBounds(c, m, g.ambient, false) && !withinBounds(c, m, g.ambient, quotedContinuity || continuousMessage(c, m, body))) {
				continue
			}
			if s > score || (s == score && (best < 0 || i < best)) {
				best, score = i, s
				bestCoreReply = quotedCore && quotedEvidence
			}
		}
		if best < 0 {
			if len(ts) == 0 || isFiller {
				continue
			}
			best = len(cs)
			candidateMarks = append(candidateMarks, 0)
			cs = append(cs, &cluster{ts: map[string]bool{}, people: map[string]bool{}, core: cloneTerms(ts), counts: map[string]int{}, seedPeople: cloneTerms(ps), corePeople: cloneTerms(ps), bodyCounts: map[string]int{}, chats: map[string]bool{}, artefacts: artefacts(m)})
		}
		c := cs[best]
		if !isFiller && !strings.HasSuffix(m.ChatJID, "@g.us") {
			if c.dmTerms == nil {
				c.dmTerms = map[string]bool{}
			}
			for t := range g.rules.directTerms(m, ts) {
				c.dmTerms[t] = true
			}
			c.dmWork = c.dmWork || taskEvidence([]model.Message{m})
			c.dmFollow = c.dmFollow || directFollowup(m)
		}
		if isFiller {
			c.ms = append(c.ms, m)
			continue
		}
		c.anchored = c.anchored || anchor
		bodyMatch := 0
		for t := range body {
			if c.core[t] {
				bodyMatch++
				c.bodyCounts[t]++
			}
		}
		if bodyMatch >= 2 || len(c.ms) == 0 {
			c.aligned++
			// Only artefacts backed by this segment's original body topic
			// become consolidation evidence. Freeze them during that pass.
			for a := range artefacts(m) {
				c.artefacts[a] = true
			}
		}
		// Explicit replies to the original body topic/document establish a
		// participant's task context even when their wording is just coordination.
		// They do not add topic terms or count as lexical cohesion.
		if bodyMatch >= 2 || len(c.ms) == 0 || bestCoreReply {
			for p := range ps {
				c.corePeople[p] = true
			}
		}
		c.chats[m.ChatJID] = true
		c.ms = append(c.ms, m)
		for t := range ts {
			if c.core[t] {
				c.counts[t]++
			}
		}

		for p := range ps {
			if !c.people[p] {
				for t := range c.ts {
					k := p + "\x00" + t
					index[k] = append(index[k], best)
				}
				c.people[p] = true
			}
		}
		for t := range ts {
			if !c.core[t] {
				continue
			}
			if !c.ts[t] {
				if !genericCore[t] {
					index["*\x00"+t] = append(index["*\x00"+t], best)
				}
				for p := range c.people {
					k := p + "\x00" + t
					index[k] = append(index[k], best)
				}
				c.ts[t] = true
			}
		}
		quotes[key(ref(m))] = best
		lastChat[m.ChatJID] = best
	}
	g.cs = cs
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func closed(text string) bool {
	if strings.Contains(text, "?") {
		return false
	}
	tokens := words(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, text))
	for _, w := range []string{"done", "submitted", "finished", "completed"} {
		if strings.Contains(text, "not "+w) || strings.Contains(text, "almost "+w) || strings.Contains(text, "nearly "+w) {
			return false
		}
		if tokens[w] {
			return true
		}
	}
	return strings.Contains(text, "see you then")
}
func status(ms []model.Message, now time.Time) string {
	last := ms[len(ms)-1]
	text := strings.ToLower(last.Content)
	if closed(text) {
		return "done"
	}
	quiet := now.Sub(last.Timestamp)
	if quiet >= 14*24*time.Hour {
		return "dormant"
	}
	if quiet >= 7*24*time.Hour && (strings.Contains(text, "agreed") || strings.Contains(text, "decided") || strings.Contains(text, "confirmed")) {
		return "done"
	}
	if strings.Contains(text, "?") || strings.Contains(text, "please") || strings.Contains(text, "can you") || strings.Contains(text, "could you") {
		if last.IsFromMe {
			return "waiting_on_them"
		}
		// A group question needs an explicit owner address. DMs are addressed to the owner.
		if !strings.HasSuffix(last.ChatJID, "@g.us") || strings.Contains(text, "@owner") {
			return "on_you"
		}
	}
	return "moving"
}
func makeEffort(ms []model.Message, names map[string]Entity, chats map[string]string, circles []model.Circle, now time.Time, q qualityRules, shared map[string]bool) Effort {
	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Timestamp.Equal(ms[j].Timestamp) {
			return key(ref(ms[i])) < key(ref(ms[j]))
		}
		return ms[i].Timestamp.Before(ms[j].Timestamp)
	})
	e := Effort{ID: "effort-" + dbutil.Digest(key(ref(ms[0])))[:24], Status: status(ms, now), FirstAt: ms[0].Timestamp, LastAt: ms[len(ms)-1].Timestamp, CreatedBy: "Limmr", Confidence: evidenceConfidence(ms, q), People: []Entity{}, Chats: []Entity{}, CircleIDs: []string{}, MessageRefs: []Ref{}, AgentActionRefs: []int64{}}
	ps, js := map[string]bool{}, map[string]bool{}
	var search strings.Builder
	for _, m := range ms {
		e.MessageRefs = append(e.MessageRefs, ref(m))
		search.WriteString(" ")
		search.WriteString(clip(m.Content, 120))
		search.WriteString(" ")
		search.WriteString(m.Filename)
		js[m.ChatJID] = true
		for p := range participants(m) {
			ps[p] = true
		}
	}
	e.search = search.String()
	for j := range js {
		e.Chats = append(e.Chats, Entity{j, chats[j]})
	}
	sort.Slice(e.Chats, func(i, j int) bool { return e.Chats[i].ID < e.Chats[j].ID })
	seen := map[string]bool{}
	for p := range ps {
		if n, ok := names[p]; ok && !seen[n.ID] {
			e.People = append(e.People, n)
			seen[n.ID] = true
		}
	}
	sort.Slice(e.People, func(i, j int) bool { return e.People[i].ID < e.People[j].ID })
	for _, c := range circles {
		if !c.Confirmed || c.Archived {
			continue
		}
		for _, j := range c.Chats {
			if js[j.ChatJID] {
				e.CircleIDs = append(e.CircleIDs, c.ID)
				break
			}
		}
	}
	if shared == nil {
		shared = sharedTerms(ms, q)
	}
	e.Title, e.titleSource = titleWithSource(ms, shared)
	e.Summary = concreteSummary(ms, e.Status, names)
	e.Due = due(ms)
	return e
}
