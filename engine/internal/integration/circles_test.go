package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func circleFixture(t *testing.T, r *rig, kind, rule string, asMe bool, jids ...string) model.Circle {
	t.Helper()
	v := model.Circle{Name: "Café Atlas", Kind: kind, SendRule: rule, People: []model.CirclePerson{{PersonID: "maya", Role: "client"}}}
	for _, j := range jids {
		v.Chats = append(v.Chats, model.CircleChat{ChatJID: j, Role: "main"})
	}
	c, e := r.mc.CircleService().Create(nil, v, "Invented fixture")
	must(t, e)
	c, e = r.mc.CircleService().Confirm(c.ID)
	must(t, e)
	return c
}
func TestCircleSendRulesAndStrictest(t *testing.T) {
	for _, rule := range []string{"free", "confirm", "never"} {
		t.Run(rule, func(t *testing.T) {
			r := newRig(t)
			circleFixture(t, r, "project", rule, false, chatA)
			d, e := r.h.DraftMessage(context.Background(), r.a, chatA, "Exact text", "as_agent", true)
			if rule == "never" {
				if e != model.ErrForbidden || len(r.fake.Sent) != 0 {
					t.Fatal("Never bypass")
				}
				return
			}
			must(t, e)
			if rule == "free" {
				if d.State != "sent" || d.ConfirmedVia != "free" || r.fake.Sent[0].Text != d.FinalText {
					t.Fatal("free mismatch")
				}
			} else {
				if d.State != "draft" || len(r.fake.Sent) != 0 {
					t.Fatal("confirm sent")
				}
			}
		})
	}
	r := newRig(t)
	circleFixture(t, r, "project", "free", false, chatA)
	circleFixture(t, r, "work", "confirm", false, chatA)
	d, e := r.h.DraftMessage(context.Background(), r.a, chatA, "Exact", "as_agent", true)
	must(t, e)
	if d.Rule != "confirm" {
		t.Fatal("strictest confirm")
	}
	circleFixture(t, r, "other", "never", false, chatA)
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrForbidden || len(r.fake.Sent) != 0 {
		t.Fatal("Never not rechecked")
	}
}
func TestDraftPermissionLegacyArgumentsAndIDGuards(t *testing.T) {
	r := newRig(t)
	circleFixture(t, r, "self", "free", true, chatA)
	p := r.a.Permissions
	p.RequestSend = false
	r.agent(t, p)
	if _, e := r.h.DraftMessage(context.Background(), r.a, chatA, "x", "as_agent", true); e != model.ErrForbidden {
		t.Fatal("permission bypass")
	}
	p.RequestSend = true
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
	r.agent(t, p)
	if _, e := r.h.DraftMessage(context.Background(), r.a, chatA, "x", "as_agent", true); e != model.ErrNotFound {
		t.Fatal("scope bypass")
	}
	p.Chats = model.Scope{Mode: "all"}
	r.agent(t, p)
	d, e := r.h.DraftMessage(context.Background(), r.a, chatA, "Owner text", "as_me", true)
	must(t, e)
	if d.FinalText != "Owner text" || r.fake.Sent[0].Text != "Owner text" {
		t.Fatal("as_me changed")
	}
	circleFixture(t, r, "family", "free", false, chatA)
	if _, e = r.h.DraftMessage(context.Background(), r.a, chatA, "x", "as_me", true); e != nil {
		t.Fatal("legacy voice was not ignored")
	}
	if _, e = r.call(t, "send_draft", map[string]any{}); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("missing id accepted")
	}
	if _, e = r.call(t, "send_draft", map[string]any{"draft_id": ""}); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("empty id accepted")
	}
	other, _, e := r.h.CreateAgent("Other", "custom", "local")
	must(t, e)
	if _, e = r.h.SendDraft(context.Background(), other, d.ID); e != model.ErrNotFound {
		t.Fatal("draft ownership")
	}
}
func TestBackgroundDraftAndSuggestionDelivery(t *testing.T) {
	r := newRig(t)
	ctx := hub.WithBackground(context.Background(), true)
	d, e := r.h.DraftMessage(ctx, r.a, chatA, "Background text", "as_agent", true)
	must(t, e)
	if d.State != "suggested" || d.SuggestionID == "" || len(r.fake.Sent) != 0 {
		t.Fatal("background not parked")
	}
	sg, e := r.h.Suggestions()
	must(t, e)
	if len(sg) != 1 || sg[0].DraftText != d.FinalText {
		t.Fatal("suggestion lost text")
	}
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrConflict {
		t.Fatal("parked draft sent via chat")
	}
	edit := "Edited exact final"
	d, e = r.h.SendSuggestion(context.Background(), d.SuggestionID, &edit)
	must(t, e)
	if d.ConfirmedVia != "mac" || r.fake.Sent[0].Text != edit {
		t.Fatal("owner edit mismatch")
	}
	if _, e = r.h.SendSuggestion(context.Background(), d.SuggestionID, nil); e != model.ErrConflict {
		t.Fatal("suggestion replay")
	}
}
func TestDraftTwoSecondsAndConcurrentSingleUse(t *testing.T) {
	r := newRig(t)
	d, e := r.h.DraftMessage(context.Background(), r.a, chatA, "Consent text", "as_agent", false)
	must(t, e)
	r.h.Now = func() time.Time { return epoch.Add(1999 * time.Millisecond) }
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrConflict {
		t.Fatal("guard accepted early")
	}
	r.h.Now = func() time.Time { return epoch.Add(2 * time.Second) }
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := r.h.SendDraft(context.Background(), r.a, d.ID); errs <- e }()
	}
	wg.Wait()
	close(errs)
	n := 0
	for e := range errs {
		if e == nil {
			n++
		} else if e != model.ErrConflict {
			t.Fatal(e)
		}
	}
	if n != 1 || len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != d.FinalText {
		t.Fatal("duplicate or changed send")
	}
	acts, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "sent"})
	must(t, e)
	if len(acts) != 1 || acts[0].ConfirmedVia != "chat" || acts[0].AgentID != r.a.ID || acts[0].Note != d.FinalText {
		t.Fatal("confirmation attribution lost")
	}
}
func TestContextRankingAccentFuzzyAndClarification(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	out, e := r.mc.CircleService().Resolve(r.a, "Cafe Atlas")
	must(t, e)
	if out.NeedsClarification || out.Candidates[0].ID != c.ID || out.Candidates[0].Score < .6 {
		t.Fatalf("accent rank: %+v", out)
	}
	out, e = r.mc.CircleService().Resolve(r.a, "Cafe Atla")
	must(t, e)
	if out.Candidates[0].ID != c.ID {
		t.Fatal("fuzzy rank")
	}
	out, e = r.mc.CircleService().Resolve(r.a, "unrelated zzzz")
	must(t, e)
	if !out.NeedsClarification {
		t.Fatal("weak guess")
	}
	circleFixture(t, r, "project", "confirm", false, chatB)
	out, e = r.mc.CircleService().Resolve(r.a, "Cafe Atlas")
	must(t, e)
	if !out.NeedsClarification {
		t.Fatal("near tie not clarified")
	}
	for _, x := range out.Candidates {
		if strings.Contains(x.Why, "conversation") {
			t.Fatal("invented conversational history")
		}
	}
}
func TestCatchUpPagingBriefVersionRevertAndScope(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA, chatB)
	s := r.mc.CircleService()
	// Timestamp ties across chats must not be dropped at page boundaries.
	for i, j := range []string{chatA, chatB} {
		must(t, r.m.PutMessage(model.Message{ID: "tie", ChatJID: j, Sender: chatA, Content: "Tie", Timestamp: epoch.Add(-10 * time.Second)}))
		_ = i
	}
	page, e := s.CatchUp(r.a, c.ID, "", "", 2)
	must(t, e)
	if page.Next == "" || !page.Cursor.IsZero() {
		t.Fatal("premature cursor")
	}
	seen := map[string]bool{}
	total := 0
	for {
		for _, g := range page.Chats {
			for _, m := range g.Messages {
				k := g.ChatJID + "/" + m.Message.ID
				if seen[k] {
					t.Fatal("page duplicate")
				}
				seen[k] = true
				total++
				if !m.Message.IsFromMe && m.Role != "client" {
					t.Fatal("role missing")
				}
			}
		}
		if page.Next == "" {
			break
		}
		page, e = s.CatchUp(r.a, c.ID, "", page.Next, 2)
		must(t, e)
	}
	if total != 8 || !page.Cursor.Equal(epoch) {
		t.Fatalf("missed messages %d", total)
	}
	b, e := s.WriteBrief(&r.a, c.ID, model.Brief{Summary: "A short stored understanding.", WhatChanged: "New plans.", Evidence: []string{firstMessage(t, r, chatA).ID}})
	must(t, e)
	if b.Version != 1 || b.WrittenBy != r.a.ID {
		t.Fatal("version attribution")
	}
	b.Summary = "Changed understanding."
	b, e = s.WriteBrief(&r.a, c.ID, b)
	must(t, e)
	if b.Version != 2 {
		t.Fatal("no version")
	}
	b, e = s.Revert(c.ID, 1)
	must(t, e)
	if b.Version != 3 || b.Summary != "A short stored understanding." {
		t.Fatal("revert rewrote history")
	}
	bs, e := s.Briefs(nil, c.ID)
	must(t, e)
	if len(bs) != 3 {
		t.Fatal("lost brief history")
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	bs, e = s.Briefs(&r.a, c.ID)
	must(t, e)
	if len(bs) != 0 {
		t.Fatal("mixed brief leaked")
	}
	page, e = s.CatchUp(r.a, c.ID, "", "", 100)
	must(t, e)
	if page.NewMessages != 4 {
		t.Fatal("scope cursor inherited inaccessible history")
	}
	for _, g := range page.Chats {
		if g.ChatJID != chatA {
			t.Fatal("hidden chat returned")
		}
	}
	b, e = s.WriteBrief(&r.a, c.ID, model.Brief{Summary: "Visible chat only."})
	must(t, e)
	if len(b.ChatJIDs) != 1 || b.ChatJIDs[0] != chatA {
		t.Fatal("scoped brief not pinned")
	}
}
func TestCircleToolsHideExcludedChatsAndAudit(t *testing.T) {
	r := newRig(t)
	must(t, r.p.PutPerson(model.Person{ID: "nora", Name: "Nora Vale", JIDs: []string{groupB}, UpdatedAt: epoch}))
	c, e := r.mc.CircleService().Create(nil, model.Circle{
		Name: "Garden friends", Kind: "friends", SendRule: "confirm",
		Chats:  []model.CircleChat{{ChatJID: chatB}, {ChatJID: groupB}},
		People: []model.CirclePerson{{PersonID: "maya"}, {PersonID: "alex"}, {PersonID: "nora"}},
	}, "Lasting garden friendships")
	must(t, e)
	c, e = r.mc.CircleService().Confirm(c.ID)
	must(t, e)
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	calls := map[string]map[string]any{
		"resolve_context": {"query": "Alex"}, "list_circles": {}, "get_circle": {"id": c.ID}, "get_brief": {"id": c.ID}, "catch_up": {"id": c.ID},
		"update_brief": {"id": c.ID, "summary": "x", "what_changed": "x"}, "suggest": {"circle_id": c.ID, "kind": "reply", "chat_jid": chatB, "draft_text": "x", "why": "x"},
		"propose_circle":  {"name": "Hidden", "kind": "friends", "chats": []string{chatB, groupB}, "people": []string{"maya", "alex", "nora"}, "why": "x"},
		"set_person_role": {"circle_id": c.ID, "person_id": "maya", "role": "x"}, "add_topic": {"circle_id": c.ID, "title": "x"}, "update_topic": {"circle_id": c.ID, "topic_id": "x"},
		"draft_message": {"chat": chatB, "text": "x", "voice": "as_agent"}, "send_draft": {"draft_id": "unknown"},
	}
	for name, args := range calls {
		out, e := r.call(t, name, args)
		if name == "resolve_context" || name == "list_circles" {
			must(t, e)
			raw := encode(out)
			if strings.Contains(raw, chatB) || strings.Contains(raw, c.ID) || strings.Contains(raw, "Alex Chen") {
				t.Fatal("excluded resource leaked")
			}
		} else if e != model.ErrNotFound {
			t.Fatalf("%s: %v", name, e)
		}
	}
	acts, e := r.h.Activity(&r.a, hub.ActivityFilter{Limit: 100})
	must(t, e)
	if len(acts) != len(calls) {
		t.Fatal("every new tool must log a row")
	}
}
func TestCircleControlCRUDAndHomeMap(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	for _, path := range []string{"/v1/home", "/v1/map", "/v1/circles", "/v1/circles/" + c.ID, "/v1/circles/" + c.ID + "/briefs", "/v1/suggestions", "/v1/settings/briefs", "/v1/refresh-plan"} {
		status(t, r.request(t, "GET", path, r.api.ControlToken, nil, nil), 200)
		status(t, r.request(t, "GET", path, r.token, nil, nil), 403)
	}
	base := "/v1/circles/" + c.ID
	status(t, r.request(t, "POST", base+"/chats", r.api.ControlToken, map[string]any{"chat_jid": chatB, "role_in_circle": "client"}, nil), 200)
	status(t, r.request(t, "PATCH", base+"/chats/"+chatB, r.api.ControlToken, map[string]any{"role_in_circle": "manager"}, nil), 200)
	status(t, r.request(t, "DELETE", base+"/chats/"+chatB, r.api.ControlToken, nil, nil), 200)
	status(t, r.request(t, "POST", base+"/people", r.api.ControlToken, map[string]any{"person_id": "alex", "role": "contractor"}, nil), 200)
	status(t, r.request(t, "PATCH", base+"/people/alex", r.api.ControlToken, map[string]any{"role": "client"}, nil), 200)
	status(t, r.request(t, "DELETE", base+"/people/alex", r.api.ControlToken, nil, nil), 200)
	status(t, r.request(t, "POST", base+"/topics", r.api.ControlToken, map[string]any{"title": "Tiles", "chats": []string{chatA}}, nil), 200)
	got, e := r.mc.CircleService().Circle(nil, c.ID)
	must(t, e)
	topic := got.Topics[0].ID
	status(t, r.request(t, "PATCH", base+"/topics/"+topic, r.api.ControlToken, map[string]any{"status": "done"}, nil), 200)
	status(t, r.request(t, "DELETE", base+"/topics/"+topic, r.api.ControlToken, nil, nil), 200)
	status(t, r.request(t, "PATCH", base, r.api.ControlToken, map[string]any{"send_rule": "never"}, nil), 200)
	status(t, r.request(t, "PATCH", base, r.api.ControlToken, map[string]any{"confirmed": true}, nil), 400)
	status(t, r.request(t, "PUT", "/v1/settings/briefs", r.api.ControlToken, map[string]any{"enabled": true, "time": "25:99"}, nil), 400)
	status(t, r.request(t, "PUT", "/v1/settings/briefs", r.api.ControlToken, map[string]any{"enabled": true, "time": "07:30"}, nil), 200)
	status(t, r.request(t, "DELETE", base, r.api.ControlToken, nil, nil), 200)
}
func TestDiscoveryNeverProposesStructure(t *testing.T) {
	r := newRig(t)
	s := r.mc.CircleService()
	_, e := s.Setup(&r.a, true)
	must(t, e)
	_, e = s.Home()
	must(t, e)
	_, e = s.Resolve(r.a, "Design Club")
	must(t, e)
	ps, e := s.Proposals()
	must(t, e)
	if len(ps) != 0 {
		t.Fatal("discovery created circles")
	}
}
func TestCircleToolAnnotationsAndBackgroundHeader(t *testing.T) {
	r := newRig(t)
	for _, tool := range r.mc.Tools(r.a) {
		if tool.Annotations == nil {
			t.Fatal("missing annotations")
		}
		if tool.Name == "get_brief" && !tool.Annotations.ReadOnlyHint {
			t.Fatal("read marked action")
		}
		if tool.Name == "send_draft" && (tool.Annotations.ReadOnlyHint || !*tool.Annotations.OpenWorldHint) {
			t.Fatal("send hints wrong")
		}
	}
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "draft_message", "arguments": map[string]any{"chat": chatA, "text": "Draft", "voice": "as_agent"}}}
	w := r.request(t, "POST", "/mcp", r.token, body, map[string]string{"X-Limmr-Background": "1", "Accept": "application/json, text/event-stream"})
	status(t, w, 200)
	var result struct {
		Result struct {
			Data       model.Draft `json:"unused"`
			Structured struct {
				Data model.Draft `json:"data"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &result))
	if result.Result.Structured.Data.State != "suggested" || len(r.fake.Sent) != 0 {
		t.Fatal("header background ignored")
	}
}

func TestSuccessfulCircleProtocolEveryToolAudited(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	s := r.mc.CircleService()
	run := func(name string, args map[string]any) any {
		t.Helper()
		out, e := r.call(t, name, args)
		must(t, e)
		return out
	}
	run("resolve_context", map[string]any{"query": "Cafe Atlas"})
	run("list_circles", map[string]any{})
	run("get_circle", map[string]any{"id": c.ID})
	run("get_brief", map[string]any{"id": c.ID})
	run("catch_up", map[string]any{"id": c.ID})
	run("update_brief", map[string]any{"id": c.ID, "summary": "The revised sketches are ready.", "what_changed": "Three messages arrived.", "evidence_message_ids": []string{firstMessage(t, r, chatA).ID}, "upcoming": []map[string]string{{"date": "2026-10-20", "text": "Review"}}, "people_notes": []map[string]string{{"person_id": "maya", "contribution": "Reviewed sketches"}}})
	suggestion := run("suggest", map[string]any{"circle_id": c.ID, "kind": "reply", "chat_jid": chatA, "draft_text": "The sketches are ready.", "why": "A review was requested."}).(model.Suggestion)
	if suggestion.DraftText != "The sketches are ready." {
		t.Fatal("suggestion body changed")
	}
	run("propose_circle", map[string]any{"name": "Studio", "kind": "project", "chats": []string{groupB}, "people": []string{}, "why": "Shared work."})
	run("set_person_role", map[string]any{"circle_id": c.ID, "person_id": "maya", "role": "reviewer"})
	run("add_topic", map[string]any{"circle_id": c.ID, "title": "Sketch review", "chats": []string{chatA}, "people": []string{"maya"}, "keywords": []string{"sketch"}})
	circle, e := s.Circle(&r.a, c.ID)
	must(t, e)
	run("update_topic", map[string]any{"circle_id": c.ID, "topic_id": circle.Topics[0].ID, "status": "watching"})
	d := run("draft_message", map[string]any{"chat": chatA, "text": "The sketches are ready.", "voice": "as_agent"}).(model.Draft)
	r.h.Now = func() time.Time { return epoch.Add(2 * time.Second) }
	run("send_draft", map[string]any{"draft_id": d.ID})
	rows, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 100})
	must(t, e)
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Kind == "denied" {
			t.Fatal("successful tool rejected")
		}
		seen[row.Tool] = true
		seen[row.CalledAs] = true
	}
	for _, name := range []string{"resolve_context", "list_circles", "get_circle", "get_brief", "catch_up", "update_brief", "suggest", "propose_circle", "set_person_role", "add_topic", "update_topic", "draft_message", "send_draft"} {
		if !seen[name] {
			t.Fatal("missing tool activity", name)
		}
	}
}
func TestPartialBriefDoesNotAdvanceWholeCirclePlan(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA, chatB)
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	s := r.mc.CircleService()
	_, e := s.CatchUp(r.a, c.ID, "", "", 100)
	must(t, e)
	_, e = s.WriteBrief(&r.a, c.ID, model.Brief{Summary: "One chat's news.", Evidence: []string{firstMessage(t, r, chatA).ID}})
	must(t, e)
	plan, e := s.Plan()
	must(t, e)
	if len(plan.Circles) != 1 || plan.Circles[0].NewMessages != 6 {
		t.Fatal("partial brief hid unprocessed chats")
	}
}
func TestBriefRejectsInvisibleEvidenceFutureCoverageAndLongSummary(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	s := r.mc.CircleService()
	_, e := s.CatchUp(r.a, c.ID, "", "", 100)
	must(t, e)
	for _, b := range []model.Brief{{Summary: "x", Evidence: []string{firstMessage(t, r, chatB).ID}}, {Summary: strings.Repeat("word ", 121)}, {Summary: "x", CoversUntil: epoch.Add(time.Second)}} {
		if _, e = s.WriteBrief(&r.a, c.ID, b); e == nil {
			t.Fatal("invalid brief accepted")
		}
	}
	_, e = r.call(t, "update_brief", map[string]any{"id": c.ID, "summary": "x", "what_changed": "x", "upcoming": []map[string]any{{"date": "2026-10-20", "text": 123}}})
	if !errors.Is(e, model.ErrInvalid) {
		t.Fatal("nested argument type confusion")
	}
}

func TestCatchUpKeepsSubmillisecondMessagesOrdered(t *testing.T) {
	r := newRig(t)
	_, e := r.m.DB.Exec("DELETE FROM messages")
	must(t, e)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	// SQLite julianday rounds these instants into one bucket; IDs deliberately
	// sort in the opposite order to timestamps.
	base := epoch.Add(-time.Second)
	for i, id := range []string{"z", "y", "x"} {
		must(t, r.m.PutMessage(model.Message{ID: id, ChatJID: chatA, Sender: chatA, Content: "Invented", Timestamp: base.Add(time.Duration(i) * time.Nanosecond)}))
	}
	ids := []string{}
	token := ""
	for {
		page, e := r.mc.CircleService().CatchUp(r.a, c.ID, "", token, 1)
		must(t, e)
		for _, g := range page.Chats {
			for _, m := range g.Messages {
				ids = append(ids, m.Message.ID)
			}
		}
		token = page.Next
		if token == "" {
			break
		}
	}
	if strings.Join(ids, ",") != "z,y,x" {
		t.Fatal("submillisecond paging reordered or lost messages")
	}
}

func TestResolveUsesOnlyCallingAgentsCursor(t *testing.T) {
	r := newRig(t)
	recent := circleFixture(t, r, "project", "confirm", false, chatA)
	older := circleFixture(t, r, "project", "confirm", false, chatB)
	s := r.mc.CircleService()
	before, e := s.Resolve(r.a, "Cafe Atlas")
	must(t, e)
	if before.Candidates[0].ID != recent.ID {
		t.Fatal("recency not ranked")
	}
	_, e = s.CatchUp(r.a, older.ID, "", "", 100)
	must(t, e)
	after, e := s.Resolve(r.a, "Cafe Atlas")
	must(t, e)
	if after.Candidates[0].ID != older.ID || !strings.Contains(after.Candidates[0].Why, "your own") {
		t.Fatal("own cursor not ranked")
	}
	other, _, e := r.h.CreateAgent("Independent AI", "custom", "local")
	must(t, e)
	out, e := s.Resolve(other, "Cafe Atlas")
	must(t, e)
	if out.Candidates[0].ID != recent.ID {
		t.Fatal("another agent inherited cursor")
	}
}
func TestBackgroundAgentAndLatePermissionChanges(t *testing.T) {
	r := newRig(t)
	nightly, _, e := r.h.CreateAgent("limmr-nightly", "codex", "local")
	must(t, e)
	d, e := r.h.DraftMessage(context.Background(), nightly, chatA, "Night draft", "as_agent", true)
	must(t, e)
	if d.State != "suggested" {
		t.Fatal("nightly agent not background")
	}
	p := nightly.Permissions
	p.RequestSend = false
	nightly.Permissions = p
	must(t, r.h.UpdateAgent(nightly))
	if _, e = r.h.SendSuggestion(context.Background(), d.SuggestionID, nil); e != model.ErrForbidden || len(r.fake.Sent) != 0 {
		t.Fatal("suggestion ignored revoked permission")
	}
	d, e = r.h.DraftMessage(context.Background(), r.a, chatA, "Normal draft", "as_agent", false)
	must(t, e)
	p = r.a.Permissions
	p.RequestSend = false
	r.agent(t, p)
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	if _, e = r.h.SendDraft(context.Background(), r.a, d.ID); e != model.ErrForbidden {
		t.Fatal("draft ignored revoked permission")
	}
}

func TestNightlyFreeDraftAlwaysParksWhileOwnerSendWorks(t *testing.T) {
	r := newRig(t)
	circleFixture(t, r, "project", "free", false, chatA)
	a, _, e := r.h.CreateAgent("limmr-nightly", "codex", "local")
	must(t, e)
	d, e := r.h.DraftMessage(context.Background(), a, chatA, "Ready for review.", "as_agent", true)
	must(t, e)
	if d.State != "suggested" || len(r.fake.Sent) != 0 {
		t.Fatal("nightly sent into free circle")
	}
	d, e = r.h.SendSuggestion(context.Background(), d.SuggestionID, nil)
	must(t, e)
	if d.State != "sent" || d.ConfirmedVia != "mac" || len(r.fake.Sent) != 1 {
		t.Fatal("owner could not send nightly suggestion")
	}
	// A foreground free draft passed to the nightly agent's send_draft path is
	// parked too, so no two-step combination can bypass the background identity.
	r.a.Name = "limmr-nightly"
	must(t, r.h.UpdateAgent(r.a))
	d, e = r.h.DraftMessage(context.Background(), r.a, chatA, "Another review.", "as_agent", false)
	must(t, e)
	if d.State != "suggested" || len(r.fake.Sent) != 1 {
		t.Fatal("nightly second path sent")
	}
}
