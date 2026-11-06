package api

import (
	"context"
	"encoding/json"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type taskMediaDownloader struct {
	path  string
	calls int
}

func (d *taskMediaDownloader) Download(context.Context, string, string) (string, error) {
	d.calls++
	return d.path, nil
}

func TestLane2TaskMediaUsesDownloaderAndRechecksVisibility(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	h.Now = func() time.Time { return testkit.Epoch }
	mediaPath := filepath.Join(t.TempDir(), "synthetic.txt")
	testkit.Must(t, os.WriteFile(mediaPath, []byte("Synthetic file"), 0600))
	downloader := &taskMediaDownloader{path: mediaPath}
	mc := &mcpserver.Service{Hub: h, People: p, Link: link, Downloader: downloader}
	svc := mc.TaskService()
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "synthetic-media", ChatJID: testkit.Group, MediaType: "document", Filename: "Workshop.txt", Timestamp: testkit.Epoch.Add(time.Minute)}))
	task, err := svc.Create(nil, tasks.Create{Title: "Workshop", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	if len(task.Items) != 1 {
		t.Fatal("media not surfaced")
	}
	server := &Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, nil)
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer fixture-control")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w
	}
	path := "/v1/tasks/" + task.ID + "/items/" + task.Items[0].ID + "/download"
	w := request(path)
	if w.Code != 200 || downloader.calls != 1 {
		t.Fatalf("media download status=%d calls=%d", w.Code, downloader.calls)
	}
	var got struct {
		Path string `json:"path"`
	}
	testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &got))
	if got.Path != mediaPath {
		t.Fatal("download path mismatch")
	}
	set := h.Settings()
	set.HiddenChats = []string{testkit.Group}
	testkit.Must(t, h.SetSettings(set))
	if request(path).Code != 404 || downloader.calls != 1 {
		t.Fatal("hidden media downloaded")
	}
	set.HiddenChats = nil
	testkit.Must(t, h.SetSettings(set))
	_, err = h.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id='synthetic-media'", testkit.Epoch)
	testkit.Must(t, err)
	if request(path).Code != 404 || downloader.calls != 1 {
		t.Fatal("deleted media downloaded")
	}
}
