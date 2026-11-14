package tasks

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
)

func TestSuggestionsNeverNeedYou(t *testing.T) {
	s, a, now := fixture(t)
	task, err := s.Suggest(a, Create{Title: "Science fair plan", Kind: "long", People: []Entity{{ID: model.PersonID(testkit.Phone)}}}, Suggestion{Signals: []string{"agent_proposed"}, DetectedAt: *now})
	testkit.Must(t, err)
	// Legacy/stale records must not leak attention flags through any read projection.
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.needs_you',json('true'),'$.needs_you_reason','Review the plan') WHERE id=?", task.ID)
	testkit.Must(t, err)
	read, err := s.Get(nil, task.ID)
	testkit.Must(t, err)
	if read.NeedsYou || read.NeedsYouReason != "" {
		t.Fatal("suggestion detail needs owner")
	}
	for _, status := range []string{"all", "suggested"} {
		xs, err := s.List(nil, status, 100)
		testkit.Must(t, err)
		if len(xs) != 1 || xs[0].NeedsYou || xs[0].NeedsYouReason != "" {
			t.Fatal("suggestion card needs owner", xs)
		}
	}
	people, err := s.NeedsYouPeople()
	testkit.Must(t, err)
	if len(people) != 0 {
		t.Fatal("suggestion lights up people", people)
	}
	flagged, err := s.NeedsYou(a, task.ID, "Review the science fair plan")
	testkit.Must(t, err)
	if flagged.NeedsYou {
		t.Fatal("agent flagged suggestion")
	}
	stored, err := s.raw(task.ID)
	testkit.Must(t, err)
	if stored.NeedsYou {
		t.Fatal("stored suggestion needs owner")
	}
	testkit.Must(t, s.Tick(context.Background()))
	stored, err = s.raw(task.ID)
	testkit.Must(t, err)
	if stored.Status != "suggested" || stored.NeedsYou {
		t.Fatal("tick promoted or flagged suggestion")
	}
}

func TestSuggestForAgentNeverAutoKeeps(t *testing.T) {
	for _, lease := range []bool{true, false} {
		s, a, now := fixture(t)
		a.Preset = "own"
		s.Hub.MayRunAutomations = func(context.Context) bool { return lease }
		task, err := s.SuggestForAgent(context.Background(), a, Create{Title: "Science fair plan", Kind: "long"}, Suggestion{Signals: []string{"Two recorded AI messages"}, DetectedAt: *now})
		testkit.Must(t, err)
		if task.Status != "suggested" || task.Origin != "agent" || task.CreatedBy.AgentID != a.ID || task.AutoKeptAt != nil || task.UndoChangeID != "" || task.NeedsYou {
			t.Fatalf("lease=%v promoted suggestion: %+v", lease, task)
		}
	}
}

func TestLegacyAutoKeptTaskUndoStillWorks(t *testing.T) {
	s, a, now := fixture(t)
	task := create(t, s, a, "long")
	record, err := s.raw(task.ID)
	testkit.Must(t, err)
	record.AutoKeptAt = now
	record.UndoChangeID = "legacy-auto-keep"
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(record), task.ID)
	testkit.Must(t, err)
	testkit.Must(t, s.Hub.Repo.Put("changes", record.UndoChangeID, hub.TaskSurfacingChange{ID: record.UndoChangeID, Kind: "auto_kept_task", TaskID: task.ID, At: *now}))
	undone, err := s.Undo(task.ID)
	testkit.Must(t, err)
	if undone.Status != "dropped" || undone.UndoChangeID != "" {
		t.Fatal("legacy Undo stopped working")
	}
}

func TestOwnerCreateAndKeepNeedNoQuote(t *testing.T) {
	s, _, now := fixture(t)
	task, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long"})
	testkit.Must(t, err)
	if task.Status != "active" || task.AskedByOwner != nil {
		t.Fatal("owner create changed")
	}
	suggestion, err := s.Suggest(nil, Create{Title: "Revision plan", Kind: "long"}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	kept, err := s.Keep(nil, suggestion.ID)
	testkit.Must(t, err)
	if kept.Status != "active" {
		t.Fatal("owner keep needs quote")
	}
}
