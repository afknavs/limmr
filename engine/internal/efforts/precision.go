package efforts

import (
	"limmr/internal/model"
	"math"
	"strings"
	"time"
)

// Hundreds of cohesive messages are legitimate. These are final sanity
// guards, not a substitute for topic and participant continuity.
const maxEffortMessages = 800
const maxEffortChats = 8

func cloneTerms(ts map[string]bool) map[string]bool {
	out := map[string]bool{}
	for t := range ts {
		out[t] = true
	}
	return out
}

func taskRequest(m model.Message) bool {
	text := strings.ToLower(m.Content)
	for _, ask := range []string{"please ", "can you ", "could you ", "will you ", "need you to ", "let's ", "let us ", "submit ", "finish "} {
		if strings.Contains(text, ask) {
			return true
		}
	}
	return false
}

func newTaskTopic(m model.Message, ts map[string]bool) bool {
	if !taskRequest(m) || len(ts) < 2 {
		return false
	}
	for t := range ts {
		if topicHeads[t] {
			return true
		}
	}
	return false
}

// Classification uses only visible, eligible messages. Most traffic in an
// ambient group lacks an explicit request, even when nouns recur.
func ambientGroups(ms []model.Message) map[string]bool {
	counts, asks := map[string]int{}, map[string]int{}
	first, last := map[string]time.Time{}, map[string]time.Time{}
	for _, m := range ms {
		if !strings.HasSuffix(m.ChatJID, "@g.us") {
			continue
		}
		counts[m.ChatJID]++
		if first[m.ChatJID].IsZero() || m.Timestamp.Before(first[m.ChatJID]) {
			first[m.ChatJID] = m.Timestamp
		}
		if m.Timestamp.After(last[m.ChatJID]) {
			last[m.ChatJID] = m.Timestamp
		}
		if taskRequest(m) {
			asks[m.ChatJID]++
		}
	}
	out := map[string]bool{}
	for j, n := range counts {
		if (n >= 80 || n >= 32 && last[j].Sub(first[j]) <= 48*time.Hour) && asks[j]*3 < n {
			out[j] = true
		}
	}
	return out
}

func withinBounds(c *cluster, m model.Message, ambient map[string]bool, continuous bool) bool {
	if len(c.ms) >= maxEffortMessages {
		return false
	}
	last := c.ms[len(c.ms)-1]
	gap := m.Timestamp.Sub(last.Timestamp)
	if gap < 0 || m.Timestamp.Sub(c.ms[0].Timestamp) > 90*24*time.Hour {
		return false
	}
	busy := ambient[m.ChatJID]
	for chat := range c.chats {
		busy = busy || ambient[chat]
	}
	if !c.chats[m.ChatJID] && len(c.chats) >= maxEffortChats {
		return false
	}
	if busy && !continuous && (gap > 6*time.Hour || m.Timestamp.Sub(c.ms[0].Timestamp) > 48*time.Hour) {
		return false
	}
	return true
}

func coreScore(c *cluster, ts, ps map[string]bool, m model.Message, anchor bool) float64 {
	n := 0
	for t := range ts {
		if c.core[t] {
			n++
		}
	}
	samePeople := overlap(ps, c.seedPeople)
	// Vocabulary changes across chats need an original external participant,
	// a distinctive original term, a work noun and an artefact/deadline.
	if m.ChatJID != c.ms[0].ChatJID && relatedTopic(ts, ps, c.core, c.seedPeople, anchor || c.anchored) {
		return float64(n) / float64(max(1, len(ts)))
	}
	// Frequency-weighted centroid restricted to the original topic. A rare
	// seed word loses influence; a newly acquired word never gains admission power.
	matched, total := 0, 0
	for t := range c.core {
		total += c.counts[t]
		if ts[t] {
			matched += c.counts[t]
		}
	}
	incoming, core := float64(n)/float64(max(1, len(ts))), float64(matched)/float64(max(1, total))
	if n < 2 || incoming < .3 || core < .5 {
		return -1
	}
	if !samePeople && (incoming < .8 || core < .8) {
		return -1
	}
	return incoming + core
}

func explicitThread(ms []model.Message, ambient map[string]bool) bool {
	if len(ambient) == 0 {
		return true
	}
	busy := false
	for _, m := range ms {
		busy = busy || ambient[m.ChatJID]
	}
	if !busy {
		return true
	}
	artefact, request, reply := false, false, false
	for i, m := range ms {
		artefact = artefact || workAnchor(m)
		if !taskRequest(m) {
			continue
		}
		request = true
		for _, later := range ms[i+1:] {
			if speaker(later) != "" && speaker(later) != speaker(m) {
				reply = true
			}
		}
	}
	return artefact && request && reply
}

// Evidence score, not a calibrated probability. Volume is not a bonus.
func evidenceConfidence(ms []model.Message, q qualityRules) float64 {
	if len(ms) == 0 {
		return 0
	}
	counts, speakers := map[string]int{}, map[string]bool{}
	artefact, deadline := false, false
	for _, m := range ms {
		for t := range q.terms(m) {
			counts[t]++
		}
		if p := speaker(m); p != "" {
			speakers[p] = true
		}
		artefact = artefact || m.MediaType == "document" && goodFile(m.Filename) || linkPattern.MatchString(m.Content)
		deadline = deadline || messageDue(m) != nil
	}
	core := map[string]bool{}
	for t, n := range counts {
		if n*2 >= len(ms) {
			core[t] = true
		}
	}
	cohesion := 0.0
	for _, m := range ms {
		ts, n := q.terms(m), 0
		for t := range ts {
			if core[t] {
				n++
			}
		}
		cohesion += float64(n) / float64(max(1, len(ts)))
	}
	score := .35 + .25*cohesion/float64(len(ms))
	score += .05 * float64(min(3, max(0, len(speakers)-1)))
	if artefact {
		score += .1
	}
	if deadline {
		score += .1
	}
	if ms[len(ms)-1].Timestamp.Sub(ms[0].Timestamp) >= 24*time.Hour {
		score += .05
	}
	return math.Round(min(.98, score)*100) / 100
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// Filenames can label an artefact, but cannot manufacture body-topic cohesion.
func (q qualityRules) bodyTerms(m model.Message) map[string]bool {
	m.Filename = ""
	return q.terms(m)
}
func (q qualityRules) groupTerms(m model.Message) map[string]bool {
	ts := q.bodyTerms(m)
	if len(ts) >= 2 {
		return ts
	}
	return q.terms(m)
}

var genericCore = words("class club group notes shared reading reference material work draft section changes feedback review prepare fix bring look looks consistent discussion discuss chat updated version revisions send bring take will ill")

func distinctiveShared(a, b map[string]bool) bool {
	for t := range a {
		if b[t] && !topicHeads[t] && !genericCore[t] {
			return true
		}
	}
	return false
}
func corePeopleOverlap(a, b map[string]bool) bool {
	for p := range a {
		if b[p] && p != "me" && !strings.HasPrefix(p, "chat:") && !strings.HasSuffix(p, "@g.us") {
			return true
		}
	}
	return false
}

// The centroid may shed incidental seed words; it never acquires bridge terms.
// Compare frequencies directly instead of allocating a stable-core map for
// every candidate/message pair.
func continuousMessage(c *cluster, m model.Message, body map[string]bool) bool {
	top := 0
	for t := range c.core {
		top = max(top, c.counts[t])
	}
	n := 0
	work := false
	for t := range body {
		if c.core[t] && c.counts[t]*2 >= top {
			n++
			work = work || topicHeads[t] && !genericCore[t]
		}
	}
	distinct := false
	for t := range body {
		distinct = distinct || c.core[t] && c.counts[t]*2 >= top && !topicHeads[t] && !genericCore[t]
	}
	return work && n >= 2 && distinct && (corePeopleOverlap(participants(m), c.corePeople) || m.IsFromMe && c.chats[m.ChatJID])
}

func continuousClusters(a, b *cluster) bool {
	sharedPerson := corePeopleOverlap(a.corePeople, b.corePeople)
	sharedChat := false
	for chat := range a.chats {
		sharedChat = sharedChat || b.chats[chat]
	}
	if !sharedPerson && !sharedChat {
		return false
	}
	n, work, distinct := 0, false, false
	for t := range a.core {
		if b.core[t] && a.bodyCounts[t] > 0 && b.bodyCounts[t] > 0 {
			n++
			work = work || topicHeads[t] && !genericCore[t]
			distinct = distinct || !topicHeads[t] && !genericCore[t]
		}
	}
	// Two original body terms, including a distinctive concept and a work noun,
	// plus the same core person (or an existing chat), can reunite sparse pieces.
	// Newly merged terms and people never expand the destination's profile.
	return n >= 2 && work && distinct
}
