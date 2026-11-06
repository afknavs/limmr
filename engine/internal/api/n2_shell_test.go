package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestN2TasksAndHomeReturnSameSafeCoverCandidate(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	mc := &mcpserver.Service{Hub: h, People: p, Link: link}
	task, err := mc.TaskService().Create(nil, tasks.Create{Title: "Invented photo task", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "new-photo", ChatJID: testkit.Phone, MediaType: "image", Filename: "Campus.png", Content: "N2 PRIVATE BODY", Timestamp: testkit.Epoch}))
	var pixels bytes.Buffer
	testkit.Must(t, png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 80, 100))))
	_, err = h.Messages.DB.Exec("UPDATE messages SET preview_thumbnail=? WHERE id='new-photo'", pixels.Bytes())
	testkit.Must(t, err)
	server := &Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18888, ControlToken: "invented-n2-control"}
	handler := server.Handler()
	get := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://127.0.0.1:18888"+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	var candidate *tasks.CoverCandidate
	for _, path := range []string{"/v1/tasks?status=all&limit=500", "/v1/home"} {
		response := get(path, "invented-n2-control")
		if response.Code != 200 {
			t.Fatalf("%s status %d", path, response.Code)
		}
		var payload struct {
			Tasks []tasks.TaskSummary `json:"tasks"`
		}
		testkit.Must(t, json.Unmarshal(response.Body.Bytes(), &payload))
		found := false
		for _, card := range payload.Tasks {
			if card.ID == task.ID {
				found = true
				if card.CoverCandidate == nil || card.CoverCandidate.MediaID != "new-photo" {
					t.Fatal("missing candidate", path)
				}
				if candidate != nil && *candidate != *card.CoverCandidate {
					t.Fatal("Home/list candidate differs")
				}
				candidate = card.CoverCandidate
			}
		}
		if !found {
			t.Fatal("task absent", path)
		}
		if strings.Contains(response.Body.String(), "N2 PRIVATE BODY") {
			t.Fatal("message body leaked")
		}
	}
	if get(candidate.ThumbnailURL, "").Code != 401 {
		t.Fatal("preview did not require admin")
	}
	response := get(candidate.ThumbnailURL, "invented-n2-control")
	var preview struct{ Path string }
	testkit.Must(t, json.Unmarshal(response.Body.Bytes(), &preview))
	_, statErr := os.Stat(preview.Path)
	if response.Code != 200 || statErr != nil || !strings.HasSuffix(preview.Path, ".cover-1280.jpg") {
		t.Fatal("cached thumbnail absent")
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, h.SetSettings(settings))
	if get(candidate.ThumbnailURL, "invented-n2-control").Code != 404 {
		t.Fatal("hidden source preview still available")
	}
}
