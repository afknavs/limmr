package model

import (
	"strings"
	"unicode"
)

// Named requires a human-readable name, never a telephone number or LID.
func Named(p Person) bool {
	n := strings.TrimSpace(p.Label)
	if n == "" {
		n = strings.TrimSpace(p.Name)
	}
	if strings.EqualFold(n, "Unknown") || strings.EqualFold(n, "Unnamed person") || strings.Contains(n, "@") {
		return false
	}
	for _, r := range n {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
func FallbackName(jid string) string {
	if strings.HasSuffix(jid, "@lid") {
		return "Unknown"
	}
	if strings.HasSuffix(jid, "@s.whatsapp.net") {
		return "+" + strings.SplitN(jid, "@", 2)[0]
	}
	return "Unknown"
}

// MaxModelText is the most characters of one message an agent sees. It is large enough for long notes and
// handoffs; anything longer is marked truncated with its full length.
const MaxModelText = 4000

// CompactMessage bounds model-facing context, leaving original text in storage.
func CompactMessage(m Message) Message { return CompactMessageTo(m, MaxModelText) }

// MaxCatchUpText bounds each message inside a multi-chat catch-up; list_messages shows up to MaxModelText.
const MaxCatchUpText = 1500

// CompactMessageTo is CompactMessage with an explicit character limit.
func CompactMessageTo(m Message, limit int) Message {
	if m.MediaType != "" {
		m.Content = "[" + m.MediaType + "] " + strings.Join(strings.Fields(m.Content), " ")
		m.Filename = ""
	}
	r := []rune(m.Content)
	if len(r) > limit {
		m.Content = string(r[:limit])
		m.Truncated = true
		m.FullLength = len(r)
	}
	return m
}
