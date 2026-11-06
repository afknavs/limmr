package tasks

import (
	"context"
	"errors"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestOct29OwnerNotesAreOwnerOnlyDurableAndPrivate(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	if _, err := s.OwnerNote(a, v.ID, "", "Changed venue", false); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("agent wrote owner note")
	}
	got, err := s.OwnerNote(nil, v.ID, "", "Changed venue to North Hall", false)
	testkit.Must(t, err)
	if len(got.OwnerNotes) != 1 {
		t.Fatal("note missing")
	}
	note := got.OwnerNotes[0]
	*now = now.Add(time.Minute)
	got, err = s.OwnerNote(nil, v.ID, note.ID, "Please involve Maya", false)
	testkit.Must(t, err)
	if got.OwnerNotes[0].CreatedAt != note.CreatedAt || got.OwnerNotes[0].Text != "Please involve Maya" {
		t.Fatal("edit lost identity")
	}
	agent, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(agent.OwnerNotes) != 1 {
		t.Fatal("agent cannot read notes")
	}
	if _, err = s.Update(a, v.ID, map[string]any{"owner_notes": []any{}}); err == nil {
		t.Fatal("agent update accepted owner notes")
	}
	var projection string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT data FROM task_cards WHERE id=?", v.ID).Scan(&projection))
	if strings.Contains(projection, "Please involve Maya") {
		t.Fatal("Home contains note body")
	}
	var updates int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates WHERE kind='task_owner_note' AND agent=?", a.ID).Scan(&updates))
	if updates != 2 {
		t.Fatal("missing durable note updates", updates)
	}
	got, err = s.OwnerNote(nil, v.ID, note.ID, "", true)
	testkit.Must(t, err)
	if len(got.OwnerNotes) != 0 {
		t.Fatal("delete failed")
	}
	var removed int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT removed FROM sync_items WHERE task_id=? AND list='owner_notes' AND id=?", v.ID, note.ID).Scan(&removed))
	if removed != 1 {
		t.Fatal("missing sync tombstone")
	}
}
func TestOct29NamesCoverSelectionAndDeletedSource(t *testing.T) {
	s, a, now := fixture(t)
	for _, m := range []model.Message{
		{ID: "poster", ChatJID: testkit.Phone, Sender: testkit.Phone, MediaType: "image", Filename: "IMG_001.jpg", Content: "Workshop poster — 5 Nov, North Campus", Timestamp: now.Add(-time.Hour)},
		{ID: "chat-shot", ChatJID: testkit.Phone, MediaType: "image", Filename: "Screenshot.png", Timestamp: now.Add(time.Second)},
	} {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	_, err := s.Hub.Messages.DB.Exec("UPDATE messages SET preview_width=1200,preview_height=1600")
	testkit.Must(t, err)
	coverOriginal(t, s, testkit.Phone, "poster", 1200, 1600)
	v, err := s.Create(a, Create{Title: "Workshop poster", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	if v.Cover == nil || v.Cover.Title != "IMG_001.jpg" {
		t.Fatal("poster not selected")
	}
	var poster Item
	for _, it := range v.Items {
		if it.Title == "IMG_001.jpg" {
			poster = it
		}
	}
	if !strings.Contains(poster.AIName, "Workshop") || poster.Sender == "" || poster.Chat == "" {
		t.Fatal("missing naming signals/provenance")
	}
	v, err = s.NameAsset(v.ID, poster.ID, "Workshop poster — 5 Nov", "Printed event poster", "NORTH CAMPUS", 1200, 1600)
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	v, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if !v.Items[0].NamedOnDevice && !v.Items[1].NamedOnDevice {
		t.Fatal("local naming not retained")
	}
	_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id='poster'", *now)
	testkit.Must(t, err)
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if v.Cover != nil {
		t.Fatal("deleted cover exposed")
	}
	cards, err := s.List(nil, "all", 100)
	testkit.Must(t, err)
	if cards[0].Cover != nil {
		t.Fatal("deleted cover in Home")
	}
}
