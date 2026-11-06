package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
)

func TestN1QuietActivityPagesPastEmptyReadsWithoutRemovingAudit(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	for i := 0; i < 2000; i++ {
		count := 0
		if i%100 == 0 {
			count = 2
		}
		testkit.Must(t, h.Record(model.Activity{AgentID: "example-ai", Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Phone}, Count: count}))
	}
	raw, e := h.Activity(nil, hub.ActivityFilter{Limit: 50})
	testkit.Must(t, e)
	if len(raw) != 50 || raw[0].Count != 0 {
		t.Fatal("raw audit changed")
	}
	page, e := h.Activity(nil, hub.ActivityFilter{HideEmpty: true, Limit: 10})
	testkit.Must(t, e)
	if len(page) != 10 {
		t.Fatalf("page has %d rows", len(page))
	}
	second, e := h.Activity(nil, hub.ActivityFilter{HideEmpty: true, Limit: 10, Before: page[9].ID})
	testkit.Must(t, e)
	if len(second) != 10 || second[0].ID >= page[9].ID {
		t.Fatal("cursor repeated or lost rows")
	}
	for _, row := range append(page, second...) {
		if row.Count != 2 {
			t.Fatal("empty read leaked")
		}
	}
	other, e := h.Activity(nil, hub.ActivityFilter{Agent: "missing", HideEmpty: true})
	testkit.Must(t, e)
	if len(other) != 0 {
		t.Fatal("agent filter ignored")
	}
	a, _, e := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, e)
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "list_messages", Kind: "read", ChatJIDs: []string{testkit.Phone}, Count: 1}))
	permissions := a.Permissions
	permissions.Chats = model.Scope{Mode: "only", JIDs: []string{}}
	a.Permissions = permissions
	private, e := h.Activity(&a, hub.ActivityFilter{HideEmpty: true})
	testkit.Must(t, e)
	if len(private) != 0 {
		t.Fatal("scope bypass")
	}
}
