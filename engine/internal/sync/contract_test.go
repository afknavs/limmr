package sync_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/people"
	syncer "limmr/internal/sync"
	"limmr/internal/tasks"
	"limmr/internal/wa"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var ctx = context.Background()

type engine struct {
	s     *syncer.Service
	h     *hub.Service
	tasks *tasks.Service
	p     *people.Service
	now   time.Time
	home  string
}

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func newEngine(t *testing.T) *engine {
	t.Helper()
	e := &engine{home: t.TempDir(), now: time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)}
	r, err := hub.Open(filepath.Join(e.home, "hub.db"))
	must(t, err)
	t.Cleanup(func() { r.Close() })
	m, err := wa.OpenStore(filepath.Join(e.home, "messages.db"))
	must(t, err)
	t.Cleanup(func() { m.Close() })
	e.h = hub.New(r, m, e.home)
	e.h.Now = func() time.Time { return e.now }
	e.h.Messenger = &wa.Fake{Messages: m}
	e.p = &people.Service{Hub: e.h}
	e.tasks = tasks.New(e.h, nil)
	e.tasks.Location = time.UTC
	e.s, err = syncer.New(r, e.home, func() time.Time { return e.now })
	must(t, err)
	e.h.MayRunAutomations = e.s.MayRunAutomations
	must(t, e.clock())
	return e
}
func (e *engine) clock() error {
	_, err := e.h.Repo.DB.Exec(`UPDATE sync_state SET wall_ms=?`, e.now.UnixMilli())
	return err
}
func pair(t *testing.T, a, b *engine) {
	t.Helper()
	must(t, a.s.Pair(b.s.EngineID, a.s.EngineID))
	must(t, b.s.Pair(a.s.EngineID, a.s.EngineID))
}
func exchange(t *testing.T, a, b *engine) {
	t.Helper()
	must(t, a.s.Exchange(ctx, syncer.HTTPInProcess{Remote: b.s}))
	must(t, b.s.Exchange(ctx, syncer.HTTPInProcess{Remote: a.s}))
}
func put(t *testing.T, e *engine, bucket, id string, v any) {
	t.Helper()
	must(t, e.h.Repo.Put(bucket, id, v))
}
func task(t *testing.T, e *engine, id, title string, steps []tasks.Step) {
	t.Helper()
	v := tasks.Task{ID: id, Title: title, Kind: "long", LifespanDays: 365, Status: "active", Chats: []tasks.Entity{{ID: "15550000001@s.whatsapp.net", Name: "Maya"}}, People: []tasks.Entity{}, CircleIDs: []string{}, Items: []tasks.Item{}, Checklist: steps, UpdatesSent: []tasks.SentUpdate{}, CreatedAt: e.now, UpdatedAt: e.now, Progress: tasks.Progress{Total: len(steps)}}
	_, err := e.h.Repo.DB.Exec(`INSERT INTO tasks VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, id, dbutil.JSON(v))
	must(t, err)
}
func knowledge(t *testing.T, e *engine) map[string]any {
	t.Helper()
	out := map[string]any{}
	rows, err := e.h.Repo.DB.Query(`SELECT bucket,id,data FROM objects o WHERE ` + hub.SyncObjectPredicate("o") + ` ORDER BY bucket,id`)
	must(t, err)
	for rows.Next() {
		var b, id, raw string
		must(t, rows.Scan(&b, &id, &raw))
		var v any
		must(t, json.Unmarshal([]byte(raw), &v))
		out[b+"/"+id] = v
	}
	must(t, rows.Err())
	rows.Close()
	for _, table := range []string{"tasks", "task_events"} {
		query := `SELECT id,data FROM tasks ORDER BY id`
		if table == "task_events" {
			query = `SELECT id,json_object('task_id',task_id,'at',at,'kind',kind,'text',text,'payload',json(payload)) FROM task_events ORDER BY id`
		}
		rows, err = e.h.Repo.DB.Query(query)
		must(t, err)
		for rows.Next() {
			var id, raw string
			must(t, rows.Scan(&id, &raw))
			var v any
			must(t, json.Unmarshal([]byte(raw), &v))
			out[table+"/"+id] = v
		}
		must(t, rows.Err())
		rows.Close()
	}
	return out
}
func same(t *testing.T, a, b *engine) {
	t.Helper()
	x, y := knowledge(t, a), knowledge(t, b)
	if !reflect.DeepEqual(x, y) {
		xb, _ := json.MarshalIndent(x, "", " ")
		yb, _ := json.MarshalIndent(y, "", " ")
		t.Fatalf("knowledge differs\nA %s\nB %s", xb, yb)
	}
}

func TestContract1AllKnowledgeConverges(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	for n, e := range []*engine{a, b} {
		prefix := fmt.Sprint(n)
		for _, bucket := range []string{"people", "circles", "facts", "relations", "memberships", "entity_icons", "task_dismissals"} {
			for _, suffix := range []string{"keep", "delete"} {
				id := prefix + bucket + suffix
				v := map[string]any{"id": id, "name": "Maya study circle", "label": "Maya", "status": "active"}
				put(t, e, bucket, id, v)
				v["notes"] = "Edited locally"
				put(t, e, bucket, id, v)
				if suffix == "delete" {
					must(t, e.h.Repo.Delete(bucket, id))
				}
			}
		}
		put(t, e, "settings", "owner_profile", map[string]any{"name": "Maya", "notes": prefix})
		put(t, e, "briefs", fmt.Sprintf("%s:%09d", prefix, 1), map[string]any{"circle_id": prefix, "version": 1, "text": "Study draft"})
		task(t, e, prefix+"task", "Study workspace", []tasks.Step{{ID: "step", Text: "Read", Order: 1}})
		var raw string
		must(t, e.h.Repo.DB.QueryRow(`SELECT data FROM tasks WHERE id=?`, prefix+"task").Scan(&raw))
		var v map[string]any
		must(t, json.Unmarshal([]byte(raw), &v))
		v["items"] = []tasks.Item{{ID: "link", Kind: "link", URL: "https://example.com", Title: "Notes"}}
		v["updates_sent"] = []tasks.SentUpdate{{ChatJID: "15550000001@s.whatsapp.net", MessageID: prefix + "msg", At: e.now}}
		_, err := e.h.Repo.DB.Exec(`UPDATE tasks SET data=? WHERE id=?`, dbutil.JSON(v), prefix+"task")
		must(t, err)
		_, err = e.h.Repo.DB.Exec(`INSERT INTO task_events VALUES(?,?,?,?,?,?)`, prefix+"event", prefix+"task", e.now.Format(time.RFC3339Nano), "note", "Invented update", "{}")
		must(t, err)
		task(t, e, prefix+"deleted-task", "Old workspace", nil)
		_, err = e.h.Repo.DB.Exec(`DELETE FROM tasks WHERE id=?`, prefix+"deleted-task")
		must(t, err)
	}
	pair(t, a, b)
	exchange(t, a, b)
	same(t, a, b)
	// Replays and deletions after pairing cannot resurrect retained rows.
	must(t, a.h.Repo.Delete("people", "0peoplekeep"))
	exchange(t, a, b)
	same(t, a, b)
	var n int
	must(t, b.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_deleted WHERE bucket='people' AND id='0peoplekeep'`).Scan(&n))
	if n != 1 {
		t.Fatal("missing retained tombstone")
	}
}
func TestContract2OfflineThreeDays(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	exchange(t, a, b)
	a.now = a.now.Add(72 * time.Hour)
	b.now = b.now.Add(72 * time.Hour)
	must(t, a.clock())
	must(t, b.clock())
	for i := 0; i < 50; i++ {
		for j, e := range []*engine{a, b} {
			id := fmt.Sprintf("person-%d-%d", j, i)
			put(t, e, "people", id, map[string]any{"id": id, "label": "Maya", "notes": "Offline notes"})
		}
	}
	exchange(t, a, b)
	same(t, a, b)
}
func TestContract3ConflictsAndItemUnion(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	task(t, a, "study", "Start", []tasks.Step{})
	put(t, a, "people", "maya", map[string]any{"id": "maya", "label": "Maya"})
	exchange(t, a, b)
	a.now = a.now.Add(time.Hour)
	must(t, a.clock())
	task(t, a, "study", "Earlier", []tasks.Step{{ID: "a", Text: "Read", Order: 1}})
	put(t, a, "people", "maya", map[string]any{"id": "maya", "label": "Earlier"})
	b.now = b.now.Add(2 * time.Hour)
	must(t, b.clock())
	task(t, b, "study", "Later", []tasks.Step{{ID: "b", Text: "Write", Order: 2}})
	put(t, b, "people", "maya", map[string]any{"id": "maya", "label": "Later"})
	exchange(t, a, b)
	same(t, a, b)
	v, err := a.tasks.Get(nil, "study")
	must(t, err)
	if v.Title != "Later" || len(v.Checklist) != 2 {
		t.Fatalf("lost concurrent edits: %+v", v)
	}
	var p map[string]any
	must(t, a.h.Repo.Get("people", "maya", &p))
	if p["label"] != "Later" {
		t.Fatal(p)
	}
	// Removing an item carries its own tombstone, independently of the row winner.
	v.Checklist = v.Checklist[1:]
	v.Progress = tasks.Progress{Done: 999, Total: 999}
	v.NeedsYou = true
	v.NeedsYouReason = "Untrusted derived value"
	_, err = a.h.Repo.DB.Exec(`UPDATE tasks SET data=? WHERE id='study'`, dbutil.JSON(v))
	must(t, err)
	exchange(t, a, b)
	same(t, a, b)
	v, err = b.tasks.Get(nil, "study")
	must(t, err)
	if len(v.Checklist) != 1 || v.Progress.Done != 0 || v.Progress.Total != 1 || v.NeedsYou {
		t.Fatal("removed item resurrected")
	}
}
func TestContract4PrePairingReview(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "a", "Riya exam prep", nil)
	task(t, b, "b", "Riya exam prep", nil)
	pair(t, a, b)
	exchange(t, a, b)
	same(t, a, b)
	for _, e := range []*engine{a, b} {
		rs, err := e.s.Reviews()
		must(t, err)
		if len(rs) != 1 || len(rs[0].IDs) != 2 {
			t.Fatalf("reviews: %+v", rs)
		}
		var n int
		must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM tasks`).Scan(&n))
		if n != 2 {
			t.Fatal("silently merged")
		}
	}
}
func TestContract5IdentityAliasAndLateEdit(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	lid, pn := "15550000009@lid", "15550000001@s.whatsapp.net"
	old, target := model.PersonID(lid), model.PersonID(pn)
	put(t, a, "people", old, model.Person{ID: old, JIDs: []string{lid}, Name: "Maya", Notes: "Original"})
	put(t, a, "people", target, model.Person{ID: target, JIDs: []string{pn}, Label: "Maya Rao"})
	pair(t, a, b)
	exchange(t, a, b)
	must(t, a.p.NormalizeMapping(lid, pn))
	exchange(t, a, b)
	same(t, a, b)
	put(t, b, "people", old, model.Person{ID: old, JIDs: []string{pn, lid}, Name: "Maya", Label: "Maya Rao", Notes: "Late edit"})
	exchange(t, a, b)
	same(t, a, b)
	for _, e := range []*engine{a, b} {
		var n int
		must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='people'`).Scan(&n))
		if n != 1 {
			t.Fatalf("people=%d", n)
		}
		var p model.Person
		must(t, e.h.Repo.Get("people", target, &p))
		if p.Notes != "Late edit" {
			t.Fatal(p)
		}
	}
}
func dueTask(t *testing.T, e *engine, id string, at time.Time) {
	t.Helper()
	task(t, e, id, "Study", nil)
	var raw string
	must(t, e.h.Repo.DB.QueryRow(`SELECT data FROM tasks WHERE id=?`, id).Scan(&raw))
	var v tasks.Task
	must(t, json.Unmarshal([]byte(raw), &v))
	v.CheckIn = &tasks.CheckIn{Rule: "once", AtTime: "12:00", To: "15550000001@s.whatsapp.net", NextAt: at}
	_, err := e.h.Repo.DB.Exec(`UPDATE tasks SET data=json_set(?,'$.draft_slot',?,'$.draft_check_in',?) WHERE id=?`, dbutil.JSON(v), at.UTC().Format(time.RFC3339Nano), "Maya has finished reading.", id)
	must(t, err)
}
func slotCounts(t *testing.T, e *engine, pending, dismissed int) {
	t.Helper()
	var p, d int
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.state')='pending'`).Scan(&p))
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.state')='dismissed' AND json_extract(data,'$.reason')='duplicate_after_sync'`).Scan(&d))
	if p != pending || d != dismissed {
		t.Fatalf("pending=%d dismissed_duplicates=%d; want %d,%d", p, d, pending, dismissed)
	}
}
func TestContract6LeaseSlotClaims(t *testing.T) {
	t.Run("legacy_claimed_draft_visible_on_both", func(t *testing.T) {
		a, b := newEngine(t), newEngine(t)
		pair(t, a, b)
		// Generate an old holder claim, then verify imported claims and sent receipts.
		a.h.MayRunAutomations = func(context.Context) bool { return true }
		dueTask(t, a, "study", a.now.Add(-time.Minute))
		exchange(t, a, b)
		if !a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
			t.Fatal("wrong holder")
		}
		must(t, a.tasks.Tick(ctx))
		must(t, b.tasks.Tick(ctx))
		slotCounts(t, a, 1, 0)
		slotCounts(t, b, 0, 0)
		exchange(t, a, b)
		for _, e := range []*engine{a, b} {
			slotCounts(t, e, 1, 0)
		}
		b.now = b.now.Add(31 * time.Minute)
		must(t, b.clock())
		if b.s.MayRunAutomations(ctx) {
			t.Fatal("non-holder took over on silence")
		}
		b.h.MayRunAutomations = func(context.Context) bool { return true }
		must(t, b.tasks.Tick(ctx))
		exchange(t, a, b)
		ids := map[string]bool{}
		for _, e := range []*engine{a, b} {
			slotCounts(t, e, 1, 0)
			var id string
			must(t, e.h.Repo.DB.QueryRow(`SELECT id FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.state')='pending'`).Scan(&id))
			ids[id] = true
		}
		if len(ids) != 1 {
			t.Fatal("claimed slot was re-queued")
		}
		must(t, a.tasks.Incoming(ctx, model.Message{ID: "owner-message", ChatJID: "15550000001@s.whatsapp.net", IsFromMe: true, Timestamp: a.now.Add(time.Second), Content: "Maya has finished reading."}))
		exchange(t, a, b)
		for _, e := range []*engine{a, b} {
			slotCounts(t, e, 0, 0)
			var n int
			must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM task_events WHERE kind='slot_sent'`).Scan(&n))
			if n != 1 {
				t.Fatal("sent slot not shared")
			}
			v, err := e.tasks.Get(nil, "study")
			must(t, err)
			if v.NeedsYou {
				t.Fatal("sent draft still needs attention")
			}
		}
	})
	t.Run("legacy_partition_duplicates_keep_earliest", func(t *testing.T) {
		a, b := newEngine(t), newEngine(t)
		pair(t, a, b)
		// Simulate claims written by older engines. This intentionally bypasses
		// the new authority gate only to preserve reconciliation coverage.
		a.h.MayRunAutomations = func(context.Context) bool { return true }
		b.h.MayRunAutomations = func(context.Context) bool { return true }
		dueTask(t, a, "study", a.now.Add(-time.Minute))
		exchange(t, a, b)
		must(t, a.tasks.Tick(ctx))
		b.now = b.now.Add(31 * time.Minute)
		must(t, b.clock())
		if b.s.MayRunAutomations(ctx) {
			t.Fatal("non-holder took over on silence")
		}
		must(t, b.tasks.Tick(ctx))
		slotCounts(t, a, 1, 0)
		slotCounts(t, b, 1, 0)
		var original string
		must(t, a.h.Repo.DB.QueryRow(`SELECT id FROM objects WHERE bucket='suggestions'`).Scan(&original))
		exchange(t, a, b)
		for _, e := range []*engine{a, b} {
			slotCounts(t, e, 1, 1)
			var kept, slot string
			must(t, e.h.Repo.DB.QueryRow(`SELECT id FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.state')='pending'`).Scan(&kept))
			if kept != original {
				t.Fatal("later claim won")
			}
			must(t, e.h.Repo.DB.QueryRow(`SELECT slot FROM sync_slot_claims WHERE state='kept'`).Scan(&slot))
			if slot != "study:check_in:"+a.now.Add(-time.Minute).Format(time.RFC3339Nano) {
				t.Fatalf("slot missing task and due time: %q", slot)
			}
			var n int
			must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_slot_claims WHERE state='dismissed' AND reason='duplicate_after_sync'`).Scan(&n))
			if n != 1 {
				t.Fatal("losing claim not dismissed")
			}
		}
		same(t, a, b)
		// Owner dismissal is an event too; rebuilding must never resurrect the draft.
		must(t, b.h.DismissSuggestion(original))
		exchange(t, a, b)
		for _, e := range []*engine{a, b} {
			slotCounts(t, e, 0, 1)
		}
	})
}

func TestContract7UnsupportedVersionAtomic(t *testing.T) {
	a := newEngine(t)
	before := knowledge(t, a)
	var count int
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&count))
	_, err := a.s.Push(ctx, syncer.PushRequest{SyncVersion: 2, Schema: 10, FromEngineID: "fake-peer", Changes: []syncer.Change{{Bucket: "people", ID: "maya", HLC: 1, Origin: "fake", Op: "upsert", Data: json.RawMessage(`{"id":"maya"}`)}}})
	if !errors.Is(err, syncer.ErrUpdateNeeded) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, knowledge(t, a)) {
		t.Fatal("version rejection changed state")
	}
	var after int
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	if count != after {
		t.Fatal("version rejection logged")
	}
}
func TestContract8Schema9MigrationPreservesData(t *testing.T) {
	e := newEngine(t)
	put(t, e, "people", "maya", model.Person{ID: "maya", Name: "Maya", Notes: "Invented notes"})
	put(t, e, "circles", "study", map[string]any{"id": "study", "name": "Study club"})
	put(t, e, "settings", "main", map[string]any{"retention_days": 180})
	task(t, e, "study", "Study workspace", nil)
	_, err := e.h.Repo.DB.Exec(`INSERT INTO credentials VALUES('invented-digest','invented-agent');INSERT INTO activity(at,data) VALUES('2030-01-02T12:00:00Z','{"kind":"people_write","note":"Invented activity"}');INSERT INTO task_events VALUES('fixture-event','study','2030-01-02T12:00:00Z','note','Read invented notes','{}')`)
	must(t, err)
	put(t, e, "agents", "invented-agent", map[string]any{"id": "invented-agent", "name": "Study AI"})
	put(t, e, "effort_edits", "local-effort", map[string]any{"title": "Study notes"})
	localBefore := localFixtureRows(t, e)
	before := knowledge(t, e)
	// Strip v10 from a populated invented fixture to obtain the exact v9 tables.
	rows, err := e.h.Repo.DB.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'sync_%'`)
	must(t, err)
	names := []string{}
	for rows.Next() {
		var n string
		must(t, rows.Scan(&n))
		names = append(names, n)
	}
	rows.Close()
	for _, n := range names {
		_, err = e.h.Repo.DB.Exec(`DROP TRIGGER ` + n)
		must(t, err)
	}
	for _, table := range []string{"sync_state", "sync_log", "sync_meta", "sync_items", "sync_deleted", "sync_briefs", "sync_reviews", "sync_slot_claims"} {
		_, err = e.h.Repo.DB.Exec(`DROP TABLE ` + table)
		must(t, err)
	}
	_, err = e.h.Repo.DB.Exec(`DELETE FROM hub_schema WHERE version=11;DELETE FROM objects WHERE bucket='settings' AND id='engine'`)
	must(t, err)
	must(t, e.h.Repo.Migrate(nil))
	if !reflect.DeepEqual(localBefore, localFixtureRows(t, e)) {
		t.Fatal("migration changed local settings, credentials, activity or effort metadata")
	}
	if !reflect.DeepEqual(before, knowledge(t, e)) {
		t.Fatal("migration lost knowledge")
	}
	must(t, e.h.Repo.Migrate(nil))
	var n int
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log WHERE bucket='people'`).Scan(&n))
	if n != 1 {
		t.Fatalf("bootstrap count=%d", n)
	}
	put(t, e, "people", "maya", model.Person{ID: "maya", Label: "Edited after upgrade"})
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log WHERE bucket='people'`).Scan(&n))
	if n != 2 {
		t.Fatal("post-upgrade capture missing")
	}
}
func TestContract9NeverPairedRunsNormally(t *testing.T) {
	e := newEngine(t)
	if !e.s.MayRunAutomations(ctx) {
		t.Fatal("unpaired denied")
	}
	task(t, e, "study", "Study", nil)
	must(t, e.tasks.Tick(ctx))
	var n int
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&n))
	if n != 0 {
		t.Fatal("standalone paired itself")
	}
	must(t, e.s.Unpair())
	if !e.s.MayRunAutomations(ctx) {
		t.Fatal("unpair denied")
	}
}

func localFixtureRows(t *testing.T, e *engine) []string {
	t.Helper()
	queries := []string{
		`SELECT data FROM objects WHERE bucket IN ('agents','effort_edits') OR (bucket='settings' AND id='main') ORDER BY bucket,id`,
		`SELECT digest||':'||agent_id FROM credentials ORDER BY digest`,
		`SELECT at||':'||data FROM activity ORDER BY id`,
	}
	out := []string{}
	for _, q := range queries {
		rows, err := e.h.Repo.DB.Query(q)
		must(t, err)
		for rows.Next() {
			var raw string
			must(t, rows.Scan(&raw))
			out = append(out, raw)
		}
		must(t, rows.Err())
		rows.Close()
	}
	return out
}
