package efforts

import (
	"sort"
	"strings"
	"unicode"
)

// These are conversational actions/filler, not noun modifiers. Do not strip by
// suffix: "funding plan" and invented names ending in -ing/-ed can be nouns.
// Ambiguous nominal words such as review, design and training remain allowed.
var titleLeadNoise = words("a an the our your my their this that these those taken taking take took onwards onward please kindly already still just really actually basically also anyway now then make making made get getting got going gone go do doing did done send sending sent bring bringing brought ask asking asked discuss discussing discussed prepare preparing prepared finish finishing finished complete completing completed confirm confirming confirmed decide deciding decided agree agreeing agreed need needing needed want wanting wanted have having had has been being was were will would can could should must let lets planning planned organizing organized organising organised arranging arranged holding held attending attended selecting selected choosing chosen booking booked")

var genericTitleHeads = words("event events session sessions meeting meetings plan plans project projects task tasks work venue venues")
var genericTitleModifiers = words("a an the our your my their this that these those first second third fourth fifth next last new old another other upcoming previous annual monthly weekly daily initial final general generic some any one two three main big small special usual regular normal common current future latest important recurring routine ever chat conversation group club team")

func genericTitle(s string) bool {
	head := false
	for _, w := range rawWords(s) {
		if genericTitleHeads[w] {
			head = true
		} else if !genericTitleModifiers[w] && !allDigits(w) {
			return false
		}
	}
	return head
}

func cleanInferredTitle(s string) string {
	ws := strings.Fields(s)
	for len(ws) > 0 {
		raw := rawWords(ws[0])
		if len(raw) != 1 || !titleLeadNoise[raw[0]] {
			break
		}
		ws = ws[1:]
	}
	s = strings.Join(ws, " ")
	if s != "" {
		r := []rune(s)
		r[0] = unicode.ToUpper(r[0])
		s = string(r)
	}
	return s
}

func titleKey(s string) string { return strings.Join(rawWords(s), " ") }

// Count extra occurrences, not pairs: three copies of one title count as two.
// Keys never escape this function; audit output contains only integers.
func titleCounts(es []Effort) (duplicates, generic int) {
	seen := map[string]bool{}
	for _, e := range es {
		k := titleKey(e.Title)
		if k == "" {
			continue
		}
		if seen[k] {
			duplicates++
		}
		seen[k] = true
		if genericTitle(e.Title) {
			generic++
		}
	}
	return
}

// E5 alone owns grouping. Resolve duplicate labels after grouping, in stable
// evidence order, using only this scope's metadata. Never join by title equality.
// Explicit edits remain authoritative and are audited if they collide.
func dedupeTitles(es []Effort, c cached, q qualityRules) {
	order := make([]int, len(es))
	reserved := map[string]bool{}
	for i, e := range es {
		order[i] = i
		if goodTitle(e.Title) {
			reserved[titleKey(e.Title)] = true
		}
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := es[order[i]], es[order[j]]
		if explicitTitle(a) != explicitTitle(b) {
			return explicitTitle(a)
		}
		if !a.FirstAt.Equal(b.FirstAt) {
			return a.FirstAt.Before(b.FirstAt)
		}
		return a.ID < b.ID
	})
	seen := map[string]bool{}
	for _, i := range order {
		e := &es[i]
		k := titleKey(e.Title)
		if !goodTitle(e.Title) {
			continue
		}
		if !seen[k] || explicitTitle(*e) {
			seen[k] = true
			continue
		}
		base := e.Title
		e.Title, e.titleSource = "", "naming_queue"
		qualifiers := []string{}
		// Only group topic words, with every known identity token removed.
		// A DM label is a person's name and is never a qualifier source.
		for _, chat := range e.Chats {
			if !strings.HasSuffix(chat.ID, "@g.us") {
				continue
			}
			for _, w := range rawWords(chat.Name) {
				if !q.names[w] && !stop[w] && !genericCore[w] && !genericTitleHeads[w] && !genericTitleModifiers[w] && !titleLeadNoise[w] && !terms(base)[w] && !allDigits(w) {
					qualifiers = append(qualifiers, w)
				}
			}
		}
		for _, r := range e.MessageRefs {
			m := c.messages[key(r)]
			if m.MediaType != "document" || !goodFile(m.Filename) {
				continue
			}
			file := cleanInferredTitle(FilenameTitle(m.Filename))
			safe := goodTitle(file)
			for _, w := range rawWords(file) {
				safe = safe && !q.names[w]
			}
			if safe && titleKey(file) != k {
				qualifiers = append(qualifiers, file)
			}
		}
		qualifiers = append(qualifiers, e.FirstAt.Format("Jan 2006"))
		for _, qualifier := range qualifiers {
			title := base + " · " + qualifier
			key := titleKey(title)
			if len([]rune(title)) <= 40 && !reserved[key] && goodTitle(title) {
				e.Title, e.titleSource = title, e.baseTitleSource
				reserved[key], seen[key] = true, true
				break
			}
		}
	}
}

func explicitTitle(e Effort) bool {
	return e.titleSource == "assigned_title" && !e.titleAutomatic && (e.CreatedBy == "" || !machineAuthor(e.CreatedBy))
}
