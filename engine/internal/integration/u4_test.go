package integration_test

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"testing"
)

func TestU4HistoryRouteBoundsAndCredentials(t *testing.T) {
	r := newRig(t)
	for _, query := range []string{"days=0", "days=-1", "days=367", "days=hi", "days=7&days=30"} {
		status(t, r.request(t, "GET", "/v1/stats/history?"+query, r.api.ControlToken, nil, nil), 400)
	}
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "GET", "/v1/stats/history", token, nil, nil), 403)
	}
	w := r.request(t, "GET", "/v1/stats/history?days=140", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var v hub.StatsHistory
	must(t, json.Unmarshal(w.Body.Bytes(), &v))
	if len(v.Days) != 140 {
		t.Fatal("missing zero-filled days")
	}
}
func TestU4EntityIconRoutesPreserveAndClear(t *testing.T) {
	r := newRig(t)
	person := model.PersonID(chatA)
	// Synthetic person is inserted explicitly so no directory/live lookup is involved.
	must(t, r.h.Repo.Put("people", person, model.Person{ID: person, Name: "Fixture person", JIDs: []string{chatA}}))
	for _, path := range []string{"/v1/people/" + person, "/v1/agents/" + r.a.ID} {
		status(t, r.request(t, "PATCH", path, r.token, map[string]any{"icon": map[string]string{"kind": "symbol", "value": "book"}}, nil), 403)
		status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"icon": map[string]string{"kind": "html", "value": "bad"}}, nil), 400)
		status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"icon": map[string]string{"kind": "symbol", "value": "book"}}, nil), 200)
		w := r.request(t, "GET", path, r.api.ControlToken, nil, nil)
		status(t, w, 200)
		if !strings.Contains(w.Body.String(), `"icon":{"kind":"symbol","value":"book"}`) {
			t.Fatal("icon not returned")
		}
		status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"icon": nil}, nil), 200)
		w = r.request(t, "GET", path, r.api.ControlToken, nil, nil)
		if strings.Contains(w.Body.String(), `"icon"`) {
			t.Fatal("icon not cleared")
		}
	}
	// Mixed fields retain the original strict validation and atomically avoid saving invalid icons.
	status(t, r.request(t, "PATCH", "/v1/agents/"+r.a.ID, r.api.ControlToken, map[string]any{"name": "Renamed fixture", "icon": map[string]string{"kind": "emoji", "value": "🌱"}}, nil), 200)
	w := r.request(t, "GET", "/v1/agents/"+r.a.ID, r.api.ControlToken, nil, nil)
	if !strings.Contains(w.Body.String(), "🌱") || !strings.Contains(w.Body.String(), "Renamed fixture") {
		t.Fatal("mixed patch lost fields")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("presentation route sent a message")
	}
}

func TestU4CircleIconSeenAndReplyRequestNeverSend(t *testing.T) {
	r := newRig(t)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	b, e := r.mc.CircleService().WriteBrief(nil, c.ID, model.Brief{Summary: "Fixture question", CoversUntil: epoch, Evidence: []string{firstMessage(t, r, chatA).ID}, OpenAsks: []model.OpenAsk{{Text: "Can you review the outline?", ChatJID: chatA, From: "Fixture person"}}})
	must(t, e)
	path := "/v1/circles/" + c.ID
	status(t, r.request(t, "PATCH", path, r.api.ControlToken, map[string]any{"icon": map[string]string{"kind": "symbol", "value": "book"}, "pinned": true}, nil), 200)
	w := r.request(t, "GET", path, r.api.ControlToken, nil, nil)
	status(t, w, 200)
	if !strings.Contains(w.Body.String(), `"icon":{"kind":"symbol","value":"book"}`) {
		t.Fatal("circle icon lost")
	}
	status(t, r.request(t, "POST", path+"/seen", r.api.ControlToken, map[string]any{}, nil), 200)
	w = r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, w, 200)
	var home struct {
		Circles []struct {
			Circle      model.Circle
			NewMessages int `json:"new_messages"`
		}
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	found := false
	for _, card := range home.Circles {
		if card.Circle.ID == c.ID {
			found = true
			if card.NewMessages != 0 {
				t.Fatal("seen messages still new")
			}
		}
	}
	if !found {
		t.Fatal("pinned circle disappeared")
	}
	request := path + "/asks/" + b.OpenAsks[0].ID + "/draft"
	status(t, r.request(t, "POST", request, r.token, map[string]any{"agent_id": r.a.ID}, nil), 403)
	w = r.request(t, "POST", request, r.api.ControlToken, map[string]any{"agent_id": r.a.ID}, nil)
	status(t, w, 200)
	var suggestion model.Suggestion
	must(t, json.Unmarshal(w.Body.Bytes(), &suggestion))
	if suggestion.Kind != "action" || suggestion.DraftText != "" || !strings.Contains(suggestion.Why, b.OpenAsks[0].Text) {
		t.Fatal("request fabricated a reply")
	}
	status(t, r.request(t, "POST", path+"/asks/"+b.OpenAsks[0].ID+"/done", r.api.ControlToken, nil, nil), 204)
	status(t, r.request(t, "POST", request, r.api.ControlToken, map[string]any{"agent_id": r.a.ID}, nil), 404)
	if len(r.fake.Sent) != 0 {
		t.Fatal("reply request sent")
	}
}
