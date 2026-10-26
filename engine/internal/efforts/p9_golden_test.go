package efforts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"limmr/internal/testkit"
	"testing"
)

// Includes every public effort field plus the counts-only audit for E5's
// scenarios, all six sustained/blob shapes and the mixed 15,752-message corpus.
// E7 intentionally replaces the pristine E6 digest (9a063b155b0860d9bbc497b5cebd80c39e842fff4863c65a038f52880cc652e8):
// short filler no longer contributes weak candidates.
// Positive/negative labels and complete task-evidence assertions remain gates.
func TestP9E5E6Golden(t *testing.T) {
	cases := append(e5PrecisionCorpus(), e5Round2Shapes()...)
	all, _ := round2MixedCorpus()
	cases = append(cases, recallCase{name: "mixed", ms: all})
	hash := sha256.New()
	for _, tc := range cases {
		s := qualitySeed(t, nil)
		putRound2(t, s, tc.ms)
		xs, err := s.List(nil, Filter{IncludeUntitled: true, Limit: 500})
		testkit.Must(t, err)
		audit, err := s.Audit()
		testkit.Must(t, err)
		raw, err := json.Marshal([]any{tc.name, xs, audit})
		testkit.Must(t, err)
		hash.Write(raw)
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	const want = "73fcabf4f535ba6407defeb271b2d6e7166c6a3979cef65d08d5bf0733dd4f75"
	if got != want {
		t.Fatalf("E7 output digest %s != %s", got, want)
	}
}
