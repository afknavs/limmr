package integration_test

import (
	"fmt"
	"limmr/internal/api"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"net/http/httptest"
	"testing"
	"time"
)

func p4Dense(t testing.TB) *circles.Service {
	h, p, _ := testkit.Open(t)
	mutations := []hub.Mutation{}
	for i := 0; i < 300; i++ {
		id := fmt.Sprintf("p4-peer-%03d", i)
		mutations = append(mutations, hub.Mutation{Bucket: "people", ID: id, Value: model.Person{ID: id, Name: fmt.Sprintf("Invented Peer %03d", i), JIDs: []string{fmt.Sprintf("155508%05d@s.whatsapp.net", i)}}})
	}
	for c := 0; c < 8; c++ {
		circle := model.Circle{ID: fmt.Sprintf("p4-circle-%d", c), Name: fmt.Sprintf("Invented Circle %d", c), Confirmed: true}
		for j := 0; j < 50; j++ {
			circle.People = append(circle.People, model.CirclePerson{PersonID: fmt.Sprintf("p4-peer-%03d", (c*37+j)%300), Source: "you"})
		}
		mutations = append(mutations, hub.Mutation{Bucket: "circles", ID: circle.ID, Value: circle})
	}
	testkit.Must(t, h.Repo.Apply(mutations...))
	return &circles.Service{Hub: h, People: p}
}
func TestP4DenseCirclesAndMap(t *testing.T) {
	c := p4Dense(t)
	start := time.Now()
	xs, e := c.List(nil)
	testkit.Must(t, e)
	elapsed := time.Since(start)
	if len(xs) != 8 {
		t.Fatal("missing circles")
	}
	for _, x := range xs {
		if len(x.People) != 50 {
			t.Fatal("confirmed people lost")
		}
	}
	t.Logf("cold owner circles: %s", elapsed)
	if elapsed >= 300*time.Millisecond {
		t.Fatal("circles exceeded 300 ms")
	}
	v, e := c.Map()
	testkit.Must(t, e)
	m := v.(circles.UnderstandingMap)
	count := 0
	for _, n := range m.Nodes {
		if n.Kind == "person" {
			count++
		}
	}
	if count != 120 {
		t.Fatalf("overview people=%d", count)
	}
	for _, x := range xs {
		links := 0
		for _, edge := range m.Edges {
			if edge.Source == x.ID {
				links++
			}
		}
		if links < 1 {
			t.Fatal("bare confirmed circle")
		}
		v, e = c.MapFocus(x.ID)
		testkit.Must(t, e)
		focus := v.(circles.UnderstandingMap)
		n := 0
		for _, node := range focus.Nodes {
			if node.Kind == "person" {
				n++
			}
		}
		if n != 50 {
			t.Fatalf("focus people=%d", n)
		}
	}
	// Visibility is recomputed after a revision, never stale after deleting a person.
	testkit.Must(t, c.Hub.Repo.Delete("people", "p4-peer-000"))
	xs, e = c.List(nil)
	testkit.Must(t, e)
	for _, x := range xs {
		for _, p := range x.People {
			if p.PersonID == "p4-peer-000" {
				t.Fatal("deleted identity retained")
			}
		}
	}
}
func BenchmarkP4DenseCircles(b *testing.B) {
	c := p4Dense(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, e := c.List(nil)
		testkit.Must(b, e)
	}
}

func BenchmarkP4DenseCirclesEndpoint(b *testing.B) {
	c := p4Dense(b)
	server := &api.Server{Hub: c.Hub, People: c.People, MCP: &mcpserver.Service{Hub: c.Hub, People: c.People}, Port: 18800, ControlToken: "invented-control"}
	handler := server.Handler()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/circles", nil)
		req.Header.Set("Authorization", "Bearer invented-control")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 {
			b.Fatal("circle endpoint failed")
		}
	}
}

func TestP4ChatOnlyCircleAndFocusRoute(t *testing.T) {
	c := p4Dense(t)
	jid := "15550999999@s.whatsapp.net"
	testkit.Must(t, c.Hub.Messages.PutChat(model.Chat{JID: jid, Name: "Invented planning chat"}))
	testkit.Must(t, c.Hub.Repo.Put("circles", "p4-chat-only", model.Circle{ID: "p4-chat-only", Name: "Invented chat circle", Confirmed: true, Chats: []model.CircleChat{{ChatJID: jid}}}))
	value, e := c.Map()
	testkit.Must(t, e)
	found := false
	for _, n := range value.(circles.UnderstandingMap).Nodes {
		if n.ID == jid && n.Kind == "chat" {
			found = true
		}
	}
	if !found {
		t.Fatal("chat-only circle lost its square")
	}
	server := &api.Server{Hub: c.Hub, People: c.People, MCP: &mcpserver.Service{Hub: c.Hub, People: c.People}, Port: 18800, ControlToken: "invented-control"}
	handler := server.Handler()
	for _, check := range []struct {
		focus  string
		status int
	}{{"p4-circle-0", 200}, {"missing", 404}} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/map?focus="+check.focus, nil)
		req.Header.Set("Authorization", "Bearer invented-control")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != check.status {
			t.Fatal("focus route returned wrong status")
		}
	}
}
