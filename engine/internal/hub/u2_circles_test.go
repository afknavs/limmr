package hub

import (
	"limmr/internal/model"
	"path/filepath"
	"testing"
)

func TestU2DraftPlacementExcludesArchivesAndKeepsConfirmationRule(t *testing.T) {
	repo, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.DB.Close()
	s := &Service{Repo: repo}
	c := model.Circle{ID: "fixture-circle", Name: "Design Club", Chats: []model.CircleChat{{ChatJID: "fixture@g.us"}}}
	for _, state := range []struct {
		confirmed, archived bool
		want                string
	}{
		{false, false, ""}, {true, true, ""}, {true, false, c.ID},
	} {
		c.Confirmed, c.Archived = state.confirmed, state.archived
		if err := repo.Put("circles", c.ID, c); err != nil {
			t.Fatal(err)
		}
		if got := s.circleForChat("fixture@g.us"); got != state.want {
			t.Fatalf("placement = %q, want %q", got, state.want)
		}
	}
}
