package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"limmr/internal/efforts"
	"limmr/internal/health"
	"limmr/internal/model"
	"limmr/internal/wa"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// No whatsmeow client: state changes cannot create network traffic.
type r4Connection struct{ linked, connected, outdated bool }

func (c *r4Connection) Linked() bool                                                  { return c.linked }
func (c *r4Connection) Connected() bool                                               { return c.connected }
func (*r4Connection) SyncDone() bool                                                  { return true }
func (c *r4Connection) Outdated() bool                                                { return c.outdated }
func (*r4Connection) PhoneSeen() *time.Time                                           { return nil }
func (*r4Connection) Reconnect(context.Context) error                                 { return nil }
func (*r4Connection) RefreshGroups(context.Context) error                             { return nil }
func (*r4Connection) RequestHistory(context.Context) error                            { return nil }
func (*r4Connection) Start(context.Context, string, string, func(wa.LinkEvent)) error { return nil }
func (*r4Connection) Cancel()                                                         {}
func (*r4Connection) Unlink(context.Context) error                                    { return nil }

func TestR4ConnectionLossCachedHTTPAndMCP(t *testing.T) {
	r := newRig(t)
	c := &r4Connection{}
	r.link.Source, r.api.Health.Connection, r.mc.Link = c, c, r.link
	ctx, session, _ := sdkSession(t, r)
	for _, phase := range []struct {
		name                        string
		linked, connected, outdated bool
	}{
		{"ready", true, true, false}, {"disconnected", true, false, false},
		{"backoff", true, false, false}, {"reconnected", true, true, false},
		{"logged_out", false, false, false}, {"stale_keys", true, false, false},
		{"outdated", true, false, true},
	} {
		t.Run(phase.name, func(t *testing.T) {
			c.linked, c.connected, c.outdated = phase.linked, phase.connected, phase.outdated
			event := wa.LinkEvent{State: "connecting"}
			if phase.connected {
				event.State = "ready"
			}
			if phase.name == "logged_out" || phase.name == "stale_keys" || phase.outdated {
				event = wa.LinkEvent{State: "error", Reason: "Synthetic session requires attention.", Fix: "Check Health."}
			}
			r.link.Emit(event)
			w := r.request(t, "GET", "/v1/status", r.api.StatusToken, nil, nil)
			status(t, w, http.StatusOK)
			var st map[string]any
			must(t, json.Unmarshal(w.Body.Bytes(), &st))
			if st["linked"] != phase.linked || st["connected"] != phase.connected || st["engine_version"] != health.Version {
				t.Fatal("status lied about connection/version")
			}
			if !phase.connected && st["health"] == "ok" {
				t.Fatal("offline status claimed healthy")
			}
			who, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
			must(t, err)
			if who.IsError {
				t.Fatal("offline identity failed")
			}
			data := who.StructuredContent.(map[string]any)["data"].(map[string]any)
			wa := data["whatsapp"].(map[string]any)
			if wa["linked"] != phase.linked || wa["connected"] != phase.connected || (!phase.connected && data["can_send"] != false) {
				t.Fatal("MCP identity lied about connection")
			}
			if event.State == "error" && data["health"] != event.Reason {
				t.Fatal("MCP described a terminal failure as a retry")
			}
			read, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "read", Arguments: map[string]any{"mode": "chat", "chat_jid": chatA}})
			must(t, err)
			if read.IsError {
				t.Fatal("offline cached MCP read failed")
			}
			if read.StructuredContent == nil {
				t.Fatal("cached read has no data")
			}
			cached := read.StructuredContent.(map[string]any)["data"].(map[string]any)["messages"].([]any)
			if len(cached) != 3 {
				t.Fatal("connection loss changed the cached read")
			}
			status(t, r.request(t, "GET", "/v1/chats", r.api.ControlToken, nil, nil), 200)
		})
	}
}

// Run only in an owned subprocess with closed stdin. os.Exit skips all cleanup,
// leaving SQLite to recover an uncommitted write transaction at the next open.
func TestR4CrashWriter(t *testing.T) {
	if os.Getenv("LIMMR_R4_CRASH_WRITER") != "1" {
		return
	}
	s, err := wa.OpenStore(filepath.Join(os.Getenv("LIMMR_HOME"), "messages.db"))
	if err != nil {
		os.Exit(21)
	}
	if _, err = s.DB.Exec("PRAGMA cache_size=10"); err != nil {
		os.Exit(24)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		os.Exit(22)
	}
	_, err = tx.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<3000)
	 INSERT INTO messages(id,chat_jid,content,timestamp)
	 SELECT 'crash-'||i,'15550100001@s.whatsapp.net','uncommitted needle '||printf('%01000d',i),CURRENT_TIMESTAMP FROM n`)
	if err != nil {
		os.Exit(23)
	}
	os.Exit(17)
}

func TestR4CrashRecoveryReplayAndEfforts(t *testing.T) {
	r := newRig(t)
	for i, text := range []string{"Pitch deck opening slides", "Pitch deck slides layout is ready", "Can you review the pitch deck slides? Due tomorrow."} {
		must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("deck-%d", i), ChatJID: chatA, Sender: chatA, Content: text, Timestamp: epoch.Add(-time.Duration(3-i) * time.Minute)}))
	}
	before, err := (&efforts.Service{Hub: r.h}).List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if len(before) == 0 {
		t.Fatal("effort fixture is empty")
	}
	must(t, r.m.Close())
	child := exec.Command(os.Args[0], "-test.run=^TestR4CrashWriter$")
	child.Env = append(os.Environ(), "LIMMR_R4_CRASH_WRITER=1", "LIMMR_HOME="+r.home)
	// nil Stdin is /dev/null; do not inherit any terminal or real data path.
	err = child.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 17 {
		t.Fatal("crash writer did not reach interrupted transaction")
	}
	start := time.Now()
	r.m, err = wa.OpenStore(filepath.Join(r.home, "messages.db"))
	must(t, err)
	defer r.m.Close()
	r.h.Messages = r.m
	var integrity string
	must(t, r.m.DB.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	if integrity != "ok" {
		t.Fatal("recovered database inconsistent")
	}
	_, err = r.m.DB.Exec(`INSERT INTO messages_fts(messages_fts,rank) VALUES('integrity-check',1)`)
	must(t, err)
	_, n := r.m.Counts()
	if n != 15 {
		t.Fatal("partial transaction survived crash")
	}
	ms, err := r.m.Messages(wa.Query{Chat: chatA}, []string{chatA})
	must(t, err)
	for repeat := 0; repeat < 3; repeat++ {
		for _, m := range ms {
			must(t, r.m.PutMessage(m))
		}
	}
	_, n = r.m.Counts()
	if n != 15 {
		t.Fatal("history replay duplicated messages")
	}
	after, err := (&efforts.Service{Hub: r.h}).List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("fresh efforts recompute diverged after crash/replay")
	}
	t.Logf("crash reopen, integrity, replay and efforts recompute %.3fms", float64(time.Since(start))/float64(time.Millisecond))
}

func TestR4ParallelMCPDuringSyncBurst(t *testing.T) {
	r := newRig(t)
	ctx, session, _ := sdkSession(t, r)
	start := make(chan struct{})
	errs := make(chan error, 200)
	latencies := make(chan time.Duration, 160)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for batch := 0; batch < 20; batch++ {
			err := r.m.Batch(func(s *wa.Store) error {
				for j := 0; j < 200; j++ {
					if e := s.PutMessage(model.Message{ID: fmt.Sprintf("sync-%d-%d", batch, j), ChatJID: chatA, Sender: chatA, Content: "Synthetic sync needle", Timestamp: epoch}); e != nil {
						return e
					}
				}
				return nil
			})
			if err != nil {
				errs <- err
				return
			}
		}
	}()
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 20; i++ {
				at := time.Now()
				name, args := "read", map[string]any{"mode": "chat", "chat_jid": chatA, "limit": 20}
				if i%2 == 1 {
					name, args = "find", map[string]any{"query": "needle", "types": []string{"message"}, "limit": 20}
				}
				out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				latencies <- time.Since(at)
				if err != nil {
					errs <- err
					return
				}
				if out.IsError {
					errs <- fmt.Errorf("MCP %s returned a client-visible error", name)
					return
				}
			}
		}()
	}
	at := time.Now()
	close(start)
	wg.Wait()
	close(errs)
	close(latencies)
	for err := range errs {
		t.Errorf("parallel request/sync: %v", err)
	}
	ds := []time.Duration{}
	for d := range latencies {
		ds = append(ds, d)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if len(ds) != 160 {
		t.Fatalf("completed %d of 160 calls", len(ds))
	}
	if ds[len(ds)-1] > 4*time.Second {
		t.Fatal("MCP latency exceeded 4s bound")
	}
	_, n := r.m.Counts()
	if n != 4012 {
		t.Fatal("sync burst lost committed messages")
	}
	t.Logf("160 MCP calls + 4000 messages: wall=%s p50=%s p95=%s max=%s", time.Since(at), ds[len(ds)/2], ds[len(ds)*95/100], ds[len(ds)-1])
}

func TestR4Retention50KAndExplicitVacuum(t *testing.T) {
	r := newRig(t)
	at := time.Now()
	must(t, r.m.Batch(func(s *wa.Store) error {
		for i := 0; i < 50000; i++ {
			if err := s.PutMessage(model.Message{ID: fmt.Sprintf("growth-%d", i), ChatJID: chatA, Content: "Synthetic archived message", Timestamp: epoch.AddDate(-1, 0, 0)}); err != nil {
				return err
			}
		}
		return nil
	}))
	seedTime := time.Since(at)
	_, err := r.h.Repo.DB.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<50000)
	 INSERT INTO activity(at,data) SELECT CASE WHEN i<=45000 THEN ? ELSE ? END,
	 json_object('agent_id',?,'tool','read','kind','read','note',printf('%01000d',i)) FROM n`, epoch.AddDate(-1, 0, 0), epoch, r.a.ID)
	must(t, err)
	// Snapshot the physical file before purging; the explicit action owns VACUUM.
	_, err = r.h.Repo.DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	must(t, err)
	path := filepath.Join(r.home, "hub.db")
	before, err := os.Stat(path)
	must(t, err)
	at = time.Now()
	must(t, r.h.Retain())
	purgeTime := time.Since(at)
	var count int
	must(t, r.h.Repo.DB.QueryRow("SELECT count(*) FROM activity").Scan(&count))
	if count != 5000 {
		t.Fatal("retention boundary lost recent activity")
	}
	_, messages := r.m.Counts()
	if messages != 50012 {
		t.Fatal("activity retention deleted message history")
	}
	at = time.Now()
	must(t, r.api.Health.Fix(context.Background(), "vacuum"))
	vacuumTime := time.Since(at)
	after, err := os.Stat(path)
	must(t, err)
	if after.Size() >= before.Size()/2 {
		t.Fatal("VACUUM did not reclaim purged activity space")
	}
	for _, db := range []string{"messages.db", "hub.db"} {
		if info, err := os.Stat(filepath.Join(r.home, db+"-wal")); err == nil && info.Size() != 0 {
			t.Fatal("VACUUM left a large WAL")
		}
	}
	t.Logf("50k-message seed=%s; 50k activity purge=%s; two-store VACUUM=%s; hub bytes %d -> %d", seedTime, purgeTime, vacuumTime, before.Size(), after.Size())
}
