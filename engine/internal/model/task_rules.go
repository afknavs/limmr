package model

import (
	"strings"
	"time"
	"unicode"
)

// NeedsYouTTL is shared by task flags, surfaced requests, suggestions and briefs.
const NeedsYouTTL = 7 * 24 * time.Hour

// LooksLikeTask is deliberately conservative and deterministic. Unknown names
// remain a judgement for the agent; false positives have an explicit override.
func LooksLikeTask(kind, name, goal string) bool {
	if kind == "project" {
		return true
	}
	text := strings.ToLower(name + " " + goal)
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	markers := map[string]bool{"project": true, "event": true, "fundraiser": true, "workshop": true, "conference": true, "deadline": true, "errand": true, "launch": true, "wedding": true, "exam": true, "submit": true, "deliver": true, "finish": true}
	for _, w := range words {
		if markers[w] {
			return true
		}
	}
	return false
}
