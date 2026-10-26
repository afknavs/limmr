package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"limmr/internal/circles"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/triggers"
	"net/url"
	"testing"
	"time"
)

func mapResponse(t *testing.T, r *rig, path string) circles.UnderstandingMap {
	t.Helper()
	w := r.request(t, "GET", path, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var m circles.UnderstandingMap
	must(t, json.Unmarshal(w.Body.Bytes(), &m))
	return m
}
func TestUnderstandingOriginsAndAuthenticatedRoleHistory(t *testing.T) {
	r := newRig(t)
	out, e := r.call(t, "propose_circle", map[string]any{"name": "Atlas renovation", "kind": "work", "chats": []string{chatA, groupA}, "people": []string{"maya"}, "why": "Materials collaboration"})
	must(t, e)
	c := out.(model.Proposal).Circle
	pending := mapResponse(t, r, "/v1/map")
	found := false
	for _, edge := range pending.Edges {
		if edge.Source == c.ID {
			found = true
			if edge.State != "suggested" || edge.Origin.By != r.a.Name || edge.Origin.ByKind != "agent" {
				t.Fatal("proposal provenance missing")
			}
		}
	}
	if !found {
		t.Fatal("pending links absent")
	}
	status(t, r.request(t, "POST", "/v1/circles/"+c.ID+"/confirm", r.api.ControlToken, map[string]any{}, nil), 200)
	_, e = r.call(t, "set_person_role", map[string]any{"circle_id": c.ID, "person_id": "maya", "role": "Materials collaborator"})
	must(t, e)
	_, e = r.call(t, "add_topic", map[string]any{"circle_id": c.ID, "title": "Tile samples", "chats": []string{chatA}, "people": []string{"maya"}})
	must(t, e)
	m := mapResponse(t, r, "/v1/map")
	kinds := map[string]bool{}
	roleID := ""
	for _, edge := range m.Edges {
		kinds[edge.Origin.ByKind] = true
		if edge.Kind == "role" && edge.Source == c.ID {
			roleID = edge.ID
			if edge.Origin.By != r.a.Name || edge.Origin.ChangeID == "" || edge.Label != "Materials collaborator" {
				t.Fatal("role origin missing")
			}
		}
	}
	if !kinds["agent"] || !kinds["you"] || !kinds["structure"] {
		t.Fatal("missing origin kinds")
	}
	events := map[string]bool{}
	for _, ev := range m.Events {
		events[ev.Kind] = true
	}
	for _, kind := range []string{"circle_proposed", "circle_confirmed", "role_set", "topic_added"} {
		if !events[kind] {
			t.Fatalf("missing %s event", kind)
		}
	}
	// The app cannot retain the AI's previous authorship on an owner role edit.
	status(t, r.request(t, "PATCH", "/v1/circles/"+c.ID+"/people/maya", r.api.ControlToken, map[string]any{"role": "Client"}, nil), 200)
	m = mapResponse(t, r, "/v1/map")
	history := 0
	for _, edge := range m.Edges {
		if edge.ID == roleID && (edge.Origin.ByKind != "you" || edge.Origin.By != "You" || edge.Label != "Client") {
			t.Fatal("owner role attributed to AI")
		}
	}
	for _, ev := range m.Events {
		if ev.Kind == "role_set" {
			history++
		}
	}
	if history != 2 {
		t.Fatal("role history overwritten")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("map caused send")
	}
}
func TestUnderstandingPeopleChangesAndReverts(t *testing.T) {
	for _, kind := range []string{"person", "fact", "relation", "self_relation"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			circleFixture(t, r, "project", "confirm", false, chatA, groupA)
			var id string
			switch kind {
			case "person":
				_, e := r.p.Update(&r.a, "maya", map[string]string{"notes": "Coordinates samples"})
				must(t, e)
				cs, e := r.p.Changes()
				must(t, e)
				id = cs[0].ID
			case "fact":
				f, e := r.p.AddFact(&r.a, model.Fact{Subject: "person", SubjectID: "maya", Text: "Coordinates samples", EvidenceMessageIDs: []string{firstMessage(t, r, chatA).ID}})
				must(t, e)
				id = f.ID
			case "relation", "self_relation":
				rel, e := r.p.AddRelation(&r.a, model.Relation{A: map[bool]string{true: "me", false: "maya"}[kind == "self_relation"], B: "alex", Kind: "collaborator"})
				must(t, e)
				id = rel.ID
			}
			m := mapResponse(t, r, "/v1/map")
			found := false
			for _, edge := range m.Edges {
				if edge.Origin.ChangeID == id {
					found = true
					if edge.Origin.ByKind != "agent" || edge.Origin.By != r.a.Name {
						t.Fatal("change author missing")
					}
					if kind == "fact" && edge.Origin.Evidence != 1 {
						t.Fatal("evidence count lost")
					}
				}
			}
			if kind == "person" {
				if found {
					t.Fatal("unconfirmed note suggestion created a map edge")
				}
				profile, e := r.p.Profile(nil, "maya")
				must(t, e)
				if profile.Person.Notes != "" || len(profile.Person.DetailSuggestions) != 1 {
					t.Fatal("agent note is not pending review")
				}
			} else if !found {
				t.Fatal("change edge missing")
			}
			must(t, r.p.Revert(id))
			for _, edge := range mapResponse(t, r, "/v1/map").Edges {
				if edge.Origin.ChangeID == id {
					t.Fatal("reverted edge remains")
				}
			}
		})
	}
}
func TestUnderstandingBriefVersionsAndRevert(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	cs := r.mc.CircleService()
	b, e := cs.WriteBrief(nil, c.ID, model.Brief{Summary: "Materials ready", CoversUntil: epoch, PeopleNotes: []model.PeopleNote{{PersonID: "maya", Contribution: "Selected stone"}}, Evidence: []string{firstMessage(t, r, chatA).ID}})
	must(t, e)
	b.WrittenBy = r.a.ID
	must(t, r.h.Repo.Put("briefs", fmt.Sprintf("%s:%09d", c.ID, b.Version), b))
	m := mapResponse(t, r, "/v1/map")
	found := false
	for _, edge := range m.Edges {
		if edge.Kind == "contribution" {
			found = true
			if edge.Origin.BriefVersion != 1 || edge.Origin.Evidence != 1 || edge.Origin.ByKind != "agent" {
				t.Fatal("brief provenance")
			}
		}
	}
	if !found {
		t.Fatal("missing brief connection")
	}
	_, e = cs.WriteBrief(nil, c.ID, model.Brief{Summary: "No current contribution", CoversUntil: epoch})
	must(t, e)
	for _, edge := range mapResponse(t, r, "/v1/map").Edges {
		if edge.Kind == "contribution" {
			t.Fatal("superseded contribution remains")
		}
	}
	_, e = cs.Revert(c.ID, 1)
	must(t, e)
	m = mapResponse(t, r, "/v1/map")
	found = false
	for _, edge := range m.Edges {
		if edge.Kind == "contribution" {
			found = true
			if edge.Origin.BriefVersion != 3 || edge.Origin.ByKind != "you" {
				t.Fatal("revert provenance")
			}
		}
	}
	if !found {
		t.Fatal("brief revert missing")
	}
}
func TestUnderstandingSinceWindowAndLimits(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	cs := r.mc.CircleService()
	for i := 0; i < 70; i++ {
		r.h.Now = func() time.Time { return epoch.Add(-time.Duration(70-i) * time.Minute) }
		_, e := cs.MutateAs(&r.a, c.ID, func(v *model.Circle) error { v.People[0].Role = fmt.Sprintf("Role %d", i); return nil })
		must(t, e)
	}
	r.h.Now = func() time.Time { return epoch }
	full := mapResponse(t, r, "/v1/map")
	if len(full.Events) != 60 {
		t.Fatal("event limit")
	}
	cutoff := epoch.Add(-5 * time.Minute)
	m := mapResponse(t, r, "/v1/map?since="+url.QueryEscape(cutoff.Format(time.RFC3339)))
	if len(m.Nodes) != len(full.Nodes) || len(m.Edges) != len(full.Edges) || m.Summary != full.Summary {
		t.Fatal("cursor filtered graph or summary")
	}
	for _, ev := range m.Events {
		if !ev.At.After(cutoff) {
			t.Fatal("since not strict")
		}
	}
	for i := 1; i < len(full.Events); i++ {
		if full.Events[i].At.After(full.Events[i-1].At) {
			t.Fatal("events unordered")
		}
	}
	for _, q := range []string{"since=bad", "since=", "since=2026-10-17T00:00:00Z&since=2026-10-17T00:00:00Z"} {
		status(t, r.request(t, "GET", "/v1/map?"+q, r.api.ControlToken, nil, nil), 400)
	}
	r.h.Now = func() time.Time { return epoch.Add(15 * 24 * time.Hour) }
	if len(mapResponse(t, r, "/v1/map").Events) != 0 {
		t.Fatal("events outside 14 days")
	}
}
func TestPresenceStatesAndExpiry(t *testing.T) {
	for _, v := range []struct{ tool, kind, state string }{{"get_brief", "read", "reading"}, {"update_brief", "summary", "writing_brief"}, {"draft_message", "send_requested", "drafting"}} {
		t.Run(v.state, func(t *testing.T) {
			r := newRig(t)
			c := circleFixture(t, r, "university", "confirm", false, groupA)
			must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: v.tool, Kind: v.kind, ChatJIDs: []string{groupA}}))
			read := func() []circles.AgentPresence {
				out, e := r.mc.CircleService().Presence()
				must(t, e)
				return out.(map[string]any)["agents"].([]circles.AgentPresence)
			}
			xs := read()
			if len(xs) != 1 || xs[0].State != v.state || len(xs[0].TargetIDs) != 1 || xs[0].TargetIDs[0] != c.ID {
				t.Fatal("presence projection")
			}
			r.h.Now = func() time.Time { return epoch.Add(119 * time.Second) }
			if len(read()) != 1 {
				t.Fatal("presence expired early")
			}
			r.h.Now = func() time.Time { return epoch.Add(120 * time.Second) }
			if len(read()) != 0 {
				t.Fatal("presence did not expire")
			}
		})
	}
}
func TestPresenceRunningHandoff(t *testing.T) {
	r := newRig(t)
	s, _, _ := setupWatches(t, r)
	d := createHandoff(t, r, nil)
	ctx := context.Background()
	must(t, s.Store.PutHit(ctx, triggers.Hit{ID: "hit-presence", WatchID: d.ID, At: epoch, Action: triggers.WakeSession, State: "recorded", Messages: []triggers.IncomingMessage{}}))
	run := triggers.RunRecord{ID: "run-presence", HandoffID: d.ID, HitID: "hit-presence", StartedAt: epoch, State: "running"}
	must(t, s.Store.PutRun(ctx, run))
	r.h.Now = func() time.Time { return epoch.Add(10 * time.Minute) }
	read := func() []circles.AgentPresence {
		out, e := r.mc.CircleService().Presence()
		must(t, e)
		return out.(map[string]any)["agents"].([]circles.AgentPresence)
	}
	xs := read()
	if len(xs) != 1 || xs[0].State != "handoff" {
		t.Fatal("running handoff omitted")
	}
	run.State = "completed"
	run.FinishedAt = epoch.Add(10 * time.Minute)
	must(t, s.Store.PutRun(ctx, run))
	if len(read()) != 0 {
		t.Fatal("completed handoff present")
	}
}
func TestUnderstandingRoutesRequireControl(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/v1/map", "/v1/presence", "/v1/people/maya/avatar"} {
		for _, token := range []string{r.token, r.api.StatusToken} {
			status(t, r.request(t, "GET", path, token, nil, nil), 403)
		}
	}
	status(t, r.request(t, "GET", "/v1/people/maya/avatar", r.api.ControlToken, nil, nil), 404)
}

func TestUnderstandingOwnerChatMembershipOrigin(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	status(t, r.request(t, "POST", "/v1/circles/"+c.ID+"/chats", r.api.ControlToken, map[string]any{"chat_jid": chatB, "role_in_circle": "Client chat"}, nil), 200)
	found := false
	for _, edge := range mapResponse(t, r, "/v1/map").Edges {
		if edge.Source == c.ID && edge.Target == chatB {
			found = true
			if edge.Origin.ByKind != "you" || edge.Origin.ChangeID == "" {
				t.Fatal("owner membership origin missing")
			}
		}
	}
	if !found {
		t.Fatal("membership missing")
	}
}

type routeAvatarFake struct {
	data  []byte
	calls int
}

func (f *routeAvatarFake) FetchAvatar(context.Context, string) ([]byte, error) {
	f.calls++
	return f.data, nil
}
func TestAvatarControlHTTPJPEGAndUnknownPerson(t *testing.T) {
	r := newRig(t)
	var b bytes.Buffer
	must(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	fake := &routeAvatarFake{data: b.Bytes()}
	r.api.Avatars = &people.AvatarCache{Hub: r.h, Lookup: fake}
	w := r.request(t, "GET", "/v1/people/maya/avatar", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), fake.data) {
		t.Fatal("avatar wire format")
	}
	status(t, r.request(t, "GET", "/v1/people/unknown/avatar", r.api.ControlToken, nil, nil), 404)
	status(t, r.request(t, "GET", "/v1/people/maya/avatar", r.token, nil, nil), 403)
	if fake.calls != 1 {
		t.Fatal("unknown or unauthorised avatar lookup")
	}
}
