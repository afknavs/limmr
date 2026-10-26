package wa

import "testing"

func TestFTSQueryQuotesPunctuation(t *testing.T) {
	for in, want := range map[string]string{
		"Inventory: Codex tasks": `"Inventory:" "Codex" "tasks"`,
		`say "hi"`:               `"say" """hi"""`,
		"   ":                    `""`,
	} {
		if got := ftsQuery(in); got != want {
			t.Fatalf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
