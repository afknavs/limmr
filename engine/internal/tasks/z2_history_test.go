package tasks

import (
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestZ2OwnerHistoryNamesAndNewestFirst(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	*now = now.Add(time.Minute)
	_, err := s.Attach(a, v.ID, Item{Kind: "note", Title: "Study plan", Text: "Use recall practice"})
	testkit.Must(t, err)
	*now = now.Add(time.Minute)
	_, err = s.Update(a, v.ID, map[string]any{"status": "done"})
	testkit.Must(t, err)
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(v.History) < 3 {
		t.Fatal("missing task history")
	}
	found := false
	for i, event := range v.History {
		if i > 0 && event.At.After(v.History[i-1].At) {
			t.Fatal("history not newest first")
		}
		found = found || event.Kind == "item_added"
	}
	if !found {
		t.Fatal("item addition was not recorded")
	}
	private, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(private.History) != 0 {
		t.Fatal("owner history crossed the agent boundary")
	}
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	tx, err := s.Hub.Repo.DB.Begin()
	testkit.Must(t, err)
	testkit.Must(t, s.event(tx, r, "review", "kept", "Task kept", map[string]string{"by": a.ID}))
	testkit.Must(t, tx.Commit())
	events, err := s.History(v.ID)
	testkit.Must(t, err)
	named := false
	for _, event := range events {
		if strings.Contains(event.Text, "Example AI") {
			named = true
		}
		if strings.Contains(event.Text, a.ID) {
			t.Fatal("history exposed an agent id")
		}
	}
	if !named {
		t.Fatal("history did not resolve the agent name")
	}
}
