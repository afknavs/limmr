package hub_test

import (
	"encoding/json"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"path/filepath"
	"testing"
)

func TestU4HistoryUsesMessageDateNotIngestionAndZeroFills(t *testing.T) {
	h, _, _ := testkit.Open(t)
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Group, Name: "Fixture group"}))
	for i, offset := range []int{-20, -6, -1, 0, 1} {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: string(rune('a' + i)), ChatJID: testkit.Group, Content: "Fixture message", Timestamp: h.Now().AddDate(0, 0, offset), IsFromMe: i%2 == 0}))
	}
	for _, offset := range []int{-20, -6, -1, 0, 1} {
		testkit.Must(t, h.Record(model.Activity{At: h.Now().AddDate(0, 0, offset), AgentID: "ai", Tool: "list_messages", Kind: "read", Count: 4}))
		testkit.Must(t, h.Record(model.Activity{At: h.Now().AddDate(0, 0, offset), AgentID: "ai", Tool: "send_message", Kind: "sent"}))
	}
	history, e := h.StatsHistory(30)
	testkit.Must(t, e)
	if len(history.Days) != 30 || history.Days[29].MessagesIn+history.Days[29].MessagesOut != 1 || history.Days[28].AgentReads != 1 || history.Days[27].AgentReads != 0 {
		t.Fatalf("bad buckets: %+v", history)
	}
	if history.Days[9].MessagesOut != 1 || history.Days[9].Agents["ai"].Sends != 1 {
		t.Fatal("historical messages/activity missing")
	}
	stats, e := h.HistoricalDashboardStats()
	testkit.Must(t, e)
	if stats.Today.Messages != 1 || stats.Week.Messages != 2 {
		t.Fatalf("legacy totals still ingestion-based: %+v", stats)
	}
	for _, days := range []int{-1, 0, 367} {
		if _, e = h.StatsHistory(days); !errors.Is(e, model.ErrInvalid) {
			t.Fatal("unbounded history accepted")
		}
	}
}
func TestU4IconPersistsAcrossReopenAndEntityReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	repo, e := hub.Open(path)
	testkit.Must(t, e)
	h := hub.New(repo, nil, t.TempDir())
	testkit.Must(t, repo.Put("people", "fixture", map[string]any{"id": "fixture", "name": "Fixture person"}))
	icon := hub.EntityIcon{Kind: "emoji", Value: "🎓"}
	testkit.Must(t, h.SetEntityIcon("people", "fixture", &icon))
	testkit.Must(t, repo.Put("people", "fixture", map[string]any{"id": "fixture", "name": "Renamed fixture"}))
	testkit.Must(t, repo.Close())
	repo, e = hub.Open(path)
	testkit.Must(t, e)
	defer repo.Close()
	h = hub.New(repo, nil, t.TempDir())
	v, e := h.WithEntityIcons(map[string]any{"person": map[string]any{"id": "fixture"}})
	testkit.Must(t, e)
	raw, _ := json.Marshal(v)
	var decoded struct{ Person struct{ Icon hub.EntityIcon } }
	testkit.Must(t, json.Unmarshal(raw, &decoded))
	if decoded.Person.Icon != icon {
		t.Fatal("icon lost")
	}
	testkit.Must(t, h.SetEntityIcon("people", "fixture", nil))
	if e = h.SetEntityIcon("people", "fixture", &hub.EntityIcon{Kind: "html", Value: "<script>"}); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("bad icon accepted")
	}
	if e = h.SetEntityIcon("people", "missing", &icon); e != model.ErrNotFound {
		t.Fatal("orphan icon accepted")
	}
}
