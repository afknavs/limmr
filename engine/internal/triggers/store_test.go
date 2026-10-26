package triggers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestMemoryStoreCRUDAndCopies(t *testing.T) {
	s := NewMemoryStore()
	r := Record{Watch: watch()}
	r.Status = Active
	must(t, s.Create(testCtx, r))
	if !errors.Is(s.Create(testCtx, r), ErrExists) {
		t.Fatal("duplicate allowed")
	}
	r.Scope.Chats[0] = "mutated"
	got, err := s.Get(testCtx, "watch")
	must(t, err)
	if got.Scope.Chats[0] != "design-club" {
		t.Fatal("store borrowed input slice")
	}
	got.Scope.Chats[0] = "mutated"
	items, err := s.List(testCtx)
	must(t, err)
	if len(items) != 1 || items[0].Scope.Chats[0] != "design-club" {
		t.Fatal("store lent output slice")
	}
	sentinel := errors.New("reject")
	if err := s.Update(testCtx, "watch", func(r *Record) error { r.Goal = "discarded"; return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	got, err = s.Get(testCtx, "watch")
	must(t, err)
	if got.Goal == "discarded" {
		t.Fatal("failed update persisted")
	}
	must(t, s.Delete(testCtx, "watch"))
	if _, err = s.Get(testCtx, "watch"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
func TestMemoryStoreHitsRunsAndReservations(t *testing.T) {
	s := NewMemoryStore()
	must(t, s.Create(testCtx, Record{Watch: watch()}))
	h := Hit{ID: "hit-1", WatchID: "watch", Messages: []IncomingMessage{msg("first")}}
	must(t, s.PutHit(testCtx, h))
	h.State = "completed"
	must(t, s.PutHit(testCtx, h))
	h.ID = "hit-2"
	must(t, s.PutHit(testCtx, h))
	hits, err := s.Hits(testCtx, "watch", 1)
	must(t, err)
	if len(hits) != 1 || hits[0].ID != "hit-2" {
		t.Fatal("hit upsert/rolling list")
	}
	run := RunRecord{ID: "run-1", HandoffID: "watch", State: "running"}
	must(t, s.PutRun(testCtx, run))
	run.State = "completed"
	must(t, s.PutRun(testCtx, run))
	runs, err := s.Runs(testCtx, "watch")
	must(t, err)
	if len(runs) != 1 || runs[0].State != "completed" {
		t.Fatal("run upsert")
	}
	now := time.Now()
	ok, err := s.ReserveReply(testCtx, "watch", now, 1)
	must(t, err)
	if !ok {
		t.Fatal("no initial slot")
	}
	ok, err = s.ReserveReply(testCtx, "watch", now, 1)
	must(t, err)
	if ok {
		t.Fatal("budget exceeded")
	}
	count, err := s.ReplyCount(testCtx, "watch", now.Add(time.Hour))
	must(t, err)
	if count != 0 {
		t.Fatal("rolling count boundary")
	}
}
func TestMemoryStoreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	s := NewMemoryStore()
	if !errors.Is(s.Create(ctx, Record{Watch: watch()}), context.Canceled) {
		t.Fatal("cancel ignored")
	}
	if _, err := s.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
}
func TestSchemaAndAppendOnlyInstructions(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "hub.db")+"?_foreign_keys=on")
	must(t, err)
	defer db.Close()
	_, err = db.Exec(Schema)
	must(t, err)
	_, err = db.Exec(Schema)
	must(t, err)
	r := Record{Watch: watch()}
	r.Status = Active
	r.Instructions = []Instruction{{Text: "First", At: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}}
	data, err := json.Marshal(r)
	must(t, err)
	_, err = db.Exec("INSERT INTO trigger_watches(id,owner,status,definition_json) VALUES(?,?,?,?)", r.ID, r.Owner, r.Status, string(data))
	must(t, err)
	update := func(r Record) error {
		b, x := json.Marshal(r)
		must(t, x)
		_, x = db.Exec("UPDATE trigger_watches SET definition_json=? WHERE id=?", string(b), r.ID)
		return x
	}
	original := copyValue(r)
	r.Instructions = append(r.Instructions, Instruction{Text: "Second", At: r.Instructions[0].At.Add(time.Minute)})
	must(t, update(r))
	if update(original) == nil {
		t.Fatal("schema allowed instruction deletion")
	}
	r.Instructions[0].Text = "rewritten"
	if update(r) == nil {
		t.Fatal("schema allowed instruction rewrite")
	}
	r.Instructions[0].Text = "First"
	r.CreatedVia.GrantText = "changed grant"
	if update(r) == nil {
		t.Fatal("schema allowed grant rewrite")
	}
	_, err = db.Exec("INSERT INTO trigger_hits(id,watch_id,at,action,state,messages_json) VALUES ('orphan','missing','now','notify','recorded','[]')")
	if err == nil {
		t.Fatal("schema allowed orphan hit")
	}
}
