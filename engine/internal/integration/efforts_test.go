package integration_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func TestEffortsMCPRoutesAndPermissions(t *testing.T) {
	r := newRig(t)
	must(t, r.h.Messages.PutMessage(model.Message{ID: "explicit-deadline", ChatJID: chatA, Sender: chatA, Content: "CANARY_MESSAGE Saturday sketches. Due 2026-10-22.", Timestamp: epoch}))
	run := func(action string, args map[string]any) (any, error) {
		return r.call(t, "more", map[string]any{"action": action, "args": args})
	}
	if len(r.mc.Tools(r.a)) != 20 {
		t.Fatal("top-level catalog changed")
	}
	v, e := run("efforts.list", map[string]any{"q": "Saturday"})
	must(t, e)
	xs := v.([]efforts.Effort)
	if len(xs) == 0 {
		t.Fatal("efforts missing")
	}
	id := xs[0].ID
	_, e = run("efforts.get", map[string]any{"id": id})
	must(t, e)
	v, e = run("efforts.split", map[string]any{"id": id, "message_refs": xs[0].MessageRefs[:1]})
	must(t, e)
	split := v.(efforts.Effort)
	_, e = run("efforts.merge", map[string]any{"id": id, "other_id": split.ID})
	must(t, e)
	_, e = run("efforts.attach", map[string]any{"id": id, "message_refs": xs[0].MessageRefs[:1]})
	must(t, e)
	v, e = run("efforts.update", map[string]any{"id": id, "title": "Weekend sketches", "summary": "Choose sketches tomorrow.", "status": "on_you", "due": "2026-10-22T00:00:00Z"})
	must(t, e)
	if v.(efforts.Effort).CreatedBy != r.a.Name {
		t.Fatal("agent attribution missing")
	}
	_, e = run("efforts.update", map[string]any{"id": id, "title": strings.Repeat("x", 41)})
	if e == nil {
		t.Fatal("accepted long title")
	}
	_, e = run("efforts.attach", map[string]any{"id": id, "message_refs": []map[string]any{{"chat_jid": "missing", "message_id": "no"}}})
	if e == nil {
		t.Fatal("attached inaccessible message")
	}
	r.a.Permissions.SaveSummaries = false
	must(t, r.h.UpdateAgent(r.a))
	_, e = run("efforts.update", map[string]any{"id": id, "title": "Denied"})
	if e == nil {
		t.Fatal("write permission ignored")
	}
	r.a.Permissions.ReadMessages = false
	must(t, r.h.UpdateAgent(r.a))
	_, e = run("efforts.list", map[string]any{})
	if e == nil {
		t.Fatal("read permission ignored")
	}
	status(t, r.request(t, "GET", "/v1/efforts", r.api.StatusToken, nil, nil), 403)
	status(t, r.request(t, "GET", "/v1/efforts", r.token, nil, nil), 403)
	w := r.request(t, "GET", "/v1/efforts?q=Saturday", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var list struct {
		Efforts []efforts.Effort `json:"efforts"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &list))
	if len(list.Efforts) == 0 {
		t.Fatal("API search empty")
	}
	status(t, r.request(t, "GET", "/v1/efforts/"+id, r.api.ControlToken, nil, nil), 200)
	status(t, r.request(t, "PATCH", "/v1/efforts/"+id, r.api.ControlToken, map[string]any{"title": "Owner title", "pinned": true}, nil), 200)
	status(t, r.request(t, "PATCH", "/v1/efforts/"+id, r.api.ControlToken, map[string]any{"status": "invalid"}, nil), 400)
	status(t, r.request(t, "GET", "/v1/efforts?limit=-1", r.api.ControlToken, nil, nil), 400)
}
func TestEffortHomeOrderingAndHiddenProjection(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 3; i++ {
		must(t, r.h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("lab-%d", i), ChatJID: chatA, Sender: chatA, Content: "Experiment laboratory method. Due 2026-10-17. Due 2026-10-22.", IsFromMe: i == 1, Timestamp: epoch.Add(-time.Duration(2-i) * time.Minute)}))
	}
	circleFixture(t, r, "university", "confirm", false, chatA, groupA)
	xs, e := r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, e)
	if len(xs) < 2 {
		t.Fatal("need two test purposes")
	}
	for i, x := range xs {
		st := "moving"
		due := ""
		if i == 1 {
			st = "on_you"
			due = ""
		}
		_, e = r.mc.EffortService().Update(nil, x.ID, efforts.Update{Title: ptrEffortTitle(x.Title), Status: &st, Due: &due})
		must(t, e)
	}
	_, e = r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	must(t, e)
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var home struct {
		Updates []efforts.CircleUpdate `json:"circle_updates"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if len(home.Updates) == 0 || home.Updates[0].Status != "on_you" {
		t.Fatal("on-you does not precede due soon")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA, groupA}
	must(t, r.h.SetSettings(settings))
	w = r.request(t, "GET", "/v1/efforts?q=Maya", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "Weekend sketches") {
		t.Fatal("hidden edit leaked")
	}
	r.a.Permissions = model.Defaults(false)
	r.a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
	must(t, r.h.UpdateAgent(r.a))
	v, e := r.call(t, "more", map[string]any{"action": "efforts.list", "args": map[string]any{}})
	must(t, e)
	for _, x := range v.([]efforts.Effort) {
		for _, c := range x.Chats {
			if c.ID != chatB {
				t.Fatal("scope leaked")
			}
		}
	}
	status(t, r.request(t, "POST", "/v1/efforts/seen", r.api.ControlToken, nil, nil), 200)
}

func ptrEffortTitle(s string) *string {
	if s == "" {
		s = "Weekend sketches"
	}
	return &s
}

func TestInternalEffortNamingRoutes(t *testing.T) {
	r := newRig(t)
	for i, text := range []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"} {
		must(t, r.h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("internal-%d", i), ChatJID: chatA, Sender: chatA, IsFromMe: i == 1, Content: text, Timestamp: epoch.Add(-time.Duration(i+1) * time.Hour)}))
	}
	var response struct {
		Efforts []efforts.Effort `json:"efforts"`
	}
	w := r.request(t, "GET", "/v1/efforts?q=budget", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &response))
	if len(response.Efforts) != 0 {
		t.Fatal("unnamed purpose surfaced in Activity")
	}
	w = r.request(t, "GET", "/v1/efforts?q=budget&untitled=true", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &response))
	if len(response.Efforts) != 1 {
		t.Fatal("local naming cannot discover internal purpose")
	}
	x := response.Efforts[0]
	status(t, r.request(t, "GET", "/v1/efforts?untitled=true", r.api.StatusToken, nil, nil), 403)
	w = r.request(t, "PATCH", "/v1/efforts/"+x.ID, r.api.ControlToken, map[string]any{"title": "Festival budget", "summary": "Maya asked about the budget proposal.", "only_unnamed": true, "expected_last_at": x.LastAt.Format(time.RFC3339Nano)}, nil)
	status(t, w, 200)
	var named efforts.Effort
	must(t, json.Unmarshal(w.Body.Bytes(), &named))
	if named.Title != "Festival budget" || named.CreatedBy != "This Mac" {
		t.Fatal("local naming was not attributed")
	}
	w = r.request(t, "GET", "/v1/efforts?q=budget", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &response))
	if len(response.Efforts) != 1 {
		t.Fatal("named effort did not surface")
	}
}

func TestE3NamingQueueAndNaturalHints(t *testing.T) {
	r := newRig(t)
	setupWatches(t, r)
	_, err := r.h.Messages.DB.Exec("DELETE FROM messages")
	must(t, err)
	for i, text := range []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"} {
		must(t, r.h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("e3-%d", i), ChatJID: chatA, Sender: chatA, IsFromMe: i == 1, Content: text, Timestamp: epoch.Add(-time.Duration(3-i) * time.Hour)}))
	}
	run := func(action string, args map[string]any) (any, error) {
		return r.call(t, "more", map[string]any{"action": action, "args": args})
	}
	out, err := run("efforts.unnamed", map[string]any{"limit": 1})
	must(t, err)
	queue := out.(map[string]any)
	xs := queue["efforts"].([]efforts.NamingCandidate)
	if queue["total"].(int) != 1 || len(xs) != 1 {
		t.Fatal("queue missing")
	}
	data, err := json.Marshal(xs)
	must(t, err)
	if strings.Contains(string(data), "snippets") || strings.Contains(string(data), "budget is for") {
		t.Fatal("agent got message text")
	}
	w := r.request(t, "GET", "/v1/efforts/unnamed", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var owner struct {
		Efforts []efforts.NamingCandidate `json:"efforts"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &owner))
	if len(owner.Efforts) != 1 || len(owner.Efforts[0].Snippets) != 3 {
		t.Fatal("owner snippets missing")
	}
	status(t, r.request(t, "GET", "/v1/efforts/unnamed", r.api.StatusToken, nil, nil), 403)
	circle := circleFixture(t, r, "university", "confirm", false, chatA)
	_, err = r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	must(t, err)
	brief, err := r.call(t, "brief", map[string]any{"action": "get", "id": circle.ID})
	must(t, err)
	if !strings.Contains(brief.(map[string]any)["effort_naming_hint"].(string), "1 efforts waiting for names") {
		t.Fatal("brief hint missing")
	}
	_, err = r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	must(t, err)
	updatePage := updates(t, r, map[string]any{})
	if !strings.Contains(updatePage.EffortNamingHint, "1 efforts waiting for names") {
		t.Fatal("updates hint missing")
	}
	r.a.Permissions.SaveSummaries = false
	must(t, r.h.UpdateAgent(r.a))
	_, err = run("efforts.update", map[string]any{"id": xs[0].ID, "title": "Festival budget"})
	if err == nil {
		t.Fatal("write permission ignored")
	}
	if updates(t, r, map[string]any{}).EffortNamingHint != "" {
		t.Fatal("read-only agent nudged to write")
	}
	r.a.Permissions.SaveSummaries = true
	must(t, r.h.UpdateAgent(r.a))
	named, err := run("efforts.update", map[string]any{"id": xs[0].ID, "title": "Festival budget", "summary": "Maya asked for a review."})
	must(t, err)
	if named.(efforts.Effort).CreatedBy != r.a.Name {
		t.Fatal("agent naming not credited")
	}
	if updates(t, r, map[string]any{}).EffortNamingHint != "" {
		t.Fatal("stale hint after naming")
	}
	out, err = run("efforts.unnamed", map[string]any{})
	must(t, err)
	if out.(map[string]any)["total"].(int) != 0 {
		t.Fatal("named effort queued")
	}
}
