// Synthetic account at the P1 scale. Never opens an installed app's data.
package perf_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"limmr/internal/api"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixture(t testing.TB) (*hub.Service, *people.Service, *circles.Service) {
	h, p, _ := testkit.Open(t)
	settings := h.Settings()
	settings.Owner = "Alex Example"
	testkit.Must(t, h.SetSettings(settings))
	xs := []hub.Mutation{}
	for i := 0; i < 3600; i++ {
		j := fmt.Sprintf("1555%07d@s.whatsapp.net", i)
		name := fmt.Sprintf("Maya Example %04d", i)
		if i >= 400 {
			name = fmt.Sprintf("1555%07d", i)
		}
		id := j
		if i == 0 {
			id = "me"
			name = "Alex Example"
		}
		xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Value: model.Person{ID: id, JIDs: []string{j}, Name: name}})
		xs = append(xs, hub.Mutation{Bucket: "memberships", ID: fmt.Sprint(i), Value: model.Membership{Group: fmt.Sprintf("%d@g.us", i%500), Person: id}})
	}
	testkit.Must(t, h.Repo.Apply(xs...))
	tx, e := h.Messages.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 600; i++ {
		j := fmt.Sprintf("%d@g.us", i)
		if i < 100 {
			j = fmt.Sprintf("1555%07d@s.whatsapp.net", i)
		}
		at := testkit.Epoch.Add(-time.Duration(i%200) * 24 * time.Hour)
		_, e = tx.Exec("INSERT INTO chats(jid,name,last_message_time) VALUES(?,?,?)", j, fmt.Sprintf("Design Club %03d", i), at)
		testkit.Must(t, e)
	}
	st, e := tx.Prepare("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me) VALUES(?,?,?,?,?,?)")
	testkit.Must(t, e)
	for i := 0; i < 50000; i++ {
		c := i % 600
		j := fmt.Sprintf("%d@g.us", c)
		if c < 100 {
			j = fmt.Sprintf("1555%07d@s.whatsapp.net", c)
		}
		_, e = st.Exec(fmt.Sprint(i), j, fmt.Sprintf("1555%07d@s.whatsapp.net", i%3600), strings.Repeat("Synthetic planning message. ", 30), testkit.Epoch.Add(-time.Duration(i%365)*24*time.Hour), false)
		testkit.Must(t, e)
	}
	testkit.Must(t, st.Close())
	testkit.Must(t, tx.Commit())
	return h, p, &circles.Service{Hub: h, People: p}
}
func BenchmarkLarge(b *testing.B) {
	h, p, c := fixture(b)
	_ = h
	for name, fn := range map[string]func() error{"People": func() error { _, e := p.People(nil, ""); return e }, "Graph": func() error { _, e := p.Graph(nil, "", 1, 150); return e }, "Map": func() error { _, e := c.Map(); return e }, "KnownMapping": func() error { return p.NormalizeMapping("900000001@lid", "15550000000@s.whatsapp.net") }} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				testkit.Must(b, fn())
			}
		})
	}
}

// Exercises the agent projection, including scope/people lookups per circle.
func BenchmarkL1ListCircles(b *testing.B) {
	h, _, c := fixture(b)
	a, _, e := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(b, e)
	for i := 0; i < 100; i++ {
		id := fmt.Sprint(i)
		testkit.Must(b, h.Repo.Put("circles", id, model.Circle{ID: id, Name: "Example circle", Confirmed: true, Chats: []model.CircleChat{{ChatJID: fmt.Sprintf("%d@g.us", 100+i)}}}))
	}
	// Compare the former repeated projection with the batched path using the
	// same warmed synthetic store, avoiding fixture and cold-cache skew.
	_, e = c.List(&a)
	testkit.Must(b, e)
	b.Run("RepeatedProjection", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			xs, e := hub.List[model.Circle](h.Repo, "circles")
			testkit.Must(b, e)
			for _, circle := range xs {
				_, e = c.Circle(&a, circle.ID)
				testkit.Must(b, e)
			}
		}
	})
	b.Run("BatchedProjection", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			xs, e := c.List(&a)
			testkit.Must(b, e)
			if len(xs) != 100 {
				b.Fatal("missing circles")
			}
		}
	})
}
func TestLargePayloads(t *testing.T) {
	h, p, c := fixture(t)
	ps, e := p.People(nil, "")
	testkit.Must(t, e)
	m, e := c.Map()
	testkit.Must(t, e)
	pb, _ := json.Marshal(ps)
	mb, _ := json.Marshal(m)
	a, token, e := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, e)
	s := &mcpserver.Service{Hub: h, People: p}
	v, e := s.Call(context.Background(), a.ID, "get_started", mcpserver.Args{})
	testkit.Must(t, e)
	setup, _ := json.Marshal(v)
	v, e = s.Call(context.Background(), a.ID, "list_messages", mcpserver.Args{"chat_jid": "15550000000@s.whatsapp.net"})
	testkit.Must(t, e)
	messages, _ := json.Marshal(v)
	t.Logf("people=%d map=%d setup=%d messages=%d orientation_tokens=%d", len(pb), len(mb), len(setup), len(messages), (len(setup)+len(messages)+3)/4)
	var exportedPeople json.RawMessage
	if os.Getenv("P1_FINAL") == "1" {
		server := &api.Server{Hub: h, People: p, MCP: s, Port: 18800, ControlToken: "synthetic-control"}
		handler := server.Handler()
		req := httptest.NewRequest("GET", "http://127.0.0.1:18800/v1/people?limit=50", nil)
		req.Header.Set("Authorization", "Bearer synthetic-control")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 {
			t.Fatal("people fixture HTTP failed")
		}
		exportedPeople = append([]byte{}, response.Body.Bytes()...)
		requestBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "scan_chats", "arguments": map[string]any{}}})
		req = httptest.NewRequest("POST", "http://127.0.0.1:18800/mcp", bytes.NewReader(requestBody))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 || response.Body.Len() >= 6000 {
			t.Fatal("scan MCP wire budget failed")
		}
		t.Logf("scan_mcp_wire_bytes=%d", response.Body.Len())
		scan, e := s.Call(context.Background(), a.ID, "scan_chats", mcpserver.Args{})
		testkit.Must(t, e)
		scanBytes, _ := json.Marshal(scan)
		pageBytes := []byte(exportedPeople)
		if len(pageBytes) >= 100000 || len(mb) >= 100000 || len(scanBytes) >= 6000 {
			t.Fatal("P1 payload budget exceeded")
		}
		var mapped struct {
			Nodes []any `json:"nodes"`
		}
		testkit.Must(t, json.Unmarshal(mb, &mapped))
		if len(mapped.Nodes) > 150 {
			t.Fatal("P1 node budget exceeded")
		}
		for _, p := range ps {
			if !model.Named(p) {
				t.Fatal("unnamed person surfaced")
			}
		}
		t.Logf("first_people_page=%d map_nodes=%d scan_bytes=%d scan_setup_tokens=%d", len(pageBytes), len(mapped.Nodes), len(scanBytes), (len(setup)+len(scanBytes)+3)/4)
	}
	if dir := os.Getenv("P1_EXPORT"); dir != "" {
		testkit.Must(t, os.MkdirAll(dir, 0700))
		chats, _ := h.Messages.Chats()
		g, _ := p.Graph(nil, "", 1, 300)
		if os.Getenv("P1_FINAL") == "1" {
			ps = ps[:min(50, len(ps))]
			chats = chats[:min(100, len(chats))]
		}
		values := map[string]any{"people": map[string]any{"people": ps}, "map": m, "chats": map[string]any{"chats": chats}, "graph": g}
		if len(exportedPeople) > 0 {
			values["people"] = exportedPeople
		}
		for name, v := range values {
			data, _ := json.Marshal(v)
			testkit.Must(t, os.WriteFile(filepath.Join(dir, name+".json"), data, 0600))
		}
	}
	if os.Getenv("P1_IDLE") == "1" {
		var before, after syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &before)
		time.Sleep(60 * time.Second)
		syscall.Getrusage(syscall.RUSAGE_SELF, &after)
		cpu := float64(after.Utime.Sec-before.Utime.Sec+after.Stime.Sec-before.Stime.Sec) + float64(after.Utime.Usec-before.Utime.Usec+after.Stime.Usec-before.Stime.Usec)/1e6
		t.Logf("idle_60s_cpu_pct=%.3f peak_rss_bytes=%d", cpu/60*100, after.Maxrss)
	}
}
