package integration_test

import (
	"encoding/json"
	"limmr/internal/hub"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func TestA7ProfileDetailsAndSuggestions(t *testing.T) {
	r := newRig(t)
	patch := map[string]any{"details": map[string]any{"emails": []any{map[string]string{"label": "work", "value": "maya@example.com", "source": "spoofed"}}, "birthday": "04-12", "location": "Bengaluru"}}
	status(t, r.request(t, "PATCH", "/v1/people/maya", r.api.ControlToken, patch, nil), 200)
	got, e := r.p.Profile(nil, "maya")
	must(t, e)
	if got.Person.Details.Emails[0].Source != "you" || len(got.Person.Details.Phones) != 1 || got.Person.Details.Phones[0].Source != "whatsapp" {
		t.Fatal("owner provenance or derived phone missing")
	}
	_, e = r.call(t, "update_person", map[string]any{"person_id": "maya", "fields": map[string]any{"details": map[string]any{"emails": []any{map[string]string{"label": "work", "value": "studio@example.com", "source": "you"}}, "location": "Mumbai"}}})
	must(t, e)
	got, e = r.p.Profile(nil, "maya")
	must(t, e)
	if got.Person.Details.Location != "Bengaluru" || got.Person.Details.Emails[0].Value != "maya@example.com" || len(got.Person.DetailSuggestions) != 2 {
		t.Fatal("agent replaced owner details or lost suggestions")
	}
	for _, s := range got.Person.DetailSuggestions {
		if s.Source != r.a.ID || s.Status != "suggested" {
			t.Fatal("suggestion source spoofed")
		}
		route := "/v1/people/maya/suggestions/" + s.ID
		status(t, r.request(t, "POST", route+"/confirm", r.token, nil, nil), 403)
		action := "/dismiss"
		if s.Field == "emails" {
			action = "/confirm"
		}
		status(t, r.request(t, "POST", route+action, r.api.ControlToken, nil, nil), 200)
	}
	got, e = r.p.Profile(nil, "maya")
	must(t, e)
	if len(got.Person.DetailSuggestions) != 0 || len(got.Person.Details.Emails) != 2 || got.Person.Details.Emails[1].Source != "you" || got.Person.Details.Location != "Bengaluru" {
		t.Fatal("review did not preserve confirmed data")
	}
	must(t, r.p.PutPerson(model.Person{ID: "maya", JIDs: []string{chatA}, Name: "Maya Rao"}))
	got, e = r.p.Profile(nil, "maya")
	must(t, e)
	if len(got.Person.Details.Emails) != 2 {
		t.Fatal("directory refresh lost profile")
	}
	for _, bad := range []any{map[string]any{"birthday": "02-31"}, map[string]any{"emails": true}, map[string]any{"unknown": "x"}} {
		status(t, r.request(t, "PATCH", "/v1/people/maya", r.api.ControlToken, map[string]any{"details": bad}, nil), 400)
	}
	p := model.Defaults(false)
	p.Chats = model.Scope{Mode: "only", JIDs: []string{groupA}}
	r.agent(t, p)
	visible, e := r.call(t, "get_person", map[string]any{"person_id": "maya"})
	must(t, e)
	if strings.Contains(encode(visible), "maya@example.com") || strings.Contains(encode(visible), strings.Split(chatA, "@")[0]) {
		t.Fatal("group-only view leaked private details")
	}
	p.WritePeople = false
	r.agent(t, p)
	_, e = r.call(t, "update_person", map[string]any{"person_id": "maya", "fields": map[string]any{"details": map[string]any{"location": "Example"}}})
	if e == nil {
		t.Fatal("read-only agent proposed details")
	}
}
func TestA7ForgetAndExport(t *testing.T) {
	r := newRig(t)
	f, e := r.p.AddFact(nil, model.Fact{Subject: "person", SubjectID: "maya", Text: "Knows the studio"})
	must(t, e)
	rel, e := r.p.AddRelation(nil, model.Relation{A: "alex", B: "maya", Kind: "friend"})
	must(t, e)
	_, e = r.p.Update(nil, "maya", map[string]string{"notes": "Owner note"})
	must(t, e)
	c := model.Circle{ID: "studio", Name: "Studio", Confirmed: true, People: []model.CirclePerson{{PersonID: "maya", Role: "Designer"}, {PersonID: "alex"}}, Topics: []model.Topic{{ID: "topic", People: []string{"maya", "alex"}}}}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	status(t, r.request(t, "GET", "/v1/people/export", r.token, nil, nil), 403)
	export := r.request(t, "GET", "/v1/people/export", r.api.ControlToken, nil, nil)
	status(t, export, 200)
	var v struct {
		People  []json.RawMessage
		Circles []json.RawMessage
	}
	must(t, json.Unmarshal(export.Body.Bytes(), &v))
	if len(v.People) != 2 || len(v.Circles) != 1 {
		t.Fatal("export incomplete")
	}
	status(t, r.request(t, "DELETE", "/v1/people/maya", r.token, nil, nil), 403)
	status(t, r.request(t, "DELETE", "/v1/people/maya", r.api.ControlToken, nil, nil), 200)
	status(t, r.request(t, "GET", "/v1/people/maya", r.api.ControlToken, nil, nil), 404)
	if _, e = r.p.Profile(nil, "alex"); e != nil {
		t.Fatal("forgot unrelated person")
	}
	if r.h.Repo.Get("facts", f.ID, &model.Fact{}) != model.ErrNotFound || r.h.Repo.Get("relations", rel.ID, &model.Relation{}) != model.ErrNotFound {
		t.Fatal("forgot left facts or relations")
	}
	changes, e := r.p.Changes()
	must(t, e)
	if len(changes) != 0 {
		t.Fatal("revert could restore forgotten data")
	}
	ms, e := hub.List[model.Membership](r.h.Repo, "memberships")
	must(t, e)
	for _, m := range ms {
		if m.Person == "maya" {
			t.Fatal("membership remains")
		}
	}
	must(t, r.h.Repo.Get("circles", c.ID, &c))
	if len(c.People) != 1 || len(c.Topics[0].People) != 1 {
		t.Fatal("circle references remain")
	}
	var count int
	must(t, r.m.DB.QueryRow("SELECT count(*) FROM messages WHERE chat_jid=?", chatA).Scan(&count))
	if count != 3 {
		t.Fatal("forget touched chats")
	}
	var logs int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM activity WHERE json_extract(data,'$.tool')='forget_person'").Scan(&logs))
	if logs != 1 {
		t.Fatal("forget not logged")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("profile actions sent a message")
	}
}
func TestA7SeedFactsAreReviewable(t *testing.T) {
	r := newRig(t)
	value, e := r.call(t, "add_fact", map[string]any{"subject": "person", "subject_id": "maya", "text": "Organises the studio", "suggested": true})
	must(t, e)
	f := value.(model.Fact)
	if f.Status != "suggested" {
		t.Fatal("seed fact not suggested")
	}
	status(t, r.request(t, "POST", "/v1/facts/"+f.ID+"/confirm", r.token, nil, nil), 403)
	status(t, r.request(t, "POST", "/v1/facts/"+f.ID+"/confirm", r.api.ControlToken, nil, nil), 200)
	var confirmed model.Fact
	must(t, r.h.Repo.Get("facts", f.ID, &confirmed))
	if confirmed.Source != "you" || confirmed.Status != "active" {
		t.Fatal("fact not confirmed")
	}
}

func TestA7RetentionKeepsPeopleAndInferences(t *testing.T) {
	r := newRig(t)
	_, e := r.p.UpdateProfile(nil, "maya", map[string]any{"details": map[string]any{"location": "Bengaluru"}})
	must(t, e)
	f, e := r.p.AddFact(nil, model.Fact{Subject: "person", SubjectID: "maya", Text: "Organises the studio"})
	must(t, e)
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Kind: "people_write", Tool: "add_fact"}))
	r.h.Now = func() time.Time { return epoch.AddDate(2, 0, 0) }
	must(t, r.h.Retain())
	profile, e := r.p.Profile(nil, "maya")
	must(t, e)
	if profile.Person.Details.Location != "Bengaluru" || len(profile.Facts) != 1 || profile.Facts[0].ID != f.ID {
		t.Fatal("activity retention removed profile knowledge")
	}
	var count int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM activity").Scan(&count))
	if count != 0 {
		t.Fatal("old activity retained")
	}
}
