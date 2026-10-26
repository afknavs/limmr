package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/wa"
)

func TestA2LargeAccountBudgets(t *testing.T) {
	r := newRig(t)
	_, err := r.m.DB.Exec("DELETE FROM messages; DELETE FROM chats")
	must(t, err)
	_, err = r.h.Repo.DB.Exec("DELETE FROM objects WHERE bucket IN ('people','groups','memberships')")
	must(t, err)
	people := []hub.Mutation{}
	circles := make([]model.Circle, 6)
	for i := range circles {
		circles[i] = model.Circle{ID: fmt.Sprintf("scale-circle-%d", i), Name: fmt.Sprintf("Example circle %d", i), Kind: "project", Confirmed: true, SendRule: "confirm"}
	}
	must(t, r.m.Batch(func(store *wa.Store) error {
		for i := 0; i < 300; i++ {
			jid := fmt.Sprintf("155502%05d@s.whatsapp.net", i)
			name := fmt.Sprintf("Example Person %d", i)
			people = append(people, hub.Mutation{Bucket: "people", ID: jid, Value: model.Person{ID: jid, JIDs: []string{jid}, Name: name}})
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
		people = append(people, hub.Mutation{Bucket: "circles", ID: c.ID, Value: c})
	}
	must(t, r.h.Repo.Apply(people...))
	svc := r.mc.EffortService()
	start := time.Now()
	xs, err := svc.List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	must(t, err)
	t.Logf("cold 50k messages / 300 people: %s; efforts=%d", time.Since(start), len(xs))
	// Sustained 166/167-message topics remain whole; message volume alone
	// must not turn each topic into multiple episodes.
	if len(xs) != 300 {
		t.Fatal("scale fixture must retain 300 complete purposes")
	}
	audit, err := svc.Audit()
	must(t, err)
	if audit.Named+audit.Internal != 300 || audit.Accepted.Messages["129-999"] != 300 || audit.Largest.Messages != 167 || audit.Largest.Chats != 1 {
		t.Fatalf("scale fixture lost topics or exceeded episode bounds: %+v", audit)
	}
	must(t, r.m.PutMessage(model.Message{ID: "a2-arrival", ChatJID: "15550200000@s.whatsapp.net", Sender: "15550200000@s.whatsapp.net", Content: "project0 slides draft", Timestamp: epoch}))
	start = time.Now()
	next, err := svc.List(nil, efforts.Filter{IncludeUntitled: true, Limit: 500})
	must(t, err)
	elapsed := time.Since(start)
	if len(next) != len(xs) {
		t.Fatal("incremental inference changed unrelated purposes")
	}
	t.Logf("incremental efforts recompute: %s", elapsed)
	if elapsed >= 200*time.Millisecond {
		t.Errorf("incremental recompute exceeds 200ms: %s", elapsed)
	}
	// Warm route construction, icons and query caches before measuring steady Home.
	status(t, r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil), 200)
	start = time.Now()
	w := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	elapsed = time.Since(start)
	status(t, w, 200)
	var home map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &home))
	if home["circle_updates"] == nil {
		t.Fatal("missing effort Home projection")
	}
	t.Logf("Home with 50k messages: %s", elapsed)
	if elapsed >= 150*time.Millisecond {
		t.Errorf("Home exceeds 150ms: %s", elapsed)
	}
}

func TestA2OfflineStatusAndRetention(t *testing.T) {
	r := newRig(t)
	r.mc.Link = r.link
	for _, state := range []string{"unlinked", "connecting", "error", "ready"} {
		r.link.Emit(wa.LinkEvent{State: state})
		for attempt := 0; attempt < 3; attempt++ {
			out, err := r.call(t, "whoami", map[string]any{})
			must(t, err)
			v := out.(map[string]any)
			if v["can_send"] != (state == "ready") {
				t.Fatal("offline state permits sends")
			}
			if state != "ready" && v["health"] == "Connected" {
				t.Fatal("offline status claims connected")
			}
		}
	}
	before := epoch.AddDate(0, 0, -180)
	// Offset timestamps must be pruned by instant, including on the boundary date.
	for i, at := range []time.Time{before.Add(-time.Second).In(time.FixedZone("offset", 14*3600)), before, before.Add(time.Second)} {
		must(t, r.h.Record(model.Activity{At: at, AgentID: r.a.ID, Tool: fmt.Sprintf("retention-%d", i), Kind: "read"}))
	}
	must(t, r.h.Retain())
	rows, err := r.h.Activity(nil, hub.ActivityFilter{Limit: 100})
	must(t, err)
	seen := map[string]bool{}
	for _, v := range rows {
		seen[v.Tool] = true
	}
	if seen["retention-0"] || !seen["retention-1"] || !seen["retention-2"] {
		t.Fatal("retention is not instant-based at the cutoff")
	}
}

func TestA2RetentionInvalidatesEffortActionCache(t *testing.T) {
	r := newRig(t)
	svc := r.mc.EffortService()
	message := firstMessage(t, r, chatA)
	must(t, r.h.Record(model.Activity{At: epoch.AddDate(-1, 0, 0), AgentID: r.a.ID, Kind: "summary", Tool: "old-summary", ChatJIDs: []string{chatA}, MessageIDs: []string{message.ID}}))
	xs, err := svc.List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	count := func(xs []efforts.Effort) int {
		n := 0
		for _, x := range xs {
			n += len(x.AgentActionRefs)
		}
		return n
	}
	if count(xs) == 0 {
		t.Fatal("fixture activity did not attach")
	}
	must(t, r.h.Retain())
	xs, err = svc.List(nil, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if count(xs) != 0 {
		t.Fatal("retained cache exposes pruned activity")
	}
}
