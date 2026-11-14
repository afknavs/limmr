package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/mcpserver"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
)

func TestTaskHistoryUndoRouteAllKinds(t *testing.T) {
	for _, kind := range []string{"attach_evidence", "owner_remove", "rule_duplicate", "rule_finished", "rule_obsolete", "rule_follows_work", "rule_protocol_demote", "rule_suggestion_expiry"} {
		t.Run(kind, func(t *testing.T) {
			h, p, link := testkit.Open(t)
			testkit.Seed(t, h, p)
			now := testkit.Epoch
			h.Now = func() time.Time { return now }
			original := tasks.Task{ID: "history-fixture", Title: "Review poster", Goal: "Review poster", Kind: "long", Status: "active", Origin: "owner", CreatedAt: now, UpdatedAt: now, LastActiveAt: now, LifespanDays: 14}
			_, err := h.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", original.ID, dbutil.JSON(original))
			testkit.Must(t, err)
			rule := ""
			if len(kind) > 5 && kind[:5] == "rule_" {
				rule = kind[5:]
			}
			ch := tasks.MembershipChange{ID: "invented-change", TaskID: original.ID, Kind: kind, Rule: rule, At: now, Text: "Changed: the work changed", Reason: "the work changed", Before: json.RawMessage(dbutil.JSON(original))}
			testkit.Must(t, h.Repo.Put("changes", ch.ID, ch))
			_, err = h.Repo.DB.Exec("INSERT INTO task_events(id,task_id,at,kind,text,payload) VALUES(?,?,?,?,?,?)", "fixture-event", original.ID, now.Format(time.RFC3339Nano), kind, ch.Text, dbutil.JSON(map[string]any{"change_id": ch.ID, "reason": ch.Reason, "history_chats": []string{}}))
			testkit.Must(t, err)
			mc := &mcpserver.Service{Hub: h, People: p, Link: link}
			server := &Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18888, ControlToken: "fixture-control", StatusToken: "fixture-status"}
			handler := server.Handler()
			request := func(method, path, token string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, nil)
				r.Host = "127.0.0.1:18888"
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			path := "/v1/tasks/" + original.ID + "/changes/" + ch.ID + "/undo"
			if w := request("POST", path, "fixture-status"); w.Code == 200 {
				t.Fatal("status credential undid task")
			}
			now = ch.At.Add(30 * 24 * time.Hour)
			if w := request("POST", path, "fixture-control"); w.Code != 409 {
				t.Fatal("expired undo", w.Code, w.Body.String())
			}
			now = now.Add(-time.Nanosecond)
			w := request("POST", path, "fixture-control")
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var got tasks.Task
			testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &got))
			if len(got.History) != 2 || !got.History[1].Undone || got.History[1].ChangeID != ch.ID || got.History[1].Reason != ch.Reason || got.History[1].UndoUntil == "" {
				t.Fatal("undo response metadata", w.Body.String())
			}
			if w := request("POST", path, "fixture-control"); w.Code != 409 {
				t.Fatal("undo reused", w.Code)
			}
			w = request("GET", "/v1/tasks/"+original.ID, "fixture-control")
			testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &got))
			if w.Code != 200 || !got.History[1].Undone {
				t.Fatal("refresh lost undo", w.Code, w.Body.String())
			}
		})
	}
}
