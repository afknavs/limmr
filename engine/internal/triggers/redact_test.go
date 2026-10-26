package triggers

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCredentialIsRedactedAcrossProcessWrites(t *testing.T) {
	// Construct a synthetic canary; never include its value in failure output.
	secret := strings.Repeat("private-canary-", 6)
	var out bytes.Buffer
	w := &credentialLog{dst: &out, secret: []byte(secret)}
	for _, chunk := range []string{"prefix ", secret[:17], secret[17:], " suffix"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal("redacting log write failed")
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal("redacting log flush failed")
	}
	if strings.Contains(out.String(), secret) || out.String() != "prefix [redacted] suffix" {
		t.Fatal("run credential was not redacted")
	}
}
