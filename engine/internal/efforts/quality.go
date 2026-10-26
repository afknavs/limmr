package efforts

import (
	"limmr/internal/model"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Quality filtering is performed before indexing, including explicit attachments.
// Names and owner identity come from directory metadata. Bot exclusions may
// additionally use scoped automation evidence.
type qualityRules struct {
	self, business, names          map[string]bool
	tokenCache                     map[termInput]map[string]bool
	ownerNames, ownerQuotes, large map[string]bool
	dmIdentity                     map[string]map[string]bool
}

func newQuality(ps []model.Person, cs []model.Circle, aliases map[string]string, owner string) qualityRules {
	q := qualityRules{self: map[string]bool{"me": true, "myself": true}, business: map[string]bool{}, names: words(""), tokenCache: map[termInput]map[string]bool{}, dmIdentity: map[string]map[string]bool{}}
	addName := func(n string) {
		for _, w := range rawWords(n) {
			q.names[w] = true
		}
	}
	addName(owner)
	for _, p := range ps {
		for _, n := range []string{p.Name, p.Label, p.PushName} {
			addName(n)
		}
		for _, j := range p.JIDs {
			q.dmIdentity[j] = words(strings.Join(rawWords(p.Name+" "+p.Label+" "+p.PushName+" "+owner), " "))
			if p.ID == "me" {
				q.self[j] = true
			}
			if p.Business || p.Broadcast {
				q.business[j] = true
			}
		}
	}
	for _, c := range cs {
		if c.Kind == "self" || strings.EqualFold(strings.TrimSpace(c.Name), "You") {
			for _, j := range c.Chats {
				q.self[j.ChatJID] = true
			}
		}
	}
	// Alias maps point alias -> canonical. Propagate both directions to a fixed point.
	for changed := true; changed; {
		changed = false
		for a, b := range aliases {
			for _, set := range []map[string]bool{q.self, q.business} {
				if set[a] != set[b] {
					set[a], set[b], changed = true, true, true
				}
			}
		}
	}
	return q
}

var codePattern = regexp.MustCompile(`(?i)\b(?:\d[ -]?){4,8}\b`)
var securityPattern = regexp.MustCompile(`(?i)\b(otp|verification|passcode|password|one[ -]time|security code|account credited|account debited|transaction alert)\b`)
var pinPattern = regexp.MustCompile(`(?i)\b(code|pin)\b`)
var shortSender = regexp.MustCompile(`^[A-Z]{2}[-_][A-Z0-9]{2,10}$`)
var onlyCode = regexp.MustCompile(`^\s*(?:\d[ -]?){4,8}\s*$`)
var systemPattern = regexp.MustCompile(`(?i)(this message was deleted|you deleted this message|messages and calls are end.to.end encrypted|security code.*changed)`)
var linkPattern = regexp.MustCompile(`(?i)(https?://|www\.)\S+`)
var forwardPattern = regexp.MustCompile(`(?i)^\s*forwarded(?: many times)?\s*(?::|\n)`)
var placeholder = words("image photo video audio document media sticker reaction gif deleted omitted attachment attached null")

func (q qualityRules) reason(m model.Message) string {
	if q.self[m.ChatJID] || strings.EqualFold(m.ChatJID, "you") {
		return "self"
	}
	// The store has no forwarded-protocol flag. Only explicit forward markers
	// can be excluded here; never infer a forward from ordinary topic words.
	if forwardPattern.MatchString(m.Content) {
		return "forward"
	}
	sender := strings.Split(m.Sender, "@")[0]
	if automatedMenu(m.Content) {
		return "otp"
	}
	if shortSender.MatchString(m.Sender) || q.business[m.ChatJID] || q.business[m.Sender] || strings.HasSuffix(m.ChatJID, "@broadcast") || strings.HasSuffix(m.ChatJID, "@newsletter") || strings.HasSuffix(m.Sender, "@broadcast") || (sender != "" && sender != "me" && allDigits(sender) && len(sender) <= 6) || securityPattern.MatchString(m.Content) || onlyCode.MatchString(m.Content) || hasSecurityCode(m.Content) {
		return "otp"
	}
	if m.DeletedAt != nil || systemPattern.MatchString(m.Content) || model.Contains([]string{"sticker", "reaction", "revoked", "deleted", "system", "protocol"}, strings.ToLower(m.MediaType)) {
		return "media"
	}
	text := strings.TrimSpace(linkPattern.ReplaceAllString(m.Content, ""))
	ws := rawWords(text)
	meaningful := false
	for _, w := range ws {
		if !placeholder[w] && w != "message" && w != "this" && w != "was" && w != "is" {
			meaningful = true
		}
	}
	if !meaningful && (m.Filename == "" || m.MediaType != "document" || !goodFile(m.Filename)) {
		return "media"
	}
	if strings.TrimSpace(m.Content) != "" && !meaningful {
		return "media"
	}
	return ""
}
func hasSecurityCode(text string) bool {
	if !pinPattern.MatchString(text) {
		return false
	}
	if codePattern.MatchString(text) {
		return true
	}
	for _, w := range rawWords(text) {
		if len(w) < 4 || len(w) > 10 {
			continue
		}
		for _, r := range w {
			if unicode.IsDigit(r) {
				return true
			}
		}
	}
	return false
}
func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}
func rawWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
func goodFile(s string) bool {
	ext := strings.ToLower(filepath.Ext(s))
	if !model.Contains([]string{".pdf", ".docx", ".pptx", ".xlsx", ".txt", ".md", ".zip"}, ext) {
		return false
	}
	ts := terms(FilenameTitle(s))
	if len(ts) < 2 {
		return false
	}
	for _, junk := range []string{"track", "mp3", "song", "album", "img", "dsc", "record", "recording"} {
		if ts[junk] {
			return false
		}
	}
	for t := range ts {
		if topicHeads[t] || t == "article" {
			return true
		}
	}
	return false
}

var filenameNoise = regexp.MustCompile(`\([^)]*\)|\[[^]]*\]|\b(?:\d{4}[-_.]\d{1,2}[-_.]\d{1,2}|\d{1,2}[-_.]\d{1,2}[-_.]\d{4}|\d{8})\b`)
var recordNumber = regexp.MustCompile(`(?i)\b(?:record|rec|id|no)[ _#.-]*\d+\b|\b\d+\b`)
var fileWordSeparators = strings.NewReplacer("_", " ", "-", " ", "–", " ", "—", " ")
var fileAcronyms = words("ADHD AI API CPU CSS CSV DM DNA HTML HTTP ID JSON MCP MRI NASA NGO PDF PhD PTSD QA RCT SQL UI UK UNESCO URL USA UX WHO iOS macOS")
var fileNumber = regexp.MustCompile(`^\s*\d+[.)_\s–—-]+`)

// FilenameTitle removes export formatting before applying the public title bound.
func FilenameTitle(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, "_", " ")
	name = filenameNoise.ReplaceAllString(name, " ")
	name = explicitDate.ReplaceAllString(name, " ")
	name = fileNumber.ReplaceAllString(name, "")
	name = recordNumber.ReplaceAllString(name, " ")
	name = fileWordSeparators.Replace(name)
	ws := strings.Fields(name)
	known := fileAcronyms
	allUpper := name == strings.ToUpper(name)
	out := ""
	for _, w := range ws {
		acronym := known[w] || (!allUpper && w == strings.ToUpper(w) && len([]rune(w)) >= 2)
		if !acronym {
			w = strings.ToLower(w)
		}
		if out == "" && !acronym {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			w = string(r)
		}
		next := strings.TrimSpace(out + " " + w)
		if len([]rune(next)) > 40 {
			break
		}
		out = next
	}
	return out
}

type termInput struct{ content, filename, media string }

// A rule set has fixed identity vocabulary. Reuse immutable token sets across
// admission, cohesion, confidence and naming rather than rerunning regexes.
func (q qualityRules) terms(m model.Message) map[string]bool {
	input := termInput{m.Content, m.Filename, m.MediaType}
	if ts, ok := q.tokenCache[input]; ok {
		return ts
	}

	text := linkPattern.ReplaceAllString(m.Content, "")
	text = explicitDate.ReplaceAllString(text, " ")
	text = clockTime.ReplaceAllString(text, " ")
	if m.MediaType == "document" && goodFile(m.Filename) {
		text += " " + strings.TrimSuffix(filepath.Base(m.Filename), filepath.Ext(m.Filename))
	}
	ts := terms(text)
	for t := range ts {
		if q.names[t] {
			delete(ts, t)
		}
	}
	// Also remove names before synonym normalization (e.g. a participant called Deck).
	for _, w := range rawWords(text) {
		if q.names[w] {
			for t := range terms(w) {
				delete(ts, t)
			}
		}
	}
	if q.tokenCache != nil {
		q.tokenCache[input] = ts
	}
	return ts
}

type pairEvidence struct {
	n             int
	senders, days map[string]bool
}

func speaker(m model.Message) string {
	if m.IsFromMe {
		return "me"
	}
	return m.Sender
}

func termPairs(ms []model.Message, q qualityRules) map[string]*pairEvidence {
	pairs := map[string]*pairEvidence{}
	for _, m := range ms {
		ts := q.terms(m)
		keys := []string{}
		for t := range ts {
			keys = append(keys, t)
		}
		sort.Strings(keys)
		// Bound pair enumeration for pasted text; long messages are still searchable.
		if len(keys) > 64 {
			keys = keys[:64]
		}
		sender, day := speaker(m), m.Timestamp.Format("2006-01-02")
		for i, a := range keys {
			for _, b := range keys[i+1:] {
				k := a + "\x00" + b
				v := pairs[k]
				if v == nil {
					v = &pairEvidence{senders: map[string]bool{}, days: map[string]bool{}}
					pairs[k] = v
				}
				v.n++
				if sender != "" {
					v.senders[sender] = true
				}
				v.days[day] = true
			}
		}
	}
	return pairs
}

// A pair must be repeated in three messages, backed by multiple speakers or dates.
func repeatedTerms(ms []model.Message, q qualityRules) map[string]bool {
	out := map[string]bool{}
	for k, v := range termPairs(ms, q) {
		if v.n >= 3 && (len(v.senders) >= 2 || len(v.days) >= 2) {
			for _, t := range strings.Split(k, "\x00") {
				out[t] = true
			}
		}
	}
	return out
}

func sharedTerms(ms []model.Message, q qualityRules) map[string]bool {
	out := map[string]bool{}
	if len(ms) == 1 {
		return out
	}
	if len(ms) >= 3 {
		out = repeatedTerms(ms, q)
	}
	if len(out) >= 2 {
		return out
	}
	// Short coordination can vary its wording. Two corroborating topic mentions
	// still need two speakers, plus either an artefact and three messages, or
	// a dated deadline and six messages. Bare repeated words remain insufficient.
	if len(ms) >= 3 {
		speakers := map[string]bool{}
		artefact, deadline := false, false
		for _, m := range ms {
			if p := speaker(m); p != "" {
				speakers[p] = true
			}
			artefact = artefact || (m.MediaType == "document" && goodFile(m.Filename)) || (linkPattern.MatchString(m.Content) && len(q.terms(m)) >= 2)
			deadline = deadline || messageDue(m) != nil
		}
		if len(speakers) >= 2 && ((artefact && len(ms) >= 3) || (deadline && len(ms) >= 6)) {
			for pair, ev := range termPairs(ms, q) {
				ts := strings.Split(pair, "\x00")
				if ev.n >= 2 && len(ev.senders) >= 2 && (topicHeads[ts[0]] || topicHeads[ts[1]]) {
					for _, term := range ts {
						out[term] = true
					}
				}
			}
		}
	}
	// A meaningful document and an explicit reply are stronger evidence than
	// repeated vocabulary. Require another speaker (or day) and an exact quote.
	byRef := map[string]model.Message{}
	for _, m := range ms {
		byRef[key(ref(m))] = m
	}
	for _, reply := range ms {
		if reply.QuotedMessageID == "" {
			continue
		}
		original, ok := byRef[key(Ref{reply.ChatJID, reply.QuotedMessageID})]
		if !ok || original.MediaType != "document" || !goodFile(original.Filename) {
			continue
		}
		a, b := original.Sender, reply.Sender
		if original.IsFromMe {
			a = "me"
		}
		if reply.IsFromMe {
			b = "me"
		}
		if (a == "" || b == "" || a == b) && original.Timestamp.Format("2006-01-02") == reply.Timestamp.Format("2006-01-02") {
			continue
		}
		for term := range q.terms(original) {
			out[term] = true
		}
	}
	return out
}

// Unknown vocabulary waits for an agent or the local language model. A small
// noun vocabulary is deliberately more conservative than capitalizing random terms.
var topicHeads = words("walkthrough rendering render renders pricing budget proposal report slides deck outline layout review plan research project schedule deadline workshop event meeting agenda poster typography design supplies garden route hike trip itinerary booking application assignment essay presentation rehearsal experiment laboratory lab results method pitch contract invoice payment fundraiser campaign website release launch script chapter manuscript notes minutes venue tickets equipment samples measurements menu recipe trip travel exam revision checklist portfolio prototype survey study analysis data program programme session ceremony exhibition film music performance training practice tournament competition grant sponsorship donation donations funding logistics")

func qualityTitle(ms []model.Message, shared map[string]bool) string {
	title, _ := titleWithSource(ms, shared)
	return title
}

func titleWithSource(ms []model.Message, shared map[string]bool) (string, string) {
	counts := map[string]int{}
	explicit := map[string]int{}
	for _, m := range ms {
		ws := rawWords(m.Content)
		for i := 0; i+1 < len(ws); i++ {
			a, b := terms(ws[i]), terms(ws[i+1])
			if len(a) != 1 || len(b) != 1 {
				continue
			}
			matches := map[string]bool{}
			for t := range a {
				if shared[t] {
					matches[t] = true
				}
			}
			for t := range b {
				if shared[t] {
					matches[t] = true
				}
			}
			if len(matches) == 2 && topicHeads[ws[i+1]] {
				phrase := ws[i] + " " + ws[i+1]
				// A generic pair can have a distinctive modifier immediately before it
				// ("zorblax first event"). Do not promote the generic tail alone.
				if genericTitle(phrase) && i > 0 && shared[ws[i-1]] {
					phrase = ws[i-1] + " " + phrase
				}
				if !goodTitle(phrase) {
					continue
				}
				counts[phrase]++
				if taskRequest(m) || strings.Contains(strings.ToLower(m.Content), "confirmed") || strings.Contains(strings.ToLower(m.Content), "agreed") || strings.Contains(strings.ToLower(m.Content), "decided") || strings.Contains(m.Content, "?") {
					explicit[phrase]++
				}
			}
		}
	}
	best := ""
	// Explicit requests and decisions outrank document labels and other
	// corroborated coordination phrases, regardless of attachment order.
	if len(explicit) > 0 {
		counts = explicit
	}
	for phrase, n := range counts {
		if n > counts[best] || n == counts[best] && phrase < best {
			best = phrase
		}
	}
	if best != "" {
		r := []rune(best)
		return clip(strings.ToUpper(string(r[0]))+string(r[1:]), 40), "request_phrase"
	}
	for _, m := range ms {
		if m.MediaType == "document" && goodFile(m.Filename) {
			if title := cleanInferredTitle(FilenameTitle(m.Filename)); goodTitle(title) {
				return title, "document_name"
			}
		}
	}
	return "", "naming_queue"
}

// Use only the last explicit ask/decision. No inferred advice or progress filler.
func concreteSummary(ms []model.Message, st string, names map[string]Entity) string {
	if st == "dormant" || len(ms) == 0 {
		return ""
	}
	m := ms[len(ms)-1]
	text := strings.TrimSpace(m.Content)
	lower := strings.ToLower(text)
	if !(st == "on_you" || st == "waiting_on_them" || closed(lower) || strings.Contains(lower, "agreed") || strings.Contains(lower, "decided") || strings.Contains(lower, "confirmed")) {
		return ""
	}
	who := ""
	if m.IsFromMe {
		who = "You"
	} else {
		who = names[m.Sender].Name
	}
	if who == "" {
		return ""
	}
	return who + ": “" + clip(text, 240) + "”"
}
func dueSoon(d *time.Time, now time.Time) bool {
	return d != nil && !d.Before(now) && !d.After(now.AddDate(0, 0, 7))
}

// Explicit names may be a single distinctive artefact name. Unknown nouns are
// allowed; title quality must not become a dictionary of known project names.
func goodTitle(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > 40 || securityPattern.MatchString(s) {
		return false
	}
	ws := rawWords(s)
	if len(ws) == 0 {
		return false
	}
	if placeholder[ws[0]] || titleLeadNoise[ws[0]] || genericTitle(s) || ws[0] == "shared" && len(ws) > 1 && ws[1] == "work" {
		return false
	}
	return len(terms(s)) > 0
}

func cleanSummary(s, status string) string {
	lower := strings.ToLower(s)
	if status == "dormant" || strings.Contains(lower, "the conversation is progressing") || strings.Contains(lower, "no progress for two weeks") || strings.Contains(lower, "decide whether to restart") || strings.Contains(lower, "no unanswered request for you") {
		return ""
	}
	return s
}

// Consolidation uses a frozen destination core and original participants.
// It cannot turn a term acquired from another cluster into a new merge edge.
func dedupeClusters(cs []*cluster) []*cluster { return consolidateClusters(cs, nil) }

func consolidateClusters(cs []*cluster, ambient map[string]bool) []*cluster {
	return consolidateCooperatively(cs, ambient, nil)
}
func consolidateCooperatively(cs []*cluster, ambient map[string]bool, yield func()) []*cluster {
	out := []*cluster{}
	index := map[string][]int{}
	artifactIndex := map[string][]int{}
	for ci, c := range cs {
		if ci%64 == 0 && yield != nil {
			yield()
		}
		candidates := map[int]bool{}
		follow := explicitFollowup(c.ms[0])
		artifactRequest := follow || taskRequest(c.ms[0])
		if artifactRequest {
			for a := range c.artefacts {
				for _, i := range artifactIndex[a] {
					candidates[i] = true
				}
			}
		}
		for t := range c.core {
			for _, i := range index[t] {
				candidates[i] = true
			}
		}
		best := -1
		for i := range candidates {
			dest := out[i]
			n := 0
			sharedWork, sharedDistinct := false, false
			for t := range c.core {
				if dest.core[t] {
					n++
					if c.bodyCounts[t] > 0 && dest.bodyCounts[t] > 0 {
						sharedWork = sharedWork || topicHeads[t] && !genericCore[t]
						sharedDistinct = sharedDistinct || !topicHeads[t] && !genericCore[t]
					}
				}
			}
			// Ambient fragments outside the short window cannot reunite
			// without body continuity or an explicit follow-up/artefact.
			// Reject before allocating merged chat sets or rescoring people.
			if !follow && !artifactRequest && !(n >= 2 && sharedWork && sharedDistinct) {
				first, last := dest.ms[0].Timestamp, dest.ms[len(dest.ms)-1].Timestamp
				if c.ms[0].Timestamp.Before(first) {
					first = c.ms[0].Timestamp
				}
				if c.ms[len(c.ms)-1].Timestamp.After(last) {
					last = c.ms[len(c.ms)-1].Timestamp
				}
				if last.Sub(first) > 48*time.Hour || c.ms[0].Timestamp.Sub(dest.ms[len(dest.ms)-1].Timestamp) > 6*time.Hour {
					busy := false
					for j := range c.chats {
						busy = busy || ambient[j]
					}
					for j := range dest.chats {
						busy = busy || ambient[j]
					}
					if busy {
						continue
					}
				}
			}
			exact := n >= 2 && float64(n)/float64(len(c.core)) >= .8 && float64(n)/float64(len(dest.core)) >= .8
			related := c.ms[0].ChatJID != dest.ms[0].ChatJID && relatedTopic(c.core, c.seedPeople, dest.core, dest.seedPeople, c.anchored || dest.anchored)
			continuous := continuousClusters(c, dest)
			followup := follow && followupScore(dest, c.core, c.seedPeople, c.ms[0])
			continuous = continuous || followup || artifactRequest && artefactFollowup(dest, c.ms[0], c.seedPeople)
			if !exact && !related && !continuous || len(c.ms)+len(dest.ms) > maxEffortMessages {
				continue
			}
			// Same-chat and long-gap segments can reunite only with body-topic
			// and core-person continuity; filenames alone are insufficient.
			chats := map[string]bool{}
			sharedChat := false
			for _, m := range dest.ms {
				chats[m.ChatJID] = true
			}
			for _, m := range c.ms {
				sharedChat = sharedChat || chats[m.ChatJID]
			}
			for _, m := range c.ms {
				chats[m.ChatJID] = true
			}
			if sharedChat && !continuous || len(chats) > maxEffortChats {
				continue
			}
			first, last := dest.ms[0].Timestamp, dest.ms[len(dest.ms)-1].Timestamp
			if c.ms[0].Timestamp.Before(first) {
				first = c.ms[0].Timestamp
			}
			if c.ms[len(c.ms)-1].Timestamp.After(last) {
				last = c.ms[len(c.ms)-1].Timestamp
			}
			bounded := last.Sub(first) <= 90*24*time.Hour
			for j := range chats {
				if ambient[j] && !continuous {
					gap := c.ms[0].Timestamp.Sub(dest.ms[len(dest.ms)-1].Timestamp)
					bounded = bounded && last.Sub(first) <= 48*time.Hour && gap <= 6*time.Hour
				}
			}
			if !continuous && (!explicitThread(c.ms, ambient) || !explicitThread(dest.ms, ambient)) {
				bounded = false
			}
			if bounded && (best < 0 || i < best) {
				best = i
			}
		}
		if best < 0 {
			// Copy before appending: the incremental grouper owns its slices/maps.
			copy := *c
			copy.ms = append([]model.Message{}, c.ms...)
			out = append(out, &copy)
			for a := range c.artefacts {
				artifactIndex[a] = append(artifactIndex[a], len(out)-1)
			}
			for t := range c.core {
				if !genericCore[t] {
					index[t] = append(index[t], len(out)-1)
				}
			}
		} else {
			dest := out[best]
			dest.ms = append(dest.ms, c.ms...)
			// Keep dest.core, seedPeople and anchored frozen throughout this pass.
			sort.Slice(dest.ms, func(i, j int) bool {
				if dest.ms[i].Timestamp.Equal(dest.ms[j].Timestamp) {
					return key(ref(dest.ms[i])) < key(ref(dest.ms[j]))
				}
				return dest.ms[i].Timestamp.Before(dest.ms[j].Timestamp)
			})
		}
	}
	return out
}
