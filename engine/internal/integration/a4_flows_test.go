package integration_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/watches"
	"strings"
	"testing"
	"time"
)

func TestA4WatchMCPToHomeHitPauseResumeStopAndExpiry(t *testing.T) {
	r := newRig(t)
	s, clock, _ := setupWatches(t, r)
	watch := createWatch(t, r, map[string]any{"expires_at": epoch.Add(time.Hour).Format(time.RFC3339)})
	read := func() watches.Detail {
		w := r.request(t, "GET", "/v1/watches/"+watch.ID, r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var detail watches.Detail
		must(t, json.Unmarshal(w.Body.Bytes(), &detail))
		return detail
	}
	homeHas := func(want bool) {
		t.Helper()
		w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var home struct{ Watches []watches.Summary }
		must(t, json.Unmarshal(w.Body.Bytes(), &home))
		found := false
		for _, w := range home.Watches {
			if w.ID == watch.ID {
				found = true
			}
		}
		if found != want {
			t.Fatal("Home watch visibility did not follow lifecycle")
		}
	}
	homeHas(true)
	incoming(t, r, s, "a4-nonmatch", chatA, "The garden is growing")
	incoming(t, r, s, "a4-match", chatA, "Exam moved to Friday")
	clock.advance(time.Minute)
	must(t, s.Engine.FlushDue(t.Context()))
	hit := read()
	if hit.NewHits != 1 || hit.Latest == nil || hit.Latest.MessageID != "a4-match" || len(hit.Hits) != 1 {
		t.Fatal("matching event did not reach owner hit list")
	}
	for _, step := range []struct {
		action string
		state  triggers.Status
	}{{"pause", triggers.Paused}, {"resume", triggers.Active}, {"stop", triggers.Stopped}} {
		status(t, r.request(t, "POST", "/v1/watches/"+watch.ID+"/"+step.action, r.api.ControlToken, nil, nil), 200)
		if read().Status != step.state {
			t.Fatal("owner lifecycle action did not persist")
		}
		homeHas(step.state != triggers.Stopped)
	}
	watch = createWatch(t, r, map[string]any{"expires_at": clock.Now().Add(time.Minute).Format(time.RFC3339)})
	clock.advance(2 * time.Minute)
	must(t, s.Engine.FlushDue(t.Context()))
	if read().Status != triggers.Expired {
		t.Fatal("expiry did not persist")
	}
	homeHas(false)
	status(t, r.request(t, "POST", "/v1/watches/"+watch.ID+"/resume", r.api.ControlToken, nil, nil), 409)
	if len(r.fake.Sent) != 0 {
		t.Fatal("watch journey sent a message")
	}
}

func TestA4EffortNamingSplitMergeAndOwnerControls(t *testing.T) {
	r := newRig(t)
	_, err := r.m.DB.Exec("DELETE FROM messages")
	must(t, err)
	for i, text := range []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"} {
		must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("a4-budget-%d", i), ChatJID: chatA, Sender: chatA, Content: text, IsFromMe: i == 1, Timestamp: epoch.Add(time.Duration(i-3) * time.Hour)}))
	}
	run := func(action string, args map[string]any) any {
		t.Helper()
		out, e := r.call(t, "more", map[string]any{"action": action, "args": args})
		must(t, e)
		return out
	}
	queue := run("efforts.unnamed", map[string]any{}).(map[string]any)
	pending := queue["efforts"].([]efforts.NamingCandidate)
	if len(pending) != 1 {
		t.Fatal("naming queue did not expose the purpose")
	}
	id := pending[0].ID
	named := run("efforts.update", map[string]any{"id": id, "title": "Festival budget", "summary": "Review the festival proposal."}).(efforts.Effort)
	if named.CreatedBy != r.a.Name {
		t.Fatal("naming credit lost")
	}
	if run("efforts.unnamed", map[string]any{}).(map[string]any)["total"].(int) != 0 {
		t.Fatal("named effort remained queued")
	}
	w := r.request(t, "GET", "/v1/efforts?q=Festival", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), "Festival budget") {
		t.Fatal("named purpose not searchable")
	}
	split := run("efforts.split", map[string]any{"id": id, "message_refs": named.MessageRefs[:1]}).(efforts.Effort)
	merged := run("efforts.merge", map[string]any{"id": id, "other_id": split.ID}).(efforts.Effort)
	if len(merged.MessageRefs) != len(named.MessageRefs) {
		t.Fatal("split/merge lost evidence")
	}
	for _, patch := range []map[string]any{{"pinned": true}, {"pinned": false}, {"hidden": true}} {
		status(t, r.request(t, "PATCH", "/v1/efforts/"+id, r.api.ControlToken, patch, nil), 200)
		w = r.request(t, "GET", "/v1/efforts/"+id, r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var detail efforts.Detail
		must(t, json.Unmarshal(w.Body.Bytes(), &detail))
		if p, ok := patch["pinned"]; ok && detail.Effort.Pinned != p.(bool) {
			t.Fatal("pin did not round-trip")
		}
		if _, ok := patch["hidden"]; ok && !detail.Effort.Hidden {
			t.Fatal("hide did not round-trip")
		}
	}
	w = r.request(t, "GET", "/v1/efforts?q=Festival", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), id) {
		t.Fatal("hidden effort still searchable")
	}
}

func TestA4EffortStatusFollowsAnsweredAsk(t *testing.T) {
	r := newRig(t)
	_, err := r.m.DB.Exec("DELETE FROM messages")
	must(t, err)
	texts := []string{"The pitch deck layout is ready.", "The pitch deck outline is ready.", "Can you review the pitch deck slides?"}
	for i, text := range texts {
		must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("a4-slides-%d", i), ChatJID: chatA, Sender: chatA, Content: text, IsFromMe: i == 1, Timestamp: epoch.Add(time.Duration(i-3) * time.Minute)}))
	}
	internal, err := r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if len(internal) != 1 {
		t.Fatalf("expected one purpose, got %d", len(internal))
	}
	_, err = r.call(t, "more", map[string]any{"action": "efforts.update", "args": map[string]any{"id": internal[0].ID, "title": "Festival presentation"}})
	must(t, err)
	list := func() []efforts.Effort {
		out, e := r.call(t, "more", map[string]any{"action": "efforts.list", "args": map[string]any{}})
		must(t, e)
		return out.([]efforts.Effort)
	}
	before := list()
	if len(before) != 1 || before[0].Status != "on_you" {
		t.Fatal("ask not shown as on you")
	}
	must(t, r.m.PutMessage(model.Message{ID: "a4-answer", ChatJID: chatA, Sender: chatA, IsFromMe: true, Content: "The pitch deck slides are submitted.", Timestamp: epoch}))
	after := list()
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].Status != "done" {
		t.Fatal("answered ask did not close the same effort")
	}
}

func TestA4SettingsReadbackAndImmediateHiddenReadsAndDigest(t *testing.T) {
	r := newRig(t)
	circleFixture(t, r, "friends", "confirm", false, chatA)
	circleFixture(t, r, "project", "confirm", false, groupA)
	s, clock, _ := setupWatches(t, r)
	standingFeed(t, r)
	incoming(t, r, s, "a4-private-dm", chatA, "A4_PRIVATE_DM")
	incoming(t, r, s, "a4-private-group", groupA, "A4_PRIVATE_GROUP")
	clock.advance(time.Minute)
	must(t, s.Engine.FlushDue(t.Context()))
	before, err := r.call(t, "get_digest", map[string]any{})
	must(t, err)
	if !strings.Contains(encode(before), "A4_PRIVATE") {
		t.Fatal("digest fixture did not contain visible content before hiding")
	}
	for _, enabled := range []bool{false, true} {
		status(t, r.request(t, "PUT", "/v1/settings/people", r.api.ControlToken, map[string]any{"enabled": enabled}, nil), 200)
		peopleRead := r.request(t, "GET", "/v1/settings/people", r.api.ControlToken, nil, nil)
		status(t, peopleRead, 200)
		var peopleSetting struct{ Enabled bool }
		must(t, json.Unmarshal(peopleRead.Body.Bytes(), &peopleSetting))
		if peopleSetting.Enabled != enabled {
			t.Fatal("people inference toggle did not round-trip")
		}
		status(t, r.request(t, "PUT", "/v1/settings/briefs", r.api.ControlToken, map[string]any{"enabled": enabled, "time": "06:00", "on_device_briefs": map[string]any{"enabled": enabled}}, nil), 200)
		briefRead := r.request(t, "GET", "/v1/settings/briefs", r.api.ControlToken, nil, nil)
		status(t, briefRead, 200)
		var briefSetting struct {
			Enabled        bool
			OnDeviceBriefs struct{ Enabled bool } `json:"on_device_briefs"`
		}
		must(t, json.Unmarshal(briefRead.Body.Bytes(), &briefSetting))
		if briefSetting.Enabled != enabled || briefSetting.OnDeviceBriefs.Enabled != enabled {
			t.Fatal("brief toggles did not round-trip")
		}
		settings := r.h.Settings()
		settings.AutoCircles = &enabled
		settings.MaxCircles = 6
		settings.RetentionDays = 30
		settings.CurrentDays = 90
		settings.HiddenChats = []string{chatA, groupA}
		status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 200)
		w := r.request(t, "GET", "/v1/settings", r.api.ControlToken, nil, nil)
		status(t, w, 200)
		var saved model.Settings
		must(t, json.Unmarshal(w.Body.Bytes(), &saved))
		if saved.AutoCirclesEnabled() != enabled || saved.RetentionDays != 30 || saved.MaxCircles != 6 || saved.CurrentDays != 90 || len(saved.HiddenChats) != 2 {
			t.Fatal("settings did not round-trip")
		}
		for _, chat := range []string{chatA, groupA} {
			_, err := r.call(t, "list_messages", map[string]any{"chat_jid": chat})
			if err != model.ErrNotFound {
				t.Fatal("hidden chat remained readable")
			}
		}
		out, err := r.call(t, "get_digest", map[string]any{})
		must(t, err)
		if strings.Contains(encode(out), "A4_PRIVATE") {
			t.Fatal("hidden content remained in digest")
		}
		settings.HiddenChats = nil
		status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 200)
		for _, chat := range []string{chatA, groupA} {
			_, err = r.call(t, "list_messages", map[string]any{"chat_jid": chat})
			must(t, err)
		}
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("settings journey sent a message")
	}
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "list_messages", Kind: "read", ChatJIDs: []string{chatB}, Count: 1}))
	r.h.Now = func() time.Time { return epoch.AddDate(0, 0, 31) }
	must(t, r.h.Retain())
	var remaining int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM activity").Scan(&remaining))
	if remaining != 0 {
		t.Fatal("30-day activity retention did not expire old records")
	}
	if _, err := r.p.Profile(nil, "maya"); err != nil {
		t.Fatal("retention removed the people directory")
	}
}

func TestA4AgentRenameAccessAndDisconnect(t *testing.T) {
	r := accessRig(t)
	created := r.request(t, "POST", "/v1/agents", r.api.ControlToken, map[string]any{"name": "Studio assistant", "kind": "custom"}, nil)
	status(t, created, 200)
	var connected struct {
		Agent model.Agent
		Token string
	}
	must(t, json.Unmarshal(created.Body.Bytes(), &connected))
	if _, err := r.h.Authenticate(connected.Token); err != nil {
		t.Fatal("new integration cannot authenticate")
	}
	status(t, r.request(t, "DELETE", "/v1/agents/"+connected.Agent.ID, r.api.ControlToken, nil, nil), 200)
	if _, err := r.h.Authenticate(connected.Token); err == nil {
		t.Fatal("disconnected integration still authenticates")
	}
	status(t, r.request(t, "PATCH", "/v1/agents/"+r.a.ID, r.api.ControlToken, map[string]any{"name": "Studio helper"}, nil), 200)
	w := r.request(t, "GET", "/v1/agents/"+r.a.ID, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var renamed model.Agent
	must(t, json.Unmarshal(w.Body.Bytes(), &renamed))
	if renamed.Name != "Studio helper" {
		t.Fatal("rename did not persist")
	}
	_, err := r.call(t, "get_brief", map[string]any{"id": "family"})
	if err != model.ErrNotFound {
		t.Fatal("circle grant bypassed")
	}
	out, err := r.call(t, "get_brief", map[string]any{"id": "work"})
	must(t, err)
	assertNoFamily(t, out)
	status(t, r.request(t, "DELETE", "/v1/agents/"+r.a.ID, r.api.ControlToken, nil, nil), 200)
	if _, err = r.h.Authenticate(r.token); err == nil {
		t.Fatal("removed credential still authenticates")
	}
}

func TestA4ReplyRequestAgentDraftEditApproveAndDeny(t *testing.T) {
	r := newRig(t)
	circle := circleFixture(t, r, "project", "confirm", false, chatA)
	brief, err := r.mc.CircleService().WriteBrief(nil, circle.ID, model.Brief{
		Summary: "Review the studio sketches.", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID},
		OpenAsks: []model.OpenAsk{{Text: "Can you review the sketches?", ChatJID: chatA, From: "Maya", At: epoch}},
	})
	must(t, err)
	path := "/v1/circles/" + circle.ID + "/asks/" + brief.OpenAsks[0].ID
	request := r.request(t, "POST", path+"/draft", r.api.ControlToken, map[string]any{"agent_id": r.a.ID}, nil)
	status(t, request, 200)
	if len(r.fake.Sent) != 0 {
		t.Fatal("request sent without a draft")
	}
	var task model.Suggestion
	must(t, json.Unmarshal(request.Body.Bytes(), &task))
	if task.Kind != "action" || task.CreatedBy != r.a.ID {
		t.Fatal("draft request not assigned to selected agent")
	}
	// The chosen AI follows up through MCP; the engine never fabricates reply prose.
	out, err := r.call(t, "suggest", map[string]any{"circle_id": circle.ID, "kind": "reply", "chat_jid": chatA, "draft_text": "First review reply", "variations": []string{"First review reply", "A shorter reply"}, "why": "Answering the requested review"})
	must(t, err)
	reply := out.(model.Suggestion)
	home := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, home, 200)
	if !strings.Contains(home.Body.String(), reply.ID) {
		t.Fatal("agent draft not queued on Home")
	}
	status(t, r.request(t, "POST", "/v1/suggestions/"+reply.ID+"/send", r.api.ControlToken, map[string]any{"text": "I reviewed the sketches. The layout is ready.", "variation": 1}, nil), 200)
	if len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != "I reviewed the sketches. The layout is ready." {
		t.Fatal("edited approval not delivered exactly once to fake")
	}
	status(t, r.request(t, "POST", "/v1/suggestions/"+reply.ID+"/send", r.api.ControlToken, map[string]any{}, nil), 409)
	denied := queued(t, r, circle, nil)
	status(t, r.request(t, "POST", "/v1/suggestions/"+denied.ID+"/dismiss", r.api.ControlToken, map[string]any{}, nil), 200)
	if len(r.fake.Sent) != 1 {
		t.Fatal("denial invoked messenger")
	}
	status(t, r.request(t, "POST", path+"/done", r.api.ControlToken, nil, nil), 204)
	current, err := r.mc.CircleService().Latest(nil, circle.ID)
	must(t, err)
	if len(current.OpenAsks) != 0 {
		t.Fatal("completed ask revived")
	}
}
