package sync_test

import (
	syncer "limmr/internal/sync"
	"strings"
	"testing"
)

func TestDeferredFirstContactNeverStartsOnTimer(t *testing.T) {
	e := newEngine(t)
	fake := bind(t, e)
	approval := queuedApproval(e, "deferred-first")
	approval.FirstContact = true
	must(t, e.h.Repo.Put("send_queue", approval.ID, syncer.QueuedSend{ID: approval.ID, Origin: e.s.EngineID, Approval: approval, Phase: "queued", At: e.now}))
	must(t, e.s.Coordinate(ctx))
	if len(fake.Sent) != 0 {
		t.Fatal("timer started a new chat")
	}
}
func TestPairedFirstContactNeverEntersPeerQueue(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	fa, fb := bind(t, a), bind(t, b)
	pair(t, a, b)
	approval := queuedApproval(a, "paired-first")
	approval.FirstContact = true
	_, err := a.s.Send(ctx, approval)
	if err == nil || !strings.Contains(err.Error(), "nothing was queued") {
		t.Fatal("paired send not refused", err)
	}
	var n int
	must(t, a.h.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='send_queue'").Scan(&n))
	if n != 0 || len(fa.Sent)+len(fb.Sent) != 0 {
		t.Fatal("paired first contact queued or sent")
	}
}
