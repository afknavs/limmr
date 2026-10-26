package perf_test

import (
	"crypto/sha256"
	"fmt"
	"limmr/internal/efforts"
	"limmr/internal/testkit"
	"sort"
	"strings"
	"testing"
)

// E7 removes uninvolved broadcasts from the 52-member synthetic groups.
// Keep every remaining message/ref stable, including all direct-chat work.
func TestE7P8OwnerInvolvementProjection(t *testing.T) {
	r := p8Open(t, p8Fixture(t))
	defer r.close()
	svc := r.mc.EffortService()
	es, err := svc.List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	testkit.Must(t, err)
	rows := []string{}
	for _, e := range es {
		for _, ref := range e.MessageRefs {
			if strings.HasSuffix(ref.ChatJID, "@g.us") {
				var group int
				if _, err := fmt.Sscanf(ref.ChatJID, "%d@g.us", &group); err != nil {
					t.Fatal(err)
				}
				// p8Fixture writes from the owner exactly when chat index % 3 == 0.
				if (536+group-100000)%3 != 0 {
					t.Fatal("uninvolved broadcast entered projected work")
				}
			}
			rows = append(rows, e.ID+"|"+ref.ChatJID+"|"+ref.MessageID)
		}
	}
	sort.Strings(rows)
	if len(es) != 74 || len(rows) != 12746 || fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(rows, "\n")))) != "6104d688b25c58d3d5c1917b2630e80ce399cdbce746f2add0f12abec4b9cd05" {
		t.Fatalf("E7 involvement projection: efforts=%d refs=%d digest=%x", len(es), len(rows), sha256.Sum256([]byte(strings.Join(rows, "\n"))))
	}
	audit, err := svc.Audit()
	testkit.Must(t, err)
	if audit.DuplicateTitles != 0 || audit.GenericTitles != 0 || audit.Named+audit.NamingQueued != 74 || audit.Named >= 74 {
		t.Fatalf("unexpected title audit: named=%d queued=%d duplicates=%d generic=%d", audit.Named, audit.NamingQueued, audit.DuplicateTitles, audit.GenericTitles)
	}
}
