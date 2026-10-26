package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/wa"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func accessRig(t *testing.T) *rig {
	r := newRig(t)
	for _, v := range []struct{ id, name, jid, group, person string }{{"work", "Work", chatA, groupA, "maya"}, {"family", "FAMILY_PRIVATE", chatB, groupB, "alex"}} {
		c := model.Circle{ID: v.id, Name: v.name, Kind: v.id, SendRule: "confirm", Confirmed: true, CreatedBy: "you", UpdatedAt: epoch, Chats: []model.CircleChat{{ChatJID: v.jid}, {ChatJID: v.group}}, People: []model.CirclePerson{{PersonID: v.person}}, Topics: []model.Topic{{ID: v.id + "-topic", Title: v.name + " topic", Status: "active", Chats: []string{v.jid}}}}
		must(t, r.h.Repo.Put("circles", c.ID, c))
		_, e := r.mc.CircleService().WriteBrief(nil, c.ID, model.Brief{Summary: v.name + " brief", Evidence: []string{firstMessage(t, r, v.jid).ID}, CoversUntil: epoch})
		must(t, e)
		must(t, r.h.Repo.Put("summaries", v.id, model.Summary{ID: v.id, AgentID: r.a.ID, ChatJID: v.jid, Text: v.name + " summary"}))
	}
	setupWatches(t, r)
	createWatch(t, r, map[string]any{"scope": map[string]any{"circles": []string{"family"}}, "goal": "FAMILY_PRIVATE watch", "grant_text": "Watch the family fixture"})
	createWatch(t, r, map[string]any{"scope": map[string]any{"circles": []string{"work"}}, "goal": "Work watch"})
	must(t, r.h.Repo.Put("approvals", "private-approval", model.Approval{ID: "private-approval", AgentID: r.a.ID, ChatJID: chatB, Text: "FAMILY_PRIVATE approval", State: "sent"}))
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "update_brief", Kind: "summary", ChatJIDs: []string{chatB}, Note: "FAMILY_PRIVATE activity"}))
	p := r.a.Permissions
	p.Access = model.Access{Circles: model.CircleSelection{"work"}}
	r.agent(t, p)
	return r
}
func assertNoFamily(t *testing.T, v any) {
	t.Helper()
	b := encode(v)
	for _, secret := range []string{"FAMILY_PRIVATE", chatB, groupB, "Alex Chen", `"alex"`, `"family"`, "family-topic"} {
		if strings.Contains(b, secret) {
			t.Fatalf("excluded fixture leaked: %s", secret)
		}
	}
}
func TestCircleAccessEveryMCPReadAndHTTPProjection(t *testing.T) {
	r := accessRig(t)
	message := firstMessage(t, r, chatA)
	private := firstMessage(t, r, chatB)
	tests := []struct {
		name   string
		args   map[string]any
		denied bool
	}{
		{"whoami", map[string]any{}, false}, {"get_started", map[string]any{}, false},
		{"list_chats", map[string]any{}, false}, {"list_chats", map[string]any{"sort": "active", "unsorted": true}, false},
		{"get_chat", map[string]any{"chat_jid": chatA}, false}, {"get_chat", map[string]any{"chat_jid": chatB}, true},
		{"list_messages", map[string]any{"chat_jid": chatA}, false}, {"list_messages", map[string]any{"chat_jid": chatB}, true},
		{"search_messages", map[string]any{"query": "Saturday"}, false}, {"search_messages", map[string]any{"query": "Saturday", "chat_jid": chatB}, true},
		{"get_message_context", map[string]any{"chat_jid": chatA, "message_id": message.ID}, false}, {"get_message_context", map[string]any{"chat_jid": chatB, "message_id": private.ID}, true},
		{"find_person", map[string]any{"query": ""}, false}, {"find_person", map[string]any{"query": "Alex"}, false},
		{"get_person", map[string]any{"person_id": "maya"}, false}, {"get_person", map[string]any{"person_id": "alex"}, true},
		{"list_groups", map[string]any{}, false}, {"get_group", map[string]any{"group_jid": groupA}, false}, {"get_group", map[string]any{"group_jid": groupB}, true},
		{"people_near", map[string]any{"focus": "me", "depth": 4}, false}, {"people_near", map[string]any{"focus": "alex"}, true},
		{"get_summaries", map[string]any{"chat_jid": chatA}, false}, {"get_summaries", map[string]any{"chat_jid": chatB}, true},
		{"check_send", map[string]any{"approval_id": "private-approval"}, true}, {"my_activity", map[string]any{}, false},
		{"resolve_context", map[string]any{"query": "project"}, false}, {"list_circles", map[string]any{}, false},
		{"get_circle", map[string]any{"id": "work"}, false}, {"get_circle", map[string]any{"id": "family"}, true},
		{"get_brief", map[string]any{"id": "work"}, false}, {"get_brief", map[string]any{"id": "family"}, true},
		{"catch_up", map[string]any{"id": "work"}, false}, {"catch_up", map[string]any{"id": "family"}, true},
		{"get_brief", map[string]any{"id": "family-topic"}, true}, {"catch_up", map[string]any{"id": "family-topic"}, true},
		{"list_watches", map[string]any{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+encode(tt.args), func(t *testing.T) {
			out, e := r.call(t, tt.name, tt.args)
			if tt.denied {
				if e != model.ErrNotFound {
					t.Fatalf("want invisible not_found, got %v", e)
				}
				return
			}
			must(t, e)
			assertNoFamily(t, out)
		})
	}
	// Every agent-readable control projection, plus admin-only aggregate surfaces.
	for _, path := range []string{"/v1/chats", "/v1/messages", "/v1/people", "/v1/people/maya", "/v1/groups", "/v1/groups/" + groupA, "/v1/graph?depth=4", "/v1/graph/export", "/v1/summaries?chat_jid=" + chatA, "/v1/activity"} {
		t.Run(path, func(t *testing.T) {
			w := r.request(t, "GET", path, r.token, nil, nil)
			status(t, w, 200)
			assertNoFamily(t, w.Body.String())
		})
	}
	for _, path := range []string{"/v1/messages?chat_jid=" + chatB, "/v1/people/alex", "/v1/groups/" + groupB, "/v1/summaries?chat_jid=" + chatB} {
		w := r.request(t, "GET", path, r.token, nil, nil)
		status(t, w, 404)
	}
	for _, path := range []string{"/v1/map", "/v1/presence", "/v1/home", "/v1/circles", "/v1/circles/family", "/v1/circles/family/briefs", "/v1/suggestions", "/v1/setup/status", "/v1/watches", "/v1/people/export"} {
		w := r.request(t, "GET", path, r.token, nil, nil)
		status(t, w, 403)
		assertNoFamily(t, w.Body.String())
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("read test sent a message")
	}
}
func TestAccessUnsortedPeopleHiddenAndOverlap(t *testing.T) {
	r := accessRig(t)
	outsider := "15550100100@s.whatsapp.net"
	at := epoch
	must(t, r.m.PutChat(model.Chat{JID: outsider, Name: "Unsorted", LastMessageTime: &at}))
	must(t, r.p.PutPerson(model.Person{ID: "member-only", Name: "Circle member", JIDs: []string{"15550100101@s.whatsapp.net"}}))
	must(t, r.p.PutPerson(model.Person{ID: "chat-only", Name: "Chat participant", JIDs: []string{outsider}}))
	_, e := r.mc.CircleService().Mutate("work", func(c *model.Circle) error {
		c.People = append(c.People, model.CirclePerson{PersonID: "member-only"})
		return nil
	})
	must(t, e)
	ps, e := r.p.People(&r.a, "")
	must(t, e)
	if !strings.Contains(encode(ps), "member-only") || strings.Contains(encode(ps), "chat-only") {
		t.Fatal("circle membership projection")
	}
	// A person participating through a visible group also qualifies.
	must(t, r.p.PutMembership(model.Membership{Group: groupA, Person: "chat-only"}))
	must(t, r.m.PutMessage(model.Message{ID: "recent-participant", ChatJID: groupA, Sender: outsider, Content: "Planning", Timestamp: epoch}))
	ps, e = r.p.People(&r.a, "")
	must(t, e)
	if !strings.Contains(encode(ps), "chat-only") || strings.Contains(encode(ps), outsider) {
		t.Fatal("visible chat member or private DM projection")
	}
	if r.h.CanChat(&r.a, outsider) {
		t.Fatal("unsorted off")
	}
	p := r.a.Permissions
	p.Access.Unsorted = true
	r.agent(t, p)
	if !r.h.CanChat(&r.a, outsider) || r.h.CanChat(&r.a, chatB) {
		t.Fatal("unsorted did not exclude confirmed Family")
	}
	out, e := r.call(t, "list_chats", map[string]any{"unsorted": true, "sort": "active"})
	must(t, e)
	if len(out.([]model.Chat)) != 1 {
		t.Fatal("unsorted list filter")
	}
	// A proposal is not confirmed membership, and remains visible to its author.
	proposal, e := r.call(t, "propose_circle", map[string]any{"name": "Unsorted friend", "kind": "friends", "chats": []string{outsider}, "people": []string{}, "why": "Owner review"})
	must(t, e)
	id := proposal.(model.Proposal).ID
	if !r.h.CanChat(&r.a, outsider) {
		t.Fatal("proposal sorted the chat")
	}
	out, e = r.call(t, "get_circle", map[string]any{"id": id})
	must(t, e)
	if out.(model.Circle).Confirmed {
		t.Fatal("proposal unexpectedly confirmed")
	}
	// Shared chats can be visible without exposing their other, excluded circle.
	_, e = r.mc.CircleService().Mutate("family", func(c *model.Circle) error { c.Chats = append(c.Chats, model.CircleChat{ChatJID: chatA}); return nil })
	must(t, e)
	_, e = r.call(t, "get_circle", map[string]any{"id": "family"})
	if e != model.ErrNotFound {
		t.Fatal("overlapping circle metadata leaked")
	}
	_, e = r.call(t, "create_watch", map[string]any{"goal": "Watch", "grant_text": "Watch", "scope": map[string]any{"circles": []string{"family"}}, "match": map[string]any{"kind": "any"}, "action": "notify"})
	if e != model.ErrNotFound {
		t.Fatal("overlap watch scope leaked")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA, outsider}
	must(t, r.h.SetSettings(settings))
	if r.h.CanChat(&r.a, chatA) || r.h.CanChat(&r.a, outsider) {
		t.Fatal("hidden did not win")
	}
	ps, e = r.p.People(&r.a, "")
	must(t, e)
	if strings.Contains(encode(ps), "maya") || strings.Contains(encode(ps), "chat-only") {
		t.Fatal("hidden person resurrected through circle")
	}
}
func TestAccessExpiryOnceRenewAndWhoami(t *testing.T) {
	r := accessRig(t)
	until := epoch
	p := r.a.Permissions
	p.Access.Until = &until
	r.agent(t, p)
	want := "Your access to Limmr ended on 16 Oct 2026. Ask the owner to renew it in Limmr."
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := r.mc.Call(context.Background(), r.a.ID, "list_chats", mcpserver.Args{})
			if e == nil || e.Error() != want {
				t.Error("expiry error mismatch")
			}
		}()
	}
	wg.Wait()
	for _, tool := range mcpserver.Catalog() {
		if tool.Name == "whoami" {
			continue
		}
		_, e := r.call(t, tool.Name, map[string]any{})
		if e == nil || e.Error() != want {
			t.Fatalf("expiry was bypassed by %s", tool.Name)
		}
	}
	out, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	access := out.(map[string]any)["access"].(map[string]any)
	if access["ended"] != true {
		t.Fatal("whoami ended missing")
	}
	xs, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "access_ended"})
	must(t, e)
	if len(xs) != 1 {
		t.Fatal("expiry event duplicated")
	}
	w := r.request(t, "GET", "/v1/chats", r.token, nil, nil)
	status(t, w, 403)
	w = r.request(t, "PATCH", "/v1/agents/"+r.a.ID+"/access", r.api.ControlToken, map[string]any{"until": nil}, nil)
	status(t, w, 200)
	_, e = r.call(t, "list_chats", map[string]any{})
	must(t, e)
	a, e := r.h.Agent(r.a.ID)
	must(t, e)
	if a.Permissions.Access.Until != nil || a.AccessSummary != "Work" {
		t.Fatal("renew did not preserve scope")
	}
	// Retention/restart cannot recreate an event for the same deadline.
	_, e = r.h.Repo.DB.Exec("DELETE FROM activity WHERE json_extract(data,'$.kind')='access_ended'")
	must(t, e)
	r.agent(t, p)
	_ = r.h.CheckAccess(r.a)
	xs, e = r.h.Activity(nil, hub.ActivityFilter{Kind: "access_ended"})
	must(t, e)
	if len(xs) != 0 {
		t.Fatal("expiry marker not durable")
	}
}
func TestSetupStagesStepsBulkReviewAndBatchWindow(t *testing.T) {
	r := newRig(t)
	c := r.mc.CircleService()
	check := func(stage string) circles.SetupStatus {
		t.Helper()
		out, e := r.call(t, "get_started", map[string]any{})
		must(t, e)
		v := out.(circles.SetupStatus)
		if v.Stage != stage {
			t.Fatalf("stage %s, want %s", v.Stage, stage)
		}
		return v
	}
	if v := check("new"); len(v.NextSteps) != 3 || v.NextSteps[0].Do != "about_you" || v.NextSteps[1].Do != "propose_circles" || v.People.WithoutDetails != 2 {
		t.Fatal("new setup guidance")
	}
	propose := func(name string, chats []string) model.Proposal {
		t.Helper()
		out, e := r.call(t, "propose_circle", map[string]any{"name": name, "kind": "work", "chats": chats, "people": []string{"maya", "alex"}, "why": "Invented test"})
		must(t, e)
		return out.(model.Proposal)
	}
	a := propose("First", []string{chatA, chatB, groupA, groupB})
	r.h.Now = func() time.Time { return epoch.Add(9 * time.Minute) }
	b := propose("Second", []string{chatA})
	if a.BatchID == "" || a.BatchID != b.BatchID || a.CreatedBy != r.a.ID {
		t.Fatal("proposal batch missing")
	}
	r.h.Now = func() time.Time { return epoch.Add(10 * time.Minute) }
	d := propose("Third", []string{chatA})
	if a.BatchID == d.BatchID {
		t.Fatal("batch crossed 10 minute window")
	}
	check("sorting")
	w := r.request(t, "POST", "/v1/circles/proposals/accept", r.api.ControlToken, map[string]any{"ids": []string{a.ID, "missing"}}, nil)
	status(t, w, 404)
	raw, e := c.Circle(nil, a.ID)
	must(t, e)
	if raw.Confirmed {
		t.Fatal("bulk review partially applied")
	}
	w = r.request(t, "POST", "/v1/circles/proposals/accept", r.api.ControlToken, map[string]any{"ids": []string{a.ID}}, nil)
	status(t, w, 200)
	w = r.request(t, "POST", "/v1/circles/proposals/dismiss", r.api.ControlToken, map[string]any{"ids": []string{b.ID, d.ID}}, nil)
	status(t, w, 200)
	v := check("filling")
	if len(v.NextSteps) != 4 || len(v.CirclesWithoutBrief) != 1 {
		t.Fatal("filling guidance")
	}
	_, e = c.WriteBrief(nil, a.ID, model.Brief{Summary: "Initial brief", Evidence: []string{firstMessage(t, r, chatA).ID}, CoversUntil: epoch})
	must(t, e)
	for _, id := range []string{"maya", "alex"} {
		_, e = r.p.Update(nil, id, map[string]string{"how_known": "Project collaborator"})
		must(t, e)
	}
	check("ready")
	p := model.Defaults(true)
	r.agent(t, p)
	v = check("ready")
	if len(v.NextSteps) != 1 {
		t.Fatal("read-only setup proposed writes")
	}
	p = model.Permissions{Chats: model.Scope{Mode: "all"}}
	r.agent(t, p)
	_, e = r.call(t, "get_started", map[string]any{})
	must(t, e)
	if utf8.RuneCountInString(mcpserver.Instructions) > 1200 {
		t.Fatal("instructions too long")
	}
	w = r.request(t, "GET", "/v1/setup/status", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var admin circles.SetupStatus
	must(t, json.Unmarshal(w.Body.Bytes(), &admin))
	if admin.Stage != "ready" {
		t.Fatal("admin setup state")
	}
}
func TestThisMacAdminBriefAttributionVersionAndNoMCP(t *testing.T) {
	r := accessRig(t)
	if _, e := r.h.Agent("this-mac"); e != model.ErrNotFound {
		t.Fatal("This Mac should be lazy")
	}
	w := r.request(t, "POST", "/v1/agents/this-mac/enable", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	a, e := r.h.Agent("this-mac")
	must(t, e)
	if a.Kind != "on_device" || a.Name != "This Mac" || len(r.mc.Tools(a)) != 0 {
		t.Fatal("built-in profile")
	}
	if _, e = r.h.Rotate(a.ID); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("This Mac minted token")
	}
	if _, e = r.h.ActiveAgent(a.ID); e != model.ErrForbidden {
		t.Fatal("This Mac authenticated")
	}
	if _, e = r.mc.Call(context.Background(), a.ID, "whoami", mcpserver.Args{}); e != model.ErrForbidden {
		t.Fatal("This Mac reachable via MCP")
	}
	w = r.request(t, "PUT", "/v1/circles/work/brief", r.api.ControlToken, map[string]any{"summary": "This Mac first brief", "what_changed": "Initial understanding", "written_by": "this-mac", "covers_until": epoch}, nil)
	status(t, w, 200)
	var brief model.Brief
	must(t, json.Unmarshal(w.Body.Bytes(), &brief))
	if brief.WrittenBy != "this-mac" || brief.Version != 2 {
		t.Fatal("brief author/version")
	}
	_, e = r.call(t, "catch_up", map[string]any{"id": "work"})
	must(t, e)
	out, e := r.call(t, "update_brief", map[string]any{"id": "work", "summary": "AI improved brief", "what_changed": "More context"})
	must(t, e)
	if out.(model.Brief).Version != 3 || out.(model.Brief).WrittenBy != r.a.ID {
		t.Fatal("AI did not supersede version")
	}
	xs, e := r.h.Activity(nil, hub.ActivityFilter{Agent: "this-mac", Kind: "summary"})
	must(t, e)
	if len(xs) != 1 {
		t.Fatal("This Mac summary log")
	}
	w = r.request(t, "POST", "/v1/suggestions", r.api.ControlToken, map[string]any{"circle_id": "work", "kind": "action", "why": "Review the brief", "created_by": "this-mac"}, nil)
	status(t, w, 200)
	w = r.request(t, "PUT", "/v1/settings/briefs", r.api.ControlToken, map[string]any{"time": "06:00", "enabled": false, "on_device_briefs": map[string]any{"enabled": true}}, nil)
	status(t, w, 200)
	w = r.request(t, "GET", "/v1/settings/briefs", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"on_device_briefs":{"enabled":true}`) {
		t.Fatal("setting missing")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("on-device writes sent a message")
	}
}

func TestAccessPatchDefaultsInvalidInputAndStoredLegacy(t *testing.T) {
	r := accessRig(t)
	path := "/v1/agents/" + r.a.ID + "/access"
	status(t, r.request(t, "PATCH", path, r.token, map[string]any{"circles": "all"}, nil), 403)
	for _, body := range []map[string]any{{"circles": "work"}, {"circles": nil}, {"unsorted": nil}, {"until": "tomorrow"}, {"extra": true}} {
		status(t, r.request(t, "PATCH", path, r.api.ControlToken, body, nil), 400)
	}
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"circles": "all"}, nil), 200)
	a, e := r.h.Agent(r.a.ID)
	must(t, e)
	if a.Permissions.Access.Circles != nil || !a.Permissions.Access.Unsorted {
		t.Fatal("all patch defaults")
	}
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"circles": []string{"work"}}, nil), 200)
	a, e = r.h.Agent(r.a.ID)
	must(t, e)
	if a.Permissions.Access.Unsorted {
		t.Fatal("limited patch default")
	}
	until := epoch.AddDate(0, 0, 30)
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"until": until}, nil), 200)
	a, e = r.h.Agent(r.a.ID)
	must(t, e)
	if a.AccessSummary != "Work · until 15 Nov" {
		t.Fatal("access summary")
	}
	// Permissions were historically JSON objects without access; no rewrite needed.
	_, e = r.h.Repo.DB.Exec("UPDATE objects SET data=json_remove(data,'$.permissions.access') WHERE bucket='agents' AND id=?", r.a.ID)
	must(t, e)
	a, e = r.h.Agent(r.a.ID)
	must(t, e)
	if a.AccessSummary != "Everything" || !r.h.CanChat(&a, chatB) {
		t.Fatal("legacy agent lost access")
	}
	// The full permissions replacement accepts the same access object.
	p := a.Permissions
	p.Access = model.Access{Circles: model.CircleSelection{}}
	status(t, r.request(t, "PUT", "/v1/agents/"+a.ID+"/permissions", r.api.ControlToken, p, nil), 200)
	a, e = r.h.Agent(r.a.ID)
	must(t, e)
	if a.Access.Circles == nil || r.h.CanChat(&a, chatA) {
		t.Fatal("empty array broadened")
	}
}

func TestSetupSortingQuarterBoundaryAndLimitedCounts(t *testing.T) {
	r := newRig(t)
	c := r.mc.CircleService()
	// Three of four active chats are sorted: exactly 25% does not trigger sorting.
	kept, e := c.Create(nil, model.Circle{Name: "Work", Kind: "work", Chats: []model.CircleChat{{ChatJID: chatA}, {ChatJID: groupA}, {ChatJID: groupB}}}, "Test")
	must(t, e)
	_, e = c.Confirm(kept.ID)
	must(t, e)
	v, e := c.Setup(&r.a, true)
	must(t, e)
	if v.Stage != "filling" || !v.Linked || v.UnsortedActiveChats != 1 {
		t.Fatal("quarter boundary")
	}
	_, e = c.Mutate(kept.ID, func(c *model.Circle) error { c.Chats = c.Chats[:2]; return nil })
	must(t, e)
	v, e = c.Setup(&r.a, true)
	must(t, e)
	if v.Stage != "sorting" {
		t.Fatal("more than quarter unsorted")
	}
	p := r.a.Permissions
	p.Access = model.Access{Circles: model.CircleSelection{kept.ID}}
	p.SaveSummaries = false
	p.WritePeople = false
	r.agent(t, p)
	v, e = c.Setup(&r.a, true)
	must(t, e)
	if v.Chats != 2 || v.People.Total != 1 || len(v.NextSteps) != 1 || v.UnsortedActiveChats != 0 {
		t.Fatal("limited setup counts or step permissions")
	}
}

func TestProposalFiftyMemberLimitAndOtherAgentIsolation(t *testing.T) {
	r := newRig(t)
	chats := []string{}
	for i := 0; i < 51; i++ {
		j := fmt.Sprintf("15550101%03d@s.whatsapp.net", i)
		must(t, r.m.PutChat(model.Chat{JID: j, Name: "Invented project", LastMessageTime: &epoch}))
		chats = append(chats, j)
	}
	args := map[string]any{"name": "Project", "kind": "community", "chats": chats[:50], "people": []string{}, "why": "Review"}
	out, e := r.call(t, "propose_circle", args)
	must(t, e)
	proposal := out.(model.Proposal)
	args["chats"] = chats
	if _, e = r.call(t, "propose_circle", args); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("51 chats accepted")
	}
	other, _, e := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, e)
	if _, e = r.mc.CircleService().Circle(&other, proposal.ID); e != model.ErrNotFound {
		t.Fatal("another agent's proposal visible")
	}
	v, e := r.mc.CircleService().Setup(&other, false)
	must(t, e)
	// The proposal's contents stay private to its author, but a pending proposal whose chats this agent can see
	// still counts, so a second AI knows circles are waiting for the owner instead of proposing them again.
	if v.Circles.Proposed != 1 || v.Stage != "sorting" {
		t.Fatal("another agent's visible proposal not counted")
	}
	other.Permissions.Access = model.Access{Circles: model.CircleSelection{"work"}}
	v, e = r.mc.CircleService().Setup(&other, false)
	must(t, e)
	if v.Circles.Proposed != 0 {
		t.Fatal("a proposal outside this agent's access was counted")
	}
}

func TestRemoteApprovalAccessAndExpiryThroughActualMCP(t *testing.T) {
	r, s := remoteRig(t)
	c := model.Circle{ID: "work", Name: "Work", Kind: "work", SendRule: "confirm", Confirmed: true, CreatedBy: "you", Chats: []model.CircleChat{{ChatJID: chatA}}, People: []model.CirclePerson{{PersonID: "maya"}}}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	f := register(t, s, "Work assistant")
	authorize(t, s, &f)
	until := epoch.Add(time.Hour)
	w := r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, map[string]any{"code": f.code, "new_agent": map[string]any{"name": "Work assistant", "kind": "chatgpt"}, "access": map[string]any{"circles": []string{"work"}, "until": until}}, nil)
	status(t, w, 200)
	pair := remoteRequest(t, s, "GET", "/pair/status?tx="+f.tx, "", nil)
	status(t, pair, 200)
	m := readMap(t, pair)
	u, e := url.Parse(m["redirect"].(string))
	must(t, e)
	f.grant = u.Query().Get("code")
	for _, client := range s.Status().Clients {
		if client.ID == f.client {
			f.agent = client.AgentID
		}
	}
	status(t, exchange(t, s, &f), 200)
	a, e := r.h.Agent(f.agent)
	must(t, e)
	if a.Permissions.Access.Unsorted || a.AccessSummary != "Work · until 16 Oct" {
		t.Fatal("remote access not applied")
	}
	if a.Permissions.WritePeople || a.Permissions.SaveSummaries {
		t.Fatal("a remote AI got write access without fill_in")
	}
	call := func(name string) *httptest.ResponseRecorder {
		return remoteRequest(t, s, "POST", "/mcp", f.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}})
	}
	w = call("list_chats")
	status(t, w, 200)
	assertNoFamily(t, w.Body.String())
	if strings.Contains(w.Body.String(), chatB) {
		t.Fatal("remote scope leak")
	}
	r.h.Now = func() time.Time { return until }
	// Also check a tool the remote's capabilities do not list: expiry must win.
	for _, name := range []string{"list_chats", "send_message"} {
		w = call(name)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), "Your access to Limmr ended on 16 Oct 2026. Ask the owner to renew it in Limmr.") {
			t.Fatal("transport expiry error missing")
		}
	}
	w = call("whoami")
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"ended":true`) {
		t.Fatal("expired remote whoami")
	}
	status(t, r.request(t, "PATCH", "/v1/agents/"+a.ID+"/access", r.api.ControlToken, map[string]any{"until": nil}, nil), 200)
	w = call("list_chats")
	status(t, w, 200)
	if strings.Contains(w.Body.String(), `"isError":true`) {
		t.Fatal("OAuth grant did not recover after renewal")
	}
}

func TestAccessPeopleNeedRealParticipationAndWatchesRecheckScope(t *testing.T) {
	r := accessRig(t)
	must(t, r.p.PutPerson(model.Person{ID: "address-book-only", Name: "Directory only", JIDs: []string{"15550100199@s.whatsapp.net"}}))
	p := r.a.Permissions
	p.Access.Unsorted = true
	r.agent(t, p)
	ps, e := r.p.People(&r.a, "")
	must(t, e)
	if strings.Contains(encode(ps), "address-book-only") {
		t.Fatal("hypothetical unsorted DM exposed directory contact")
	}
	// A sender in a visible group qualifies even if the membership cache is stale.
	must(t, r.m.PutMessage(model.Message{ID: "visible-participant", ChatJID: groupA, Sender: "15550100199@s.whatsapp.net", Content: "Invented work update", Timestamp: epoch}))
	ps, e = r.p.People(&r.a, "")
	must(t, e)
	if !strings.Contains(encode(ps), "address-book-only") {
		t.Fatal("visible message participant missing")
	}
	_, e = r.mc.CircleService().Mutate("family", func(c *model.Circle) error { c.Chats = append(c.Chats, model.CircleChat{ChatJID: chatA}); return nil })
	must(t, e)
	s := r.mc.Watches
	must(t, s.Handle(context.Background(), wa.StoredEvent{Message: model.Message{ID: "shared-access-event", ChatJID: chatA, Sender: chatA, Content: "exam update", Timestamp: epoch}}))
	records, e := s.Store.List(context.Background())
	must(t, e)
	for _, record := range records {
		hits, e := s.Store.Hits(context.Background(), record.ID, 0)
		must(t, e)
		if strings.Contains(record.Goal, "FAMILY_PRIVATE") && len(hits) != 0 {
			t.Fatal("narrowed circle watch consumed shared chat")
		}
	}
}

func TestRemoteApprovalFillInGrantsSetupWritesOnly(t *testing.T) {
	r, s := remoteRig(t)
	f := register(t, s, "ChatGPT")
	authorize(t, s, &f)
	w := r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, map[string]any{"code": f.code, "new_agent": map[string]any{"name": "ChatGPT", "kind": "chatgpt"}, "fill_in": true}, nil)
	status(t, w, 200)
	for _, client := range s.Status().Clients {
		if client.ID == f.client {
			f.agent = client.AgentID
		}
	}
	a, e := r.h.Agent(f.agent)
	must(t, e)
	p := a.Permissions
	if !p.WritePeople || !p.SaveSummaries || p.RequestSend || p.DownloadMedia {
		t.Fatal("fill_in must grant people and brief writes only")
	}
}
