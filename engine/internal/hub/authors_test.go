package hub_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"path/filepath"
	"testing"
	"time"
)

func TestL1AuthorBackfillAndRetention(t *testing.T) {
	h, _, _ := testkit.Open(t)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	// Old stores have successful drafts/approvals but no author table.
	testkit.Must(t, h.Repo.Put("drafts", "old-draft", model.Draft{ID: "old-draft", AgentID: a.ID, ChatJID: "example@g.us", MessageID: "shared-id", State: "sent"}))
	testkit.Must(t, h.Repo.Put("approvals", "old-approval", model.Approval{ID: "old-approval", AgentID: a.ID, ChatJID: "example@g.us", MessageID: "old-approval-id", State: "sent"}))
	_, err = h.Repo.DB.Exec("DROP TABLE send_authors")
	testkit.Must(t, err)
	// A separate open runs the production upgrade without reading a live store.
	repo, err := hub.Open(filepath.Join(h.Home, "hub.db"))
	testkit.Must(t, err)
	defer repo.Close()
	upgraded := hub.New(repo, h.Messages, h.Home)
	for _, id := range []string{"shared-id", "old-approval-id"} {
		m, err := upgraded.Attribute(model.Message{ChatJID: "example@g.us", ID: id, IsFromMe: true, Timestamp: time.Now().Add(-time.Hour)})
		testkit.Must(t, err)
		if m.Author.Type != "agent" || m.Author.AgentID != a.ID {
			t.Fatal("legacy send not backfilled")
		}
	}
	testkit.Must(t, repo.Delete("drafts", "old-draft"))
	testkit.Must(t, repo.Delete("approvals", "old-approval"))
	testkit.Must(t, repo.Delete("agents", a.ID))
	m, err := upgraded.Attribute(model.Message{ChatJID: "example@g.us", ID: "shared-id", IsFromMe: true})
	testkit.Must(t, err)
	if m.Author.Type != "agent" || m.Author.AgentName != "Example AI" {
		t.Fatal("retention lost author")
	}
	m, err = upgraded.Attribute(model.Message{ChatJID: "different@g.us", ID: "shared-id", Timestamp: time.Now()})
	testkit.Must(t, err)
	if m.Author.Type != "contact" {
		t.Fatal("attribution crossed chats")
	}
}

func TestL1AuthorSurvivesChatAlias(t *testing.T) {
	h, _, _ := testkit.Open(t)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	_, err = h.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", "900000001@lid", "aliased-id", a.ID, a.Name)
	testkit.Must(t, err)
	testkit.Must(t, h.Messages.NormalizeMapping("900000001@lid", "15550100001@s.whatsapp.net"))
	m, err := h.Attribute(model.Message{ChatJID: "15550100001@s.whatsapp.net", ID: "aliased-id", IsFromMe: true, Timestamp: time.Now()})
	testkit.Must(t, err)
	if m.Author.Type != "agent" || m.Author.AgentID != a.ID {
		t.Fatal("chat normalization lost author")
	}
}
