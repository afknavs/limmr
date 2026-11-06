package tasks

import (
	"context"
	"image"
	"image/png"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func coverOriginal(t *testing.T, s *Service, chat, id string, w, h int) {
	t.Helper()
	dir := filepath.Join(s.Hub.Home, "media")
	testkit.Must(t, os.MkdirAll(dir, 0700))
	f, err := os.Create(filepath.Join(dir, dbutil.Digest(chat+"\x00"+id)))
	testkit.Must(t, err)
	testkit.Must(t, png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))))
	testkit.Must(t, f.Close())
}
func TestN1CoverProtocol(t *testing.T) {
	for _, c := range []struct {
		name, chat, caption string
		w, h                int
		download, want      bool
	}{
		{"meme in DM", testkit.Phone, "When you finish exam preparation lol", 1200, 1600, true, false},
		{"matching group poster", testkit.Group, "Exam preparation workshop poster for Friday", 1200, 1600, true, true},
		{"unrelated group poster", testkit.Group, "Cinema club poster for Friday", 1200, 1600, true, false},
		{"matching DM poster", testkit.Phone, "Exam preparation workshop poster for Friday", 1200, 1600, true, true},
		{"thumbnail", testkit.Group, "Exam preparation workshop poster for Friday", 240, 240, true, false},
		{"undownloaded original", testkit.Group, "Exam preparation workshop poster for Friday", 1200, 1600, false, false},
		{"emoji caption", testkit.Group, "😂", 1200, 1600, true, false},
		{"chat screenshot", testkit.Group, "Exam preparation chat screenshot for Friday", 1200, 1600, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, a, now := fixture(t)
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "cover", ChatJID: c.chat, Sender: testkit.Phone, MediaType: "image", Filename: "poster.png", Content: c.caption, Timestamp: now.Add(-time.Hour)}))
			_, err := s.Hub.Messages.DB.Exec("UPDATE messages SET preview_width=1600,preview_height=1600 WHERE id='cover'")
			testkit.Must(t, err)
			if c.download {
				coverOriginal(t, s, c.chat, "cover", c.w, c.h)
			}
			v, err := s.Create(a, Create{Title: "Exam preparation", Kind: "long", Chats: []Entity{{ID: c.chat}}})
			testkit.Must(t, err)
			if (v.Cover != nil) != c.want {
				t.Fatalf("cover decision=%v want %v", v.Cover != nil, c.want)
			}
		})
	}
}
func TestN1CoverUpgradeReevaluatesOnceAndKeepsOwnerChoice(t *testing.T) {
	s, a, now := fixture(t)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "meme", ChatJID: testkit.Phone, MediaType: "image", Filename: "meme.png", Content: "When you finish exam preparation lol", Timestamp: *now}))
	coverOriginal(t, s, testkit.Phone, "meme", 1200, 1600)
	v, err := s.Create(a, Create{Title: "Exam preparation", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	item := v.Items[0]
	// Model an earlier app's automatic choice, then run the real upgrade migration.
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.cover',json(?)) WHERE id=?", dbutil.JSON(item), v.ID)
	testkit.Must(t, err)
	owner, err := s.Create(a, Create{Title: "Owner pick", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.cover',json(?),'$.cover_item_id',?) WHERE id=?", dbutil.JSON(item), owner.Items[0].ID, owner.ID)
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("DELETE FROM hub_schema WHERE version=17")
	testkit.Must(t, err)
	testkit.Must(t, s.Hub.Repo.Migrate(nil))
	testkit.Must(t, s.ReevaluateCovers(context.Background()))
	testkit.Must(t, s.ReevaluateCovers(context.Background()))
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if v.Cover != nil {
		t.Fatal("unsafe auto cover survived upgrade")
	}
	owner, err = s.Get(nil, owner.ID)
	testkit.Must(t, err)
	if owner.Cover == nil || owner.CoverItemID == "" {
		t.Fatal("owner choice lost")
	}
	var n int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM task_events WHERE kind='cover_reviewed'").Scan(&n))
	if n != 1 {
		t.Fatal("cover review repeated", n)
	}
}
