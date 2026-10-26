package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/demo"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"limmr/internal/watches"
	"strings"
	"testing"
	"time"
)

func queued(t *testing.T, r *rig, c model.Circle, voice *string) model.Suggestion {
	t.Helper()
	x, e := r.mc.CircleService().Suggest(r.a, model.Suggestion{CircleID: c.ID, Kind: "reply", ChatJID: chatA, DraftText: "First body", Variations: []string{"First body", "Second body", "Third body"}, Why: "Invented review"})
	must(t, e)
	return x
}
func TestHomeV2OwnerSendAndAudit(t *testing.T) {
	for _, rule := range []string{"confirm", "never"} {
		t.Run(rule, func(t *testing.T) {
			r := newRig(t)
			c := circleFixture(t, r, "university", rule, false, chatA)
			x := queued(t, r, c, nil)
			w := r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{"voice": "as_me", "variation": 1}, nil)
			if rule == "never" {
				status(t, w, 403)
				if len(r.fake.Sent) != 0 {
					t.Fatal("Never delivered")
				}
				return
			}
			status(t, w, 200)
			if len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != "Second body" {
				t.Fatal("owner send was not delivered unchanged")
			}
			acts, e := r.h.Activity(nil, hub.ActivityFilter{Kind: "sent"})
			must(t, e)
			if len(acts) != 1 || acts[0].Description != "sent by you from Limmr" || acts[0].DraftedBy != r.a.Name || acts[0].Edited == nil || *acts[0].Edited || acts[0].Variation == nil || *acts[0].Variation != 1 || acts[0].ConfirmedVia != "mac" {
				t.Fatal("owner attribution lost")
			}
			status(t, r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{}, nil), 409)
		})
	}
}
func TestHomeV2PlainEditedVariation(t *testing.T) {
	for _, voice := range []string{"as_agent", "as_me", "unknown"} {
		t.Run(voice, func(t *testing.T) {
			r := newRig(t)
			c := circleFixture(t, r, "university", "confirm", false, chatA)
			x := queued(t, r, c, nil)
			text := "Edited body\n— My own closing"
			w := r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{"text": text, "voice": voice, "signed": true, "variation": 2}, nil)
			status(t, w, 200)
			if r.fake.Sent[0].Text != text {
				t.Fatal("edited text changed")
			}
			var d model.Draft
			must(t, json.Unmarshal(w.Body.Bytes(), &d))
			if !d.Edited || d.Variation != 2 {
				t.Fatal("edit provenance mismatch")
			}
		})
	}
}
func TestHomeV2VariationBoundsAndVisibility(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "university", "confirm", false, chatA)
	x := queued(t, r, c, nil)
	for _, index := range []int{-1, 3, 100} {
		status(t, r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{"variation": index, "text": "Owner edit"}, nil), 400)
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("invalid variation sent")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(settings))
	status(t, r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{"voice": "as_me"}, nil), 404)
	if len(r.fake.Sent) != 0 {
		t.Fatal("hidden chat sent")
	}
}
func TestHomeV2PlainDefaultsAndPins(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "family", "free", false, chatA)
	x := queued(t, r, c, nil)
	if x.Body != "First body" || x.Variations[0] != x.Body {
		t.Fatal("body mismatch")
	}
	status(t, r.request(t, "PATCH", "/v1/circles/"+c.ID, r.api.ControlToken, map[string]any{"pinned": true}, nil), 200)
	var current model.Circle
	must(t, r.h.Repo.Get("circles", c.ID, &current))
	if !current.Pinned {
		t.Fatal("pin lost")
	}
	status(t, r.request(t, "PATCH", "/v1/circles/"+c.ID, r.api.ControlToken, map[string]any{"pinned": nil}, nil), 400)
	_, err := r.mc.CircleService().Mutate(c.ID, func(c *model.Circle) error { c.SendRule = "never"; return nil })
	must(t, err)
	status(t, r.request(t, "POST", "/v1/suggestions/"+x.ID+"/send", r.api.ControlToken, map[string]any{"voice": "as_agent"}, nil), 403)
}
func TestHomeV2MCPVariationsAndAskMetadata(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "university", "confirm", false, chatA)
	for _, name := range []string{"suggest", "draft_message"} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"variations": []string{"First", "Second"}}
			if name == "suggest" {
				args["circle_id"] = c.ID
				args["kind"] = "reply"
				args["why"] = "Review"
				args["chat_jid"] = chatA
				args["draft_text"] = "First"
				args["voice"] = "as_agent"
			} else {
				args["chat"] = chatA
				args["text"] = "First"
				args["voice"] = "as_agent"
			}
			_, e := r.call(t, name, args)
			must(t, e)
			for _, bad := range [][]string{{"a", "b", "c", "d"}, {strings.Repeat("x", 1001)}, {""}} {
				args["variations"] = bad
				if _, e = r.call(t, name, args); e == nil {
					t.Fatal("invalid variations accepted")
				}
			}
		})
	}
	// Background draft variations survive parking.
	d, e := r.h.DraftMessageVariations(hub.WithBackground(context.Background(), true), r.a, chatA, "First", "as_agent", false, []string{"First", "Second"})
	must(t, e)
	xs, e := r.h.Suggestions()
	must(t, e)
	found := false
	for _, x := range xs {
		if x.ID == d.SuggestionID {
			found = true
			if len(x.Variations) != 2 {
				t.Fatal("park lost variations")
			}
		}
	}
	if !found {
		t.Fatal("missing parked draft")
	}
	_, e = r.call(t, "catch_up", map[string]any{"id": c.ID})
	must(t, e)
	out, e := r.call(t, "update_brief", map[string]any{"id": c.ID, "summary": "A review is due.", "evidence_message_ids": []string{firstMessage(t, r, chatA).ID}, "what_changed": "An ask arrived.", "open_asks": []any{"Legacy string", map[string]any{"text": "Check sketches", "chat_jid": chatA, "from": "Maya", "at": epoch.Format(time.RFC3339)}}})
	must(t, e)
	b := out.(model.Brief)
	if len(b.OpenAsks) != 2 || b.OpenAsks[0].ID == "" || b.OpenAsks[1].Chat != "Maya Rao" || b.OpenAsks[1].From != "Maya" {
		t.Fatal("MCP ask metadata lost")
	}
}
func TestHomeV2DoneAskVersionAndRestart(t *testing.T) {
	r := newRig(t)
	s := r.mc.CircleService()
	c := circleFixture(t, r, "work", "confirm", false, chatA)
	ask := model.OpenAsk{Text: "Review the sketches", ChatJID: chatA, From: "Maya", At: epoch}
	b, e := s.WriteBrief(nil, c.ID, model.Brief{Summary: "Review due", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID}, OpenAsks: []model.OpenAsk{ask}})
	must(t, e)
	id := b.OpenAsks[0].ID
	if id != circles.AskID(c.ID, ask.Text, chatA) {
		t.Fatal("unstable id")
	}
	status(t, r.request(t, "POST", "/v1/circles/"+c.ID+"/asks/"+id+"/done", r.api.ControlToken, nil, nil), 204)
	check := func(s *circles.Service, want int) {
		t.Helper()
		b, e := s.Latest(nil, c.ID)
		must(t, e)
		if len(b.OpenAsks) != want {
			t.Fatal("done state revived or did not reappear")
		}
	}
	check(s, 0)
	// Independent database handle/service models a fresh process read.
	repo, e := hub.Open(r.home + "/hub.db")
	must(t, e)
	defer repo.Close()
	h := hub.New(repo, r.m, r.home)
	h.Now = r.h.Now
	fresh := &circles.Service{Hub: h, People: r.p}
	check(fresh, 0)
	must(t, repo.Put("briefs", fmt.Sprintf("%s:%09d", c.ID, b.Version), b))
	check(fresh, 0)
	_, e = s.WriteBrief(nil, c.ID, model.Brief{Summary: "Nothing outstanding"})
	must(t, e)
	check(s, 0)
	next, e := s.WriteBrief(nil, c.ID, model.Brief{Summary: "Review still needed", OpenAsks: []model.OpenAsk{ask}})
	must(t, e)
	if next.OpenAsks[0].ID != id {
		t.Fatal("new version changed id")
	}
	check(s, 1)
	status(t, r.request(t, "POST", "/v1/circles/"+c.ID+"/asks/unknown/done", r.api.ControlToken, nil, nil), 404)
	status(t, r.request(t, "POST", "/v1/circles/"+c.ID+"/asks/"+id+"/done", r.token, nil, nil), 403)
}
func TestHomeV2LiveCircles(t *testing.T) {
	r := newRig(t)
	s := r.mc.CircleService()
	pin := circleFixture(t, r, "work", "confirm", false, chatB)
	_, e := s.Mutate(pin.ID, func(c *model.Circle) error { c.Pinned = true; return nil })
	must(t, e)
	quiet := circleFixture(t, r, "work", "confirm", false, groupB)
	ask := circleFixture(t, r, "work", "confirm", false, chatA)
	_, e = s.WriteBrief(nil, ask.ID, model.Brief{Summary: "An ask", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID}, OpenAsks: []model.OpenAsk{{Text: "Check this", ChatJID: chatA}}})
	must(t, e)
	busy := circleFixture(t, r, "work", "confirm", false, groupA)
	_, e = s.WriteBrief(nil, busy.ID, model.Brief{Summary: "Earlier messages covered", Evidence: []string{firstMessage(t, r, groupA).ID}, CoversUntil: epoch.Add(-90 * time.Minute)})
	must(t, e)
	for i := 0; i < 5; i++ {
		must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("busy-%d", i), ChatJID: groupA, Sender: chatA, Content: "Invented", Timestamp: epoch.Add(-time.Hour)}))
	}
	// A brief covers existing quiet messages; quiet has no asks even with upcoming.
	_, e = s.WriteBrief(nil, quiet.ID, model.Brief{Summary: "Quiet", Evidence: []string{firstMessage(t, r, groupB).ID}, CoversUntil: epoch, Upcoming: []model.Upcoming{{Date: "2026-10-20", Text: "Later"}}})
	must(t, e)
	// Ask and busy circles plus more live circles exercise the cap.
	for i := 0; i < 4; i++ {
		c := circleFixture(t, r, "work", "confirm", false, chatA)
		_, e = s.WriteBrief(nil, c.ID, model.Brief{Summary: "Ask", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID}, OpenAsks: []model.OpenAsk{{Text: fmt.Sprintf("Ask %d", i)}}})
		must(t, e)
	}
	v, e := s.Home()
	must(t, e)
	home := v.(map[string]any)
	cards := home["circles"].([]map[string]any)
	if len(cards) != 4 || cards[0]["circle"].(model.Circle).ID != pin.ID || cards[0]["why_shown"] != "pinned" {
		t.Fatal("pinned ordering/live cap")
	}
	for _, card := range cards {
		if card["circle"].(model.Circle).ID == quiet.ID {
			t.Fatal("quiet circle shown")
		}
	}
	// Remove the extra asks to inspect the exact busy boundary.
	cs, e := s.List(nil)
	must(t, e)
	for _, c := range cs {
		if c.ID != pin.ID && c.ID != quiet.ID && c.ID != ask.ID && c.ID != busy.ID {
			must(t, s.Delete(c.ID))
		}
	}
	assertBusy := func(want bool) {
		t.Helper()
		v, e := s.Home()
		must(t, e)
		seen := false
		for _, card := range v.(map[string]any)["circles"].([]map[string]any) {
			if card["circle"].(model.Circle).ID == busy.ID {
				seen = true
				if card["why_shown"] != "busy" {
					t.Fatal("busy reason")
				}
			}
		}
		if seen != want {
			t.Fatal("live rule mismatch")
		}
	}
	assertBusy(true)
	_, e = r.m.DB.Exec("DELETE FROM messages WHERE chat_jid=? AND id='busy-0'", groupA)
	must(t, e)
	// Four recent messages plus old/future rows do not make a circle live.
	for i, at := range []time.Time{epoch.Add(-25 * time.Hour), epoch.Add(time.Hour)} {
		must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("outside-window-%d", i), ChatJID: groupA, Content: "Invented", Timestamp: at}))
	}
	assertBusy(false)
	must(t, r.m.PutMessage(model.Message{ID: "busy-0", ChatJID: groupA, Content: "Invented", Timestamp: epoch.Add(-time.Hour)}))
	assertBusy(true)
	_, e = s.WriteBrief(nil, busy.ID, model.Brief{Summary: "Caught up", CoversUntil: epoch})
	must(t, e)
	assertBusy(false)
	r.h.Now = func() time.Time { return epoch.Add(25 * time.Hour) }
	assertBusy(false)
}

func TestHomeV2OwnerHandoffKeepsGrantChecks(t *testing.T) {
	r := newRig(t)
	s, c, requests := setupWatches(t, r)
	watch := createHandoff(t, r, &triggers.QuietHours{Start: "23:00", End: "08:00", Timezone: "UTC"})
	req := wake(t, s, c, requests)
	ctx := runContext(t, r, req)
	circleFixture(t, r, "university", "confirm", false, chatA)
	draft, e := r.h.DraftMessage(ctx, r.a, chatA, "Review this reply", "as_agent", true)
	must(t, e)
	if draft.State != "suggested" {
		t.Fatal("handoff did not park")
	}
	status(t, r.request(t, "POST", "/v1/suggestions/"+draft.SuggestionID+"/send", r.api.ControlToken, map[string]any{"voice": "as_me"}, nil), 200)
	if len(r.fake.Sent) != 1 || r.fake.Sent[0].Text != "Review this reply" {
		t.Fatal("owner handoff payload changed")
	}
	detail, e := s.Detail(context.Background(), watch.ID)
	must(t, e)
	if detail.Stats.Replies != 1 {
		t.Fatal("owner handoff reply not counted")
	}
	next, e := r.h.DraftMessage(ctx, r.a, chatA, "Another reply", "as_agent", true)
	must(t, e)
	must(t, s.Status(context.Background(), watch.ID, triggers.Paused))
	w := r.request(t, "POST", "/v1/suggestions/"+next.SuggestionID+"/send", r.api.ControlToken, map[string]any{"voice": "as_me"}, nil)
	if w.Code == 200 || len(r.fake.Sent) != 1 {
		t.Fatal("owner send bypassed paused grant")
	}
}

func TestHomeV2DemoHTTPHandlerContract(t *testing.T) {
	r := newRig(t)
	must(t, demo.Seed(r.h, r.p, r.link))
	s, e := watches.New(r.h, r.p, watches.Options{Runner: demo.WatchRunner{}, Clock: demo.WatchClock{}})
	must(t, e)
	defer s.Engine.Close()
	r.mc.Watches = s
	must(t, demo.SeedWatches(s))
	var home struct {
		Queued     []model.Suggestion `json:"queued"`
		NeedsReply []model.OpenAsk    `json:"needs_reply"`
		Circles    []struct {
			Circle model.Circle `json:"circle"`
			Pinned bool         `json:"pinned"`
			Why    string       `json:"why_shown"`
		} `json:"circles"`
		Activity []model.Activity  `json:"recent_activity"`
		Watches  []watches.Summary `json:"watches"`
		Today    []any             `json:"today"`
		Cards    []any             `json:"cards"`
	}
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if len(home.Queued) != 3 || len(home.NeedsReply) != 3 || len(home.Circles) != 7 || len(home.Activity) != 6 || len(home.Watches) != 3 || len(home.Today) != 7 || len(home.Cards) != 7 {
		t.Fatal("Home demo contract counts")
	}
	pins := map[string]bool{}
	for i, c := range home.Circles {
		if i < 2 {
			if !c.Pinned || c.Why != "pinned" {
				t.Fatal("pin presentation")
			}
			pins[c.Circle.ID] = true
		} else if c.Pinned || (c.Circle.ID == "flatmates" && c.Why != "open_ask") {
			t.Fatal("live demo circle")
		}
	}
	if !pins["university"] || !pins["atlas"] {
		t.Fatal("demo pins")
	}
	for _, x := range home.Queued {
		if len(x.Variations) < 2 || x.Variations[0] != x.Body {
			t.Fatal("demo body/variations")
		}
	}
	for _, a := range home.NeedsReply {
		if a.ID == "" || a.ChatJID == "" || a.Chat == "" || a.From == "" || a.At.IsZero() {
			t.Fatal("demo ask metadata")
		}
	}
	status(t, r.request(t, "POST", "/v1/suggestions/suggestion-1/send", r.api.ControlToken, map[string]any{"text": "Edited train update", "voice": "as_me", "variation": 1}, nil), 200)
	status(t, r.request(t, "POST", "/v1/suggestions/suggestion-2/send", r.api.ControlToken, map[string]any{"voice": "as_agent", "variation": 2}, nil), 200)
	if len(r.fake.Sent) != 2 || r.fake.Sent[0].Text != "Edited train update" || strings.Contains(r.fake.Sent[1].Text, "on behalf of") {
		t.Fatal("demo fake send bodies")
	}
}
