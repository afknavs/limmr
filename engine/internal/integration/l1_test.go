package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestL1Rename(t *testing.T) {
	r := newRig(t)
	path := "/v1/agents/" + r.a.ID
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "PATCH", path, token, map[string]any{"name": "Renamed AI"}, nil), 403)
	}
	for _, name := range []any{"", "  ", strings.Repeat("a", 41), "line\nbreak", "tab\t", "\x00", nil, 23} {
		status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"name": name}, nil), 400)
	}
	status(t, r.request(t, "PATCH", "/v1/agents/unknown", r.api.ControlToken, map[string]any{"name": "Valid"}, nil), 404)
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"name": "  " + strings.Repeat("界", 40) + "  "}, nil), 200)
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"name": "  Renamed AI  "}, nil), 200)
	a, e := r.h.Authenticate(r.token)
	must(t, e)
	if a.ID != r.a.ID || a.Name != "Renamed AI" || a.Kind != r.a.Kind {
		t.Fatal("identity/credential changed")
	}
	for _, tool := range []string{"whoami", "check_connection"} {
		v, e := r.call(t, tool, map[string]any{})
		must(t, e)
		agent := v.(map[string]any)["agent"]
		if !strings.Contains(encode(agent), `"name":"Renamed AI"`) {
			t.Fatal("stale identity")
		}
	}
	v, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	if _, ok := v.(map[string]any)["signature_preview"]; ok {
		t.Fatal("stale signature")
	}
	n1Circle(t, r, chatA, "free", false)
	v, e = r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Example reply"})
	must(t, e)
	if v.(model.Draft).FinalText != "Example reply" {
		t.Fatal("stale send signature")
	}
}

func TestL1MessageAuthorsAndRadius(t *testing.T) {
	r := newRig(t)
	_, e := r.h.Repo.DB.Exec("UPDATE author_meta SET value=? WHERE key='since'", epoch.Add(-time.Hour).Format(time.RFC3339Nano))
	must(t, e)
	for _, m := range []model.Message{
		{ID: "owner", ChatJID: chatA, IsFromMe: true, Content: "Attribution example", Timestamp: epoch},
		{ID: "old", ChatJID: chatA, IsFromMe: true, Content: "Attribution example", Timestamp: epoch.Add(-2 * time.Hour)},
		{ID: "contact", ChatJID: chatA, Content: "Attribution example — Example AI", Timestamp: epoch.Add(-time.Minute)},
	} {
		must(t, r.m.PutMessage(m))
	}
	n1Circle(t, r, chatA, "free", true)
	r.fake.Messages = r.m
	sent := []string{}
	for _, voice := range []string{"as_agent", "as_me"} {
		v, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Attribution example", "voice": voice})
		must(t, e)
		sent = append(sent, v.(model.Draft).MessageID)
	}
	// Legacy owner-approval path also records provenance.
	ap, e := r.h.RequestSend(r.a, "message", chatA, "Attribution example", "as_agent", "", "")
	must(t, e)
	ap, e = r.h.Decide(context.Background(), ap.ID, true, nil, "")
	must(t, e)
	sent = append(sent, ap.MessageID)
	// Renames must update author display without changing the stored identity.
	status(t, r.request(t, "PATCH", "/v1/agents/"+r.a.ID, r.api.ControlToken, map[string]any{"name": "Renamed AI"}, nil), 200)
	want := map[string]string{"owner": "owner", "old": "owner_or_unknown", "contact": "contact"}
	for _, id := range sent {
		want[id] = "agent"
	}
	for _, tool := range []string{"list_messages", "search_messages"} {
		args := map[string]any{"chat_jid": chatA, "limit": 50}
		if tool == "search_messages" {
			args["query"] = "Attribution"
		}
		v, e := r.call(t, tool, args)
		must(t, e)
		seen := map[string]bool{}
		for _, m := range v.([]model.Message) {
			if typ, ok := want[m.ID]; ok {
				seen[m.ID] = true
				if m.Author.Type != typ {
					t.Fatalf("%s: %s author=%s", tool, m.ID, m.Author.Type)
				}
				if typ == "agent" && (m.Author.AgentID != r.a.ID || m.Author.AgentName != "Renamed AI") {
					t.Fatal("missing agent identity")
				}
			}
		}
		if len(seen) != len(want) {
			t.Fatal("missing attribution test messages")
		}
	}
	for _, radius := range []int{0, 1, 50} {
		v, e := r.call(t, "get_message_context", map[string]any{"chat_jid": chatA, "message_id": "owner", "radius": radius})
		must(t, e)
		ms := v.([]model.Message)
		if radius == 0 && (len(ms) != 1 || ms[0].ID != "owner" || ms[0].Author.Type != "owner") {
			t.Fatal("radius zero included neighbours")
		}
	}
	for _, radius := range []int{-1, 51} {
		_, e := r.call(t, "get_message_context", map[string]any{"chat_jid": chatA, "message_id": "owner", "radius": radius})
		if e == nil || !strings.Contains(e.Error(), "radius must be between 0 and 50") {
			t.Fatal("radius error not actionable")
		}
		if _, e := r.m.Context(chatA, "owner", radius, []string{chatA}); e == nil {
			t.Fatal("store radius accepted")
		}
	}
	cs := &circles.Service{Hub: r.h, People: r.p}
	list, e := cs.List(&r.a)
	must(t, e)
	if len(list) == 0 {
		t.Fatal("missing circle")
	}
	v, e := r.call(t, "catch_up", map[string]any{"id": list[0].ID, "since": epoch.Add(-24 * time.Hour).Format(time.RFC3339Nano)})
	must(t, e)
	for _, chat := range v.(circles.CatchUp).Chats {
		for _, rm := range chat.Messages {
			if rm.Message.Author.Type == "" {
				t.Fatal("catch_up missing author")
			}
		}
	}
	v, e = r.call(t, "scan_chats", map[string]any{})
	must(t, e)
	if !strings.Contains(encode(v), `"author":{"type":"agent"`) {
		t.Fatal("scan preview missing author")
	}
}

func TestL1GetStartedCallableSteps(t *testing.T) {
	for _, preset := range []string{"own", "given", "custom"} {
		t.Run(preset, func(t *testing.T) {
			r := newRig(t)
			p := model.Preset(preset)
			if preset == "custom" {
				p = model.Preset("own")
				p.ReadPeople = false
			}
			r.agent(t, p)
			v, e := r.call(t, "get_started", map[string]any{})
			must(t, e)
			setup := v.(circles.SetupStatus)
			if preset == "own" && len(setup.NextSteps) == 0 {
				t.Fatal("own agent lost setup steps")
			}
			for _, step := range setup.NextSteps {
				for _, name := range step.Tools {
					if !mcpserver.Permitted(p, name) {
						t.Fatalf("uncallable next step %s", name)
					}
				}
			}
			if preset != "own" && strings.Contains(encode(setup.NextSteps), "update_owner_profile") {
				t.Fatal("profile tool recommended without permission")
			}
		})
	}
}

func TestL1CancelledQueryDoesNotResumeWriting(t *testing.T) {
	r := newRig(t)
	// Hold the only DB connection: the first agent query must be cancellable.
	tx, e := r.h.Repo.DB.Begin()
	must(t, e)
	r.mc.ToolCallTimeout = 30 * time.Millisecond
	started := time.Now()
	_, e = r.call(t, "list_circles", map[string]any{})
	if e == nil || !strings.Contains(e.Error(), "query") || !strings.Contains(e.Error(), "smaller") || time.Since(started) > time.Second {
		t.Fatal("unbounded/unhelpful query timeout")
	}
	must(t, tx.Rollback())
	r.mc.ToolCallTimeout = time.Second
	_, e = r.call(t, "list_circles", map[string]any{})
	must(t, e)
	// A cancelled scoped transaction must reject subsequent writes.
	ctx, cancel := context.WithCancel(context.Background())
	h := r.h.WithContext(ctx)
	cancel()
	if err := h.Repo.Put("test", "late", map[string]string{"value": "late"}); err == nil {
		t.Fatal("write survived cancellation")
	}
	if err := r.h.Repo.Get("test", "late", &map[string]string{}); err != model.ErrNotFound {
		t.Fatal("late write committed")
	}
}

type l1SlowDownloader struct{}

func (l1SlowDownloader) Download(ctx context.Context, _, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

type l1SlowMessenger struct{}

func (l1SlowMessenger) Send(ctx context.Context, _ model.Approval) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}
func TestL1BridgeTimeout(t *testing.T) {
	r := newRig(t)
	r.mc.ToolCallTimeout = 40 * time.Millisecond
	r.mc.Downloader = l1SlowDownloader{}
	_, e := r.call(t, "download_media", map[string]any{"chat_jid": chatA, "message_id": firstMessage(t, r, chatA).ID})
	if e == nil || !strings.Contains(e.Error(), "bridge") || !strings.Contains(e.Error(), "reconnect") {
		t.Fatal("unhelpful bridge timeout")
	}
}
func TestL1EveryToolHasDeadline(t *testing.T) {
	r := newRig(t)
	for _, tool := range mcpserver.Catalog() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, e := r.mc.Call(ctx, r.a.ID, tool.Name, mcpserver.Args{})
		if e == nil || !strings.Contains(e.Error(), "deadline") {
			t.Fatal(fmt.Sprintf("%s lacks deadline", tool.Name))
		}
	}
}

func TestL1TimedOutSendKeepsAuthor(t *testing.T) {
	r := newRig(t)
	n1Circle(t, r, chatA, "free", false)
	r.h.Messenger = l1SlowMessenger{}
	r.mc.ToolCallTimeout = 100 * time.Millisecond
	_, e := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Synthetic timeout payload"})
	if e == nil || !strings.Contains(e.Error(), "send_status") {
		t.Fatal("send timeout missing recovery")
	}
	var id string
	must(t, r.h.Repo.DB.QueryRow("SELECT message_id FROM send_authors WHERE agent_id=?", r.a.ID).Scan(&id))
	m, e := r.h.Attribute(model.Message{ChatJID: chatA, ID: id, IsFromMe: true, Timestamp: time.Now()})
	must(t, e)
	if m.Author.Type != "agent" || m.Author.AgentID != r.a.ID {
		t.Fatal("lost acknowledgement lost provenance")
	}
}

func TestL1SlowCallLogIsRedacted(t *testing.T) {
	r := newRig(t)
	var output bytes.Buffer
	r.mc.Logger = log.New(&output, "", 0)
	r.mc.Downloader = l1SlowDownloader{}
	r.mc.ToolCallTimeout = 2100 * time.Millisecond
	_, e := r.call(t, "download_media", map[string]any{"chat_jid": chatA, "message_id": firstMessage(t, r, chatA).ID})
	if e == nil {
		t.Fatal("slow call succeeded")
	}
	logged := output.String()
	if !strings.Contains(logged, "tool=download_media") || !strings.Contains(logged, "duration_ms=") || !strings.Contains(logged, "part=bridge") {
		t.Fatal("missing slow request metadata")
	}
	for _, private := range []string{chatA, r.token, r.a.ID, "CANARY_MESSAGE"} {
		if strings.Contains(logged, private) {
			t.Fatal("private data in slow log")
		}
	}
}

func TestL1HTTPTimeoutIsToolError(t *testing.T) {
	r := newRig(t)
	r.mc.ToolCallTimeout = 40 * time.Millisecond
	tx, e := r.h.Repo.DB.Begin()
	must(t, e)
	defer tx.Rollback()
	req := httptest.NewRequest("POST", "http://127.0.0.1:18800/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_circles","arguments":{}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req = mcpserver.WithAgent(req, r.a)
	out := httptest.NewRecorder()
	started := time.Now()
	r.mc.Handler(false).ServeHTTP(out, req)
	if time.Since(started) > time.Second || out.Code != 200 || !strings.Contains(out.Body.String(), `"isError":true`) || !strings.Contains(out.Body.String(), "local query") || strings.Contains(out.Body.String(), "-32603") {
		t.Fatal("timeout was not an actionable MCP tool error")
	}
}
