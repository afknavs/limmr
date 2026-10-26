package integration_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"strings"
	"testing"
)

func TestP3AffinityManualPlacementAndNoSignal(t *testing.T) {
	r := newRig(t)
	c := model.Circle{ID: "studio", Name: "Studio ceramics", Description: "Ceramics kiln workshop", Confirmed: true}
	for i := 0; i < 3; i++ {
		g := fmt.Sprintf("studio-%d@g.us", i)
		c.Chats = append(c.Chats, model.CircleChat{ChatJID: g})
		must(t, r.p.PutGroup(model.Group{JID: g, Name: "Studio group"}))
		must(t, r.p.PutMembership(model.Membership{Group: g, Person: "maya"}))
	}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	_, e := r.h.Messages.DB.Exec("UPDATE messages SET content=? WHERE chat_jid=?", "Ceramics kiln workshop PRIVATE_TEXT_P3", chatA)
	must(t, e)
	p, e := r.p.Profile(nil, "maya")
	must(t, e)
	if p.Person.PrimaryCircleID != "studio" || len(p.Person.CircleAffinity) != 1 || p.Person.CircleAffinity[0].Score < .9 {
		t.Fatal("three groups plus matching DM did not select the circle")
	}
	if strings.Contains(encode(p.Person.CircleAffinity), "PRIVATE_TEXT_P3") {
		t.Fatal("affinity leaked an excerpt")
	}
	p, e = r.p.Profile(nil, "alex")
	must(t, e)
	if p.Person.PrimaryCircleID != "" || len(p.Person.CircleAffinity) != 0 {
		t.Fatal("no-signal contact assigned a circle")
	}
	manual := model.Circle{ID: "reading", Name: "Reading", Confirmed: true, People: []model.CirclePerson{{PersonID: "maya", Source: "you"}}}
	must(t, r.h.Repo.Put("circles", manual.ID, manual))
	p, e = r.p.Profile(nil, "maya")
	must(t, e)
	if p.Person.PrimaryCircleID != "reading" {
		t.Fatal("manual placement lost to inferred affinity")
	}
	// Agent cannot learn a circle from chats outside its scope.
	a := r.a
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	p, e = r.p.Profile(&a, "maya")
	must(t, e)
	for _, v := range p.Person.CircleAffinity {
		if v.CircleID == "studio" {
			t.Fatal("hidden circle signal escaped scope")
		}
	}
}
func TestP3UnderstandingToolAndPrivateInference(t *testing.T) {
	r := newRig(t)
	a := r.a
	a.Preset = "own"
	a.Permissions = model.Preset("own")
	must(t, r.h.Repo.Put("agents", a.ID, a))
	r.a = a
	// Remove seeded context so this contact needs understanding.
	_, e := r.p.Update(nil, "maya", map[string]string{"how_known": "", "role": ""})
	must(t, e)
	must(t, r.h.Repo.DB.QueryRow("SELECT 1").Scan(new(int)))
	v, e := r.call(t, "people_to_understand", map[string]any{})
	must(t, e)
	b := encode(v)
	if !strings.Contains(b, "maya") || strings.Contains(b, "content") || strings.Contains(b, "text") || strings.Contains(b, chatA) {
		t.Fatal("tool did not return a metadata-only queue")
	}
	// Local inferred sensitive fields must never appear in any agent projection.
	inference := model.RelationshipInference{Label: "your adviser", HowKnown: "Regular private advice sessions", Confidence: .92, Sensitive: true}
	status(t, r.request(t, "PUT", "/v1/people/maya/inference", r.api.ControlToken, inference, nil), 200)
	p, e := r.p.Profile(nil, "maya")
	must(t, e)
	if p.Person.InferredRelationship == nil {
		t.Fatal("owner inference missing")
	}
	p, e = r.p.Profile(&a, "maya")
	must(t, e)
	if p.Person.InferredRelationship != nil || strings.Contains(encode(p), "private advice") {
		t.Fatal("unconfirmed inference reached an agent")
	}
	status(t, r.request(t, "POST", "/v1/people/maya/inference/confirm", r.token, nil, nil), 403)
	status(t, r.request(t, "POST", "/v1/people/maya/inference/confirm", r.api.ControlToken, nil, nil), 200)
	p, e = r.p.Profile(&a, "maya")
	must(t, e)
	if p.Person.HowKnown != inference.HowKnown {
		t.Fatal("confirmation did not publish the reviewed field")
	}
	_, e = r.call(t, "update_person", map[string]any{"person_id": "maya", "fields": map[string]any{"how_known": "New suggestion"}})
	must(t, e)
	p, e = r.p.Profile(nil, "maya")
	must(t, e)
	if p.Person.HowKnown != inference.HowKnown || len(p.Person.DetailSuggestions) == 0 {
		t.Fatal("agent overwrote a confirmed relationship")
	}
	// Read-only/non-own callers cannot use the queue.
	a.Preset = "work"
	must(t, r.h.Repo.Put("agents", a.ID, a))
	_, e = r.call(t, "people_to_understand", map[string]any{})
	if e == nil {
		t.Fatal("non-own queue accepted")
	}
}
func TestP3InferenceSettingsAndValidation(t *testing.T) {
	r := newRig(t)
	status(t, r.request(t, "PUT", "/v1/settings/people", r.api.ControlToken, map[string]bool{"enabled": false}, nil), 200)
	w := r.request(t, "GET", "/v1/settings/people", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var v struct{ Enabled bool }
	must(t, json.Unmarshal(w.Body.Bytes(), &v))
	if v.Enabled {
		t.Fatal("toggle not persisted")
	}
	status(t, r.request(t, "PUT", "/v1/people/maya/inference", r.token, map[string]any{}, nil), 403)
	status(t, r.request(t, "PUT", "/v1/people/maya/inference", r.api.ControlToken, model.RelationshipInference{Confidence: .1}, nil), 400)
}

func TestP3AffinitySuggestionsUseUndoAndRespectAskFirst(t *testing.T) {
	for _, auto := range []bool{true, false} {
		t.Run(fmt.Sprintf("auto=%v", auto), func(t *testing.T) {
			r := newRig(t)
			a := r.a
			a.Preset = "own"
			a.Permissions = model.Preset("own")
			must(t, r.h.Repo.Put("agents", a.ID, a))
			settings := r.h.Settings()
			settings.AutoCircles = &auto
			must(t, r.h.SetSettings(settings))
			c := model.Circle{ID: "studio", Name: "Studio", Confirmed: true, SendRule: "confirm", Kind: "project"}
			for i := 0; i < 3; i++ {
				g := fmt.Sprintf("studio-%d@g.us", i)
				must(t, r.h.Messages.PutChat(model.Chat{JID: g, Name: "Studio"}))
				must(t, r.p.PutGroup(model.Group{JID: g, Name: "Studio"}))
				must(t, r.p.PutMembership(model.Membership{Group: g, Person: "maya"}))
				c.Chats = append(c.Chats, model.CircleChat{ChatJID: g})
			}
			must(t, r.h.Repo.Put("circles", c.ID, c))
			service := r.api.MCP.CircleService()
			_, e := service.SuggestAffinity()
			must(t, e)
			changes, e := service.AutoChanges()
			must(t, e)
			additions, e := service.Additions()
			must(t, e)
			if auto {
				if len(changes) != 1 || len(additions) != 0 {
					t.Fatal("auto membership did not offer Undo")
				}
				must(t, service.UndoAuto(changes[0].ID))
				_, e = service.SuggestAffinity()
				must(t, e)
				must(t, r.h.Repo.Get("circles", c.ID, &c))
				if len(c.People) != 0 {
					t.Fatal("automatic refresh re-added an undone person")
				}
			} else {
				if len(changes) != 0 || len(additions) != 1 || !strings.Contains(additions[0].Why, "shared groups") {
					t.Fatal("ask-first membership missing reasons")
				}
			}
		})
	}
}

func TestP3StructuralSuggestionWithoutAnOwnAI(t *testing.T) {
	r := newRig(t)
	a := r.a
	a.Preset = "work"
	must(t, r.h.Repo.Put("agents", a.ID, a))
	c := model.Circle{ID: "studio", Name: "Studio", Confirmed: true, SendRule: "confirm", Kind: "project"}
	for i := 0; i < 3; i++ {
		g := fmt.Sprintf("studio-%d@g.us", i)
		c.Chats = append(c.Chats, model.CircleChat{ChatJID: g})
		must(t, r.h.Messages.PutChat(model.Chat{JID: g, Name: "Studio"}))
		must(t, r.p.PutGroup(model.Group{JID: g}))
		must(t, r.p.PutMembership(model.Membership{Group: g, Person: "maya"}))
	}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	s := r.api.MCP.CircleService()
	_, e := s.SuggestAffinity()
	must(t, e)
	additions, e := s.Additions()
	must(t, e)
	if len(additions) != 1 || additions[0].AgentID != "structure" {
		t.Fatal("missing local structural suggestion")
	}
	_, e = s.ReviewAddition(additions[0].ID, true)
	must(t, e)
	must(t, r.h.Repo.Get("circles", c.ID, &c))
	if len(c.People) != 1 || c.People[0].Source != "you" {
		t.Fatal("owner confirmation did not keep membership")
	}
}

func TestP3SilentMembershipAndGroupsInCommonProjection(t *testing.T) {
	r := newRig(t)
	must(t, r.p.PutPerson(model.Person{ID: "silent", Name: "Avery Lane", JIDs: []string{"15550100999@s.whatsapp.net"}}))
	c := model.Circle{ID: "studio", Name: "Studio", Confirmed: true}
	for i := 0; i < 3; i++ {
		g := fmt.Sprintf("quiet-%d@g.us", i)
		at := r.h.Now().AddDate(0, 0, -i)
		must(t, r.h.Messages.PutChat(model.Chat{JID: g, Name: fmt.Sprintf("Workshop %d", i), LastMessageTime: &at}))
		must(t, r.p.PutGroup(model.Group{JID: g, Name: fmt.Sprintf("Workshop %d", i)}))
		must(t, r.p.PutMembership(model.Membership{Group: g, Person: "silent"}))
		c.Chats = append(c.Chats, model.CircleChat{ChatJID: g})
	}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	p, e := r.p.Profile(nil, "silent")
	must(t, e)
	if p.Person.PrimaryCircleID != "studio" || len(p.Person.GroupsInCommon) != 3 || p.Person.GroupsInCommon[0].ID != "quiet-0@g.us" || len(p.Person.GroupsInCommon[0].CircleIDs) != 1 {
		t.Fatal("silent member lacks primary circle or ordered common groups")
	}
	visible, e := r.p.People(nil, "")
	must(t, e)
	found := false
	for _, v := range visible {
		found = found || v.ID == "silent"
	}
	if !found {
		t.Fatal("silent member omitted from current circle")
	}
	a := r.a
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{"quiet-1@g.us"}}
	p, e = r.p.Profile(&a, "silent")
	must(t, e)
	if len(p.Person.GroupsInCommon) != 1 || p.Person.GroupsInCommon[0].ID != "quiet-1@g.us" || len(p.Person.GroupsInCommon[0].CircleIDs) != 0 {
		t.Fatal("common groups leaked hidden groups/circles")
	}
	graph, e := r.p.Graph(nil, "", 4, 300)
	must(t, e)
	found = false
	for _, n := range graph.Nodes {
		if n.ID == "silent" {
			found = len(n.GroupsInCommon) == 3
		}
	}
	if !found {
		t.Fatal("graph missing common-group metadata")
	}
}
