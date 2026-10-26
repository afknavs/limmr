package remote

import (
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMediaLinksSingleUsePrefixExpiryAndRevocation(t *testing.T) {
	for _, mode := range []string{"single-use", "expired", "revoked", "scope", "hidden", "disabled", "tampered", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			h, p, _ := testkit.Open(t)
			testkit.Seed(t, h, p)
			s := New(h, &mcpserver.Service{Hub: h, People: p}, 18801)
			s.Now = func() time.Time { return testkit.Epoch }
			s.config = Config{Enabled: true, PublicURL: "https://limmr.example/limmr"}
			a, _, e := h.CreateAgent("Remote AI", "chatgpt", "remote")
			testkit.Must(t, e)
			root := filepath.Join(h.Home, "media")
			testkit.Must(t, os.MkdirAll(root, 0700))
			file := filepath.Join(root, "sample.txt")
			testkit.Must(t, os.WriteFile(file, []byte("invented file"), 0600))
			link, e := s.MediaURL(a, file, testkit.Phone, "message")
			testkit.Must(t, e)
			if !strings.HasPrefix(link, "https://limmr.example/limmr/media/") {
				t.Fatal("prefix missing")
			}
			u, e := url.Parse(link)
			testkit.Must(t, e)
			path := strings.TrimPrefix(u.Path, "/limmr")
			switch mode {
			case "expired":
				s.Now = func() time.Time { return testkit.Epoch.Add(15 * time.Minute) }
			case "revoked":
				a.Permissions.DownloadMedia = false
				testkit.Must(t, h.UpdateAgent(a))
			case "scope":
				a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
				testkit.Must(t, h.UpdateAgent(a))
			case "hidden":
				settings := h.Settings()
				settings.HiddenChats = []string{testkit.Phone}
				testkit.Must(t, h.SetSettings(settings))
			case "disabled":
				s.config.Enabled = false
			case "tampered":
				path += "x"
			case "symlink":
				testkit.Must(t, os.Remove(file))
				outside := filepath.Join(h.Home, "outside.txt")
				testkit.Must(t, os.WriteFile(outside, []byte("not media"), 0600))
				testkit.Must(t, os.Symlink(outside, file))
			}
			request := func() *httptest.ResponseRecorder {
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "https://limmr.example"+path, nil))
				return w
			}
			w := request()
			if mode == "single-use" {
				if w.Code != 200 || w.Body.String() != "invented file" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("media not served")
				}
				if request().Code != 404 {
					t.Fatal("link reused")
				}
			} else if w.Code != 404 {
				t.Fatalf("invalid grant served: %d", w.Code)
			}
		})
	}
}

func TestMediaLinkRootAndBound(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	s := New(h, &mcpserver.Service{Hub: h, People: p}, 18801)
	s.config = Config{Enabled: true, PublicURL: "https://limmr.example"}
	s.Now = func() time.Time { return testkit.Epoch }
	a, _, e := h.CreateAgent("Remote AI", "chatgpt", "remote")
	testkit.Must(t, e)
	root := filepath.Join(h.Home, "media")
	testkit.Must(t, os.MkdirAll(root, 0700))
	outside := filepath.Join(h.Home, "outside")
	testkit.Must(t, os.WriteFile(outside, []byte("x"), 0600))
	if _, e = s.MediaURL(a, outside, testkit.Phone, "x"); e == nil {
		t.Fatal("outside media exposed")
	}
	path := filepath.Join(root, "x")
	testkit.Must(t, os.WriteFile(path, []byte("x"), 0600))
	for i := 0; i < 256; i++ {
		_, e = s.MediaURL(a, path, testkit.Phone, "x")
		testkit.Must(t, e)
	}
	if _, e = s.MediaURL(a, path, testkit.Phone, "x"); e != model.ErrConflict {
		t.Fatal("grant budget ignored")
	}
	s.Now = func() time.Time { return testkit.Epoch.Add(16 * time.Minute) }
	_, e = s.MediaURL(a, path, testkit.Phone, "x")
	testkit.Must(t, e)
}
