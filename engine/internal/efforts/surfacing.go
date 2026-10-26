package efforts

import (
	"limmr/internal/model"
	"strings"
	"time"
	"unicode"
)

// A sender-level exclusion is deliberately stronger than a per-message filter:
// once a menu identifies a bot, its ordinary-looking promotions cannot seed work.
// Normalize before matching so WhatsApp emphasis and pictographs cannot hide
// automation. This affects classification only; stored/timeline text stays exact.
func automationText(text string) string {
	text = strings.Map(func(r rune) rune {
		if strings.ContainsRune("*_~`", r) {
			return -1
		}
		if unicode.IsSymbol(r) || unicode.IsMark(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, text)
	return strings.Join(strings.Fields(text), " ")
}
func menuText(text string) bool {
	for _, phrase := range []string{"main menu", "type hi", "select from the options", "select an option below", "click below", "reply with", "press 1", "this is an automated message", "our service is currently unavailable", "services are temporarily unavailable", "service unavailable", "service is unavailable", "unable to process your request", "thank you for contacting", "do not reply to this message"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
func automatedMenu(text string) bool { return menuText(automationText(text)) }
func automationTemplate(text string) string {
	text = strings.Join(rawWords(text), " ")
	if len([]rune(text)) < 30 {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return '#'
		}
		return r
	}, text)
}

type senderHistory struct {
	total         int
	menus         int
	templates     map[string]int
	menuTemplates map[string]int
}

// Context is append-only while the source revision is append-only. Rebuilding
// it on every arrival would rescan large histories and violate the idle budget.
type surfacingContext struct {
	ownerQuotes, ownerChats, automated map[string]bool
	speakers                           map[string]map[string]bool
	histories                          map[string]*senderHistory
	chatHistories                      map[string]*senderHistory
}

func newSurfacingContext() *surfacingContext {
	return &surfacingContext{ownerQuotes: map[string]bool{}, ownerChats: map[string]bool{}, automated: map[string]bool{}, speakers: map[string]map[string]bool{}, histories: map[string]*senderHistory{}, chatHistories: map[string]*senderHistory{}}
}
func (c *surfacingContext) add(source []model.Message) {
	for _, m := range source {
		if m.IsFromMe {
			c.ownerChats[m.ChatJID] = true
			c.ownerQuotes[key(ref(m))] = true
		}
		if strings.HasSuffix(m.ChatJID, "@g.us") {
			if c.speakers[m.ChatJID] == nil {
				c.speakers[m.ChatJID] = map[string]bool{}
			}
			c.speakers[m.ChatJID][speaker(m)] = true
		}
		if m.IsFromMe {
			continue
		}
		text := automationText(m.Content)
		menu := menuText(text)
		sender := m.Sender
		if sender == "" && !strings.HasSuffix(m.ChatJID, "@g.us") {
			sender = m.ChatJID
		}
		if menu && sender != "" {
			c.automated[sender] = true
		}
		template := automationTemplate(text)
		add := func(histories map[string]*senderHistory, id string) {
			if id == "" {
				return
			}
			h := histories[id]
			if h == nil {
				h = &senderHistory{templates: map[string]int{}, menuTemplates: map[string]int{}}
				histories[id] = h
			}
			h.total++
			if menu {
				h.menus++
			}
			if template != "" {
				h.templates[template]++
				if menu {
					h.menuTemplates[template]++
				}
			}
		}
		add(c.histories, sender)
		if !strings.HasSuffix(m.ChatJID, "@g.us") {
			add(c.chatHistories, m.ChatJID)
		}

	}
}

// The context contains only visible messages in the 90-day window. No unseen
// sender, group, quote or message is consulted for inference.
func (q *qualityRules) configure(context *surfacingContext, chats map[string]string, groups []model.Group, aliases map[string]string, owner string, people []model.Person) {
	q.ownerNames = words(strings.ToLower(owner))
	q.ownerQuotes = context.ownerQuotes
	q.large = map[string]bool{}
	speakers, ownerChats := context.speakers, context.ownerChats
	for _, g := range groups {
		if g.MemberCount > 8 {
			q.large[g.JID] = true
		}
	}

	for j, ps := range speakers {
		if len(ps) > 8 {
			q.large[j] = true
		}
	}
	numericName := func(name string) bool {
		compact := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || strings.ContainsRune("+-()", r) {
				return -1
			}
			return r
		}, name)
		return allDigits(compact)
	}
	markNumeric := func(j, name string) {
		h := context.chatHistories[j]
		templateLike := 0
		if h != nil {
			templateLike = h.menus
			for template, n := range h.templates {
				if n >= 2 {
					templateLike += n - h.menuTemplates[template]
				}
			}
		}
		if numericName(name) && (!ownerChats[j] || h != nil && templateLike*5 >= h.total*3) && !strings.HasSuffix(j, "@g.us") {
			q.business[j] = true
		}
	}
	for j, name := range chats {
		markNumeric(j, name)
		if !strings.HasSuffix(j, "@g.us") {
			if q.dmIdentity[j] == nil {
				q.dmIdentity[j] = map[string]bool{}
			}
			for _, w := range rawWords(name) {
				q.dmIdentity[j][w] = true
			}
		}
	}
	for _, p := range people {
		name := p.Label
		if name == "" {
			name = p.Name
		}
		if name == "" {
			name = p.PushName
		}
		for _, j := range p.JIDs {
			if _, visible := chats[j]; visible {
				markNumeric(j, name)
			}
		}
	}

	for sender := range context.automated {
		q.business[sender] = true
	}
	for sender, h := range context.histories {
		for _, n := range h.templates {
			if n >= 4 && n*2 > h.total {
				q.business[sender] = true
			}
		}
	}
	// Directory and detected identities use the same alias equivalence.
	for changed := true; changed; {
		changed = false
		for a, b := range aliases {
			if q.business[a] != q.business[b] {
				q.business[a], q.business[b], changed = true, true, true
			}
		}
	}
}

func (q qualityRules) addressed(m model.Message) bool {
	if q.business[m.ChatJID] || q.business[m.Sender] {
		return false
	}
	if m.IsFromMe {
		return true
	}
	if m.QuotedMessageID != "" && q.ownerQuotes[key(Ref{m.ChatJID, m.QuotedMessageID})] {
		return true
	}
	// Mention metadata isn't persisted by the current message store. Only exact
	// rendered mention tokens count; substrings and mentions of others don't.
	for _, token := range strings.Fields(m.Content) {
		token = strings.Trim(token, ",.:;!?()[]")
		if !strings.HasPrefix(token, "@") {
			continue
		}
		id := strings.ToLower(strings.TrimPrefix(token, "@"))
		if id == "owner" || q.self[id] {
			return true
		}
		for jid := range q.self {
			if strings.Split(jid, "@")[0] == id {
				return true
			}
		}
	}
	if !taskRequest(m) && !strings.Contains(m.Content, "?") {
		return false
	}
	for _, w := range rawWords(m.Content) {
		if q.ownerNames[w] {
			return true
		}
		if w != "hi" && w != "hey" && w != "hello" && w != "dear" && w != "please" {
			break
		}
	}
	return false
}

func (q qualityRules) involved(ms []model.Message) bool {
	needed, involved := map[string]bool{}, map[string]bool{}
	for _, m := range ms {
		if q.large[m.ChatJID] {
			needed[m.ChatJID] = true
			involved[m.ChatJID] = involved[m.ChatJID] || q.addressed(m)
		}
	}
	for j := range needed {
		if !involved[j] {
			return false
		}
	}
	return true
}

var acknowledgements = words("yeah yep yup sure okay ok yes no nah thanks thank you for certainly absolutely totally lol haha hahaha hehe pls please bro sis yaar bhai ji haan acha accha cool great nice right fine welcome anytime definitely course of for real indeed agreed confirmed done submitted")

func filler(m model.Message) bool {
	if workAnchor(m) {
		return false
	}
	ws := rawWords(m.Content)
	if len(ws) == 0 {
		return true
	}
	onlyAck := true
	for _, w := range ws {
		onlyAck = onlyAck && acknowledgements[w]
	}
	if onlyAck {
		return true
	}
	// A compact topic noun phrase is substantive ("estuary survey"). Tiny
	// conversational replies without a task noun cannot create a grouping edge.
	ts := terms(m.Content)
	if len(ts) == 0 {
		return true
	}
	if len(ts) >= 2 {
		for _, w := range ws {
			if w == "agreed" || w == "confirmed" || w == "approved" || w == "decided" || w == "completed" || w == "submitted" {
				return false
			}
		}
	}
	for t := range ts {
		if topicHeads[t] && len(ts) >= 2 {
			return false
		}
	}
	return len(ws) < 4
}

var weekdays = words("monday tuesday wednesday thursday friday saturday sunday")

func taskEvidence(ms []model.Message) bool {
	if accountArgument(ms) {
		return false
	}
	for _, m := range ms {
		if workAnchor(m) {
			return true
		}
		text := strings.ToLower(m.Content)
		// A named weekday is date evidence without inventing a precise due
		// instant (the due parser still owns that separate, stricter field).
		if len(terms(text)) >= 2 {
			for _, w := range rawWords(text) {
				if weekdays[w] {
					return true
				}
			}
		}
		if taskRequest(m) {
			actions := words("submit finish send write prepare review arrange book contact email draft build schedule deliver share sign return pay confirm compare update")
			for _, w := range rawWords(text) {
				if actions[w] {
					return true
				}
			}
		}
		tokens := words(strings.Join(rawWords(text), " "))
		for _, decision := range []string{"agreed", "decided", "confirmed", "approved", "submitted", "completed"} {
			if tokens[decision] {
				return true
			}
		}
		// A concrete task noun is an explicit plan, rather than a question mark in
		// banter ("can I take a patch?"). Repetition alone isn't work evidence.
		for t := range terms(text) {
			if topicHeads[t] && t != "music" && t != "notes" && t != "review" {
				return true
			}
		}
	}
	return false
}

func explicitFollowup(m model.Message) bool {
	text := strings.ToLower(strings.TrimSpace(m.Content))
	for _, prefix := range []string{"also ", "also,", "quick update", "following up", "follow up", "for the same ", "on that "} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// A changing subtask may retain only its distinctive goal label. Require an
// explicit follow-up and work vocabulary on both sides; never admit glue alone.
func followupScore(c *cluster, ts, ps map[string]bool, m model.Message) bool {
	if !explicitFollowup(m) || filler(m) || !distinctiveShared(ts, c.core) {
		return false
	}
	if !c.chats[m.ChatJID] && (!corePeopleOverlap(ps, c.seedPeople) || !workAnchor(m) && !c.anchored) {
		return false
	}
	if m.Timestamp.Sub(c.ms[len(c.ms)-1].Timestamp) > 7*24*time.Hour {
		return false
	}
	workA, workB := false, false
	for t := range c.core {
		workA = workA || topicHeads[t]
	}
	for t := range ts {
		workB = workB || topicHeads[t]
	}
	return workA && workB
}

// Artefact identity is exact and frozen before consolidation. Generic class
// filenames cannot supply a cross-topic edge, even when attached repeatedly.
func artefacts(m model.Message) map[string]bool {
	out := map[string]bool{}
	if m.MediaType == "document" && goodFile(m.Filename) {
		distinctive := false
		for term := range terms(FilenameTitle(m.Filename)) {
			distinctive = distinctive || !genericCore[term] && !topicHeads[term]
		}
		if distinctive {
			out["file:"+strings.ToLower(m.Filename)] = true
		}
	}
	for _, link := range linkPattern.FindAllString(m.Content, -1) {
		out["link:"+strings.TrimRight(link, ".,;!?)")] = true
	}
	return out
}
func artefactFollowup(c *cluster, m model.Message, ps map[string]bool) bool {
	return (taskRequest(m) || explicitFollowup(m)) && !filler(m) && overlap(artefacts(m), c.artefacts) && (c.chats[m.ChatJID] || corePeopleOverlap(ps, c.seedPeople))
}

// A broadcast must not poison genuine work from another chat in the same raw
// cluster. Remove uninvolved group evidence and regroup the remaining messages
// before consolidation; retain rejected segments only for aggregate diagnostics.
func (q qualityRules) ownerClusters(cs []*cluster, ambient map[string]bool) (kept, rejected []*cluster) {
	for _, c := range cs {
		involved := map[string]bool{}
		for _, m := range c.ms {
			if q.large[m.ChatJID] {
				involved[m.ChatJID] = involved[m.ChatJID] || q.addressed(m)
			}
		}
		blocked := false
		for _, yes := range involved {
			blocked = blocked || !yes
		}
		if !blocked {
			kept = append(kept, c)
			continue
		}
		ms := make([]model.Message, 0, len(c.ms))
		for _, m := range c.ms {
			if !q.large[m.ChatJID] || involved[m.ChatJID] {
				ms = append(ms, m)
			}
		}
		if len(ms) == 0 {
			rejected = append(rejected, c)
			continue
		}
		g := newGrouper()
		g.rules = q
		g.ambient = ambient
		g.addFiltered(ms)
		kept = append(kept, g.cs...)
	}
	return kept, rejected
}

// Access-sharing disputes are conversation, not a work plan. A real artefact,
// date or explicit decision can still turn such a discussion into concrete work.
func accountArgument(ms []model.Message) bool {
	denied, alternative := false, false
	for _, m := range ms {
		text := strings.ReplaceAll(strings.ToLower(m.Content), "’", "'")
		denied = denied || strings.Contains(text, "don't use my account") || strings.Contains(text, "do not use my account") || strings.Contains(text, "stop using my account")
		alternative = alternative || strings.Contains(text, "instead") || strings.Contains(text, "doesn't work") || strings.Contains(text, "does not work")
	}
	if !denied || !alternative {
		return false
	}
	for _, m := range ms {
		if workAnchor(m) {
			return false
		}
		for _, w := range rawWords(m.Content) {
			if w == "agreed" || w == "decided" || w == "confirmed" || w == "approved" || weekdays[w] {
				return false
			}
		}
	}
	return true
}
