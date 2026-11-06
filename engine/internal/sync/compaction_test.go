package sync_test

import (
	"fmt"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"testing"
)

func TestCompactionConvergesFromEveryCursor(t *testing.T) {
	a := newEngine(t)
	for i := 0; i < 4; i++ {
		put(t, a, "people", "maya", model.Person{ID: "maya", Name: []string{"Maya", "Maya Rao", "Maya R.", "Maya Rao"}[i]})
	}
	put(t, a, "facts", "removed", model.Fact{ID: "removed", Text: "invented"})
	must(t, a.h.Repo.Delete("facts", "removed"))
	var maxSeq int64
	must(t, a.h.Repo.DB.QueryRow(`SELECT max(seq) FROM sync_log`).Scan(&maxSeq))
	var after int64
	for {
		next, _, err := a.h.Repo.CompactSyncBatch(ctx, after, 2)
		must(t, err)
		if next == after {
			break
		}
		after = next
	}
	var duplicates int
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM (SELECT bucket,id FROM sync_log GROUP BY bucket,id HAVING count(*)>1)`).Scan(&duplicates))
	if duplicates != 0 {
		t.Fatal("superseded rows remain")
	}
	var tombstone string
	must(t, a.h.Repo.DB.QueryRow(`SELECT op FROM sync_log WHERE bucket='facts' AND id='removed'`).Scan(&tombstone))
	if tombstone != "tombstone" {
		t.Fatal("newest deletion lost")
	}
	// Each peer already has everything up to its arbitrary old cursor. Subsequent
	// pull must deliver final state, including to a cursor that now lies in a gap.
	for cursor := int64(0); cursor <= maxSeq; cursor++ {
		b := newEngine(t)
		must(t, a.h.Repo.Delete("settings", "sync_peer"))
		pair(t, a, b)
		// Seed the final object if its newest row is already behind this cursor.
		var newest int64
		must(t, a.h.Repo.DB.QueryRow(`SELECT seq FROM sync_log WHERE bucket='people' AND id='maya'`).Scan(&newest))
		if cursor >= newest {
			put(t, b, "people", "maya", model.Person{ID: "maya", Name: "Maya Rao"})
		}
		page, err := a.s.Pull(ctx, syncer.PullRequest{SinceSeq: cursor, PeerEngineID: b.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
		must(t, err)
		_, err = b.s.Push(ctx, syncer.PushRequest{Changes: page.Changes, FromEngineID: a.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
		must(t, err)
		var p model.Person
		must(t, b.h.Repo.Get("people", "maya", &p))
		if p.Name != "Maya Rao" {
			t.Fatal("peer did not converge")
		}
	}
}

func TestNoopCaptureAndOldTriggerMigration(t *testing.T) {
	a := newEngine(t)
	put(t, a, "people", "maya", model.Person{ID: "maya", Name: "Maya Rao"})
	task(t, a, "task", "Synthetic task", nil)
	var before, after int
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&before))
	_, err := a.h.Repo.DB.Exec(`UPDATE objects SET data=data; UPDATE tasks SET data=data; UPDATE task_events SET payload=payload`)
	must(t, err)
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	if before != after {
		t.Fatal("no-op capture added rows")
	}
	// Simulate an already-installed schema-11 trigger, then reopen migration.
	_, err = a.h.Repo.DB.Exec(`DELETE FROM hub_schema WHERE version=12; DROP TRIGGER sync_objects_UPDATE; CREATE TRIGGER sync_objects_UPDATE AFTER UPDATE ON objects BEGIN SELECT 1; END`)
	must(t, err)
	must(t, a.h.Repo.Migrate(nil))
	_, err = a.h.Repo.DB.Exec(`UPDATE objects SET data=data`)
	must(t, err)
	must(t, a.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
	if before != after {
		t.Fatal("migration retained unguarded triggers")
	}
}

func TestRemoteEqualPayloadKeepsVersionFence(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	put(t, a, "people", "maya", model.Person{ID: "maya", Name: "Maya Rao"})
	page, err := a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
	must(t, err)
	var c syncer.Change
	for _, change := range page.Changes {
		if change.Bucket == "people" {
			c = change
		}
	}
	push := func(change syncer.Change) {
		_, err := b.s.Push(ctx, syncer.PushRequest{Changes: []syncer.Change{change}, FromEngineID: a.s.EngineID, SyncVersion: syncer.Version, Schema: syncer.Schema})
		must(t, err)
	}
	push(c)
	c.HLC += 10
	push(c)
	c.HLC -= 5
	c.Data = []byte(`{"id":"maya","name":"Stale"}`)
	push(c)
	var p model.Person
	must(t, b.h.Repo.Get("people", "maya", &p))
	if p.Name != "Maya Rao" {
		t.Fatal("same-data version fence did not advance")
	}
}

// A fresh peer must see a task before its events, even when later task edits
// land beyond a page boundary. Sorting a single page cannot repair that order.
func TestCompactionPreservesTaskBeforeEventsAcrossPages(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	task(t, a, "study", "Read chapter", nil)
	_, err := a.h.Repo.DB.Exec(`INSERT INTO task_events VALUES('study:note','study','2030-01-02T12:00:00Z','note','Invented note','{}')`)
	must(t, err)
	for i := 0; i < 510; i++ {
		put(t, a, "facts", fmt.Sprintf("f%d", i), model.Fact{ID: fmt.Sprintf("f%d", i), Text: "Invented fact"})
	}
	_, err = a.h.Repo.DB.Exec(`UPDATE tasks SET data=json_set(data,'$.title','Read two chapters') WHERE id='study'`)
	must(t, err)
	var after int64
	for {
		next, _, err := a.h.Repo.CompactSyncBatch(ctx, after, 256)
		must(t, err)
		if next == after {
			break
		}
		after = next
	}
	must(t, b.s.Exchange(ctx, syncer.InProcess{Remote: a.s}))
	var title string
	must(t, b.h.Repo.DB.QueryRow(`SELECT json_extract(data,'$.title') FROM tasks WHERE id='study'`).Scan(&title))
	if title != "Read two chapters" {
		t.Fatal("latest task state missing")
	}
	var events int
	must(t, b.h.Repo.DB.QueryRow(`SELECT count(*) FROM task_events WHERE task_id='study'`).Scan(&events))
	if events != 1 {
		t.Fatal("task history missing")
	}
}
