package sync_test

import (
	"limmr/internal/tasks"
	"testing"
	"time"
)

func TestN1QuietWakeSyncToPairedDot(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "study", "Study guide", nil)
	pair(t, a, b)
	exchange(t, a, b)
	a.now = a.now.Add(time.Hour)
	_, err := a.tasks.Update(nil, "study", map[string]any{"status": "quiet"})
	must(t, err)
	exchange(t, a, b)
	got, err := b.tasks.Get(nil, "study")
	must(t, err)
	if got.Status != "quiet" {
		t.Fatal("quiet not synced to dot")
	}
	b.now = a.now.Add(time.Hour)
	_, err = b.tasks.Checklist(nil, "study", "add", []tasks.Step{{Text: "Review memory"}})
	must(t, err)
	exchange(t, a, b)
	exchange(t, a, b)
	for _, e := range []*engine{a, b} {
		got, err := e.tasks.Get(nil, "study")
		must(t, err)
		if got.Status != "active" || len(got.Checklist) != 1 {
			t.Fatal("dot activity wake lost")
		}
	}
	a.now = b.now.Add(time.Hour)
	_, err = a.tasks.Update(nil, "study", map[string]any{"status": "quiet"})
	must(t, err)
	exchange(t, a, b)
	b.now = a.now.Add(time.Hour)
	_, err = b.tasks.Update(nil, "study", map[string]any{"status": "active"})
	must(t, err)
	exchange(t, a, b)
	got, err = a.tasks.Get(nil, "study")
	must(t, err)
	if got.Status != "active" {
		t.Fatal("owner wake lost")
	}
}
