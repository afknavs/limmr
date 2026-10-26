package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"limmr/internal/circles"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/remote"
	"limmr/internal/startup"
	"limmr/internal/wa"
	"limmr/internal/watches"
)

// Same 300-person, 300-chat, six-circle, 50k-message shape as A2. Entirely synthetic.
func seedStartupScale(t *testing.T, home string) {
	t.Helper()
	r, err := hub.Open(filepath.Join(home, "hub.db"))
	mustStartup(t, err)
	defer r.Close()
	m, err := wa.OpenStore(filepath.Join(home, "messages.db"))
	mustStartup(t, err)
	defer m.Close()
	epoch := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	circles := make([]model.Circle, 6)
	mutations := []hub.Mutation{}
	for i := range circles {
		circles[i] = model.Circle{ID: fmt.Sprintf("scale-circle-%d", i), Name: fmt.Sprintf("Example circle %d", i), Kind: "project", Confirmed: true, SendRule: "confirm"}
	}
	mustStartup(t, m.Batch(func(store *wa.Store) error {
		for i := 0; i < 300; i++ {
			jid := fmt.Sprintf("155502%05d@s.whatsapp.net", i)
			name := fmt.Sprintf("Example Person %d", i)
			mutations = append(mutations, hub.Mutation{Bucket: "people", ID: jid, Value: model.Person{ID: jid, JIDs: []string{jid}, Name: name}})
			circles[i%6].Chats = append(circles[i%6].Chats, model.CircleChat{ChatJID: jid})
			if err := store.PutChat(model.Chat{JID: jid, Name: name, LastMessageTime: &epoch}); err != nil {
				return err
			}
		}
		for i := 0; i < 50000; i++ {
			jid := fmt.Sprintf("155502%05d@s.whatsapp.net", i%300)
			if err := store.PutMessage(model.Message{ID: fmt.Sprint(i), ChatJID: jid, Sender: jid, Content: fmt.Sprintf("project%d slides draft", i%300), Timestamp: epoch.Add(-time.Duration(50000-i) * time.Minute), IsFromMe: i%2 == 0}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, c := range circles {
		mutations = append(mutations, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c})
	}
	mustStartup(t, r.Apply(mutations...))
	// Exercise the first legacy FTS backfill on cold open, and the marker on warm open.
	_, err = m.DB.Exec("DELETE FROM engine_meta WHERE key='fts_backfilled_v1'")
	mustStartup(t, err)
}
func mustStartup(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestR3StartupTimings(t *testing.T) {
	for _, scale := range []bool{false, true} {
		home := t.TempDir()
		if scale {
			seedStartupScale(t, home)
		}
		for _, pass := range []string{"cold", "warm"} {
			t.Run(fmt.Sprintf("scale=%t/%s", scale, pass), func(t *testing.T) {
				start := time.Now()
				observer := startup.Observer(func(stage string, d time.Duration) {
					if d > 0 {
						t.Logf("%s %.3fms", stage, float64(d)/float64(time.Millisecond))
					}
				})
				var r *hub.Repo
				var m *wa.Store
				mustStartup(t, startup.Measure(observer, "hub.open", func() (err error) { r, err = hub.OpenBase(filepath.Join(home, "hub.db")); return }))
				defer r.Close()
				mustStartup(t, startup.Measure(observer, "messages.open", func() (err error) { m, err = wa.OpenBaseStore(filepath.Join(home, "messages.db")); return }))
				defer m.Close()
				t.Logf("listener eligible %.3fms", float64(time.Since(start))/float64(time.Millisecond))
				mustStartup(t, r.Migrate(observer))
				mustStartup(t, m.UpgradeTimed(observer))
				var h *hub.Service
				mustStartup(t, startup.Measure(observer, "warmup.hub", func() error { h = hub.New(r, m, home); return nil }))
				h.Now = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }
				p := &people.Service{Hub: h}
				mc := &mcpserver.Service{Hub: h, People: p}
				var ws *watches.Service
				mustStartup(t, startup.Measure(observer, "warmup.watches", func() (err error) { ws, err = watches.New(h, p, watches.Options{}); return }))
				defer ws.Engine.Close()
				// An empty temporary device store only: never Connect, Resume or pair.
				var live *wa.Live
				mustStartup(t, startup.Measure(observer, "warmup.whatsapp", func() (err error) { live, err = wa.NewLive(home, m, p); return }))
				defer live.Close()
				if live.Linked() {
					t.Fatal("startup fixture must be unlinked")
				}

				rem := remote.New(h, mc, 18801)
				defer rem.Close()
				mustStartup(t, startup.Measure(observer, "warmup.people", func() error { _, err := p.People(nil, ""); return err }))
				mustStartup(t, startup.Measure(observer, "warmup.circles", func() error { _, err := (&circles.Service{Hub: h, People: p}).List(nil); return err }))
				mustStartup(t, startup.Measure(observer, "warmup.efforts", func() error {
					_, err := mc.EffortService().List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
					return err
				}))
				mustStartup(t, startup.Measure(observer, "remote.start", rem.Start))
				t.Logf("ready %.3fms", float64(time.Since(start))/float64(time.Millisecond))
			})
		}
	}
}
