package mcpserver_test

import (
	"context"
	"encoding/json"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"limmr/internal/watches"
	"strings"
	"testing"
	"time"
)

func TestN1RecentlyFiltersBeforeWindowAndCountsAndReachesMCP(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	ws, err := watches.New(h, p, watches.Options{})
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p, Watches: ws}
	svc := mc.TaskService()
	for _, id := range []string{"study", "private"} {
		testkit.Must(t, h.Repo.Put("circles", id, model.Circle{ID: id, Name: id, Confirmed: true, Chats: []model.CircleChat{}, People: []model.CirclePerson{}}))
	}
	public, err := svc.Create(&a, tasks.Create{Title: "Exam guide", Kind: "long"})
	testkit.Must(t, err)
	_, err = svc.Update(nil, public.ID, map[string]any{"circle_ids": []string{"study"}})
	testkit.Must(t, err)
	_, err = svc.OwnerNote(nil, public.ID, "", "Invented note text stays out of summary", false)
	testkit.Must(t, err)
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "brief", Kind: "summary", ChatJIDs: []string{testkit.Phone}, Count: 1}))
	ownerLines, err := svc.Recently(nil, "")
	testkit.Must(t, err)
	if !strings.Contains(strings.Join(ownerLines, " "), "1 other AI action") {
		t.Fatal("stored AI action missing")
	}
	now = now.Add(48 * time.Hour)
	private, err := svc.Create(nil, tasks.Create{Title: "Secret fixture task", Kind: "long"})
	testkit.Must(t, err)
	_, err = svc.Update(nil, private.ID, map[string]any{"circle_ids": []string{"private"}})
	testkit.Must(t, err)
	a.Permissions.Access.Circles = []string{"study"}
	a.Permissions.Access.Unsorted = false
	testkit.Must(t, h.UpdateAgent(a))
	lines, err := svc.Recently(&a, "")
	testkit.Must(t, err)
	text := strings.Join(lines, "\n")
	if len(lines) < 3 || len(lines) > 5 || !strings.Contains(text, "last 3 days") || !strings.Contains(text, "1 task") || !strings.Contains(text, "You") || strings.Contains(text, "Secret") || strings.Contains(text, "Invented note text") {
		t.Fatalf("wrong scoped summary: %s", text)
	}
	updates, err := mc.Call(context.Background(), a.ID, "get_updates", map[string]any{})
	testkit.Must(t, err)
	data, err := json.Marshal(updates)
	testkit.Must(t, err)
	if !strings.Contains(string(data), `"recently"`) || strings.Contains(string(data), "Secret fixture") {
		t.Fatal("MCP updates missing filtered Recently")
	}
	brief, err := mc.Call(context.Background(), a.ID, "brief", map[string]any{"action": "get", "id": "study"})
	testkit.Must(t, err)
	if len(brief.(map[string]any)["recently"].([]string)) < 3 {
		t.Fatal("brief missing Recently")
	}
	a.Permissions.Access.Circles = []string{}
	testkit.Must(t, h.UpdateAgent(a))
	lines, err = svc.Recently(&a, "")
	testkit.Must(t, err)
	if strings.Contains(strings.Join(lines, " "), "Exam guide") {
		t.Fatal("revoked circle leaked title")
	}
}
