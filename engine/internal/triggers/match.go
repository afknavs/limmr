package triggers

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return cases.Fold().String(b.String())
}
func word(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.IsMark(r)
}
func containsPhrase(text, phrase string) bool {
	hay, needle := []rune(fold(text)), []rune(fold(strings.TrimSpace(phrase)))
	if len(needle) == 0 {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) != string(needle) {
			continue
		}
		end := i + len(needle)
		if (i == 0 || !word(hay[i-1])) && (end == len(hay) || !word(hay[end])) {
			return true
		}
	}
	return false
}
func has(v []string, s string) bool {
	for _, x := range v {
		if x == s && s != "" {
			return true
		}
	}
	return false
}
func inScope(s Scope, m IncomingMessage) bool {
	if s.All || has(s.Chats, m.ChatJID) || has(s.People, m.SenderPersonID) || has(s.People, m.Sender) {
		return true
	}
	for _, id := range m.CircleIDs {
		if has(s.Circles, id) {
			return true
		}
	}
	return false
}

// Matches uses a union of explicit scope selectors. Empty scope matches nothing.
// Regex uses Go's RE2 syntax (use (?i) for case-insensitive regex).
func Matches(w Watch, m IncomingMessage) bool {
	if m.FromMe || m.FromLimmr || !inScope(w.Scope, m) {
		return false
	}
	switch w.Match.Kind {
	case Any:
		return true
	case Keywords:
		for _, k := range w.Match.Keywords {
			if containsPhrase(m.Text, k) {
				return true
			}
		}
	case Regex:
		r, err := regexp.Compile(w.Match.Regex)
		return err == nil && r.MatchString(m.Text)
	case MentionsMe:
		return m.MentionsMe
	case FromPeople:
		return has(w.Match.People, m.SenderPersonID) || has(w.Match.People, m.Sender)
	}
	return false
}
