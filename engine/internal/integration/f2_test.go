package integration_test

import (
	"encoding/json"
	"errors"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func f2Circle() model.Circle {
	return model.Circle{Name: "Design friends", Kind: "friends", Chats: []model.CircleChat{{ChatJID: chatA}, {ChatJID: groupA}}, People: []model.CirclePerson{{PersonID: "maya"}, {PersonID: "alex"}}}
}
func TestF2PresetsAndLocalDefaults(t *testing.T) {
	r := newRig(t)
	a := r.a
	p := a.Permissions
	if a.Preset != "own" || p.Chats.Mode != "all" || p.Access.Circles != nil || !p.Access.Unsorted || p.Access.Until != nil || !p.ReadMessages || !p.Search || !p.ReadPeople || !p.WritePeople || !p.SaveSummaries || !p.DownloadMedia || !p.RequestSend {
		t.Fatal("own must have full permissions")
	}
	for _, kind := range []string{"claude_code", "codex", "cursor", "custom"} {
		a, _, e := r.h.CreateAgent("Example", kind, "local")
		must(t, e)
		if a.Preset != "own" || !a.Permissions.WritePeople {
			t.Fatal(kind)
		}
	}
	p = model.Preset("given")
	if p.Access.Circles == nil || p.Access.Unsorted || p.WritePeople || p.SaveSummaries || p.RequestSend || p.DownloadMedia {
		t.Fatal("given not limited")
	}
	for _, preset := range []string{"given", "own"} {
		w := r.request(t, "POST", "/v1/agents/"+r.a.ID+"/preset", r.api.ControlToken, map[string]string{"preset": preset}, nil)
		status(t, w, 200)
		a, e := r.h.Agent(r.a.ID)
		must(t, e)
		if a.Preset != preset || a.Permissions.RequestSend != (preset == "own") {
			t.Fatal("switch did not persist")
		}
	}
	status(t, r.request(t, "POST", "/v1/agents/"+r.a.ID+"/preset", r.api.ControlToken, map[string]string{"preset": "unknown"}, nil), 400)
	v, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	if !strings.Contains(encode(v), "Your own AI: full access") {
		t.Fatal("whoami summary")
	}
}
func TestF2RemotePresetApproval(t *testing.T) {
	for _, preset := range []string{"own", "given", ""} {
		t.Run(preset, func(t *testing.T) {
			r, s := remoteRig(t)
			f := register(t, s, "ChatGPT fixture")
			authorize(t, s, &f)
			body := map[string]any{"code": f.code, "new_agent": map[string]string{"name": "ChatGPT", "kind": "chatgpt"}}
			if preset != "" {
				body["preset"] = preset
				body["access"] = map[string]any{"circles": []string{}, "unsorted": false}
				body["fill_in"] = false
			}
			status(t, r.request(t, "POST", "/v1/remote/approve", r.api.ControlToken, body, nil), 200)
			for _, client := range s.Status().Clients {
				if client.ID == f.client {
					a, e := r.h.Agent(client.AgentID)
					must(t, e)
					own := preset != "given"
					if (a.Preset == "own") != own || a.Permissions.WritePeople != own || a.Permissions.RequestSend != own || (a.Permissions.Access.Circles == nil) != own {
						t.Fatal("remote preset not applied")
					}
				}
			}
			if len(r.fake.Sent) > 0 {
				t.Fatal("approval sent a message")
			}
		})
	}
}
func TestF2OwnerCRUDSuggestionsAndSetupOrder(t *testing.T) {
	r := newRig(t)
	status(t, r.request(t, "GET", "/v1/owner", r.api.StatusToken, nil, nil), 403)
	out, e := r.call(t, "get_started", map[string]any{})
	must(t, e)
	if out.(circles.SetupStatus).NextSteps[0].Do != "about_you" {
		t.Fatal("about_you must be first")
	}
	status(t, r.request(t, "PUT", "/v1/owner", r.api.ControlToken, map[string]any{"name": "Alex", "studies": "Northfield University, BSc Psychology", "about": "I like design", "people": []map[string]any{{"name": "Maya Rao", "relation": "sister", "person_id": "maya"}}, "projects": []map[string]any{{"name": "Design Club 2026", "with": []string{"Maya Rao"}}}}, nil), 200)
	settings := r.h.Settings()
	settings.Owner = "Old name"
	must(t, r.h.SetSettings(settings))
	if r.h.Settings().Owner != "Alex" {
		t.Fatal("stale general settings overwrote About you name")
	}
	if p, e := r.p.Profile(nil, "me"); e != nil || p.Person.Name != "Alex" {
		t.Fatal("About you must be owner person")
	}
	out, e = r.call(t, "get_started", map[string]any{})
	must(t, e)
	v := out.(circles.SetupStatus)
	if v.OwnerProfile.Thin() || v.NextSteps[0].Do == "about_you" {
		t.Fatal("confirmed context not used")
	}
	_, e = r.call(t, "update_owner_profile", map[string]any{"fields": map[string]any{"studies": "Other University", "work": "Library assistant"}})
	must(t, e)
	_, e = r.call(t, "update_owner_profile", map[string]any{"fields": map[string]any{"people": []map[string]any{{"name": "Nora", "relation": "best friend", "person_id": nil}}, "projects": []map[string]any{{"name": "Design Club 2026", "with": []string{"Nora"}}}}})
	must(t, e)
	profile := r.h.OwnerProfile()
	if profile.Studies != "Northfield University, BSc Psychology" || profile.Work != "" || len(profile.Suggestions) != 4 || profile.UpdatedBy != r.a.ID {
		t.Fatal("AI overwrote owner words")
	}
	for _, s := range profile.Suggestions {
		decision := "dismiss"
		if s.Field == "work" {
			decision = "confirm"
		}
		status(t, r.request(t, "POST", "/v1/owner/suggestions/"+s.ID+"/"+decision, r.api.ControlToken, nil, nil), 200)
	}
	profile = r.h.OwnerProfile()
	if profile.Work != "Library assistant" || len(profile.Suggestions) != 0 || profile.UpdatedBy != "you" {
		t.Fatal("review failed")
	}
	status(t, r.request(t, "PUT", "/v1/owner", r.api.ControlToken, map[string]string{"about": strings.Repeat("a", 601)}, nil), 400)
	status(t, r.request(t, "PUT", "/v1/owner", r.api.ControlToken, map[string]string{"unknown": "value"}, nil), 400)
	status(t, r.request(t, "PUT", "/v1/owner", r.api.ControlToken, map[string]string{"work": "New role"}, nil), 200)
	if r.h.OwnerProfile().Studies != profile.Studies {
		t.Fatal("merge lost field")
	}
	rows, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 100})
	must(t, e)
	found := false
	for _, a := range rows {
		found = found || a.Tool == "remember" && a.CalledAs == "update_owner_profile" && a.AgentID == r.a.ID
	}
	if !found {
		t.Fatal("missing AI attribution log")
	}
}
func TestF2OwnerContextResolutionAndScope(t *testing.T) {
	r := newRig(t)
	status(t, r.request(t, "PUT", "/v1/owner", r.api.ControlToken, map[string]any{"studies": "Northfield University, BSc Psychology", "people": []map[string]any{{"name": "Maya Rao", "relation": "sister", "person_id": "maya"}, {"name": "Alex Chen", "relation": "mum", "person_id": "alex"}}}, nil), 200)
	c := circleFixture(t, r, "university", "confirm", false, groupA)
	_, e := r.mc.CircleService().Mutate(c.ID, func(c *model.Circle) error { c.Name = "Northfield University"; return nil })
	must(t, e)
	for _, q := range []string{"my sister", "mum", "my university"} {
		v, e := r.call(t, "resolve_context", map[string]any{"query": q})
		must(t, e)
		out := v.(circles.Resolution)
		if len(out.Candidates) == 0 || out.NeedsClarification {
			t.Fatalf("profile alias unresolved: %s", q)
		}
	}
	out, e := r.call(t, "find_person", map[string]any{"query": "my sister"})
	must(t, e)
	if ps := out.([]model.Person); len(ps) != 1 || ps[0].ID != "maya" {
		t.Fatal("sister alias")
	}
	a := r.a
	a.Permissions.Access = model.Access{Circles: model.CircleSelection{}, Unsorted: false}
	must(t, r.h.UpdateAgent(a))
	out, e = r.call(t, "get_owner_profile", map[string]any{})
	must(t, e)
	if strings.Contains(encode(out), "BSc Psychology") || strings.Contains(encode(out), "sister") {
		t.Fatal("scoped profile leak")
	}
}
func TestF2CircleProtocolRules(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*model.Circle)
		why    string
		review bool
		reject bool
	}{
		{"valid", func(*model.Circle) {}, "Maya Rao and Design Club are current conversations", false, false},
		{"single DM", func(c *model.Circle) { c.Chats = c.Chats[:1]; c.People = nil }, "Maya Rao", false, false},
		{"single DM family", func(c *model.Circle) { c.Kind = "family"; c.Chats = c.Chats[:1] }, "Maya Rao", false, false},
		{"single DM work", func(c *model.Circle) { c.Kind = "work"; c.Chats = c.Chats[:1] }, "Maya Rao", true, false},
		{"project goal", func(c *model.Circle) {
			c.Kind = "project"
			c.Goal = "Build a reading room"
			c.Chats = c.Chats[:1]
			c.People = nil
		}, "Maya Rao is building the reading room", true, false},
		{"single DM with two people", func(c *model.Circle) { c.Chats = c.Chats[:1] }, "Maya Rao", false, false},
		{"no evidence", func(*model.Circle) {}, "The owner probably needs this", true, false},
		{"opaque name", func(c *model.Circle) { c.Name = "PSY C 2026-27 community" }, "Design Club", true, false},
		{"old chats", func(c *model.Circle) { c.Chats = []model.CircleChat{{ChatJID: "15550100999@s.whatsapp.net"}} }, "Old group", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t)
			old := epoch.AddDate(-2, 0, 0)
			must(t, r.m.PutChat(model.Chat{JID: "15550100999@s.whatsapp.net", Name: "Old group", LastMessageTime: &old}))
			c := f2Circle()
			test.change(&c)
			v, e := r.mc.CircleService().Create(&r.a, c, test.why)
			if test.reject {
				if e == nil {
					t.Fatal("old proposal accepted")
				}
				return
			}
			must(t, e)
			if v.Confirmed == test.review {
				t.Fatal("wrong approval path")
			}
			if !test.review && v.ConfirmedBy != "auto" {
				t.Fatal("auto provenance")
			}
			if test.name == "project goal" && v.ReviewReason != "One-off projects are tasks: use task create." {
				t.Fatal("one-chat project must direct the agent to task creation")
			}
			if test.name == "single DM work" && !strings.Contains(v.ReviewReason, "Close people") {
				t.Fatal("single work DM must direct the agent to Close people")
			}
		})
	}
}
func TestF2OverlapAutoReviewLimitUndoAndAdditions(t *testing.T) {
	r := newRig(t)
	svc := r.mc.CircleService()
	c, e := svc.Create(&r.a, f2Circle(), "Maya Rao and Design Club")
	must(t, e)
	_, e = svc.Create(&r.a, f2Circle(), "Design Club")
	if !errors.Is(e, model.ErrConflict) || !strings.Contains(e.Error(), "add_to_circle") {
		t.Fatal("duplicate accepted")
	}
	changes, e := svc.AutoChanges()
	must(t, e)
	if len(changes) != 1 {
		t.Fatal("missing undo")
	}
	must(t, svc.UndoAuto(changes[0].ID))
	if _, e = svc.Circle(nil, c.ID); e != model.ErrNotFound {
		t.Fatal("undo did not delete")
	}
	c, e = svc.Create(&r.a, f2Circle(), "Design Club")
	must(t, e)
	before := c
	_, e = svc.AddTo(r.a, c.ID, []string{groupB}, nil, "Garden Club is part of the same design project")
	must(t, e)
	changes, e = svc.AutoChanges()
	must(t, e)
	for _, ch := range changes {
		if ch.Action == "updated" {
			must(t, svc.UndoAuto(ch.ID))
		}
	}
	restored, e := svc.Circle(nil, c.ID)
	must(t, e)
	if len(restored.Chats) != len(before.Chats) {
		t.Fatal("addition undo failed")
	}
	_, e = svc.AddTo(r.a, c.ID, []string{groupB}, nil, "Garden Club")
	must(t, e)
	changes, e = svc.AutoChanges()
	must(t, e)
	_, e = svc.Mutate(c.ID, func(c *model.Circle) error { c.Name = "Owner name"; return nil })
	must(t, e)
	for _, ch := range changes {
		if ch.Action == "updated" && svc.UndoAuto(ch.ID) == nil {
			t.Fatal("undo overwrote owner edit")
		}
	}
	settings := r.h.Settings()
	off := false
	settings.AutoCircles = &off
	must(t, r.h.SetSettings(settings))
	other := f2Circle()
	other.Chats = []model.CircleChat{{ChatJID: chatB}, {ChatJID: groupB}}
	other.People = nil
	// No overlap with the kept circle except groupB: use a new fixture for settings tests.
	r2 := newRig(t)
	settings = r2.h.Settings()
	settings.AutoCircles = &off
	must(t, r2.h.SetSettings(settings))
	v, e := r2.mc.CircleService().Create(&r2.a, f2Circle(), "Design Club")
	must(t, e)
	if v.Confirmed {
		t.Fatal("Ask me first ignored")
	}
	r3 := newRig(t)
	settings = r3.h.Settings()
	settings.MaxCircles = 1
	must(t, r3.h.SetSettings(settings))
	circleFixture(t, r3, "family", "free", false, chatB)
	cc := f2Circle()
	cc.People = nil
	v, e = r3.mc.CircleService().Create(&r3.a, cc, "Design Club")
	must(t, e)
	if v.Confirmed || !strings.Contains(v.ReviewReason, "Ask the owner") {
		t.Fatal("limit ignored")
	}
}
func TestF2GivenCircleAndAdditionReview(t *testing.T) {
	r := newRig(t)
	r.a.Preset = "given"
	must(t, r.h.UpdateAgent(r.a))
	svc := r.mc.CircleService()
	c, e := svc.Create(&r.a, f2Circle(), "Design Club")
	must(t, e)
	if c.Confirmed {
		t.Fatal("given auto-confirmed")
	}
	c, e = svc.Confirm(c.ID)
	must(t, e)
	v, e := svc.AddTo(r.a, c.ID, []string{groupB}, nil, "Garden Club")
	must(t, e)
	addition := v.(circles.Addition)
	c, e = svc.Circle(nil, c.ID)
	must(t, e)
	if len(c.Chats) != 2 {
		t.Fatal("given applied directly")
	}
	_, e = svc.ReviewAddition(addition.ID, true)
	must(t, e)
	c, e = svc.Circle(nil, c.ID)
	must(t, e)
	if len(c.Chats) != 3 {
		t.Fatal("owner review not applied")
	}
}
func TestF2ArchiveSuggestionKeepsBriefsAndLeavesSetup(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, groupA)
	svc := r.mc.CircleService()
	_, e := svc.Suggest(r.a, model.Suggestion{CircleID: c.ID, Kind: "archive", Why: "Quiet project"})
	if e == nil {
		t.Fatal("active circle archived")
	}
	_, e = svc.WriteBrief(nil, c.ID, model.Brief{Summary: "An old project", Evidence: []string{firstMessage(t, r, groupA).ID}, CoversUntil: epoch})
	must(t, e)
	r.h.Now = func() time.Time { return epoch.AddDate(0, 0, 61) }
	_, e = svc.Suggest(r.a, model.Suggestion{CircleID: c.ID, Kind: "archive", Why: "No messages in 60 days"})
	must(t, e)
	_, e = svc.Mutate(c.ID, func(c *model.Circle) error { c.Archived = true; return nil })
	must(t, e)
	setup, e := svc.Setup(nil, true)
	must(t, e)
	if setup.Circles.Confirmed != 0 {
		t.Fatal("archived setup count")
	}
	v, e := svc.Home()
	must(t, e)
	if strings.Contains(encode(v.(map[string]any)["circles"]), c.ID) {
		t.Fatal("archived Home card")
	}
	if _, e = svc.Circle(nil, c.ID); e != nil {
		t.Fatal("archive deleted circle")
	}
	briefs, e := svc.Briefs(nil, c.ID)
	must(t, e)
	if len(briefs) != 1 {
		t.Fatal("archive discarded briefs")
	}
}
func TestF2BusinessesAndOneWayDMsAreChatsNotPeople(t *testing.T) {
	r := newRig(t)
	for i, name := range []string{"Northfield Bank", "Broker app", "Campus newsletter", "Nora Example"} {
		id := []string{"bank", "broker", "newsletter", "nora"}[i]
		j := "1555010080" + string(rune('0'+i)) + "@s.whatsapp.net"
		must(t, r.p.PutPerson(model.Person{ID: id, JIDs: []string{j}, Name: name, Business: i < 2, Broadcast: i == 2}))
		must(t, r.m.PutChat(model.Chat{JID: j, Name: name, LastMessageTime: &epoch}))
		must(t, r.m.PutMessage(model.Message{ID: "incoming-" + id, ChatJID: j, Sender: j, Timestamp: epoch, Content: "A notice"}))
	}
	ps, e := r.p.People(nil, "")
	must(t, e)
	for _, p := range ps {
		if p.ID == "bank" || p.ID == "broker" || p.ID == "newsletter" || p.ID == "nora" {
			t.Fatal("one-way sender surfaced")
		}
	}
	ps, e = r.p.People(nil, "Northfield Bank")
	must(t, e)
	if len(ps) != 0 {
		t.Fatal("business searchable as person")
	}
	for _, name := range []string{"Northfield Bank", "Broker app", "Campus newsletter"} {
		out, e := r.call(t, "find_person", map[string]any{"query": name})
		must(t, e)
		if len(out.([]model.Person)) != 0 {
			t.Fatal("MCP business person surfaced")
		}
	}
	mapped, e := r.mc.CircleService().Map()
	must(t, e)
	if strings.Contains(encode(mapped), "Northfield Bank") || strings.Contains(encode(mapped), "Broker app") {
		t.Fatal("business surfaced on map")
	}
	// Contact names remain stable when business verification arrives later.
	must(t, r.p.MarkBusiness(chatA))
	var maya model.Person
	must(t, r.h.Repo.Get("people", "maya", &maya))
	if maya.Name != "Maya Rao" {
		t.Fatal("business metadata renamed contact")
	}

	out, e := r.call(t, "list_chats", map[string]any{"query": "Northfield Bank"})
	must(t, e)
	if !strings.Contains(encode(out), "Northfield Bank") {
		t.Fatal("business chat lost")
	}
	_, e = r.p.Update(nil, "bank", map[string]string{"notes": "My bank adviser"})
	must(t, e)
	ps, e = r.p.People(nil, "Northfield Bank")
	must(t, e)
	if len(ps) != 1 {
		t.Fatal("explicit details did not surface business")
	}
	must(t, r.m.PutMessage(model.Message{ID: "owner-reply", Content: "A reply", ChatJID: "15550100803@s.whatsapp.net", Timestamp: epoch, IsFromMe: true}))
	ps, e = r.p.People(nil, "")
	must(t, e)
	if !strings.Contains(encode(ps), "Nora Example") {
		t.Fatal("owner-sent DM not relevant")
	}
	must(t, r.p.PutMembership(model.Membership{Group: groupB, Person: "nora"}))
	scoped := r.a
	scoped.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{groupB}}
	scopedPeople, e := r.p.People(&scoped, "")
	must(t, e)
	if strings.Contains(encode(scopedPeople), "Nora Example") {
		t.Fatal("excluded outgoing DM affected scoped relevance")
	}
	_, e = r.p.Profile(nil, "broker")
	if e != model.ErrNotFound {
		t.Fatal("business profile leaked")
	}
	w := r.request(t, "GET", "/v1/people?all=true", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "Broker app") {
		t.Fatal("picker leaked business")
	}
	var payload map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &payload))
	if len(r.fake.Sent) != 0 {
		t.Fatal("test sent a WhatsApp message")
	}
}
