package sync_test

import (
	"strings"
	"testing"
	"time"
)

func TestSendingStatusOwnerAndLastSync(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	if a.s.SendingStatus()["holder_side"] != "standalone" {
		t.Fatal("standalone")
	}
	pair(t, a, b)
	exchange(t, a, b)
	for _, side := range []*engine{a, b} {
		status := side.s.SendingStatus()
		if status["last_synced_at"] == nil || !strings.Contains(status["summary"].(string), "holds sending") {
			t.Fatalf("status: %v", status)
		}
		if _, ok := status["merge_reviews"]; ok {
			t.Fatal("private knowledge exposed")
		}
	}
	must(t, a.s.GiveLease())
	exchange(t, a, b)
	if a.s.SendingStatus()["holder_side"] != "peer" || b.s.SendingStatus()["holder_side"] != "this_device" {
		t.Fatal("handover status")
	}
	b.now = b.now.Add(time.Hour)
	if b.s.SendingStatus()["lease_active"] != false || !strings.Contains(b.s.SendingStatus()["summary"].(string), "paused") {
		t.Fatal("expiry status")
	}
}
