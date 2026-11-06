package perf_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/api"
	"limmr/internal/dbutil"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// Exact N1 workload, entirely invented; HTTP handlers run without a listener.
func TestN1SpeedRoutes(t *testing.T) {
	h, p, link := testkit.Open(t)
	settings := h.Settings()
	settings.Owner = "Alex Example"
	testkit.Must(t, h.SetSettings(settings))
	phone := func(i int) string { return fmt.Sprintf("1555%07d@s.whatsapp.net", i) }
	for i := 0; i < 400; i++ {
		testkit.Must(t, h.Repo.Put("people", fmt.Sprint(i), model.Person{ID: fmt.Sprint(i), Name: fmt.Sprintf("Maya Example %03d", i), JIDs: []string{phone(i)}}))
	}
	tx, e := h.Messages.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 300; i++ {
		_, e = tx.Exec("INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?)", phone(i), fmt.Sprintf("Design chat %03d", i), testkit.Epoch)
		testkit.Must(t, e)
	}
	for i := 0; i < 15000; i++ {
		_, e = tx.Exec("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me,media_type,preview_thumbnail) VALUES(?,?,?,?,?,?,?,?)", fmt.Sprint(i), phone(i%300), phone(i%400), "Invented trip planning", testkit.Epoch.Add(-time.Duration(i)*time.Second), false, "image", []byte("synthetic-thumbnail"))
		testkit.Must(t, e)
	}
	testkit.Must(t, tx.Commit())
	for i := 0; i < 40; i++ {
		v := tasks.Task{ID: fmt.Sprintf("task-%02d", i), Title: "Prepare workshop", Status: "active", Kind: "long", CreatedAt: testkit.Epoch, UpdatedAt: testkit.Epoch, Chats: []tasks.Entity{{ID: phone(i), Name: "Design chat"}}, Items: []tasks.Item{{ID: "image", Kind: "file", Title: "Workshop poster.jpg", MediaType: "image", MessageRef: &tasks.Ref{ChatJID: phone(i), MessageID: fmt.Sprint(i)}}}}
		_, e = h.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", v.ID, dbutil.JSON(v))
		testkit.Must(t, e)
	}
	tx, e = h.Repo.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 2000; i++ {
		v := model.Activity{AgentID: "example-ai", Tool: "list_messages", Kind: "read", ChatJIDs: []string{phone(i % 300)}, At: testkit.Epoch.Add(time.Duration(i) * time.Minute), Count: i % 3}
		_, e = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", v.At, dbutil.JSON(v))
		testkit.Must(t, e)
	}
	testkit.Must(t, tx.Commit())
	mc := &mcpserver.Service{Hub: h, People: p, Link: link}
	s := &api.Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18800, ControlToken: "invented-control"}
	handler := s.Handler()
	for _, path := range []string{"/v1/home", "/v1/tasks?status=all&limit=500", "/v1/tasks/task-00", "/v1/tasks/task-00/items/image/preview", "/v1/activity?limit=50", "/v1/activity?limit=50&agent=missing", "/v1/map"} {
		samples := []float64{}
		size := 0
		for i := 0; i < 21; i++ {
			req := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
			req.Header.Set("Authorization", "Bearer invented-control")
			w := httptest.NewRecorder()
			start := time.Now()
			handler.ServeHTTP(w, req)
			elapsed := float64(time.Since(start)) / float64(time.Millisecond)
			if w.Code != 200 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
			if !json.Valid(w.Body.Bytes()) {
				t.Fatal("invalid JSON")
			}
			if i == 0 {
				t.Logf("%s cold_ms=%.3f", path, elapsed)
			} else {
				samples = append(samples, elapsed)
			}
			size = w.Body.Len()
		}
		sort.Float64s(samples)
		t.Logf("%s p50_ms=%.3f p95_ms=%.3f bytes=%d", path, samples[9], samples[18], size)
	}
}
