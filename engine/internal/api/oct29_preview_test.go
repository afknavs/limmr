package api

import (
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOct29PreviewNeverDownloadsAndHonorsHiddenDeletedSources(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	h.Now = func() time.Time { return testkit.Epoch }
	d := &taskMediaDownloader{}
	mc := &mcpserver.Service{Hub: h, People: p, Link: link, Downloader: d}
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "poster-preview", ChatJID: testkit.Group, MediaType: "image", Filename: "Poster.jpg", Timestamp: testkit.Epoch}))
	_, err := h.Messages.DB.Exec("UPDATE messages SET preview_thumbnail=? WHERE id='poster-preview'", []byte("synthetic-thumbnail"))
	testkit.Must(t, err)
	task, err := mc.TaskService().Create(nil, tasks.Create{Title: "Workshop", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	server := &Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18800, ControlToken: "fixture-control", StatusToken: "fixture-status"}
	path := "/v1/tasks/" + task.ID + "/items/" + task.Items[0].ID + "/preview"
	request := func() int {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer fixture-control")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		return w.Code
	}
	if request() != 200 || d.calls != 0 {
		t.Fatal("preview tried to download")
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Group}
	testkit.Must(t, h.SetSettings(settings))
	if request() != 404 || d.calls != 0 {
		t.Fatal("hidden thumbnail leaked")
	}
	settings.HiddenChats = nil
	testkit.Must(t, h.SetSettings(settings))
	_, err = h.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id='poster-preview'", testkit.Epoch)
	testkit.Must(t, err)
	if request() != 404 || d.calls != 0 {
		t.Fatal("deleted thumbnail leaked")
	}
}
