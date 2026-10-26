package integration_test

import (
	"encoding/json"
	"errors"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestD1DigestToolAndControlRoute(t *testing.T) {
	r := newRig(t)
	for _, c := range []model.Circle{
		{ID: "design", Name: "Design Club", Confirmed: true, Chats: []model.CircleChat{{ChatJID: groupA}}},
		{ID: "garden", Name: "Garden Club", Confirmed: true, Chats: []model.CircleChat{{ChatJID: groupB}}},
	} {
		must(t, r.h.Repo.Put("circles", c.ID, c))
	}
	p := model.Defaults(false)
	p.Access = model.Access{Circles: []string{"design"}}
	r.agent(t, p)
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Kind: "read", Tool: "list_messages", ChatJIDs: []string{groupA}, Count: 3}))
	x, err := r.call(t, "get_digest", map[string]any{})
	must(t, err)
	d := x.(hub.Digest)
	if len(d.CircleUpdates) != 1 || d.CircleUpdates[0].CircleID != "design" || len(d.AgentWork) != 1 || d.AgentWork[0].Messages != 3 {
		t.Fatalf("tool projection: %+v", d)
	}
	w := r.request(t, "GET", "/v1/digest", r.token, nil, nil)
	status(t, w, 200)
	var control hub.Digest
	must(t, json.Unmarshal(w.Body.Bytes(), &control))
	if encode(control) != encode(d) {
		t.Fatal("tool/control digest shape or projection differs")
	}
	for _, test := range []struct {
		token string
		code  int
	}{{"", 401}, {r.api.StatusToken, 403}, {r.api.ControlToken, 200}} {
		status(t, r.request(t, "GET", "/v1/digest", test.token, nil, nil), test.code)
	}
	status(t, r.request(t, "GET", "/v1/digest?circle_id=garden", r.token, nil, nil), 404)
	status(t, r.request(t, "GET", "/v1/digest?circle_id=missing", r.token, nil, nil), 404)
	status(t, r.request(t, "GET", "/v1/digest?period=month", r.token, nil, nil), 400)
	status(t, r.request(t, "GET", "/v1/digest?period=since_last&cursor=invalid", r.token, nil, nil), 400)
	for _, args := range []map[string]any{{"period": "month"}, {"period": 3}, {"unknown": true}, {"period": "today", "cursor": d.Cursor}} {
		if _, err = r.call(t, "get_digest", args); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid arguments accepted: %v", err)
		}
	}
	r.h.Now = func() time.Time { return epoch.Add(time.Hour) }
	w = r.request(t, "GET", "/v1/digest?period=since_last&cursor="+url.QueryEscape(d.Cursor), r.token, nil, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &control))
	if len(control.CircleUpdates)+len(control.AgentWork) != 0 {
		t.Fatal("cursor repeated data or counted get_digest as message reads")
	}
	settings := r.h.Settings()
	settings.HiddenChats = []string{groupA}
	must(t, r.h.SetSettings(settings))
	w = r.request(t, "GET", "/v1/digest", r.token, nil, nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "Design Club") || strings.Contains(w.Body.String(), groupA) {
		t.Fatal("hidden chat leaked")
	}
	p.ReadMessages = false
	r.agent(t, p)
	status(t, r.request(t, "GET", "/v1/digest", r.token, nil, nil), 403)
	if _, err = r.call(t, "get_digest", map[string]any{}); err == nil {
		t.Fatal("tool message permission bypass")
	}
	p.ReadMessages = true
	p.ReadPeople = false
	r.agent(t, p)
	status(t, r.request(t, "GET", "/v1/digest", r.token, nil, nil), 403)
	if _, err = r.call(t, "get_digest", map[string]any{}); err == nil {
		t.Fatal("tool people permission bypass")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("digest invoked messenger")
	}
}

func TestD1DigestCatalogContract(t *testing.T) {
	for _, tool := range mcpserver.Catalog() {
		if tool.Name != "get_digest" {
			continue
		}
		if tool.Description != "A summary of what changed for the owner. Use when the owner asks what they missed." || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || *tool.Annotations.OpenWorldHint {
			t.Fatal("digest contract or safety annotation changed")
		}
		props := tool.InputSchema["properties"].(map[string]any)
		if props["period"].(map[string]any)["default"] != "today" {
			t.Fatal("missing period default")
		}
		return
	}
	t.Fatal("missing digest tool")
}
