package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/wa"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLIDMigrationAndScopeAliases(t *testing.T) {
	r := newRig(t)
	lid := "900000001@lid"
	at := epoch
	must(t, r.m.PutChat(model.Chat{JID: lid, Name: "Maya alias", LastMessageTime: &at}))
	must(t, r.m.PutMessage(model.Message{ID: "lid-message", ChatJID: lid, Sender: "900000001", Content: "aliasword", Timestamp: at, MediaType: "document", Filename: "sketch.txt", QuotedMessageID: "quoted-id"}))
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{lid}}
	r.agent(t, p)
	must(t, r.m.NormalizeMapping(lid, chatA))
	out, e := r.call(t, "search_messages", map[string]any{"query": "aliasword"})
	must(t, e)
	ms := out.([]model.Message)
	if len(ms) != 1 || ms[0].ChatJID != chatA || ms[0].Sender != chatA || ms[0].QuotedMessageID != "quoted-id" {
		t.Fatal("LID data lost or scope not canonicalised")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{lid}
	must(t, r.h.SetSettings(settings))
	out, e = r.call(t, "search_messages", map[string]any{"query": "aliasword"})
	must(t, e)
	if len(out.([]model.Message)) != 0 {
		t.Fatal("hidden LID bypassed through phone JID")
	}
	cs, e := r.m.Chats()
	must(t, e)
	for _, c := range cs {
		if c.JID == lid {
			t.Fatal("duplicate LID chat not merged")
		}
	}
}
func TestWhoamiDoesNotRevealExcludedOrHiddenLists(t *testing.T) {
	r := newRig(t)
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "all_except", JIDs: []string{chatB}}
	r.agent(t, p)
	settings := r.h.Settings()
	settings.HiddenChats = []string{groupB}
	must(t, r.h.SetSettings(settings))
	out, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	raw := encode(out)
	if strings.Contains(raw, chatB) || strings.Contains(raw, groupB) {
		t.Fatal("whoami revealed inaccessible chat list")
	}
}
func TestHiddenPersonAliasesAndGroupCounts(t *testing.T) {
	r := newRig(t)
	must(t, r.p.PutPerson(model.Person{ID: "maya", JIDs: []string{chatA, "900000001@lid"}, Name: "Maya Rao", UpdatedAt: epoch}))
	must(t, r.m.NormalizeMapping("900000001@lid", chatA))
	settings := r.h.Settings()
	settings.HiddenChats = []string{"900000001@lid"}
	must(t, r.h.SetSettings(settings))
	ps, e := r.p.People(&r.a, "")
	must(t, e)
	if strings.Contains(encode(ps), "Maya Rao") {
		t.Fatal("hidden alias did not hide person")
	}
	gs, e := r.p.Groups(&r.a, "")
	must(t, e)
	for _, g := range gs {
		if g.JID == groupA && g.MemberCount != 0 {
			t.Fatal("hidden member counted")
		}
	}
}
func TestRecentAgentReadMarksGraphEdges(t *testing.T) {
	r := newRig(t)
	old := epoch.Add(-10 * 24 * time.Hour)
	_, e := r.m.DB.Exec("UPDATE messages SET timestamp=?", old)
	must(t, e)
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "list_messages", Kind: "read", ChatJIDs: []string{groupA}, Count: 1}))
	g, e := r.p.Graph(&r.a, "me", 4, 50)
	must(t, e)
	found := false
	for _, edge := range g.Edges {
		if edge.Kind == "member" && (edge.A == groupA || edge.B == groupA) {
			found = true
			if !edge.Recent {
				t.Fatal("agent read did not animate membership")
			}
		}
	}
	if !found {
		t.Fatal("missing group edge")
	}
}
func TestLastSeenCannotOverwriteConcurrentPermissions(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.h.Authenticate(r.token); errs <- e }()
	}
	p := r.a.Permissions
	p.RequestSend = false
	p.ReadPeople = false
	r.agent(t, p)
	wg.Wait()
	close(errs)
	for e := range errs {
		must(t, e)
	}
	a, e := r.h.Agent(r.a.ID)
	must(t, e)
	if a.Permissions.RequestSend || a.Permissions.ReadPeople {
		t.Fatal("last_seen write restored stale permissions")
	}
}
func TestRemoteOriginAndConfidentialClient(t *testing.T) {
	r, s := remoteRig(t)
	req := httptest.NewRequest("POST", "https://limmr.example/mcp", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	status(t, w, 403)
	w = remoteRequest(t, s, "POST", "/register", "", map[string]any{"client_name": "Confidential fixture", "redirect_uris": []string{"https://client.example/callback"}, "token_endpoint_auth_method": "client_secret_post"})
	status(t, w, 201)
	m := readMap(t, w)
	f := register(t, s, "PKCE fixture")
	f.client = m["client_id"].(string)
	secret := m["client_secret"].(string)
	authorize(t, s, &f)
	approve(t, r, s, &f)
	status(t, exchange(t, s, &f), 401)
	w = remoteRequest(t, s, "POST", "/token", "", url.Values{"grant_type": {"authorization_code"}, "client_id": {f.client}, "client_secret": {secret}, "code": {f.grant}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {f.verifier}})
	status(t, w, 200)
	var persisted string
	must(t, r.h.Repo.DB.QueryRow("SELECT data FROM objects WHERE bucket='oauth_clients' AND id=?", f.client).Scan(&persisted))
	if strings.Contains(persisted, secret) {
		t.Fatal("raw client secret persisted")
	}
}
func TestMCPDeniedAndMalformedCallsAreAudited(t *testing.T) {
	r := newRig(t)
	p := model.Defaults(true)
	r.agent(t, p)
	for i, params := range []map[string]any{{"name": "send_message", "arguments": map[string]any{"recipient": chatA, "text": "Draft", "voice": "as_agent"}}, {"name": "whoami", "arguments": map[string]any{"unexpected": true}}, {"name": "whoami", "arguments": []any{"wrong"}}} {
		w := r.request(t, "POST", "/mcp", r.token, map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": "tools/call", "params": params}, map[string]string{"Accept": "application/json, text/event-stream"})
		status(t, w, 200)
	}
	xs, e := r.h.Activity(&r.a, hub.ActivityFilter{})
	must(t, e)
	if len(xs) != 3 {
		t.Fatalf("want 3 audited rejections, got %d", len(xs))
	}
	for _, a := range xs {
		if a.Kind != "denied" {
			t.Fatal("denied call audited as read")
		}
	}
}
func TestCallEveryToolWritesOneRow(t *testing.T) {
	r := newRig(t)
	before, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 500})
	must(t, e)
	tools := r.mc.Tools(r.a)
	for _, tool := range tools {
		_, _ = r.call(t, tool.Name, map[string]any{})
	}
	after, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 500})
	must(t, e)
	if len(after)-len(before) != len(tools) {
		t.Fatal("tool audit count differs from call count")
	}
}
func TestPlainTextAndEditExactThroughAPI(t *testing.T) {
	r := newRig(t)
	p := r.a.Permissions
	r.agent(t, p)
	out, e := r.mc.Call(hub.WithBackground(context.Background(), true), r.a.ID, "draft_message", mcpserver.Args{"chat": chatA, "text": "Draft", "voice": "as_agent"})
	must(t, e)
	d := out.(model.Draft)
	if d.FinalText != "Draft" {
		t.Fatal("draft text changed")
	}
	w := r.request(t, "POST", "/v1/suggestions/"+d.SuggestionID+"/send", r.api.ControlToken, map[string]string{"text": "Exact final"}, nil)
	status(t, w, 200)
	if r.fake.Sent[0].Text != "Exact final" {
		t.Fatal("owner body edit changed")
	}
}
func TestActivitySubscriptionAndSummary(t *testing.T) {
	r := newRig(t)
	ch, done := r.h.Subscribe()
	defer done()
	out, e := r.call(t, "save_summary", map[string]any{"chat_jid": chatA, "period": "Today", "text": "Invented summary"})
	must(t, e)
	if out.(model.Summary).AgentID != r.a.ID {
		t.Fatal("summary unattributed")
	}
	select {
	case act := <-ch:
		if act.Kind != "summary" || act.Note != "Invented summary" {
			t.Fatal("summary audit lost")
		}
	case <-time.After(time.Second):
		t.Fatal("no activity notification")
	}
	out, e = r.call(t, "get_summaries", map[string]any{"chat_jid": chatA})
	must(t, e)
	if len(out.([]model.Summary)) != 1 {
		t.Fatal("summary not saved")
	}
}
func TestAttachmentRejectsInvalidInput(t *testing.T) {
	r := newRig(t)
	if _, e := r.h.Attachment(nil, "sketch.txt"); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("empty attachment")
	}
	at, e := r.h.Attachment([]byte("invented bytes"), "../../sketch.txt")
	must(t, e)
	if at.Name != "sketch.txt" || at.SHA256 != dbutil.Digest("invented bytes") {
		t.Fatal("unconfined filename or wrong digest")
	}
	_, e = r.call(t, "send_file", map[string]any{"recipient": chatA, "voice": "as_agent", "attachment_id": "../../etc/passwd"})
	if e != model.ErrNotFound {
		t.Fatal("arbitrary file path accepted")
	}
}
func TestLinkSSEZeroProgressShape(t *testing.T) {
	b, e := json.Marshal(wa.LinkEvent{State: "syncing"})
	must(t, e)
	for _, key := range []string{"progress", "chats", "messages"} {
		if !strings.Contains(string(b), `"`+key+`":0`) {
			t.Fatalf("missing zero-valued syncing field %s", key)
		}
	}
}

func TestGraphKeepsOwnerWhenRankedPeopleDominate(t *testing.T) {
	r := newRig(t)
	g, e := r.p.Graph(nil, "", 4, 2)
	must(t, e)
	if len(g.Nodes) == 0 || g.Nodes[0].ID != "me" {
		t.Fatal("owner not in default graph")
	}
}
func TestExportMatchesProfileAndEvidenceProjection(t *testing.T) {
	r := newRig(t)
	m := firstMessage(t, r, groupA)
	_, e := r.p.AddFact(&r.a, model.Fact{Subject: "person", SubjectID: "maya", Text: "Fact", EvidenceMessageIDs: []string{m.ID}})
	must(t, e)
	out, e := r.p.Export(&r.a)
	must(t, e)
	profiles := out.(map[string]any)["people"].([]people.Profile)
	for _, p := range profiles {
		profile, e := r.p.Profile(&r.a, p.Person.ID)
		must(t, e)
		if encode(p) != encode(profile) {
			t.Fatal("bulk export differs from individual profile")
		}
	}
}
func TestMigrationPreservesExistingDisabledPlist(t *testing.T) {
	s, runner := migrationRig(t)
	path := filepath.Join(s.Plists, "dev.limmr.whatsapp-bridge.plist.disabled")
	must(t, os.WriteFile(path, []byte("existing backup"), 0600))
	_, e := s.Run(context.Background(), false)
	if e != model.ErrConflict || len(runner.calls) != 0 {
		t.Fatal("migration overwrote an existing backup")
	}
	b, e := os.ReadFile(path)
	must(t, e)
	if string(b) != "existing backup" {
		t.Fatal("backup changed")
	}
}

func TestCorruptHiddenChatSettingsFailClosed(t *testing.T) {
	r := newRig(t)
	_, e := r.h.Repo.DB.Exec("UPDATE objects SET data='{' WHERE bucket='settings' AND id='main'")
	must(t, e)
	out, e := r.call(t, "list_chats", map[string]any{})
	must(t, e)
	if len(out.([]model.Chat)) != 0 {
		t.Fatal("unreadable hidden settings exposed chats")
	}
	ps, e := r.p.People(&r.a, "")
	must(t, e)
	if len(ps) != 0 {
		t.Fatal("unreadable hidden settings exposed people")
	}
}

type fixtureDownloader struct{ path string }

func (d fixtureDownloader) Download(context.Context, string, string) (string, error) {
	return d.path, nil
}
func TestEverySuccessfulToolWritesExactlyOneActivityRow(t *testing.T) {
	r := newRig(t)
	message := firstMessage(t, r, chatA)
	path := filepath.Join(r.home, "invented-media")
	must(t, os.WriteFile(path, []byte("invented bytes"), 0600))
	r.mc.Downloader = fixtureDownloader{path: path}
	at, e := r.h.Attachment([]byte("invented attachment"), "sketch.txt")
	must(t, e)
	args := map[string]map[string]any{
		"whoami": {}, "list_chats": {}, "get_chat": {"chat_jid": chatA}, "list_messages": {"chat_jid": chatA},
		"search_messages": {"query": "Saturday"}, "get_message_context": {"chat_jid": chatA, "message_id": message.ID},
		"find_person": {"query": "Maya"}, "get_person": {"person_id": "maya"}, "list_groups": {}, "get_group": {"group_jid": groupA},
		"people_near": {"focus": "maya"}, "add_fact": {"subject": "person", "subject_id": "maya", "text": "Organises sessions", "evidence_message_ids": []string{message.ID}},
		"update_person": {"person_id": "maya", "fields": map[string]string{"notes": "Prefers mornings"}}, "add_relation": {"a": "me", "b": "maya", "kind": "friend"},
		"save_summary": {"chat_jid": chatA, "period": "Today", "text": "Invented summary"}, "get_summaries": {"chat_jid": chatA},
		"download_media": {"chat_jid": chatA, "message_id": message.ID}, "send_message": {"recipient": chatA, "text": "Draft", "voice": "as_agent"},
		"send_file": {"recipient": chatA, "attachment_id": at.ID, "voice": "as_agent"}, "send_reaction": {"recipient": chatA, "message_id": message.ID, "text": "Thanks", "voice": "as_agent"},
		"my_activity": {},
	}
	for _, tool := range mcpserver.LegacyCatalog() {
		if _, ok := args[tool.Name]; !ok && tool.Name != "check_send" {
			continue
		}
		if tool.Name == "check_send" {
			apps, e := hub.List[model.Draft](r.h.Repo, "drafts")
			must(t, e)
			args["check_send"] = map[string]any{"approval_id": apps[0].ID}
		}
		out, e := r.call(t, tool.Name, args[tool.Name])
		must(t, e)
		if out == nil {
			t.Fatalf("missing result for %s", tool.Name)
		}
	}
	rows, e := r.h.Activity(&r.a, hub.ActivityFilter{Limit: 500})
	must(t, e)
	if len(rows) != 22 {
		t.Fatalf("want 22 successful tool rows, got %d", len(rows))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Kind == "denied" {
			t.Fatalf("successful tool rejected: %s", row.Tool)
		}
		name := row.CalledAs
		if name == "" {
			name = row.Tool
		}
		if seen[name] {
			t.Fatalf("duplicate tool audit: %s", name)
		}
		seen[name] = true
	}
	if len(r.fake.Sent) != 0 || r.h.PendingCount() != 0 {
		t.Fatal("legacy send tools bypassed drafts")
	}
}
