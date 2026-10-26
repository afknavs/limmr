package model

import (
	"strings"
	"testing"
)

func TestCompactMessageKeepsLongNotes(t *testing.T) {
	long := strings.Repeat("a", 1659)
	if m := CompactMessage(Message{Content: long}); m.Content != long || m.Truncated || m.FullLength != 0 {
		t.Fatalf("a 1659-char note must come through whole, got %d chars truncated=%v", len([]rune(m.Content)), m.Truncated)
	}
	huge := strings.Repeat("é", MaxModelText+10)
	m := CompactMessage(Message{Content: huge})
	if len([]rune(m.Content)) != MaxModelText || !m.Truncated || m.FullLength != MaxModelText+10 {
		t.Fatalf("over-long text: got %d runes truncated=%v full=%d", len([]rune(m.Content)), m.Truncated, m.FullLength)
	}
}
