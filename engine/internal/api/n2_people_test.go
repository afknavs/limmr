package api

import (
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// 120 invented people, 12 groups, 6 circles, 165 days, 39,600 messages,
// 13,464,000 bytes of invented content. No copied account data or sockets.
func n2Fixture(t testing.TB) *hub.Service {
	h, p, _ := testkit.Open(t)
	xs := []hub.Mutation{}
	phone := func(i int) string { return fmt.Sprintf("1555%07d@s.whatsapp.net", i) }
	tx, e := h.Messages.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 120; i++ {
		id := fmt.Sprintf("n2-%03d", i)
		xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Value: model.Person{ID: id, JIDs: []string{phone(i)}, Name: fmt.Sprintf("Maya Example %03d", i)}})
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: id, Value: model.Membership{Person: id, Group: fmt.Sprintf("155500%d@g.us", i%12)}})
		_, e = tx.Exec("INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?)", phone(i), "Invented conversation", testkit.Epoch)
		testkit.Must(t, e)
	}
	for i := 0; i < 12; i++ {
		jid := fmt.Sprintf("155500%d@g.us", i)
		xs = append(xs, hub.Mutation{Bucket: "groups", ID: jid, Value: model.Group{JID: jid, Name: fmt.Sprintf("Sketch Club %d", i), MemberCount: 10}})
		_, e = tx.Exec("INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?)", jid, "Invented group", testkit.Epoch)
		testkit.Must(t, e)
	}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("circle-%d", i)
		xs = append(xs, hub.Mutation{Bucket: "circles", ID: id, Value: model.Circle{ID: id, Name: fmt.Sprintf("Sketch circle %d", i), Confirmed: true, Chats: []model.CircleChat{{ChatJID: fmt.Sprintf("155500%d@g.us", i)}, {ChatJID: fmt.Sprintf("155500%d@g.us", i+6)}}}})
	}
	stmt, e := tx.Prepare("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me) VALUES(?,?,?,?,?,?)")
	testkit.Must(t, e)
	text := strings.Repeat("Invented sketch planning. ", 14)[:340]
	for day := 0; day < 165; day++ {
		for i := 0; i < 240; i++ {
			chat := phone(i % 120)
			if i >= 120 {
				chat = fmt.Sprintf("155500%d@g.us", i%12)
			}
			_, e = stmt.Exec(fmt.Sprintf("m-%d-%d", day, i), chat, phone(i%120), text, testkit.Epoch.AddDate(0, 0, -day), i%2)
			testkit.Must(t, e)
		}
	}
	testkit.Must(t, stmt.Close())
	testkit.Must(t, tx.Commit())
	testkit.Must(t, h.Repo.Apply(xs...))
	_ = p
	return h
}
func n2Handler(h *hub.Service) http.Handler {
	p := &people.Service{Hub: h}
	return (&Server{Hub: h, People: p, MCP: &mcpserver.Service{Hub: h, People: p}, Port: 18800, ControlToken: "invented-n2"}).Handler()
}
func n2Request(t testing.TB, handler http.Handler, path string) {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:18800"
	r.Header.Set("Authorization", "Bearer invented-n2")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
}
func BenchmarkN2People(b *testing.B) {
	h := n2Fixture(b)
	for _, path := range []string{"/v1/people/n2-001", "/v1/people/n2-001/summary", "/v1/map", "/v1/graph"} {
		b.Run(path, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				handler := n2Handler(h)
				b.StartTimer()
				n2Request(b, handler, path)
			}
		})
	}
}
func TestN2PeopleBudgets(t *testing.T) {
	h := n2Fixture(t)
	t.Cleanup(func() {
		service := &people.Service{Hub: h}
		profile, err := service.Profile(nil, "n2-001")
		testkit.Must(t, err)
		if profile.Stats.DM30 != 31 || profile.Stats.DM365 != 165 || profile.Stats.GroupMessages != 165 || len(profile.Groups) != 1 || profile.Stats.LastDM == nil || !profile.Stats.LastDM.Equal(testkit.Epoch) {
			t.Fatalf("wrong fixture statistics: %+v", profile.Stats)
		}
		testkit.Must(t, h.Repo.Put("person_summaries", "n2-001", people.PersonSummary{PersonID: "n2-001", Text: "Invented planning summary"}))
		start := time.Now()
		summary, err := service.PersonSummary("n2-001")
		testkit.Must(t, err)
		if summary == nil || summary.Text != "Invented planning summary" {
			t.Fatal("eligible saved summary missing")
		}
		if elapsed := time.Since(start); elapsed > n2Budget(50*time.Millisecond) {
			t.Errorf("saved summary: %v exceeds %v", elapsed, n2Budget(50*time.Millisecond))
		}
	})
	for _, v := range []struct {
		path   string
		budget time.Duration
	}{{"/v1/people/n2-001", 150 * time.Millisecond}, {"/v1/people/n2-001/summary", 50 * time.Millisecond}, {"/v1/map", 500 * time.Millisecond}, {"/v1/graph", 500 * time.Millisecond}} {
		t.Run(v.path, func(t *testing.T) {
			handler := n2Handler(h)
			start := time.Now()
			n2Request(t, handler, v.path)
			elapsed := time.Since(start)
			t.Logf("cold: %.2f ms", float64(elapsed.Microseconds())/1000)
			if budget := n2Budget(v.budget); elapsed > budget {
				t.Errorf("%v exceeds %v", elapsed, budget)
			}
		})
	}
}

// Shared CI runners are slower and noisier than a developer Mac; keep local budgets strict.
func n2Budget(d time.Duration) time.Duration {
	if os.Getenv("CI") != "" {
		return 3 * d
	}
	return d
}
