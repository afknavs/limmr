package efforts

import (
	"limmr/internal/dbutil"
	"limmr/internal/testkit"
	"testing"
)

func TestSuggestionEndingsDoNotFinishEfforts(t *testing.T) {
	for _, ending := range []string{"dismissed", "expired", "merged", "legacy-dismissed", "legacy-expired", "legacy-merged", "kept-task", "auto-kept-task", "direct-task"} {
		t.Run(ending, func(t *testing.T) {
			s, _ := seed(t)
			xs, err := s.List(nil, Filter{})
			testkit.Must(t, err)
			e := xs[0]
			before, err := s.Get(nil, e.ID)
			testkit.Must(t, err)
			if e.Status != "on_you" {
				t.Fatal("fixture lost open request", e.Status)
			}
			r := map[string]any{"id": "ending", "effort_id": e.ID, "status": "dropped", "suggestion": map[string]any{"confidence": 0.8}}
			want := "on_you"
			switch ending {
			case "dismissed":
				r["ended_as_suggestion"], r["close_reason"] = true, "not_a_task"
			case "expired":
				r["ended_as_suggestion"], r["close_reason"] = true, "suggestion_expired"
			case "merged":
				r["ended_as_suggestion"], r["close_reason"] = true, "merged"
			case "legacy-dismissed":
				r["close_reason"] = "not_a_task"
			case "legacy-expired":
				r["close_reason"] = "suggestion_expired"
			case "legacy-merged":
				r["close_reason"] = "merged"
			case "kept-task":
				want = "done"
			case "auto-kept-task":
				want = "done"
				r["auto_kept_at"] = s.Hub.Now()
			case "direct-task":
				want = "done"
				delete(r, "suggestion")
			}
			_, err = s.Hub.Repo.DB.Exec("INSERT INTO tasks VALUES('ending',?)", dbutil.JSON(r))
			testkit.Must(t, err)
			if ending == "kept-task" {
				_, err = s.Hub.Repo.DB.Exec("INSERT INTO task_events VALUES('ending:kept','ending',?,'kept','Task kept','{}')", s.Hub.Now())
				testkit.Must(t, err)
			}
			got, err := s.Get(nil, e.ID)
			testkit.Must(t, err)
			if got.Status != want || len(got.Timeline) != len(before.Timeline) {
				t.Fatal("wrong effort projection or modified timeline", got.Status, want)
			}
		})
	}
}
