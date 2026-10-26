package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestU3SessionsDigestPersistenceAndScope(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, e := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, e)
	other, _, e := h.CreateAgent("Other AI", "custom", "local")
	testkit.Must(t, e)
	a.Permissions = model.Defaults(false)
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	testkit.Must(t, h.UpdateAgent(a))
	now := testkit.Epoch.Add(3 * time.Hour)
	h.Now = func() time.Time { return now }
	events := []model.Activity{
		{AgentID: a.ID, At: now.Add(-40 * time.Minute), Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 8},
		{AgentID: a.ID, At: now.Add(-10 * time.Minute), Kind: "search", Tool: "search_messages", ChatJIDs: []string{testkit.Group}, Count: 6},
		{AgentID: a.ID, At: now.Add(-5 * time.Minute), Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 0},
		{AgentID: a.ID, At: now.Add(-4 * time.Minute), Kind: "read", Tool: "list_chats", Count: 300},
		{AgentID: a.ID, At: now.Add(-time.Minute), Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Phone}, Count: 4},
		{AgentID: other.ID, At: now, Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 12},
	}
	for _, v := range events {
		testkit.Must(t, h.Record(v))
	}
	sessions := hub.GroupReadSessions(events)
	if len(sessions) != 2 || sessions[0].Messages != 18 {
		t.Fatalf("bad sessions: %+v", sessions)
	}
	owner, e := h.ActivityDigests(nil)
	testkit.Must(t, e)
	if len(owner) != 2 {
		t.Fatalf("owner digest count %d", len(owner))
	}
	agent, e := h.ActivityDigests(&a)
	testkit.Must(t, e)
	if len(agent) != 1 || agent[0].Messages != 14 || !strings.Contains(agent[0].Text, "Design Club · 14 messages across 1 read session.") || strings.Contains(agent[0].Text, "Maya") {
		t.Fatalf("scope/text: %+v", agent)
	}
	var saved hub.ActivityDigest
	testkit.Must(t, h.Repo.Get("activity_digests_agent", agent[0].ID, &saved))
	if saved != agent[0] {
		t.Fatal("digest not persisted")
	}
	// Current access is rechecked even after a privileged digest was saved.
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Group}
	testkit.Must(t, h.SetSettings(settings))
	agent, e = h.ActivityDigests(&a)
	testkit.Must(t, e)
	if len(agent) != 0 {
		t.Fatal("stale hidden-chat digest leaked")
	}
	a.Permissions.ReadMessages = false
	if _, e = h.ActivityDigests(&a); e != model.ErrForbidden {
		t.Fatal("read permission bypass")
	}
}
func TestU3StatsCountsBeyondActivityPageAndMatchesReceipts(t *testing.T) {
	h, _, _ := testkit.Open(t)
	a, _, e := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, e)
	for i := 0; i < 70; i++ {
		testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "send_draft", Kind: "sent", Count: 1}))
	}
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "send_draft", Kind: "sent", ChatJIDs: []string{testkit.Group}, MessageIDs: []string{"outbound"}, Note: "A useful reply", Count: 1}))
	testkit.Must(t, h.Repo.Put("drafts", "draft", model.Draft{ID: "draft", ChatJID: testkit.Group, MessageID: "outbound", Status: "read"}))
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "list_messages", Kind: "read", Count: 4}))
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Tool: "list_messages", Kind: "read", Count: 0}))
	stats, e := h.DashboardStats()
	testkit.Must(t, e)
	if stats.Today.Sends != 71 || stats.Week.Sends != 71 || stats.Today.ReadSessions != 1 {
		t.Fatalf("bad totals %+v", stats)
	}
	found := false
	for _, v := range stats.RecentSends {
		if len(v.MessageIDs) > 0 && v.MessageIDs[0] == "outbound" {
			found = v.Status == "read" && v.Note == "A useful reply"
		}
	}
	if !found {
		t.Fatal("receipt enrichment lost")
	}
	if len(stats.RecentSends) != 12 {
		t.Fatal("recent sends not bounded")
	}
}
func TestU3IngestionCountsOnceAndPersists(t *testing.T) {
	h, _, _ := testkit.Open(t)
	h.Now = time.Now
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Group, Name: "Design Club"}))
	// Historical message timestamps must not be confused with sync time.
	m := model.Message{ID: "old", ChatJID: testkit.Group, Content: "Fixture", Timestamp: testkit.Epoch.AddDate(-2, 0, 0)}
	testkit.Must(t, h.Messages.PutMessage(m))
	testkit.Must(t, h.Messages.PutMessage(m))
	stats, e := h.DashboardStats()
	testkit.Must(t, e)
	if stats.Today.Messages != 1 || stats.Week.Messages != 1 || stats.LastSyncAt == nil || stats.TrackingSince == nil {
		t.Fatalf("ingestion stats: %+v", stats)
	}
	again, e := h.DashboardStats()
	testkit.Must(t, e)
	if again.Today.Messages != 1 || !again.LastSyncAt.Equal(*stats.LastSyncAt) {
		t.Fatal("read changed sync state")
	}
}
func TestU3SessionDayAndGapBoundaries(t *testing.T) {
	at := time.Date(2026, 10, 16, 23, 59, 0, 0, time.UTC)
	xs := []model.Activity{{ID: 1, At: at, AgentID: "a", Kind: "read", Tool: "list_messages", Count: 1}, {ID: 2, At: at.Add(time.Minute), AgentID: "a", Kind: "read", Tool: "list_messages", Count: 2}, {ID: 3, At: at.Add(31*time.Minute + time.Second), AgentID: "a", Kind: "read", Tool: "list_messages", Count: 3}}
	if len(hub.GroupReadSessions(xs)) != 3 {
		t.Fatal("day or inactivity boundary lost")
	}
}
