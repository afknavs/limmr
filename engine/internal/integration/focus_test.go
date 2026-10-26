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

func TestFocusCurrentWindowListsSetupAndHints(t *testing.T) {
	r := newRig(t)
	old := epoch.AddDate(-2, 0, 0)
	oldJID := "120363009999@g.us"
	must(t, r.m.PutChat(model.Chat{JID: oldJID, Name: "Former school batch", LastMessageTime: &old}))
	must(t, r.m.PutMessage(model.Message{ID: "old-message", ChatJID: oldJID, Timestamp: old, Sender: chatA, Content: "Old plans"}))
	boundary := epoch.AddDate(0, 0, -180)
	must(t, r.m.PutChat(model.Chat{JID: chatB, Name: "Alex Chen", LastMessageTime: &boundary}))
	parent := "120363008888@g.us"
	must(t, r.m.PutChat(model.Chat{JID: parent, Name: "Campus network"}))
	must(t, r.p.PutGroup(model.Group{JID: parent, Name: "Campus network", IsCommunity: true}))
	must(t, r.p.PutGroup(model.Group{JID: groupA, Name: "Design Club", ParentJID: parent, MemberCount: 1}))
	must(t, r.p.PutGroup(model.Group{JID: oldJID, Name: "Former school batch", ParentJID: parent, MemberCount: 24}))
	out, e := r.call(t, "get_started", map[string]any{})
	must(t, e)
	setup := out.(circles.SetupStatus)
	if setup.CurrentDays != 180 || setup.Chats != 4 || setup.UnsortedActiveChats != 4 || len(setup.Hints.RecentDMs) != 2 || setup.Hints.RecentDMs[0].Messages != 3 || len(setup.Hints.Communities) != 1 || len(setup.Hints.Communities[0].Groups) != 1 || strings.Contains(encode(setup), oldJID) {
		t.Fatal("current setup/hints leaked old chats or missed boundary")
	}
	for _, args := range []map[string]any{{"unsorted": true}, {"sort": "active"}} {
		out, e = r.call(t, "list_chats", args)
		must(t, e)
		if len(out.([]model.Chat)) != 4 {
			t.Fatal("active default includes old/null dates")
		}
	}
	out, e = r.call(t, "list_chats", map[string]any{"unsorted": true, "include_old": true})
	must(t, e)
	if len(out.([]model.Chat)) != 6 {
		t.Fatal("explicit history missing")
	}
	out, e = r.call(t, "list_chats", map[string]any{"sort": "active", "query": "Former school"})
	must(t, e)
	if len(out.([]model.Chat)) != 1 {
		t.Fatal("name query cannot find old chat")
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{groupA}}
	r.agent(t, p)
	out, e = r.call(t, "get_started", map[string]any{})
	must(t, e)
	if strings.Contains(encode(out), parent) || strings.Contains(encode(out), chatB) {
		t.Fatal("hint escaped access")
	}
	p.ReadMessages = false
	r.agent(t, p)
	out, e = r.call(t, "get_started", map[string]any{})
	must(t, e)
	hints := out.(circles.SetupStatus).Hints
	if len(hints.RecentDMs)+len(hints.RecentGroups)+len(hints.Communities) != 0 {
		t.Fatal("hints bypassed read capabilities")
	}

}
func TestFocusSettingsAndQuietCircleBriefStep(t *testing.T) {
	r := newRig(t)
	settings := r.h.Settings()
	if settings.CurrentDays != 180 {
		t.Fatal("legacy default")
	}
	settings.CurrentDays = 30
	status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 200)
	var saved model.Settings
	w := r.request(t, "GET", "/v1/settings", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &saved))
	if saved.CurrentDays != 30 {
		t.Fatal("window not persisted")
	}
	settings.CurrentDays = -1
	status(t, r.request(t, "PUT", "/v1/settings", r.api.ControlToken, settings, nil), 400)
	old := epoch.AddDate(0, 0, -60)
	must(t, r.m.PutChat(model.Chat{JID: chatA, Name: "Maya Rao", LastMessageTime: &old}))
	_, e := r.m.DB.Exec("UPDATE messages SET timestamp=? WHERE chat_jid=?", old, chatA)
	must(t, e)
	_, e = r.m.DB.Exec("UPDATE chats SET last_message_time=? WHERE jid=?", old, chatA)
	must(t, e)
	c := circleFixture(t, r, "friends", "free", false, chatA)
	setup, e := r.mc.CircleService().Setup(&r.a, true)
	must(t, e)
	if setup.Chats != 3 || model.Contains(setup.CirclesWithoutBrief, c.ID) {
		t.Fatal("quiet circle requested first brief")
	}
	page, e := r.mc.CircleService().CatchUp(r.a, c.ID, "", "", 100)
	must(t, e)
	if page.NewMessages != 0 {
		t.Fatal("first catch-up pulled old history")
	}
	page, e = r.mc.CircleService().CatchUp(r.a, c.ID, epoch.AddDate(-2, 0, 0).Format(time.RFC3339), "", 100)
	must(t, e)
	if page.NewMessages != 3 {
		t.Fatal("explicit historical catch-up missing")
	}
}
func TestFocusProposalGuardsAndReviewRelease(t *testing.T) {
	r := newRig(t)
	old := epoch.AddDate(-2, 0, 0)
	j := "120363009999@g.us"
	must(t, r.m.PutChat(model.Chat{JID: j, Name: "Old batch", LastMessageTime: &old}))
	propose := func(chats []string) (any, error) {
		return r.call(t, "propose_circle", map[string]any{"name": "Close people", "kind": "friends", "chats": chats, "people": []string{}, "why": "Owner-known context"})
	}
	for _, chats := range [][]string{{j}, {}} {
		if _, e := propose(chats); !errors.Is(e, model.ErrInvalid) {
			t.Fatal("all-old or empty proposal accepted")
		}
	}
	ids := []string{}
	for i := 0; i < 8; i++ {
		out, e := propose([]string{chatA, j})
		must(t, e)
		ids = append(ids, out.(model.Proposal).ID)
	}
	if _, e := propose([]string{chatA}); e == nil || e.Error() != "Ask the owner to review the suggested circles first." {
		t.Fatal("proposal cap")
	}
	setup, e := r.mc.CircleService().Setup(&r.a, true)
	must(t, e)
	for _, step := range setup.NextSteps {
		if step.Do == "propose_circles" {
			t.Fatal("setup asks for more before review")
		}
	}
	_, e = r.mc.CircleService().ReviewProposals(ids[:1], false)
	must(t, e)
	_, e = propose([]string{chatA})
	must(t, e)
	other, _, e := r.h.CreateAgent("Other AI", "custom", "local")
	must(t, e)
	_, e = r.mc.CircleService().Create(&other, model.Circle{Name: "Work", Kind: "work", Chats: []model.CircleChat{{ChatJID: chatA}}}, "Owner context")
	must(t, e)
}
func TestFocusFirstBriefAndRelationshipEvidence(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "friends", "free", false, chatA)
	_, e := r.call(t, "update_brief", map[string]any{"id": c.ID, "summary": "No evidence", "what_changed": "None"})
	if e == nil || e.Error() != "Catch up first; there's nothing to summarise yet." {
		t.Fatal("empty first brief accepted")
	}
	_, e = r.call(t, "catch_up", map[string]any{"id": c.ID})
	must(t, e)
	id := firstMessage(t, r, chatA).ID
	_, e = r.call(t, "update_brief", map[string]any{"id": c.ID, "summary": "A plan", "what_changed": "New plans", "evidence_message_ids": []string{id}})
	must(t, e)
	_, e = r.call(t, "update_brief", map[string]any{"id": c.ID, "summary": "Existing understanding", "what_changed": "Kept"})
	must(t, e)
	out, e := r.call(t, "add_relation", map[string]any{"a": "me", "b": "maya", "kind": "friend", "evidence_message_ids": []string{id}})
	must(t, e)
	relation := out.(model.Relation)
	if len(relation.EvidenceChats) != 1 || relation.EvidenceChats[0] != chatA {
		t.Fatal("relation evidence missing")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(settings))
	rs, e := r.p.Relations(&r.a)
	must(t, e)
	if len(rs) != 0 {
		t.Fatal("hidden relationship evidence exposed")
	}
}
func TestFocusResetPreservesOwnerDataAndHistory(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "friends", "free", false, chatA)
	_, e := r.mc.CircleService().WriteAdminBrief(c.ID, model.Brief{Summary: "Plan", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID}})
	must(t, e)
	_, e = r.mc.CircleService().Suggest(r.a, model.Suggestion{CircleID: c.ID, Kind: "action", Why: "Review"})
	must(t, e)
	_, e = r.p.AddFact(nil, model.Fact{Subject: "person", SubjectID: "maya", Text: "Likes design"})
	must(t, e)
	_, e = r.p.AddRelation(nil, model.Relation{A: "me", B: "maya", Kind: "friend"})
	must(t, e)
	must(t, r.h.Repo.Put("cursors", "old", epoch))
	must(t, r.h.Repo.Put("catch_pages", "old", circles.Page{}))
	must(t, r.h.Record(model.Activity{AgentID: "you", Kind: "read", Note: "Prior history"}))
	before, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 500})
	must(t, e)
	for _, body := range []map[string]any{{}, {"confirm": false}} {
		status(t, r.request(t, "POST", "/v1/circles/reset", r.api.ControlToken, body, nil), 400)
	}
	status(t, r.request(t, "POST", "/v1/circles/reset", r.token, map[string]any{"confirm": true}, nil), 403)
	status(t, r.request(t, "POST", "/v1/circles/reset", r.api.ControlToken, map[string]any{"confirm": true}, nil), 200)
	for _, bucket := range []string{"circles", "proposals", "briefs", "cursors", "catch_pages", "suggestions"} {
		var n int
		must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket=?", bucket).Scan(&n))
		if n != 0 {
			t.Fatalf("reset left %s", bucket)
		}
	}
	ps, e := r.p.People(nil, "")
	must(t, e)
	facts, e := hub.List[model.Fact](r.h.Repo, "facts")
	must(t, e)
	relations, e := r.p.Relations(nil)
	must(t, e)
	chats, messages := r.m.Counts()
	after, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 500})
	must(t, e)
	if len(ps) != 2 || len(facts) != 1 || len(relations) != 1 || chats != 4 || messages != 12 || len(after) != len(before)+1 || after[0].Tool != "reset_circles" {
		t.Fatal("reset changed owner data/history")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("reset sent a message")
	}
}

func TestFocusOldChatsRankLowerAndDoNotNeedReply(t *testing.T) {
	r := newRig(t)
	old := epoch.AddDate(-2, 0, 0)
	oldJID := "120363009999@g.us"
	must(t, r.m.PutChat(model.Chat{JID: oldJID, Name: "University", LastMessageTime: &old}))
	must(t, r.m.PutMessage(model.Message{ID: "old-university", ChatJID: oldJID, Timestamp: old, Sender: chatA, Content: "An old ask"}))
	must(t, r.m.PutChat(model.Chat{JID: groupA, Name: "University class", LastMessageTime: &epoch}))
	c := circleFixture(t, r, "university", "confirm", false, oldJID)
	_, e := r.mc.CircleService().WriteAdminBrief(c.ID, model.Brief{Summary: "An old plan", CoversUntil: epoch, Evidence: []string{"old-university"}, OpenAsks: []model.OpenAsk{{Text: "Old ask", ChatJID: oldJID, At: old}}})
	must(t, e)
	out, e := r.mc.CircleService().Resolve(r.a, "University")
	must(t, e)
	scores := map[string]float64{}
	for _, candidate := range out.Candidates {
		if candidate.Kind == "chat" {
			scores[candidate.ID] = candidate.Score
		}
	}
	if scores[oldJID] >= .6 || scores[groupA] <= scores[oldJID] {
		t.Fatal("old chat outranked current context")
	}
	v, e := r.mc.CircleService().Home()
	must(t, e)
	if len(v.(map[string]any)["needs_reply"].([]any)) != 0 {
		t.Fatal("old circle ask on Home")
	}
}
