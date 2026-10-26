package integration_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"strings"
	"testing"
	"time"
)

func TestP1NamedRelevantPagingSearchAndInvalidation(t *testing.T) {
	r := newRig(t)
	old := "15550100900@s.whatsapp.net"
	must(t, r.p.PutPerson(model.Person{ID: "stranger", JIDs: []string{old}, Name: "Riley Example"}))
	must(t, r.p.PutMembership(model.Membership{Group: groupA, Person: "stranger"}))
	must(t, r.p.PutPerson(model.Person{ID: "numeric", JIDs: []string{"900111@lid"}, Name: "900111"}))
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("detail-%03d", i)
		must(t, r.p.PutPerson(model.Person{ID: id, Name: "Maya Example", Notes: "Owner added a detail"}))
	}
	// Owner-only enrichment may be surfaced even without an actual chat.
	page := r.request(t, "GET", "/v1/people?limit=50", r.api.ControlToken, nil, nil)
	status(t, page, 200)
	var v struct {
		People []model.Person `json:"people"`
		Next   string         `json:"next_cursor"`
	}
	must(t, json.Unmarshal(page.Body.Bytes(), &v))
	if len(v.People) != 50 || v.Next == "" || page.Body.Len() >= 100000 {
		t.Fatal("unbounded page")
	}
	for _, p := range v.People {
		if !model.Named(p) || p.ID == "stranger" || p.ID == "numeric" {
			t.Fatal("unnamed or irrelevant person surfaced")
		}
	}
	seen := map[string]bool{}
	for {
		for _, p := range v.People {
			if seen[p.ID] {
				t.Fatal("duplicate page")
			}
			seen[p.ID] = true
		}
		if v.Next == "" {
			break
		}
		page = r.request(t, "GET", "/v1/people?limit=50&cursor="+v.Next, r.api.ControlToken, nil, nil)
		status(t, page, 200)
		must(t, json.Unmarshal(page.Body.Bytes(), &v))
	}
	if len(seen) != 122 {
		t.Fatalf("people lost: %d", len(seen))
	}
	ps, e := r.p.People(nil, "Riley")
	must(t, e)
	if len(ps) != 1 {
		t.Fatal("search did not include named stranger")
	}
	must(t, r.m.PutMessage(model.Message{ID: "present", ChatJID: groupA, Sender: old, Content: "Design planning", Timestamp: epoch}))
	ps, e = r.p.People(nil, "")
	must(t, e)
	if !strings.Contains(encode(ps), "stranger") {
		t.Fatal("message counter did not invalidate relevance")
	}
	_, e = r.p.Profile(nil, "numeric")
	if e != model.ErrNotFound {
		t.Fatal("numeric profile surfaced")
	}
}
func TestP1OwnerAliasesAndSelfDraft(t *testing.T) {
	r := newRig(t)
	settings := r.h.Settings()
	settings.Owner = "Alex Example"
	must(t, r.h.SetSettings(settings))
	own := "15550100099@s.whatsapp.net"
	lid := "900000099@lid"
	must(t, r.p.PutPerson(model.Person{ID: "legacy-owner", JIDs: []string{own, lid}, Name: "Alex Example", Notes: "Owner's own note"}))
	must(t, r.p.KeepOwner(model.Person{Name: "Alex Example", JIDs: []string{own, lid}}))
	ps, e := r.p.People(nil, "")
	must(t, e)
	if ps[0].ID != "me" || !strings.Contains(encode(ps[0]), "Owner's own note") || strings.Contains(encode(ps), "legacy-owner") {
		t.Fatal("owner promotion lost details or duplicated")
	}
	for _, query := range []string{"me", "I", "myself", "Alex Example"} {
		v, e := r.call(t, "find_person", map[string]any{"query": query})
		must(t, e)
		if !strings.Contains(encode(v), `"id":"me"`) {
			t.Fatal("owner alias unresolved")
		}
	}
	v, e := r.call(t, "whoami", map[string]any{})
	must(t, e)
	if !strings.Contains(encode(v), `"owner":{"details"`) {
		t.Fatal("missing owner")
	}
	v, e = r.call(t, "resolve_context", map[string]any{"query": "send it to me"})
	must(t, e)
	if !strings.Contains(encode(v), `"id":"me"`) || strings.Contains(encode(v), `"needs_clarification":true`) {
		t.Fatal("ambiguous owner")
	}
	v, e = r.call(t, "get_person", map[string]any{"person_id": "me"})
	must(t, e)
	v, e = r.call(t, "draft_message", map[string]any{"chat": "me", "text": "A note to myself", "voice": "as_agent"})
	must(t, e)
	if !strings.Contains(encode(v), own) || !strings.Contains(encode(v), `"rule":"free"`) {
		t.Fatal("wrong self chat or policy")
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("draft sent")
	}
}
func TestP1ScanAndMessageWindow(t *testing.T) {
	r := newRig(t)
	must(t, r.m.PutMessage(model.Message{ID: "old", ChatJID: chatA, Sender: chatA, Content: "Historical", Timestamp: epoch.AddDate(-2, 0, 0)}))
	must(t, r.m.PutMessage(model.Message{ID: "long", ChatJID: chatA, Sender: chatA, Content: strings.Repeat("あ", model.MaxModelText+400), Timestamp: epoch}))
	v, e := r.call(t, "scan_chats", map[string]any{})
	must(t, e)
	if len(encode(v)) >= 6000 || !strings.Contains(encode(v), "Maya Rao") {
		t.Fatal("scan size or senders")
	}
	v, e = r.call(t, "list_messages", map[string]any{"chat_jid": chatA})
	must(t, e)
	ms := v.([]model.Message)
	for _, m := range ms {
		if m.ID == "old" {
			t.Fatal("default read years of history")
		}
		if len([]rune(m.Content)) > model.MaxModelText {
			t.Fatal("unbounded text")
		}
		if m.ID == "long" && !m.Truncated {
			t.Fatal("missing truncation flag")
		}
	}
	v, e = r.call(t, "list_messages", map[string]any{"chat_jid": chatA, "since": epoch.AddDate(-3, 0, 0).Format(time.RFC3339)})
	must(t, e)
	if !strings.Contains(encode(v), "Historical") {
		t.Fatal("explicit historical access lost")
	}
	for _, name := range []string{"scan_chats", "list_messages"} {
		p := r.a.Permissions
		p.ReadMessages = false
		if mcpserver.Permitted(p, name) {
			t.Fatal("read permission bypass")
		}
	}
	c := model.Circle{ID: "p1circle", Name: "Design", Kind: "work", Confirmed: true, Chats: []model.CircleChat{{ChatJID: chatA}}}
	must(t, r.h.Repo.Put("circles", c.ID, c))
	must(t, r.m.PutMessage(model.Message{ID: "eight-days", ChatJID: chatA, Sender: chatA, Content: "Outside first catchup", Timestamp: epoch.AddDate(0, 0, -8)}))
	v, e = r.call(t, "catch_up", map[string]any{"id": c.ID})
	must(t, e)
	if strings.Contains(encode(v), "eight-days") {
		t.Fatal("first catchup went past 7 days")
	}
	for _, g := range v.(circles.CatchUp).Chats {
		for _, m := range g.Messages {
			if len([]rune(m.Message.Content)) > model.MaxCatchUpText {
				t.Fatal("catchup unbounded")
			}
		}
	}
}
func TestP1GraphStatsCounterAndScope(t *testing.T) {
	r := newRig(t)
	g, e := r.p.Graph(nil, "", 1, 150)
	must(t, e)
	must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Kind: "read", ChatJIDs: []string{groupA}}))
	g2, e := r.p.Graph(nil, "", 1, 150)
	must(t, e)
	_ = g
	_ = g2
	must(t, r.m.PutMessage(model.Message{ID: "newdm", ChatJID: chatA, Content: "Planning", Timestamp: epoch}))
	p, e := r.p.Profile(nil, "maya")
	must(t, e)
	if p.Stats.DM30 != 4 {
		t.Fatal("stats did not invalidate")
	}
	r.a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chatB}}
	ps, e := r.p.People(&r.a, "")
	must(t, e)
	if strings.Contains(encode(ps), chatA) {
		t.Fatal("cache leaked scope")
	}
	// Keep direct SQL invalidation covered, not only service wrappers.
	var raw model.Person
	must(t, r.h.Repo.Get("people", "maya", &raw))
	raw.Name = "Maya Renamed"
	must(t, r.h.Repo.Apply(hub.Mutation{Bucket: "people", ID: raw.ID, Value: raw}))
	ps, e = r.p.People(nil, "")
	must(t, e)
	if !strings.Contains(encode(ps), "Maya Renamed") {
		t.Fatal("identity cache stale")
	}
}

func TestP1MapDeltaAndRemovals(t *testing.T) {
	r := newRig(t)
	first := mapResponse(t, r, "/v1/map")
	if !strings.HasPrefix(first.Cursor, "p1.") {
		t.Fatal("missing map cursor")
	}
	delta := mapResponse(t, r, "/v1/map?since="+first.Cursor)
	if !delta.Delta || len(delta.Nodes) != 0 || len(delta.Edges) != 0 {
		t.Fatal("unchanged graph reloaded")
	}
	var p model.Person
	must(t, r.h.Repo.Get("people", "maya", &p))
	p.Name = "Maya New Name"
	must(t, r.p.PutPerson(p))
	delta = mapResponse(t, r, "/v1/map?since="+first.Cursor)
	found := false
	for _, n := range delta.Nodes {
		if n.ID == "maya" && n.Title == p.Name {
			found = true
		}
	}
	if !found {
		t.Fatal("changed node missing from delta")
	}
	second := delta.Cursor
	must(t, r.h.Repo.Delete("people", "maya"))
	delta = mapResponse(t, r, "/v1/map?since="+second)
	if !model.Contains(delta.RemovedNodes, "maya") {
		t.Fatal("removed node missing")
	}
	full := mapResponse(t, r, "/v1/map?since=p1.0000000000000000")
	if full.Delta {
		t.Fatal("expired cursor did not resync")
	}
}

func TestP1AboutYouNameResolvesOwner(t *testing.T) {
	r := newRig(t)
	must(t, r.p.KeepOwner(model.Person{Name: "Alex Push", JIDs: []string{"15550100099@s.whatsapp.net"}}))
	settings := r.h.Settings()
	settings.Owner = "Alex About"
	must(t, r.h.SetSettings(settings))
	for _, name := range []string{"Alex About", "Alex Push", "MYSELF", "i"} {
		v, e := r.call(t, "find_person", map[string]any{"query": name})
		must(t, e)
		if !strings.Contains(encode(v), `"id":"me"`) {
			t.Fatal("owner About you alias missing")
		}
	}
}
