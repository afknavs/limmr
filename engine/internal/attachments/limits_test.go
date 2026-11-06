package attachments

import (
	"strings"
	"testing"
)

func TestMediaSpecificLimits(t *testing.T) {
	for _, m := range []string{"application/pdf", "image/png", "video/mp4", "audio/ogg"} {
		_, limit := Limit(m)
		if e := CheckSize(limit, m); e != nil {
			t.Fatal(e)
		}
		e := CheckSize(limit+1, m)
		if e == nil || !strings.Contains(e.Error(), "Send a link") || !strings.Contains(e.Error(), "MiB") {
			t.Fatalf("%s: %v", m, e)
		}
	}
}
