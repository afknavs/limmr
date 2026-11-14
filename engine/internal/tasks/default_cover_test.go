package tasks

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPaperDefaultCoversReplaceCachedLegacyFiles(t *testing.T) {
	s, _, _ := fixture(t)
	dir := filepath.Join(s.Hub.Home, "media")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	seen := map[string][]byte{}
	for i := 0; len(seen) < 3 && i < 100; i++ {
		id := fmt.Sprintf("paper-fixture-%d", i)
		key := defaultCoverID(id)
		if _, ok := seen[key]; ok {
			continue
		}
		legacy := filepath.Join(dir, "task-"+strings.TrimPrefix(key, "default:")+".jpg")
		if err := os.WriteFile(legacy, []byte("old cached cover"), 0600); err != nil {
			t.Fatal(err)
		}
		path, err := s.defaultCover(id)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "task-paper-v1-"+strings.TrimPrefix(key, "default:")+".jpg" {
			t.Fatalf("unversioned cover: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 800 {
			t.Fatal("download dimensions changed")
		}
		// JPEG may round channels by one. Paper must stay warm and bright,
		// including the quiet top-left area reserved for status badges.
		for _, point := range [][2]int{{0, 0}, {160, 160}, {639, 799}} {
			r, g, b, _ := img.At(point[0], point[1]).RGBA()
			if r>>8 < 230 || g>>8 < 226 || b>>8 < 218 || r <= b {
				t.Fatal("legacy dark palette returned")
			}
		}
		info, _ := os.Stat(path)
		again, err := s.defaultCover(id)
		if err != nil || again != path {
			t.Fatal("unstable cached path", err)
		}
		after, _ := os.Stat(path)
		if !info.ModTime().Equal(after.ModTime()) {
			t.Fatal("rewrote cached Paper cover")
		}
		old, _ := os.ReadFile(legacy)
		if string(old) != "old cached cover" {
			t.Fatal("modified legacy file")
		}
		for _, previous := range seen {
			if bytes.Equal(previous, data) {
				t.Fatal("compositions are identical")
			}
		}
		seen[key] = data
	}
	if len(seen) != 3 {
		t.Fatal("did not exercise all three default IDs")
	}
}
