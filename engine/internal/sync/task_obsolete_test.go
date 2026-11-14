package sync_test

import (
	"context"
	"testing"
	"time"

	"limmr/internal/tasks"
)

func TestR3TestKindObsoleteAndLifespanSync(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	v, err := a.tasks.Create(nil, tasks.Create{Title: "Scratch poster", Kind: "test", LifespanDays: 14})
	must(t, err)
	exchange(t, a, b)
	got, err := b.tasks.Get(nil, v.ID)
	must(t, err)
	if got.Kind != "test" || !got.LifespanSet || got.LifespanDays != 14 {
		t.Fatal("sync lost contract", got)
	}
	a.now = a.now.Add(24 * time.Hour)
	b.now = a.now
	must(t, a.clock())
	must(t, b.clock())
	a.h.MayRunAutomations = func(context.Context) bool { return true }
	must(t, a.tasks.Tick(ctx))
	exchange(t, a, b)
	got, err = b.tasks.Get(nil, v.ID)
	must(t, err)
	if got.Kind != "test" || got.Status != "dropped" || got.CloseReason != "obsolete" || got.NeedsYou {
		t.Fatal("sync lost ending", got)
	}
	history, err := b.tasks.History(v.ID)
	must(t, err)
	if history[0].Kind != "rule_obsolete" || history[0].ChangeID == "" {
		t.Fatal("sync lost rule history", history)
	}
	// Undo snapshots are local in the existing protocol; restored state replicates.
	restored, err := a.tasks.UndoMembershipChange(nil, v.ID, history[0].ChangeID)
	must(t, err)
	if restored.Kind != "test" || restored.Status != "active" || restored.CloseReason != "" || !restored.LifespanSet {
		t.Fatal("source undo", restored)
	}
	exchange(t, a, b)
	got, err = b.tasks.Get(nil, v.ID)
	must(t, err)
	if got.Status != "active" || got.Kind != "test" || got.CloseReason != "" {
		t.Fatal("restored state did not replicate", got)
	}
}
