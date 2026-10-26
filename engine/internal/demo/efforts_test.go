package demo_test

import (
	"limmr/internal/demo"
	"limmr/internal/efforts"
	"limmr/internal/testkit"
	"testing"
)

func TestFiveInventedEffortsAndRestart(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	want := map[string]string{"Pitch deck with Maya": "on_you", "Lab report": "waiting_on_them", "Weekend plan": "moving", "Garden supplies": "dormant", "Design Club poster": "done"}
	for pass := 0; pass < 2; pass++ {
		svc := &efforts.Service{Hub: h}
		xs, e := svc.List(nil, efforts.Filter{})
		testkit.Must(t, e)
		found := 0
		for _, x := range xs {
			if st, ok := want[x.Title]; ok {
				found++
				if x.Status != st || x.CreatedBy != "Codex" {
					t.Fatal("demo state/credit drifted")
				}
				d, e := svc.Get(nil, x.ID)
				testkit.Must(t, e)
				if len(d.MessageRefs) < 3 || len(d.Timeline) < 3 {
					t.Fatal("demo evidence missing")
				}
			}
		}
		if found != len(want) {
			t.Fatalf("found %d of five demo purposes", found)
		}
		queue, _, err := svc.Unnamed(nil, 500)
		testkit.Must(t, err)
		budget := false
		for _, candidate := range queue {
			detail, err := svc.Get(nil, candidate.ID)
			testkit.Must(t, err)
			for _, ref := range detail.MessageRefs {
				if ref.MessageID == "a6-budget-0" {
					budget = len(candidate.Snippets) > 0 && candidate.MessageCount == 3
				}
			}
		}
		if !budget {
			t.Fatal("demo lost the deliberate budget naming candidate")
		}
		testkit.Must(t, demo.Seed(h, p, l))
	}
}
