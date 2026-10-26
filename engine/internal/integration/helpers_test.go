package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"limmr/internal/api"
	"limmr/internal/dbutil"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/wa"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const chatA = "15550100001@s.whatsapp.net"
const chatB = "15550100002@s.whatsapp.net"
const groupA = "120363000001@g.us"
const groupB = "120363000002@g.us"

var epoch = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

type rig struct {
	home  string
	h     *hub.Service
	p     *people.Service
	m     *wa.Store
	mc    *mcpserver.Service
	link  *wa.Link
	fake  *wa.Fake
	a     model.Agent
	token string
	api   *api.Server
}

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func newRig(t *testing.T) *rig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("LIMMR_HOME", home)
	repo, e := hub.Open(filepath.Join(home, "hub.db"))
	must(t, e)
	t.Cleanup(func() { repo.Close() })
	messages, e := wa.OpenStore(filepath.Join(home, "messages.db"))
	must(t, e)
	t.Cleanup(func() { messages.Close() })
	h := hub.New(repo, messages, home)
	h.Now = func() time.Time { return epoch }
	p := &people.Service{Hub: h}
	fake := &wa.Fake{}
	link := wa.NewLink(fake)
	h.Messenger = fake
	mc := &mcpserver.Service{Hub: h, People: p}
	a, token, e := h.CreateAgent("Example AI", "custom", "local")
	must(t, e)
	r := &rig{home: home, h: h, p: p, m: messages, mc: mc, link: link, fake: fake, a: a, token: token}
	for i, c := range []struct{ jid, name string }{{chatA, "Maya Rao"}, {chatB, "Alex Chen"}, {groupA, "Design Club"}, {groupB, "Garden Club"}} {
		at := epoch.Add(-time.Duration(i) * time.Hour)
		must(t, messages.PutChat(model.Chat{JID: c.jid, Name: c.name, LastMessageTime: &at}))
		for j := 0; j < 3; j++ {
			// A varied human conversation, rather than a majority repeated bot template.
			text := []string{"CANARY_MESSAGE Saturday sketches", "CANARY_MESSAGE Saturday sketches composition", "CANARY_MESSAGE Saturday sketches shading"}[j]
			must(t, messages.PutMessage(model.Message{ID: dbutil.Digest(c.jid)[:8] + string(rune('a'+j)), ChatJID: c.jid, IsFromMe: i < 2 && j == 0, Sender: chatA, Content: text, Timestamp: at.Add(-time.Duration(j) * time.Minute)}))
		}
	}
	must(t, p.PutPerson(model.Person{ID: "maya", JIDs: []string{chatA}, Name: "Maya Rao", UpdatedAt: epoch}))
	must(t, p.PutPerson(model.Person{ID: "alex", JIDs: []string{chatB}, Name: "Alex Chen", UpdatedAt: epoch}))
	must(t, p.PutGroup(model.Group{JID: groupA, Name: "Design Club", MemberCount: 1}))
	must(t, p.PutGroup(model.Group{JID: groupB, Name: "Garden Club", MemberCount: 1}))
	must(t, p.PutMembership(model.Membership{Group: groupA, Person: "maya"}))
	must(t, p.PutMembership(model.Membership{Group: groupB, Person: "alex"}))
	hs := &health.Service{Hub: h, Link: link, Home: home}
	r.api = &api.Server{Hub: h, People: p, MCP: mc, Link: link, Health: hs, Port: 18800, ControlToken: "control-" + dbutil.ID() + dbutil.ID(), StatusToken: "status-" + dbutil.ID() + dbutil.ID()}
	return r
}
func (r *rig) agent(t *testing.T, p model.Permissions) {
	t.Helper()
	r.a.Permissions = p
	must(t, r.h.UpdateAgent(r.a))
}
func (r *rig) call(t *testing.T, name string, raw any) (any, error) {
	t.Helper()
	b, e := json.Marshal(raw)
	must(t, e)
	a, e := mcpserver.DecodeArgs(b)
	must(t, e)
	return r.mc.Call(context.Background(), r.a.ID, name, a)
}
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func (r *rig) request(t *testing.T, method, path, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:18800"+path, bytes.NewReader(data))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	r.api.Handler().ServeHTTP(w, req)
	return w
}
func status(t *testing.T, w *httptest.ResponseRecorder, n int) {
	t.Helper()
	if w.Code != n {
		t.Fatalf("HTTP %d, want %d", w.Code, n)
	}
}
func firstMessage(t *testing.T, r *rig, chat string) model.Message {
	t.Helper()
	xs, e := r.m.Messages(wa.Query{Chat: chat, Limit: 1}, []string{chat})
	must(t, e)
	if len(xs) != 1 {
		t.Fatal("missing fixture")
	}
	return xs[0]
}
