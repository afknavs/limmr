package tasks

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN2CoverCandidatesUseMetadataAndPriority(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	// Add after creation so this image is not scanned into the workspace.
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new-media", ChatJID: testkit.Phone, MediaType: "image", Filename: "Walk.jpg", Content: "PRIVATE MESSAGE BODY", Timestamp: now.Add(time.Minute)}))
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "older-media", ChatJID: testkit.Phone, MediaType: "image", Filename: "Earlier.jpg", Timestamp: *now}))
	_, err := s.Hub.Messages.DB.Exec("UPDATE messages SET preview_thumbnail=? WHERE id='new-media'", coverTestPNG(t, 80, 100))
	testkit.Must(t, err)
	items := []Item{
		{ID: "link", Kind: "link", Title: "Guide", URL: "https://example.invalid/guide", Text: "PRIVATE WORKSPACE BODY"},
		{ID: "first", AttachmentID: "first", Kind: "file", Title: "First.png", MediaType: "image", Text: "PRIVATE WORKSPACE BODY", Caption: "PRIVATE CAPTION", OCRText: "PRIVATE OCR"},
		{ID: "chosen", Kind: "file", Title: "Chosen.png", MediaType: "image"},
	}
	set := func(items []Item, chosen string, removed bool) {
		_, e := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?),'$.cover_item_id',?,'$.cover_removed',json(?)) WHERE id=?", dbutil.JSON(items), chosen, dbutil.JSON(removed), v.ID)
		testkit.Must(t, e)
	}
	get := func() *CoverCandidate {
		cards, e := s.List(nil, "all", 100)
		testkit.Must(t, e)
		for _, card := range cards {
			if card.ID == v.ID {
				raw, _ := json.Marshal(card)
				if strings.Contains(string(raw), "PRIVATE") {
					t.Fatal("body leaked into cards")
				}
				return card.CoverCandidate
			}
		}
		t.Fatal("task missing")
		return nil
	}
	testkit.Must(t, s.Hub.Repo.Put("attachments", "first", model.Attachment{ID: "first"}))
	testkit.Must(t, os.MkdirAll(filepath.Join(s.Hub.Home, "outbox"), 0700))
	testkit.Must(t, os.WriteFile(filepath.Join(s.Hub.Home, "outbox", "first"), coverTestPNG(t, 80, 100), 0600))
	coverOriginal(t, s, testkit.Phone, "older-media", 80, 100)
	set(items, "chosen", false)
	if c := get(); c == nil || c.ItemID != "chosen" || !strings.HasPrefix(c.ThumbnailURL, "/v1/tasks/") {
		t.Fatalf("owner choice: %+v", c)
	}
	set(items, "", false)
	if c := get(); c == nil || c.ItemID != "first" {
		t.Fatalf("first workspace image: %+v", c)
	}
	set(items[:1], "", false)
	if c := get(); c == nil || c.MediaID != "older-media" {
		t.Fatalf("file preferred to newer embedded thumbnail: %+v", c)
	}
	preview, e := s.CoverPreview(nil, v.ID)
	testkit.Must(t, e)
	if !regularFile(preview["path"].(string)) {
		t.Fatal("downloaded thumbnail missing")
	}
	testkit.Must(t, os.Remove(s.coverMediaPath(&CoverCandidate{ChatJID: testkit.Phone, MediaID: "older-media"})))
	if c := get(); c == nil || c.MediaID != "new-media" {
		t.Fatal("thumbnail-only fallback missing", c)
	}
	coverOriginal(t, s, testkit.Phone, "older-media", 80, 100)
	_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id='new-media'", *now)
	testkit.Must(t, err)
	if c := get(); c == nil || c.MediaID != "older-media" {
		t.Fatalf("deleted source retained: %+v", c)
	}
	_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE media_type='image'", *now)
	testkit.Must(t, err)
	if c := get(); c == nil || c.ItemID != "link" || c.LinkURL == "" {
		t.Fatalf("link fallback: %+v", c)
	}
	set(nil, "", false)
	if c := get(); c != nil {
		t.Fatalf("expected typographic fallback: %+v", c)
	}
	set(items, "chosen", true)
	if c := get(); c != nil {
		t.Fatal("owner removal ignored")
	}
}

func TestN2CoverSourceVisibilityRechecked(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "photo", ChatJID: testkit.Phone, MediaType: "image", Timestamp: *now}))
	coverOriginal(t, s, testkit.Phone, "photo", 80, 100)
	c := s.coverCandidate(nil, v)
	if c == nil {
		t.Fatal("photo missing")
	}
	a.Permissions.Chats.Mode = "only"
	a.Permissions.Chats.JIDs = []string{testkit.Group}
	if s.coverCandidate(a, v) != nil {
		t.Fatal("inaccessible media exposed")
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, s.Hub.SetSettings(settings))
	if s.coverCandidate(nil, v) != nil {
		t.Fatal("hidden media exposed")
	}
	if _, err := s.CoverPreview(nil, v.ID); err == nil {
		t.Fatal("hidden cached preview exposed")
	}
}

func TestN2RecentListUsesRecencyBeforeNeedsYou(t *testing.T) {
	s, a, now := fixture(t)
	older := create(t, s, a, "long")
	newer := create(t, s, a, "long")
	_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.last_active_at',?,'$.needs_you',json('true')) WHERE id=?", now.Add(-time.Hour).Format(time.RFC3339Nano), older.ID)
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.last_active_at',?,'$.needs_you',json('false')) WHERE id=?", now.Format(time.RFC3339Nano), newer.ID)
	testkit.Must(t, err)
	priority, err := s.List(nil, "all", 1)
	testkit.Must(t, err)
	recent, err := s.ListRecent(nil, "all", "", 1)
	testkit.Must(t, err)
	if len(priority) != 1 || priority[0].ID != older.ID || len(recent) != 1 || recent[0].ID != newer.ID {
		t.Fatal("startup recency and gallery priority must remain distinct")
	}
}
