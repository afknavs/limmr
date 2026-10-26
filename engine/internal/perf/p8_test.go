package perf_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"limmr/internal/api"
	"limmr/internal/circles"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"limmr/internal/wa"
	"limmr/internal/watches"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// P8 reproduces the measured people-heavy counts without reading any live store.
// Unspecified dimensions: six circles, one AI, one brief per circle. All names,
// messages, identities and timestamps are invented and deterministic.
func p8Fixture(t testing.TB) string {
	h, _, _ := testkit.Open(t)
	settings := h.Settings()
	settings.Owner = "Alex Example"
	testkit.Must(t, h.SetSettings(settings))
	xs := []hub.Mutation{{Bucket: "agents", ID: "example-ai", Value: model.Agent{ID: "example-ai", Name: "Example AI", Kind: "custom", Transport: "local", Enabled: true, Preset: "own", Permissions: model.Preset("own"), CreatedAt: testkit.Epoch}}}
	phone := func(i int) string { return fmt.Sprintf("1555%07d@s.whatsapp.net", i) }
	for i := 0; i < 4655; i++ {
		id := fmt.Sprintf("person-%04d", i)
		if i == 0 {
			id = "me"
		}
		js := []string{phone(i)}
		if i < 1400 {
			js = append(js, fmt.Sprintf("%d@lid", 900000000+i))
		}
		xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Value: model.Person{ID: id, JIDs: js, Name: fmt.Sprintf("Maya Example %04d", i), UpdatedAt: testkit.Epoch}})
		if i < 4376 {
			xs = append(xs, hub.Mutation{Bucket: "memberships", ID: fmt.Sprint(i), Value: model.Membership{Group: fmt.Sprintf("%d@g.us", 100000+i%85), Person: id}})
		}
		if i < 4362 {
			xs = append(xs, hub.Mutation{Bucket: "stats", ID: id, Value: people.Stats{SharedGroups: []string{fmt.Sprintf("%d@g.us", 100000+i%85)}, DM30: i % 20, DM365: i % 80}})
		}
		if i < 1006 {
			xs = append(xs, hub.Mutation{Bucket: "normalized_pairs", ID: fmt.Sprintf("%d@lid", 900000000+i), Value: phone(i)})
		}
	}
	for i := 0; i < 85; i++ {
		j := fmt.Sprintf("%d@g.us", 100000+i)
		xs = append(xs, hub.Mutation{Bucket: "groups", ID: j, Value: model.Group{JID: j, Name: fmt.Sprintf("Design Club %03d", i), MemberCount: 52}})
	}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("circle-%d", i)
		c := model.Circle{ID: id, Name: fmt.Sprintf("Design circle %d", i), Kind: "friends", Confirmed: true, SendRule: "confirm", CreatedBy: "you", Pinned: i == 0, UpdatedAt: testkit.Epoch, Chats: []model.CircleChat{}, People: []model.CirclePerson{}, Topics: []model.Topic{}}
		for k := i; k < 85; k += 6 {
			c.Chats = append(c.Chats, model.CircleChat{ChatJID: fmt.Sprintf("%d@g.us", 100000+k), IncludedSince: testkit.Epoch.AddDate(0, -1, 0)})
		}
		for k := i; k < 198; k += 6 {
			c.People = append(c.People, model.CirclePerson{PersonID: fmt.Sprintf("person-%04d", k+1), Source: "auto"})
		}
		xs = append(xs, hub.Mutation{Bucket: "circles", ID: id, Value: c}, hub.Mutation{Bucket: "briefs", ID: id + ":000000001", Value: model.Brief{CircleID: id, Version: 1, Summary: "Design planning and garden sketches.", ChatJIDs: circles.JIDs(c), CoversUntil: testkit.Epoch.Add(-48 * time.Hour), WrittenAt: testkit.Epoch, WrittenBy: "example-ai", OpenAsks: []model.OpenAsk{}, Upcoming: []model.Upcoming{}, PeopleNotes: []model.PeopleNote{}, Evidence: []string{}}})
	}
	for i := 0; i < 198; i++ {
		id := fmt.Sprintf("change-%03d", i)
		cid := fmt.Sprintf("circle-%d", i%6)
		xs = append(xs, hub.Mutation{Bucket: "affinity_proposed", ID: id, Value: true}, hub.Mutation{Bucket: "auto_circle_changes", ID: id, Value: map[string]any{"Change": circles.AutoChange{ID: id, CircleID: cid, Name: fmt.Sprintf("Design circle %d", i%6), Agent: "example-ai", Action: "placed", At: testkit.Epoch.Add(-time.Duration(i) * time.Minute), Count: 1, PeopleIDs: []string{fmt.Sprintf("person-%04d", i+1)}}, "AddedPeople": []model.CirclePerson{{PersonID: fmt.Sprintf("person-%04d", i+1), Source: "auto"}}}})
	}
	for i := 0; i < 42; i++ {
		id := fmt.Sprintf("draft-%d", i)
		xs = append(xs, hub.Mutation{Bucket: "drafts", ID: id, Value: model.Draft{ID: id, AgentID: "example-ai", ChatJID: phone(i + 1), FinalText: "Synthetic draft for review", State: "draft", CreatedAt: testkit.Epoch, ExpiresAt: testkit.Epoch.Add(time.Hour)}})
	}
	testkit.Must(t, h.Repo.Apply(xs...))
	_, e := h.Repo.DB.Exec("UPDATE agent_updates SET at=?", testkit.Epoch.Format(time.RFC3339Nano))
	testkit.Must(t, e)
	tx, e := h.Messages.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 1400; i++ {
		_, e = tx.Exec("INSERT INTO lid_aliases VALUES(?,?)", fmt.Sprintf("%d@lid", 900000000+i), phone(i))
		testkit.Must(t, e)
	}
	chat := func(i int) string {
		if i < 536 {
			return phone(i + 1)
		}
		return fmt.Sprintf("%d@g.us", 100000+i-536)
	}
	for i := 0; i < 621; i++ {
		_, e = tx.Exec("INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?)", chat(i), fmt.Sprintf("Design conversation %03d", i), testkit.Epoch.Add(-time.Duration(i%200)*time.Hour))
		testkit.Must(t, e)
	}
	st, e := tx.Prepare("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me) VALUES(?,?,?,?,?,?)")
	testkit.Must(t, e)
	for i := 0; i < 14000; i++ {
		_, e = st.Exec(fmt.Sprintf("message-%05d", i), chat(i%621), phone(i%4655), fmt.Sprintf("Synthetic design planning garden sketches project %d", i%12), testkit.Epoch.Add(-time.Duration(i%200)*time.Hour), i%3 == 0)
		testkit.Must(t, e)
	}
	testkit.Must(t, st.Close())
	testkit.Must(t, tx.Commit())
	tx, e = h.Repo.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 864; i++ {
		v := model.Activity{AgentID: "example-ai", Tool: "list_messages", Kind: "read", ChatJIDs: []string{chat(i % 621)}, MessageIDs: []string{fmt.Sprintf("message-%05d", i)}, At: testkit.Epoch.Add(-time.Duration(i) * time.Minute), Count: 1}
		raw, _ := json.Marshal(v)
		_, e = tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", v.At, string(raw))
		testkit.Must(t, e)
	}
	testkit.Must(t, tx.Commit())
	for bucket, want := range map[string]int{"people": 4655, "memberships": 4376, "stats": 4362, "normalized_pairs": 1006, "affinity_proposed": 198, "auto_circle_changes": 198, "groups": 85, "drafts": 42} {
		var n int
		testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket=?", bucket).Scan(&n))
		if n != want {
			t.Fatalf("%s count %d != %d", bucket, n, want)
		}
	}
	for table, want := range map[string]int{"chats": 621, "messages": 14000, "lid_aliases": 1400} {
		var n int
		testkit.Must(t, h.Messages.DB.QueryRow("SELECT count(*) FROM "+table).Scan(&n))
		if n != want {
			t.Fatalf("%s count %d != %d", table, n, want)
		}
	}
	var n int
	testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM activity").Scan(&n))
	if n != 864 {
		t.Fatal("activity shape")
	}
	home := h.Home
	testkit.Must(t, h.Repo.Close())
	testkit.Must(t, h.Messages.Close())
	return home
}

type p8Rig struct {
	h       *hub.Service
	mc      *mcpserver.Service
	handler http.Handler
}

func p8Open(t testing.TB, home string) *p8Rig {
	r, e := hub.Open(filepath.Join(home, "hub.db"))
	testkit.Must(t, e)
	m, e := wa.OpenStore(filepath.Join(home, "messages.db"))
	testkit.Must(t, e)
	h := hub.New(r, m, home)
	h.Now = func() time.Time { return testkit.Epoch }
	p := &people.Service{Hub: h}
	w, e := watches.New(h, p, watches.Options{})
	testkit.Must(t, e)
	mc := &mcpserver.Service{Hub: h, People: p, Watches: w}
	s := &api.Server{Hub: h, People: p, MCP: mc, Port: 18800, ControlToken: "synthetic-p8-control"}
	return &p8Rig{h: h, mc: mc, handler: s.Handler()}
}
func (r *p8Rig) close() { r.h.Repo.Close(); r.h.Messages.Close() }

var p8Cases = []struct {
	name, path string
	args       mcpserver.Args
}{{"Home", "/v1/home", nil}, {"Circles", "/v1/circles", nil}, {"People", "/v1/people", nil}, {"Map", "/v1/map", nil}, {"Efforts", "/v1/efforts", nil}, {"Brief", "brief", mcpserver.Args{"action": "get", "id": "circle-0"}}, {"Updates", "get_updates", mcpserver.Args{"wait_seconds": 0}}}

func (r *p8Rig) call(t testing.TB, i int) []byte {
	c := p8Cases[i]
	if c.path[0] == '/' {
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800"+c.path, nil)
		req.Header.Set("Authorization", "Bearer synthetic-p8-control")
		w := httptest.NewRecorder()
		r.handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s HTTP %d: %s", c.name, w.Code, w.Body.String())
		}
		var out any
		testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &out))
		raw, e := json.Marshal(out)
		testkit.Must(t, e)
		return raw
	}
	argsRaw, e := json.Marshal(c.args)
	testkit.Must(t, e)
	args, e := mcpserver.DecodeArgs(argsRaw)
	testkit.Must(t, e)
	v, e := r.mc.Call(context.Background(), "example-ai", c.path, args)
	testkit.Must(t, e)
	// Compare the authenticated cursor payload, not its randomized ciphertext.
	if u, ok := v.(watches.Updates); ok {
		cursor, e := r.h.ReadUpdateCursor("example-ai", u.Cursor)
		testkit.Must(t, e)
		u.Cursor = ""
		v = map[string]any{"updates": u, "cursor_payload": cursor}
	}
	raw, e := json.Marshal(v)
	testkit.Must(t, e)
	return raw
}
func TestP8Golden(t *testing.T) {
	home := p8Fixture(t)
	for i, c := range p8Cases {
		t.Run(c.name, func(t *testing.T) {
			r := p8Open(t, home)
			defer r.close()
			// P9 deliberately serves cold Home/hints without efforts. Keep comparing
			// the completed response against the goldens. E7 updates only Brief/Updates
			// for the lower naming count after uninvolved broadcasts are removed.
			primeHints := func() {
				if c.name == "Home" || c.name == "Brief" || c.name == "Updates" {
					_, err := r.mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
					testkit.Must(t, err)
				}
			}
			primeHints()
			got := p9WithoutReadiness(t, r.call(t, i))
			path := filepath.Join("testdata", "p8-"+c.name+".json.gz")
			if os.Getenv("P8_WRITE_GOLDEN") == "1" {
				f, e := os.Create(path)
				testkit.Must(t, e)
				z := gzip.NewWriter(f)
				_, e = z.Write(got)
				testkit.Must(t, e)
				testkit.Must(t, z.Close())
				testkit.Must(t, f.Close())
			}
			f, e := os.Open(path)
			testkit.Must(t, e)
			defer f.Close()
			z, e := gzip.NewReader(f)
			testkit.Must(t, e)
			defer z.Close()
			want, e := io.ReadAll(z)
			testkit.Must(t, e)
			want = p9WithoutReadiness(t, want)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s cold JSON differs from pre-optimization golden", c.name)
			}
			primeHints()
			if warm := p9WithoutReadiness(t, r.call(t, i)); !bytes.Equal(warm, want) {
				t.Fatalf("%s warm JSON differs from pre-optimization golden", c.name)
			}
		})
	}
}
func BenchmarkP8(b *testing.B) {
	home := p8Fixture(b)
	for i, c := range p8Cases {
		b.Run(c.name, func(b *testing.B) {
			for _, cold := range []bool{true, false} {
				label := "Warm"
				if cold {
					label = "Cold"
				}
				b.Run(label, func(b *testing.B) {
					r := p8Open(b, home)
					defer func() { r.close() }()
					if !cold {
						r.call(b, i)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for n := 0; n < b.N; n++ {
						if cold && n > 0 {
							b.StopTimer()
							r.close()
							r = p8Open(b, home)
							b.StartTimer()
						}
						r.call(b, i)
					}
				})
			}
		})
	}
}

// Readiness is the only additive contract change; all existing response fields
// still compare byte-for-byte to the stored E6 references after warmup.
func p9WithoutReadiness(t testing.TB, raw []byte) []byte {
	var value any
	testkit.Must(t, json.Unmarshal(raw, &value))
	var strip func(any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "efforts_ready")
			for _, child := range x {
				strip(child)
			}
		case []any:
			for _, child := range x {
				strip(child)
			}
		}
	}
	strip(value)
	out, err := json.Marshal(value)
	testkit.Must(t, err)
	return out
}
