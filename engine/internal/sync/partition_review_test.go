package sync_test

import (
	syncer "limmr/internal/sync"
	"testing"
	"time"
)

// The holder keeps running through any partition; the other engine never takes
// over on silence, so a split can never produce two automation holders.
func TestPairedLeaseHasOneHolderAcrossPartition(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 31 * time.Minute, 72 * time.Hour} {
		t.Run(elapsed.String(), func(t *testing.T) {
			a, b := newEngine(t), newEngine(t)
			pair(t, a, b)
			dueTask(t, a, "study", a.now.Add(-time.Minute))
			exchange(t, a, b)
			for _, e := range []*engine{a, b} {
				e.now = e.now.Add(elapsed)
				must(t, e.clock())
				must(t, e.tasks.Tick(ctx))
			}
			if !a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
				t.Fatal("partition changed the holder")
			}
			slotCounts(t, a, 1, 0)
			slotCounts(t, b, 0, 0)
			// A paused transport changes nothing either.
			var peer syncer.Peer
			must(t, b.h.Repo.Get("settings", "sync_peer", &peer))
			peer.Paused = true
			put(t, b, "settings", "sync_peer", peer)
			if b.s.MayRunAutomations(ctx) || b.s.GiveLease() == nil {
				t.Fatal("non-holder gained or gave authority")
			}
			peer.Paused = false
			put(t, b, "settings", "sync_peer", peer)
			exchange(t, a, b)
			for _, e := range []*engine{a, b} {
				slotCounts(t, e, 1, 0)
			}
		})
	}
}

// Handoff leaves a gap, never an overlap: the giver stops at once and the
// receiver starts only after it has synced the handoff.
func TestPairedLeaseHandoffNeverOverlaps(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	exchange(t, a, b)
	must(t, a.s.GiveLease())
	if a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
		t.Fatal("overlap or early start during handoff")
	}
	exchange(t, a, b)
	if a.s.MayRunAutomations(ctx) || !b.s.MayRunAutomations(ctx) {
		t.Fatal("handoff did not complete")
	}
	if a.s.GiveLease() == nil {
		t.Fatal("former holder could still give")
	}
	must(t, b.s.GiveLease())
	exchange(t, a, b)
	if !a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
		t.Fatal("hand back failed")
	}
}

// A stale self-lease from a standalone period must not survive pairing.
func TestPairingOverridesStaleStandaloneLease(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	must(t, b.s.Unpair())
	pair(t, a, b)
	if !a.s.MayRunAutomations(ctx) || b.s.MayRunAutomations(ctx) {
		t.Fatal("stale lease authorized the non-holder")
	}
}

func TestUnpairedAutomationBehaviorIsPreserved(t *testing.T) {
	e := newEngine(t)
	if !e.s.MayRunAutomations(ctx) {
		t.Fatal("never-paired engine should remain standalone")
	}
	dueTask(t, e, "study", e.now.Add(-time.Minute))
	must(t, e.tasks.Tick(ctx))
	slotCounts(t, e, 1, 0)
}
