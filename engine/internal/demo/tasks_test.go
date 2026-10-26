package demo

import (
	"limmr/internal/efforts"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"testing"
)

func TestTasksSeedInventedWorkspacesAndIdempotence(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, Seed(h, p, l))
	s := tasks.New(h, &efforts.Service{Hub: h})
	for pass := 0; pass < 2; pass++ {
		testkit.Must(t, SeedTasks(s))
		xs, err := s.List(nil, "all", 100)
		testkit.Must(t, err)
		if len(xs) != 6 {
			t.Fatal("expected six demo tasks")
		}
		long, short, waiting := false, false, false
		for _, x := range xs {
			if x.Kind == "short" {
				short = true
			}
			if x.Progress.Total == 2 && x.Progress.Done == 1 && x.NextCheckInAt != nil {
				long = true
			}
			if x.Waiting != nil && x.NeedsYou {
				waiting = true
			}
		}
		if !long || !short || !waiting {
			t.Fatal("missing demo workspace state")
		}
	}
	xs, err := h.Suggestions()
	testkit.Must(t, err)
	nudges := 0
	for _, x := range xs {
		if x.TaskID != "" && x.Kind == "nudge" {
			nudges++
		}
	}
	if nudges != 1 {
		t.Fatal("demo nudge missing or duplicated")
	}
}
