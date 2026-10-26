package circles

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"strings"
	"testing"
)

func TestP4MetadataCannotEraseThreadsOrCounts(t *testing.T) {
	out := UnderstandingMap{Summary: MapSummary{Connections: 120, ByAI: 30, AIThisWeek: 20}, Events: []MapEvent{}, Pulses: []MapPulse{}}
	for i := 0; i < 8; i++ {
		out.Nodes = append(out.Nodes, MapNode{ID: fmt.Sprint(i), Kind: "circle", Title: "Invented community circle"})
	}
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("peer-%d", i)
		n := MapNode{ID: id, Kind: "person", Title: "Invented Peer", HowKnown: strings.Repeat("Shared projects and planning. ", 20), GroupsInCommonCount: 3}
		for c := 0; c < 8; c++ {
			n.CircleAffinity = append(n.CircleAffinity, model.CircleAffinity{CircleID: fmt.Sprint(c), Score: .8, Reasons: []string{strings.Repeat("Many shared groups. ", 30), strings.Repeat("Recent contact. ", 30)}})
		}
		for g := 0; g < 3; g++ {
			n.GroupsInCommon = append(n.GroupsInCommon, model.GroupInCommon{ID: fmt.Sprint(g), Name: strings.Repeat("Invented planning group ", 12), CircleIDs: []string{"0", "1", "2", "3"}})
		}
		out.Nodes = append(out.Nodes, n)
		out.Edges = append(out.Edges, MapEdge{ID: fmt.Sprintf("edge-%03d", i), Source: fmt.Sprint(i % 8), Target: id, Kind: "membership", Origin: model.Origin{ByKind: "structure"}})
	}
	boundMap(&out)
	if len(out.Edges) != 120 {
		t.Fatalf("metadata erased threads: %d", len(out.Edges))
	}
	if out.Summary.Connections != 120 || out.Summary.ByAI != 30 || out.Summary.AIThisWeek != 20 {
		t.Fatal("display cap corrupted real totals")
	}
	data, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if len(data) >= 100000 {
		t.Fatalf("payload bytes=%d", len(data))
	}
	t.Logf("dense enriched payload: %d bytes, %d edges", len(data), len(out.Edges))
}
