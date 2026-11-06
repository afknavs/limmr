package attachments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLocalBoundaries(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(home, "poster.pdf")
	write(file, "%PDF-invented")
	data, name, err := ReadLocal("~/poster.pdf", home, filepath.Join(home, "limmr-data"))
	if err != nil || string(data) != "%PDF-invented" || name != "poster.pdf" {
		t.Fatalf("read: %q %q %v", data, name, err)
	}
	write(file, "changed")
	if string(data) != "%PDF-invented" {
		t.Fatal("read did not preserve bytes")
	}
	write(filepath.Join(home, ".ssh", "id_rsa"), "invented")
	write(filepath.Join(home, "limmr-data", "data"), "invented")
	write(filepath.Join(outside, "poster.pdf"), "invented")
	if err := os.Symlink(outside, filepath.Join(home, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(home, ".ssh", "id_rsa"), filepath.Join(home, "limmr-data", "data"), filepath.Join(home, "escape", "poster.pdf"), filepath.Join(outside, "poster.pdf"), filepath.Join(home, "api-token.txt"), "relative"} {
		if _, _, err := ReadLocal(path, home, filepath.Join(home, "limmr-data")); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
	large := filepath.Join(home, "large.pdf")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFetch + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, _, err := ReadLocal(large, home, ""); err == nil || !strings.Contains(err.Error(), "100 MiB") {
		t.Fatalf("oversize: %v", err)
	}
}
