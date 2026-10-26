package efforts

import (
	"limmr/internal/model"
	"strings"
)

func machineAuthor(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "this mac", "this-mac", "limmr", "naming queue", "heuristic", "heuristics":
		return true
	}
	return strings.Contains(name, "naming queue")
}
func titleOrigin(o override) string {
	if o.TitleOrigin != "" {
		return o.TitleOrigin
	}
	if machineAuthor(o.By) {
		return "machine"
	}
	if o.By == "you" {
		return "user"
	}
	return "agent"
}
func deliberateTitle(o override) bool {
	if o.Update.Title == nil || !goodTitle(*o.Update.Title) {
		return false
	}
	origin := titleOrigin(o)
	return origin == "user" || origin == "agent"
}

// Manual names keep intent, not extra visibility. Hidden/expired/bot refs have
// already failed the source safety check before this eligibility exemption.
func eligibleOverride(o override, q qualityRules, messages map[string]model.Message, ambient map[string]bool) bool {
	if deliberateTitle(o) {
		return true
	}
	ms := make([]model.Message, 0, len(o.Refs))
	for _, r := range o.Refs {
		ms = append(ms, messages[key(r)])
	}
	if len(ms) == 0 || !taskEvidence(ms) || !q.involved(ms) {
		return false
	}
	// Explicit attach/split still needs work and involvement, but can intentionally
	// contain a short part. Automatic names must pass the full inference gate.
	if o.Update.Title == nil && !machineAuthor(o.By) {
		return true
	}
	return len(sharedTerms(ms, q)) >= 2 && explicitThread(ms, ambient)
}
