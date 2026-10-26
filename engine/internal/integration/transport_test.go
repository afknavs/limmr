package integration_test

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

type pipeListener struct {
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 18800} }

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copy)
}

// Exercise both SDKs and real HTTP serialization without binding a socket. This
// supplements direct handler tests in sandboxes where loopback bind is denied.
func sdkSession(t *testing.T, r *rig) (context.Context, *mcp.ClientSession, *http.Client) {
	t.Helper()
	listener := &pipeListener{incoming: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: r.api.Handler()}
	done := make(chan struct{})
	go func() { _ = server.Serve(listener); close(done) }()
	t.Cleanup(func() { server.Close(); listener.Close(); <-done })
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, conn := net.Pipe()
		select {
		case listener.incoming <- conn:
			return client, nil
		case <-ctx.Done():
			client.Close()
			conn.Close()
			return nil, ctx.Err()
		}
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: bearerTransport{base: transport, token: r.token}, Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	sdk := mcp.NewClient(&mcp.Implementation{Name: "Fixture client", Version: "1"}, nil)
	session, e := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://127.0.0.1:18800/mcp", HTTPClient: client, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	must(t, e)
	t.Cleanup(func() { session.Close() })
	return ctx, session, client
}

func TestSDKClientThroughHTTPPipe(t *testing.T) {
	r := newRig(t)
	setupWatches(t, r)
	ctx, session, client := sdkSession(t, r)
	tools, e := session.ListTools(ctx, nil)
	must(t, e)
	if len(tools.Tools) != 20 {
		t.Fatalf("want 20 tools, got %d", len(tools.Tools))
	}
	who, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
	must(t, e)
	if who.IsError {
		t.Fatal("whoami rejected")
	}
	data, ok := who.StructuredContent.(map[string]any)
	if !ok {
		t.Fatal("missing structured content")
	}
	agent := data["data"].(map[string]any)["agent"].(map[string]any)
	if agent["id"] != r.a.ID {
		t.Fatal("SDK identity mismatch")
	}
	// Exercise the complete Circles protocol through SDK client + serialized
	// HTTP while the sandbox disallows real loopback listeners.
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		must(t, err)
		if result.IsError {
			t.Fatal("SDK circle call rejected", name)
		}
		return result.StructuredContent.(map[string]any)["data"].(map[string]any)
	}
	createdWatch := call("create_watch", map[string]any{"goal": "Tell me about exam news", "grant_text": "Tell me about exam news", "scope": map[string]any{"chats": []string{chatA}}, "match": map[string]any{"kind": "any"}, "action": "notify"})
	if createdWatch["status"] != "active" {
		t.Fatal("SDK create_watch did not persist")
	}
	updatedWatch := call("update_watch", map[string]any{"id": createdWatch["id"], "instruction": "Only changed dates", "digest": "Exam date is Tuesday", "status": "paused"})
	if updatedWatch["digest"] != "Exam date is Tuesday" || updatedWatch["status"] != "paused" {
		t.Fatal("SDK update_watch failed")
	}
	if call("stop_watch", map[string]any{"id": createdWatch["id"]})["status"] != "stopped" {
		t.Fatal("SDK stop_watch failed")
	}
	resolved := call("resolve_context", map[string]any{"query": "Cafe Atlas"})
	if resolved["needs_clarification"] != false {
		t.Fatal("SDK ranking clarification")
	}
	brief := call("get_brief", map[string]any{"id": c.ID})
	if brief["brief"] != nil {
		t.Fatal("unexpected stored brief")
	}
	page := call("catch_up", map[string]any{"id": c.ID, "limit": 1})
	total := int(page["new_messages"].(float64))
	for page["next_page_token"] != nil && page["next_page_token"] != "" {
		page = call("catch_up", map[string]any{"id": c.ID, "limit": 1, "page_token": page["next_page_token"]})
		total += int(page["new_messages"].(float64))
	}
	if total != 3 {
		t.Fatal("SDK paged catch-up")
	}
	updated := call("update_brief", map[string]any{"id": c.ID, "summary": "Sketches are ready.", "evidence_message_ids": []string{firstMessage(t, r, chatA).ID}, "what_changed": "New plans.", "upcoming": []map[string]string{{"date": "2026-10-20", "text": "Review"}}, "people_notes": []map[string]string{{"person_id": "maya", "contribution": "Prepared sketches"}}})
	if updated["version"].(float64) != 1 {
		t.Fatal("SDK brief version")
	}
	call("suggest", map[string]any{"circle_id": c.ID, "kind": "reply", "chat_jid": chatA, "draft_text": "Ready for review.", "why": "The group asked."})
	draft := call("draft_message", map[string]any{"chat": chatA, "text": "Ready for review.", "voice": "cached-value", "signed": true})
	rejected, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "send_draft", Arguments: map[string]any{"draft_id": draft["draft_id"]}})
	must(t, e)
	if !rejected.IsError {
		t.Fatal("SDK early confirm bypass")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:18800/v1/graph?limit=2", nil)
	must(t, e)
	response, e := client.Do(req)
	must(t, e)
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	must(t, e)
	var graph struct {
		Nodes []any `json:"nodes"`
	}
	must(t, json.Unmarshal(body, &graph))
	if response.StatusCode != 200 || len(graph.Nodes) > 2 {
		t.Fatal("HTTP graph projection failed")
	}
}
