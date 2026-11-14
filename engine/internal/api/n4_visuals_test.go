package api

import (
	"encoding/json"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"net/http/httptest"
	"testing"
	"time"
)

func TestN4ReadModelsMetadataAndAuthorization(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	mcp := &mcpserver.Service{Hub: h, People: p, Link: link}
	handler := (&Server{Hub: h, People: p, MCP: mcp, Link: link, Port: 18800, ControlToken: "n4-control", StatusToken: "n4-status"}).Handler()
	request := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	id := model.PersonID(testkit.Phone)
	alias := "15550100001@lid"
	testkit.Must(t, p.PutPerson(model.Person{ID: id, JIDs: []string{testkit.Phone, alias}, Name: "Maya Rao"}))
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: alias, Name: "Maya Rao"}))
	now := testkit.Epoch
	entries := []model.Message{
		{ID: "current", ChatJID: testkit.Phone, Timestamp: now.Add(-time.Hour), MediaType: "image", Filename: "Poster.jpg"},
		{ID: "prior-week", ChatJID: alias, Timestamp: now.AddDate(0, 0, -7), MediaType: "document", Filename: "Survey.pdf"},
		{ID: "old-file", ChatJID: testkit.Phone, Timestamp: now.AddDate(0, 0, -150), MediaType: "image", Filename: "Old.jpg"},
		{ID: "deleted", ChatJID: testkit.Phone, Timestamp: now.Add(-2 * time.Hour), MediaType: "image", Filename: "Deleted.jpg"},
		{ID: "future", ChatJID: testkit.Phone, Timestamp: now.Add(time.Hour), MediaType: "image", Filename: "Future.jpg"},
		{ID: "group-only", ChatJID: testkit.Group, Sender: testkit.Phone, Timestamp: now.Add(-time.Hour), MediaType: "image", Filename: "Group.jpg"},
	}
	for _, m := range entries {
		testkit.Must(t, h.Messages.PutMessage(m))
	}
	_, e := h.Messages.DB.Exec(`UPDATE messages SET deleted_at=? WHERE id='deleted'`, now)
	testkit.Must(t, e)
	_, e = h.Messages.DB.Exec(`UPDATE messages SET preview_thumbnail=? WHERE id='current'`, []byte{1, 2, 3})
	testkit.Must(t, e)
	_, credential, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	path := "/v1/people/" + id + "/conversation-history"
	for _, token := range []string{"n4-status", credential} {
		if w := request(path, token); w.Code != 403 {
			t.Fatalf("non-admin history %d", w.Code)
		}
		if w := request("/v1/map/mesh", token); w.Code != 403 {
			t.Fatalf("non-admin map %d", w.Code)
		}
	}
	var beforeHub, beforeMessages int64
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT value FROM p1_revision WHERE id=1`).Scan(&beforeHub))
	testkit.Must(t, h.Messages.DB.QueryRow(`SELECT value FROM p1_revision WHERE id=1`).Scan(&beforeMessages))
	w := request(path, "n4-control")
	if w.Code != 200 {
		t.Fatalf("history %d %s", w.Code, w.Body.String())
	}
	var history people.ConversationHistory
	testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &history))
	sum := 0
	for _, week := range history.Weeks {
		sum += week.Messages
	}
	if len(history.Weeks) != 12 || sum != 2 || history.SharedFilesTotal != 3 || len(history.SharedFiles) != 3 || len(history.SharedFiles[0].Thumbnail) != 3 {
		t.Fatalf("wrong history %+v", history)
	}
	var afterHub, afterMessages int64
	testkit.Must(t, h.Repo.DB.QueryRow(`SELECT value FROM p1_revision WHERE id=1`).Scan(&afterHub))
	testkit.Must(t, h.Messages.DB.QueryRow(`SELECT value FROM p1_revision WHERE id=1`).Scan(&afterMessages))
	if beforeHub != afterHub || beforeMessages != afterMessages {
		t.Fatal("read model changed a store")
	}
	var settings model.Settings
	testkit.Must(t, h.Repo.Get("settings", "main", &settings))
	settings.HiddenChats = []string{alias}
	testkit.Must(t, h.Repo.Put("settings", "main", settings))
	value, e := p.ConversationHistory(nil, id)
	testkit.Must(t, e)
	sum = 0
	for _, week := range value.Weeks {
		sum += week.Messages
	}
	if sum != 1 || value.SharedFilesTotal != 2 {
		t.Fatal("hidden alias leaked into history")
	}
}
func TestN4CompleteMapKeepsPeopleGroupsAndTaskExpiry(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	changes := []hub.Mutation{}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("n4-%03d", i)
		jid := fmt.Sprintf("1555%07d@s.whatsapp.net", i)
		changes = append(changes, hub.Mutation{Bucket: "people", ID: id, Value: model.Person{ID: id, JIDs: []string{jid}, Name: fmt.Sprintf("Invented peer %d", i)}})
		changes = append(changes, hub.Mutation{Bucket: "memberships", ID: id, Value: model.Membership{Person: id, Group: testkit.Group}})
		testkit.Must(t, h.Messages.PutChat(model.Chat{JID: jid, Name: "Invented chat"}))
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: id, ChatJID: jid, Sender: jid, Timestamp: testkit.Epoch.Add(-time.Hour), Content: "Invented hello"}))
	}
	testkit.Must(t, h.Repo.Apply(changes...))
	mcp := &mcpserver.Service{Hub: h, People: p, Link: link}
	handler := (&Server{Hub: h, People: p, MCP: mcp, Link: link, Port: 18800, ControlToken: "n4-control"}).Handler()
	svc := mcp.TaskService()
	task, e := svc.Create(nil, tasks.Create{Title: "Review survey", Goal: "Survey approved", Kind: "long", People: []tasks.Entity{{ID: "n4-001", Name: "Invented peer 1"}}})
	testkit.Must(t, e)
	_, e = svc.NeedsYou(nil, task.ID, "Check the questions")
	testkit.Must(t, e)
	fetch := func(path string) circles.UnderstandingMap {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer n4-control")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %d %s", path, w.Code, w.Body.String())
		}
		var out circles.UnderstandingMap
		testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	bounded := fetch("/v1/map")
	if len(bounded.Nodes) > 150 {
		t.Fatal("default budget changed")
	}
	complete := fetch("/v1/map/mesh")
	count := 0
	found := false
	for _, n := range complete.Nodes {
		if n.Kind == "person" {
			count++
		}
		if n.ID == "n4-001" {
			found = true
			if len(n.SharedGroupIDs) != 1 || n.NeedsYou == nil || !*n.NeedsYou || n.Messages30 == nil || *n.Messages30 != 1 || n.Closeness == nil || *n.Closeness <= 0 || n.NewThisWeek == nil || !*n.NewThisWeek || n.LastContact == nil {
				t.Fatalf("missing projection fields %+v", n)
			}
		}
	}
	if count < 500 || !found {
		t.Fatalf("map lost people: %d", count)
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(model.NeedsYouTTL) }
	expired := fetch("/v1/map/mesh")
	for _, n := range expired.Nodes {
		if n.ID == "n4-001" && n.NeedsYou != nil && *n.NeedsYou {
			t.Fatal("expired task still needs you")
		}
	}
}

// The additive app route must not change the existing map wire cursor.
func TestN4DefaultMapCursorRemainsCompatible(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	svc := &circles.Service{Hub: h, People: p}
	_ = link
	var hubRevision, messageRevision int64
	testkit.Must(t, h.Repo.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&hubRevision))
	testkit.Must(t, h.Messages.DB.QueryRow("SELECT value FROM p1_revision WHERE id=1").Scan(&messageRevision))
	value, e := svc.Map()
	testkit.Must(t, e)
	raw, e := json.Marshal(value)
	testkit.Must(t, e)
	var before circles.UnderstandingMap
	testkit.Must(t, json.Unmarshal(raw, &before))
	expected := "p1." + dbutil.Digest(fmt.Sprintf("%d/%d/%d/", hubRevision, messageRevision, h.Now().Unix()/60))[:16]
	if before.Cursor != expected {
		t.Fatalf("default cursor changed: %s", before.Cursor)
	}
	_, e = svc.MapMesh()
	testkit.Must(t, e)
	value, e = svc.Map()
	testkit.Must(t, e)
	after, e := json.Marshal(value)
	testkit.Must(t, e)
	if string(raw) != string(after) {
		t.Fatal("mesh read changed bounded map")
	}
}
