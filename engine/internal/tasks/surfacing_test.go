package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func surfacingChat(t *testing.T, s *Service, a *model.Agent, now time.Time, jid, name, text string, group bool, members int, own bool) {
	t.Helper()
	testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: jid, Name: name, IsGroup: group, LastMessageTime: &now}))
	if group {
		testkit.Must(t, s.Hub.Repo.Put("groups", jid, model.Group{JID: jid, Name: name, MemberCount: members}))
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("%s-%d", jid, i)
		m := model.Message{ID: id, ChatJID: jid, Sender: testkit.Phone, Content: text, IsFromMe: i < 2, Timestamp: now.Add(time.Duration(i-4) * time.Hour)}
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
		if own && i < 2 {
			_, err := s.Hub.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", jid, id, a.ID, a.Name)
			testkit.Must(t, err)
		}
	}
}
func TestSurfacingPrecisionAndLease(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		group      bool
		count      int
		own        bool
		want       int
	}{
		{"Sibling study material", "Please prepare exam material and quizzes for Riya and her parent", true, 3, true, 1},
		{"Parent job search", "Please review the CV application before the deadline", true, 3, true, 1},
		{"Large class", "Please prepare exam material before the deadline", true, 140, true, 0},
		{"Lunch", "lunch?", false, 0, false, 0},
		{"Example bot", "Please prepare exam material", false, 0, true, 0},
		{"Weekly newsletter", "Please prepare exam material", false, 0, true, 0},
		{"Self chat", "Please review the CV application before the deadline", false, 0, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, a, now := fixture(t)
			a.Preset = "own"
			testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
			jid := "15550100333@s.whatsapp.net"
			if tc.group {
				jid = "120363003333@g.us"
			}
			if tc.name == "Weekly newsletter" {
				jid = "333@newsletter"
			}
			if tc.name == "Self chat" {
				// The owner's own chat holds their AIs' notifications.
				testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", Name: "Owner", JIDs: []string{jid}}))
			}
			surfacingChat(t, s, a, *now, jid, tc.name, tc.text, tc.group, tc.count, tc.own)
			testkit.Must(t, s.Tick(context.Background()))
			xs, err := s.List(nil, "all", 100)
			testkit.Must(t, err)
			if len(xs) != tc.want {
				t.Fatalf("got %d tasks, want %d", len(xs), tc.want)
			}
			if tc.want > 0 {
				x, err := s.Get(nil, xs[0].ID)
				testkit.Must(t, err)
				if want := map[string]string{"Sibling study material": "Sibling study material: Exam", "Parent job search": "Parent job search: CV"}[tc.name]; x.Title != want {
					t.Fatalf("title %q, want %q", x.Title, want)
				}
				if x.Status != "active" || x.Origin != "agent" || x.CreatedBy.AgentID != a.ID || x.AutoKeptAt == nil || x.UndoChangeID == "" || x.Suggestion == nil || x.Suggestion.Confidence < 0.6 {
					t.Fatalf("auto keep missing: %+v", x)
				}
				*now = now.Add(11 * time.Minute)
				testkit.Must(t, s.Tick(context.Background()))
				xs, err = s.List(nil, "all", 100)
				testkit.Must(t, err)
				if len(xs) != 1 {
					t.Fatal("detector duplicated task")
				}
				_, err = s.Undo(x.ID)
				testkit.Must(t, err)
				*now = now.Add(11 * time.Minute)
				testkit.Must(t, s.Tick(context.Background()))
				xs, err = s.List(nil, "open", 100)
				testkit.Must(t, err)
				if len(xs) != 0 {
					t.Fatal("undo was auto-kept again")
				}
			}
		})
	}
	t.Run("lease denies auto keep", func(t *testing.T) {
		s, a, now := fixture(t)
		a.Preset = "own"
		testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
		calls := 0
		s.MayRunAutomations = func(context.Context) bool {
			calls++
			var n int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM tasks").Scan(&n))
			if n != 0 {
				t.Error("lease checked after task creation")
			}
			return false
		}
		surfacingChat(t, s, a, *now, testkit.Group, "Study material", "exam material is due", true, 3, true)
		testkit.Must(t, s.Tick(context.Background()))
		xs, err := s.List(nil, "suggested", 100)
		testkit.Must(t, err)
		if len(xs) != 1 || calls != 1 || xs[0].Origin != "limmr" || xs[0].NeedsYou {
			t.Fatalf("lease not respected: %+v calls=%d", xs, calls)
		}
		testkit.Must(t, s.Tick(context.Background()))
		if calls != 1 {
			t.Fatal("ten minute gate failed")
		}
	})
	t.Run("sync lease on the hub denies auto keep", func(t *testing.T) {
		s, a, now := fixture(t)
		a.Preset = "own"
		testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
		calls := 0
		holder := false
		s.Hub.MayRunAutomations = func(context.Context) bool { calls++; return holder }
		surfacingChat(t, s, a, *now, testkit.Group, "Study material", "exam material is due", true, 3, true)
		// Not the lease holder: find nothing, so two paired engines never both suggest.
		testkit.Must(t, s.Tick(context.Background()))
		xs, err := s.List(nil, "all", 100)
		testkit.Must(t, err)
		if len(xs) != 0 || calls == 0 {
			t.Fatalf("non-holder surfaced tasks: %+v calls=%d", xs, calls)
		}
		// Holding the lease: the own-AI case is found and auto-kept.
		holder = true
		*now = now.Add(11 * time.Minute)
		testkit.Must(t, s.Tick(context.Background()))
		xs, err = s.List(nil, "open", 100)
		testkit.Must(t, err)
		if len(xs) != 1 || xs[0].Origin != "agent" {
			t.Fatalf("holder did not auto-keep: %+v", xs)
		}
	})
}
func TestSuggestedLifecyclePermissionsExpiryAndDismissal(t *testing.T) {
	s, a, now := fixture(t)
	sig := Suggestion{Signals: []string{"Exam material needs a plan"}, Confidence: 0.8, DetectedAt: *now, EffortIDs: []string{"effort-original"}}
	x, err := s.Suggest(nil, Create{Title: "Exam material", Kind: "short", Chats: []Entity{{ID: testkit.Phone}}}, sig)
	testkit.Must(t, err)
	for _, status := range []string{"open", "closed"} {
		xs, err := s.List(nil, status, 100)
		testkit.Must(t, err)
		if len(xs) != 0 {
			t.Fatal("suggested task in", status)
		}
	}
	// Ordinary agents can keep fully visible suggestions they did not create.
	kept, err := s.Keep(a, x.ID)
	testkit.Must(t, err)
	if kept.Status != "active" || kept.Origin != "limmr" {
		t.Fatal("keep changed origin")
	}
	var payload string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='kept'", x.ID).Scan(&payload))
	var who map[string]string
	testkit.Must(t, json.Unmarshal([]byte(payload), &who))
	if who["by"] != a.ID {
		t.Fatal("keep attribution missing")
	}
	x, err = s.Suggest(nil, Create{Title: "CV application", Kind: "short", Chats: []Entity{{ID: testkit.Phone}, {ID: testkit.Group}}}, sig)
	testkit.Must(t, err)
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
	if _, err = s.Keep(a, x.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("partial scope kept task", err)
	}
	a.Preset = "own"
	if _, err = s.Dismiss(a, x.ID, ""); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("own bypassed visibility", err)
	}
	a.Permissions.Chats = model.Scope{Mode: "all"}
	a.Permissions.SaveSummaries = false
	if _, err = s.Dismiss(a, x.ID, ""); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("write capability bypassed", err)
	}
	a.Permissions.SaveSummaries = true
	dropped, err := s.Dismiss(a, x.ID, "Not ongoing work")
	testkit.Must(t, err)
	if dropped.CloseReason != "not_a_task" {
		t.Fatal("dismiss reason missing")
	}
	// The suppression remains independently of the task record and its 90-day purge.
	_, err = s.Hub.Repo.DB.Exec("DELETE FROM tasks WHERE id=?", x.ID)
	testkit.Must(t, err)
	var n int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='task_dismissals' AND id='effort:effort-original'").Scan(&n))
	if n != 1 {
		t.Fatal("dismissal not durable")
	}
	for _, candidate := range []Task{
		{Title: "Renamed work", Suggestion: &sig},
		{Title: "CV review", EffortID: "regrouped-effort", Chats: []Entity{{ID: testkit.Phone}}},
	} {
		blocked, err := s.covered(candidate)
		testkit.Must(t, err)
		if !blocked {
			t.Fatal("stored dismissal did not suppress regrouped/renamed work")
		}
	}
	blocked, err := s.covered(Task{Title: "Dentist appointment", Chats: []Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	if blocked {
		t.Fatal("dismissal suppressed an unrelated topic")
	}
	var stored map[string]any
	testkit.Must(t, s.Hub.Repo.Get("task_dismissals", "effort:effort-original", &stored))
	if stored["id"] != "effort:effort-original" || stored["task_id"] != x.ID {
		t.Fatal("dismissal object missing identity")
	}
	x, err = s.Suggest(nil, Create{Title: "Print guide", Kind: "short", Chats: []Entity{{ID: testkit.Group}}}, sig)
	testkit.Must(t, err)
	*now = now.AddDate(0, 0, 13)
	testkit.Must(t, s.Tick(context.Background()))
	x, err = s.Get(nil, x.ID)
	testkit.Must(t, err)
	if x.Status != "suggested" || x.NeedsYou {
		t.Fatal("suggestion ran automation")
	}
	*now = now.AddDate(0, 0, 1)
	testkit.Must(t, s.Tick(context.Background()))
	x, err = s.Get(nil, x.ID)
	testkit.Must(t, err)
	if x.Status != "dropped" || x.CloseReason != "suggestion_expired" {
		t.Fatal("expiry missing", x.Status, x.CloseReason)
	}
}
func TestProjectCircleMigrationAndUndo(t *testing.T) {
	s, a, now := fixture(t)
	c := model.Circle{ID: "project-old", Name: "Kitchen renovation", Kind: "project", Confirmed: true, CreatedBy: a.ID, Chats: []model.CircleChat{{ChatJID: testkit.Group}}, People: []model.CirclePerson{{PersonID: model.PersonID(testkit.Phone)}}}
	testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	testkit.Must(t, s.Hub.Repo.Put("briefs", "brief-old", model.Brief{CircleID: c.ID, Version: 2, Summary: "The cabinet guide is ready", WrittenBy: a.ID, WrittenAt: *now}))
	_, err := s.Hub.Repo.DB.Exec("DELETE FROM hub_schema WHERE version=10")
	testkit.Must(t, err)
	for i := 0; i < 2; i++ {
		testkit.Must(t, s.Hub.Repo.MigrateTaskSurfacing(*now))
	}
	xs, err := s.List(nil, "open", 100)
	testkit.Must(t, err)
	if len(xs) != 1 {
		t.Fatal("migration not exactly once")
	}
	x, err := s.Get(nil, xs[0].ID)
	testkit.Must(t, err)
	if x.Origin != "agent" || x.CreatedBy.AgentID != a.ID || len(x.CircleIDs) != 0 || len(x.People) != 1 || x.Summary.Text != "The cabinet guide is ready" {
		t.Fatalf("migration lost metadata %+v", x)
	}
	var archived model.Circle
	testkit.Must(t, s.Hub.Repo.Get("circles", c.ID, &archived))
	if !archived.Archived {
		t.Fatal("circle not archived")
	}
	archived.Name = "Owner edited"
	testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, archived))
	if _, err = s.Undo(x.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatal("undo overwrote edit", err)
	}
	archived.Name = c.Name
	testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, archived))
	x, err = s.Undo(x.ID)
	testkit.Must(t, err)
	// Decode into a fresh value: archived:false is omitted from stored JSON.
	var restored model.Circle
	testkit.Must(t, s.Hub.Repo.Get("circles", c.ID, &restored))
	if dbutil.JSON(restored) != dbutil.JSON(c) || x.Status != "dropped" {
		t.Fatal("undo did not restore circle")
	}
	var changes []hub.TaskSurfacingChange
	changes, err = hub.List[hub.TaskSurfacingChange](s.Hub.Repo, "changes")
	testkit.Must(t, err)
	for _, ch := range changes {
		if ch.TaskID == x.ID {
			t.Fatal("undo change remained")
		}
	}
}

func TestSuggestedTitleReadsNaturally(t *testing.T) {
	for _, tc := range [][4]string{
		{"Career help", "Ravi", "cv", "Ravi's CV"},
		{"Ravi", "Ravi", "exam", "Ravi's exam"},
		{"Study group", "", "exam", "Study group: Exam"},
		{"Study group", "", "", "Study group: Ongoing work"},
	} {
		if got := suggestedTitle(tc[0], tc[1], tc[2]); got != tc[3] {
			t.Fatalf("suggestedTitle(%q,%q,%q)=%q", tc[0], tc[1], tc[2], got)
		}
	}
}
