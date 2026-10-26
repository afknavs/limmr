package triggers

import (
	"testing"
)

func TestMatching(t *testing.T) {
	tests := []struct {
		name    string
		match   Match
		text    string
		person  string
		mention bool
		want    bool
	}{
		{name: "any", match: Match{Kind: Any}, want: true},
		{name: "accent_case", match: Match{Kind: Keywords, Keywords: []string{"cafe"}}, text: "CAFÉ tomorrow", want: true},
		{name: "decomposed_accent", match: Match{Kind: Keywords, Keywords: []string{"résumé"}}, text: "re\u0301sume\u0301", want: true},
		{name: "unicode_fold", match: Match{Kind: Keywords, Keywords: []string{"strasse"}}, text: "Straße", want: true},
		{name: "word_boundary", match: Match{Kind: Keywords, Keywords: []string{"exam"}}, text: "example preexam exam2", want: false},
		{name: "unicode_boundary", match: Match{Kind: Keywords, Keywords: []string{"exam"}}, text: "αexam exam中", want: false},
		{name: "punctuation_boundary", match: Match{Kind: Keywords, Keywords: []string{"exam"}}, text: "(EXAM)!", want: true},
		{name: "phrase", match: Match{Kind: Keywords, Keywords: []string{"demo day"}}, text: "About demo day?", want: true},
		{name: "empty_keyword", match: Match{Kind: Keywords, Keywords: []string{""}}, text: "anything", want: false},
		{name: "regex", match: Match{Kind: Regex, Regex: `(?i)demo\s+\d+`}, text: "DEMO 42", want: true},
		{name: "invalid_regex", match: Match{Kind: Regex, Regex: `[`}, text: "hi", want: false},
		{name: "mention_metadata", match: Match{Kind: MentionsMe}, text: "hello", mention: true, want: true},
		{name: "mention_text_not_metadata", match: Match{Kind: MentionsMe}, text: "@me", want: false},
		{name: "from_person", match: Match{Kind: FromPeople, People: []string{"maya"}}, person: "maya", want: true},
		{name: "other_person", match: Match{Kind: FromPeople, People: []string{"maya"}}, person: "noah", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Watch{Scope: Scope{Chats: []string{"chat"}}, Match: tt.match}
			m := IncomingMessage{ChatJID: "chat", Text: tt.text, SenderPersonID: tt.person, MentionsMe: tt.mention}
			if got := Matches(w, m); got != tt.want {
				t.Fatalf("Matches=%v want %v", got, tt.want)
			}
		})
	}
}
func TestScopeUnion(t *testing.T) {
	for _, scope := range []Scope{{Chats: []string{"chat"}}, {People: []string{"maya"}}, {Circles: []string{"club"}}} {
		w := Watch{Scope: scope, Match: Match{Kind: Any}}
		if !Matches(w, IncomingMessage{ChatJID: "chat", SenderPersonID: "maya", CircleIDs: []string{"club"}}) {
			t.Fatal("selector should match")
		}
	}
	if Matches(Watch{Match: Match{Kind: Any}}, IncomingMessage{ChatJID: "chat"}) {
		t.Fatal("empty scope must not match")
	}
}
