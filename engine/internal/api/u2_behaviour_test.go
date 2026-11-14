package api

import (
	"encoding/json"
	"limmr/internal/circles"
	"limmr/internal/health"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"limmr/internal/watches"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestU2LaterOwnerRoutesAndAllReadSurfaces(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	ws, err := watches.New(h, p, watches.Options{})
	testkit.Must(t, err)
	mc := &mcpserver.Service{Hub: h, People: p, Link: link, Watches: ws}
	svc := mc.TaskService()
	a, credential, err := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, err)
	v, err := svc.Create(&a, tasks.Create{Title: "Review study guide", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}, People: []tasks.Entity{{ID: model.PersonID(testkit.Phone), Name: "Maya Rao"}}})
	testkit.Must(t, err)
	_, err = svc.NeedsYou(&a, v.ID, "Choose a time")
	testkit.Must(t, err)
	testkit.Must(t, h.Repo.Put("suggestions", "fixture-review", model.Suggestion{ID: "fixture-review", TaskID: v.ID, Kind: "check_in", CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(24 * time.Hour), State: "pending", CreatedBy: a.ID}))
	handler := (&Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18800, ControlToken: "fixture-control", Health: &health.Service{Hub: h, Link: link, Home: h.Home}}).Handler()
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/v1/tasks/" + v.ID + "/later"
	if w := request("POST", path, credential, `{}`); w.Code != 403 {
		t.Fatal("AI snoozed owner task", w.Code)
	}
	for _, body := range []string{`{"until":"invalid"}`, `{"until":"2000-01-01T09:00:00Z"}`, `{"extra":true}`} {
		if w := request("POST", path, "fixture-control", body); w.Code != 400 {
			t.Fatal("invalid later accepted", w.Code)
		}
	}
	w := request("POST", path, "fixture-control", ``)
	if w.Code != 200 {
		t.Fatal("default later", w.Code, w.Body.String())
	}
	for _, read := range []string{"/v1/home", "/v1/tasks", "/v1/tasks/" + v.ID, "/v1/tasks/feed"} {
		w := request("GET", read, "fixture-control", "")
		if w.Code != 200 {
			t.Fatal(read, w.Code)
		}
		if strings.Contains(w.Body.String(), `"needs_you":true`) || !strings.Contains(w.Body.String(), `"later_until"`) {
			t.Fatal("snooze projection differs", read, w.Body.String())
		}
	}
	for _, read := range []string{"/v1/home", "/v1/suggestions"} {
		w := request("GET", read, "fixture-control", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "fixture-review") {
			t.Fatal("snoozed review still needs owner", read, w.Code)
		}
	}
	w = request("GET", "/v1/map/mesh", "fixture-control", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var m circles.UnderstandingMap
	testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &m))
	for _, n := range m.Nodes {
		if n.NeedsYou != nil && *n.NeedsYou {
			t.Fatal("map retained red marker")
		}
	}
	got, err := svc.List(&a, "open", 100)
	testkit.Must(t, err)
	if len(got) != 1 || got[0].NeedsYou || got[0].LaterUntil == nil {
		t.Fatal("AI list differs")
	}
	// A pre-snooze due event is filtered by the same attention projection.
	_, err = h.Repo.DB.Exec("INSERT INTO task_events VALUES('fixture-due',?,?, 'task_due','Check-in fired','{}')", v.ID, now.Format(time.RFC3339Nano))
	testkit.Must(t, err)
	_, err = h.Repo.DB.Exec("INSERT INTO agent_updates(agent,at,kind,ref) VALUES(?,?,'task_due','fixture-due')", a.ID, now.Format(time.RFC3339Nano))
	testkit.Must(t, err)
	updates, err := mc.Call(httptest.NewRequest("GET", "/", nil).Context(), a.ID, "get_updates", map[string]any{})
	testkit.Must(t, err)
	data, err := json.Marshal(updates)
	testkit.Must(t, err)
	if strings.Contains(string(data), `"kind":"task_due"`) {
		t.Fatal("get_updates reraised snoozed ask")
	}
	w = request("DELETE", path, "fixture-control", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"needs_you":true`) {
		t.Fatal("bring back failed", w.Code)
	}
}

func TestU2ArchivedCircleAllOwnerAndAgentSurfaces(t *testing.T) {
	h, p, link := testkit.Open(t)
	testkit.Seed(t, h, p)
	cs := &circles.Service{Hub: h, People: p}
	archived := model.Circle{ID: "old-circle", Name: "Old study circle", Kind: "friends", Confirmed: true, Archived: true, SendRule: "confirm", Chats: []model.CircleChat{{ChatJID: testkit.Phone}}, People: []model.CirclePerson{{PersonID: model.PersonID(testkit.Phone)}}}
	live := model.Circle{ID: "live-circle", Name: "Design circle", Kind: "community", Confirmed: true, SendRule: "confirm", Chats: []model.CircleChat{{ChatJID: testkit.Group}}}
	for _, c := range []model.Circle{archived, live} {
		testkit.Must(t, h.Repo.Put("circles", c.ID, c))
	}
	a, _, err := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, err)
	a.Permissions.Access = model.Access{Circles: []string{archived.ID}, Unsorted: false}
	testkit.Must(t, h.Repo.Put("agents", a.ID, a))
	// Before this fix this exact archived-only grant could still read Phone.
	if h.CanChat(&a, testkit.Phone) {
		t.Fatal("archived circle grants chats")
	}
	a.Permissions.Access = model.Access{Circles: []string{}, Unsorted: true}
	if h.CanChat(&a, testkit.Phone) {
		t.Fatal("archive widened unsorted access")
	}
	if _, err = cs.Circle(&a, archived.ID); err != model.ErrNotFound {
		t.Fatal("AI found archive", err)
	}
	for _, agent := range []*model.Agent{nil, &a} {
		list, err := cs.List(agent)
		testkit.Must(t, err)
		for _, c := range list {
			if !c.Live() {
				t.Fatal("nonlive list entry")
			}
		}
	}
	raw, err := cs.Circle(nil, archived.ID)
	testkit.Must(t, err)
	if !raw.Archived {
		t.Fatal("owner cannot restore")
	}
	for _, mapRead := range []func() (any, error){cs.Map, cs.MapMesh} {
		value, err := mapRead()
		testkit.Must(t, err)
		m := value.(circles.UnderstandingMap)
		for _, n := range m.Nodes {
			if n.ID == archived.ID || n.CircleID == archived.ID || n.PrimaryCircleID == archived.ID || model.Contains(n.Circles, archived.ID) {
				t.Fatal("archive in map node")
			}
		}
		for _, e := range m.Edges {
			if e.Source == archived.ID || e.Target == archived.ID {
				t.Fatal("archive in map edge")
			}
		}
	}
	if _, err = cs.MapFocus(archived.ID); err != model.ErrNotFound {
		t.Fatal("archive map focus found")
	}
	ps, err := p.PeopleAll(nil, "", true)
	testkit.Must(t, err)
	for _, person := range ps {
		if person.PrimaryCircleID == archived.ID {
			t.Fatal("archive primary circle")
		}
		for _, af := range person.CircleAffinity {
			if af.CircleID == archived.ID {
				t.Fatal("archive affinity")
			}
		}
	}
	groups, err := p.Groups(nil, "")
	testkit.Must(t, err)
	for _, g := range groups {
		if model.Contains(g.CircleIDs, archived.ID) {
			t.Fatal("archive group circle")
		}
	}
	mc := &mcpserver.Service{Hub: h, People: p, Link: link}
	v, err := mc.TaskService().Create(nil, tasks.Create{Title: "Review questions", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	_, err = h.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.circle_ids',json(?)) WHERE id=?", `["old-circle"]`, v.ID)
	testkit.Must(t, err)
	cards, err := mc.TaskService().List(nil, "all", 100)
	testkit.Must(t, err)
	for _, c := range cards {
		if model.Contains(c.CircleIDs, archived.ID) {
			t.Fatal("archive in task card")
		}
	}
	handler := (&Server{Hub: h, People: p, MCP: mc, Link: link, Port: 18800, ControlToken: "fixture-control"}).Handler()
	for _, query := range []string{"/v1/circles", "/v1/circles?archived=1"} {
		r := httptest.NewRequest("GET", query, nil)
		r.Host = "127.0.0.1:18800"
		r.Header.Set("Authorization", "Bearer fixture-control")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(query, w.Code)
		}
		var result struct {
			Circles []model.Circle `json:"circles"`
		}
		testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &result))
		if len(result.Circles) != 1 || result.Circles[0].Archived != (query != "/v1/circles") {
			t.Fatal("archive filter failed", query)
		}
	}
}

func TestU2SendPolicyIgnoresArchivedAndPendingCircles(t *testing.T) {
	h, _, _ := testkit.Open(t)
	c := model.Circle{ID: "fixture-policy", Name: "Design Club", Chats: []model.CircleChat{{ChatJID: testkit.Group}}}
	for _, rule := range []string{"free", "never", "ask_in_limmr"} {
		for _, state := range []struct {
			confirmed, archived bool
			want                string
		}{
			{false, false, "confirm"}, {true, true, "confirm"}, {true, false, rule},
		} {
			c.Confirmed, c.Archived, c.SendRule = state.confirmed, state.archived, rule
			testkit.Must(t, h.Repo.Put("circles", c.ID, c))
			got, err := h.SendPolicy(testkit.Group)
			testkit.Must(t, err)
			if got != state.want {
				t.Fatalf("policy = %q, want %q", got, state.want)
			}
		}
	}
}
