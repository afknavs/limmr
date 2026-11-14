package tasks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

// The only input stores are built by testkit with invented people and chats.
// Checkpointed copies combine those stores, never any external databases.
func protocolCopy(t *testing.T, s *Service, messages bool) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "offline.db")
	_, err := s.Hub.Repo.DB.Exec("VACUUM INTO ?", path)
	testkit.Must(t, err)
	db, err := sql.Open("sqlite3", path)
	testkit.Must(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if messages {
		for _, table := range []string{"chats", "messages", "lid_aliases"} {
			var schema string
			testkit.Must(t, s.Hub.Messages.DB.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&schema))
			_, err = db.Exec(schema)
			testkit.Must(t, err)
		}
		var seq int
		var name, source string
		testkit.Must(t, s.Hub.Messages.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &source))
		_, err = db.Exec("ATTACH DATABASE ? AS invented", source)
		testkit.Must(t, err)
		for _, table := range []string{"chats", "messages", "lid_aliases"} {
			_, err = db.Exec("INSERT INTO " + table + " SELECT * FROM invented." + table)
			testkit.Must(t, err)
		}
		_, err = db.Exec("DETACH DATABASE invented")
		testkit.Must(t, err)
	}
	return db, path
}
func protocolByID(t *testing.T, p ProtocolPlan, id string) ProtocolTask {
	t.Helper()
	for _, v := range p.Tasks {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("missing %s", id)
	return ProtocolTask{}
}
func protocolHas(v ProtocolTask, rule, action string) bool {
	for _, a := range v.Actions {
		if a.Rule == rule && a.Action == action {
			return true
		}
	}
	return false
}
func protocolSeed(t *testing.T, s *Service, now time.Time) []record {
	t.Helper()
	base := record{Task: Task{Kind: "long", Status: "active", Origin: "owner", CreatedBy: Credit{AgentID: "you", Name: "You"}, CreatedAt: now.Add(-22 * 24 * time.Hour), UpdatedAt: now, LastActiveAt: now.Add(-time.Hour), LifespanDays: 14}}
	rs := []record{}
	for _, id := range []string{"auto", "kept", "circle", "owner", "asked", "unasked", "obsolete", "explicit", "deadline", "dup-a", "dup-b", "suggestion", "expired", "test", "reopened", "future"} {
		r := base
		r.ID = id
		r.Title = "Fixture " + id
		r.Goal = r.Title
		switch id {
		case "auto", "kept", "circle":
			r.Origin = "agent"
			r.CreatedBy = Credit{"ai", "Fixture AI"}
			r.AutoKeptAt = ptrTime(now.Add(-24 * time.Hour))
		case "asked":
			r.Origin = "agent"
			r.CreatedBy = Credit{"ai", "Fixture AI"}
			r.AskedByOwner = &AskedByOwner{OwnerRequest: OwnerRequest{Text: "private owner quote"}, At: now}
		case "unasked":
			r.Origin = "agent"
			r.CreatedBy = Credit{"ai", "Fixture AI"}
		case "obsolete":
			r.LastActiveAt = r.CreatedAt
		case "explicit":
			r.LastActiveAt = r.CreatedAt
			r.LifespanDays = 40
			r.LifespanSet = true
		case "deadline":
			r.EndAt = ptrTime(now.Add(-24 * time.Hour))
		case "dup-a", "dup-b":
			r.Title = "Science fair poster"
			r.Goal = "Science fair poster"
			r.Chats = []Entity{{ID: testkit.Group, Name: "Secret fixture chat"}}
			r.People = []Entity{{ID: model.PersonID(testkit.Phone), Name: "Secret fixture person"}}
		case "suggestion", "expired":
			r.Status = "suggested"
			r.Suggestion = &Suggestion{DetectedAt: now.Add(-13 * 24 * time.Hour)}
			if id == "expired" {
				r.Suggestion = &Suggestion{DetectedAt: now.Add(-14 * 24 * time.Hour)}
			}
		case "test":
			r.Kind = "test"
			r.LastActiveAt = now.Add(-24 * time.Hour)
		case "reopened":
			r.LastActiveAt = r.CreatedAt
			r.ReopenedAt = ptrTime(now.Add(-10 * 24 * time.Hour))
		case "future":
			r.EndAt = ptrTime(now.Add(24 * time.Hour))
			r.LastActiveAt = r.CreatedAt
		}
		_, err := s.Hub.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", r.ID, dbutil.JSON(r))
		testkit.Must(t, err)
		rs = append(rs, r)
	}
	_, err := s.Hub.Repo.DB.Exec("INSERT INTO task_events VALUES(?,?,?,?,?,?)", "owner-kept", "kept", now.Format(time.RFC3339Nano), "kept", "private history text", `{"by":"you"}`)
	testkit.Must(t, err)
	testkit.Must(t, s.Hub.Repo.Put("circles", "old-circle", model.Circle{ID: "old-circle", Name: "Secret archived circle", Archived: true}))
	testkit.Must(t, s.Hub.Repo.Put("changes", "migration", hub.TaskSurfacingChange{Kind: "circle_to_task", TaskID: "circle"}))
	testkit.Must(t, s.Hub.Repo.Put("effort_edits", "not-a-cache", map[string]string{"title": "Secret cached title"}))
	return rs
}

func TestProtocolHubOnlyAndLiveAgreement(t *testing.T) {
	s, _, now := fixture(t)
	protocolSeed(t, s, *now)
	db, path := protocolCopy(t, s, false)
	before, err := os.ReadFile(path)
	testkit.Must(t, err)
	_, err = db.Exec("PRAGMA query_only=ON")
	testkit.Must(t, err)
	plan, err := PlanProtocolPass(db, *now, time.UTC)
	testkit.Must(t, err)
	again, err := PlanProtocolPass(db, *now, time.UTC)
	testkit.Must(t, err)
	if !reflect.DeepEqual(plan, again) {
		t.Fatal("nondeterministic plan")
	}
	if len(plan.Tasks) != 16 {
		t.Fatal(len(plan.Tasks))
	}
	for id, want := range map[string]string{"auto": "Becomes a suggestion", "kept": "Stays a task", "circle": "Becomes a suggestion", "owner": "Stays a task", "asked": "Stays a task", "unasked": "Becomes a suggestion", "obsolete": "Ends", "explicit": "Stays a task", "deadline": "Ends", "dup-a": "Left for you", "dup-b": "Left for you", "suggestion": "Existing suggestions", "expired": "Ends", "test": "Ends", "reopened": "Stays a task", "future": "Stays a task"} {
		v := protocolByID(t, plan, id)
		if v.Outcome != want {
			t.Errorf("%s: %s want %s", id, v.Outcome, want)
		}
	}
	if !strings.Contains(protocolByID(t, plan, "kept").Actions[0].Reason, "you kept it") {
		t.Fatal("owner keep unexplained")
	}
	for _, id := range []string{"kept", "owner", "asked", "explicit", "dup-a", "dup-b", "reopened", "future"} {
		v := protocolByID(t, plan, id)
		for _, rule := range []string{"finished", "follows_work"} {
			if !protocolHas(v, rule, "not checked") {
				t.Errorf("%s %s missing not checked", id, rule)
			}
		}
	}
	expiry := protocolByID(t, plan, "suggestion").Actions[0].Evidence.Dates
	if len(expiry) != 1 || !expiry[0].Equal(now.Add(24*time.Hour)) {
		t.Fatal("expiry", expiry)
	}
	var text bytes.Buffer
	testkit.Must(t, WriteProtocolText(&text, plan))
	raw, err := json.Marshal(plan)
	testkit.Must(t, err)
	for _, secret := range []string{"Secret fixture person", "Secret fixture chat", "Secret archived circle", "Secret cached title", "private owner quote", "private history text", testkit.Phone, testkit.Group} {
		if strings.Contains(text.String(), secret) || bytes.Contains(raw, []byte(secret)) {
			t.Errorf("private field leaked: %s", secret)
		}
	}
	after, err := os.ReadFile(path)
	testkit.Must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("planner wrote input")
	}
	// The live rules do not implement one-time demotion. Project only those
	// proposed statuses into a SECOND invented fixture, then run the real Tick.
	live, _ := protocolCopy(t, s, true)
	for _, v := range plan.Tasks {
		if protocolHas(v, "demote", "back to suggestion") {
			_, err = live.Exec("UPDATE tasks SET data=json_set(data,'$.status','suggested','$.suggestion',json(?)) WHERE id=?", dbutil.JSON(Suggestion{DetectedAt: *now}), v.ID)
			testkit.Must(t, err)
		}
	}
	h := hub.New(&hub.Repo{DB: live}, &wa.Store{DB: live}, t.TempDir())
	h.Now = func() time.Time { return *now }
	h.Messenger = &wa.Fake{}
	actual := New(h, &efforts.Service{Hub: h})
	actual.Location = time.UTC
	testkit.Must(t, actual.Tick(context.Background()))
	for _, v := range plan.Tasks {
		r, err := actual.raw(v.ID)
		testkit.Must(t, err)
		want := v.Status
		switch {
		case protocolHas(v, "demote", "back to suggestion"):
			want = "suggested"
		case protocolHas(v, "deadline", "ends as done"):
			want = "done"
		case protocolHas(v, "obsolete", "ends as dropped"), protocolHas(v, "suggestion_expiry", "expires"):
			want = "dropped"
		}
		if want == "active" && r.Status == "quiet" {
			continue
		} // ordinary quiet bookkeeping, not a protocol action
		if r.Status != want {
			t.Errorf("%s planned %s; live %s", v.ID, want, r.Status)
		}
		if protocolHas(v, "obsolete", "ends as dropped") && r.CloseReason != "obsolete" {
			t.Error("obsolete reason", r.CloseReason)
		}
		if strings.HasPrefix(v.ID, "dup-") && r.MergedInto != "" {
			t.Error("real tasks merged")
		}
	}
	var c model.Circle
	testkit.Must(t, h.Repo.Get("circles", "old-circle", &c))
	if !c.Archived {
		t.Fatal("circle unarchived")
	}
	t.Logf("16 invented records; counts: %v", plan.Counts)
}

func TestProtocolOwnerTouches(t *testing.T) {
	_, _, now := fixture(t)
	base := record{Task: Task{ID: "touch", Title: "Review poster", Kind: "long", Status: "active", Origin: "agent", AutoKeptAt: now, CreatedAt: *now, LastActiveAt: *now, LifespanDays: 14}}
	cases := []struct {
		name string
		edit func(*protocolInput)
	}{
		{"owner kept", func(x *protocolInput) { x.events = []protocolEvent{{ID: "event", Kind: "kept", By: "you", At: *now}} }},
		{"reopened", func(x *protocolInput) { x.events = []protocolEvent{{ID: "event", Kind: "reopened", At: *now}} }},
		{"note", func(x *protocolInput) { x.record.OwnerNotes = []OwnerNote{{ID: "note", Text: "Private prose"}} }},
		{"deleted note", func(x *protocolInput) { x.events = []protocolEvent{{ID: "event", Kind: "task_owner_note"}} }},
		{"item", func(x *protocolInput) { x.record.Items = []Item{{ID: "item", AddedBy: "you"}} }},
		{"subject", func(x *protocolInput) { x.record.SubjectEdited = true }},
		{"circles", func(x *protocolInput) { x.record.CirclesEdited = true }},
		{"update", func(x *protocolInput) { x.record.UpdatesSent = []SentUpdate{{By: "you", TextPreview: "Private prose"}} }},
		{"checklist done", func(x *protocolInput) { x.events = []protocolEvent{{Kind: "step_done", By: "you"}} }},
		{"checklist undone", func(x *protocolInput) { x.events = []protocolEvent{{Kind: "step_reopened", By: "you"}} }},
		{"status", func(x *protocolInput) { x.events = []protocolEvent{{Kind: "status_changed", By: "you"}} }},
		{"membership removal", func(x *protocolInput) { x.record.RemovedMembers.Chats = []string{"private-chat"} }},
		{"removal history", func(x *protocolInput) { x.events = []protocolEvent{{Kind: "owner_remove"}} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := protocolInput{record: base}
			tc.edit(&in)
			before := dbutil.JSON(in.record)
			p := planProtocolPass([]protocolInput{in}, *now, time.UTC)
			if protocolHas(p.Tasks[0], "demote", "back to suggestion") || !strings.Contains(p.Tasks[0].Actions[0].Reason, "you ") {
				t.Fatal(p.Tasks[0])
			}
			if dbutil.JSON(in.record) != before {
				t.Fatal("mutated input")
			}
		})
	}
	for _, by := range []string{"agent", "This Mac"} {
		t.Run(by+" is not owner", func(t *testing.T) {
			p := planProtocolPass([]protocolInput{{record: base, events: []protocolEvent{{Kind: "status_changed", By: by}, {Kind: "kept", By: by}}}}, *now, time.UTC)
			if !protocolHas(p.Tasks[0], "demote", "back to suggestion") {
				t.Fatal(p)
			}
		})
	}
}

func TestProtocolMessagesUseLiveFinishedRule(t *testing.T) {
	for _, tc := range []struct {
		name, author, text string
		done, hint         bool
	}{
		{"owner", "owner", "Submitted the poster", true, false},
		{"contact", "contact", "Submitted the poster", false, true},
		{"AI", "agent", "Submitted the poster", false, true},
		{"negation", "owner", "not submitted the poster", false, false},
		{"unrelated", "owner", "Paid the parking fee", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, now, v := finishedFixture(t)
			finishMessagePut(t, s, now, tc.text, tc.author, now.Add(time.Minute))
			db, path := protocolCopy(t, s, true)
			before, err := os.ReadFile(path)
			testkit.Must(t, err)
			_, err = db.Exec("PRAGMA query_only=ON")
			testkit.Must(t, err)
			plan, err := PlanProtocolPass(db, *now, time.UTC)
			testkit.Must(t, err)
			got := protocolByID(t, plan, v.ID)
			if protocolHas(got, "finished", "ends as done") != tc.done || protocolHas(got, "finished", "looks finished?") != tc.hint {
				t.Fatal(got)
			}
			_, err = s.applyFinished(mustRaw(t, s, v.ID))
			testkit.Must(t, err)
			live := mustRaw(t, s, v.ID)
			if ended(live.Task) != tc.done || (live.FinishHint != nil) != tc.hint {
				t.Fatal("live disagrees")
			}
			after, err := os.ReadFile(path)
			testkit.Must(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("changed snapshot")
			}
			var text bytes.Buffer
			testkit.Must(t, WriteProtocolText(&text, plan))
			if strings.Contains(text.String(), tc.text) {
				t.Fatal("message leaked")
			}
		})
	}
}

func TestProtocolFollowsWorkUsesLiveSnapshotAndCandidates(t *testing.T) {
	for _, mode := range []string{"automatic", "legacy", "hidden", "missing effort"} {
		t.Run(mode, func(t *testing.T) {
			s, v, _, _ := followsFixture(t)
			switch mode {
			case "legacy":
				r := mustRaw(t, s, v.ID)
				r.Membership = nil
				_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), v.ID)
				testkit.Must(t, err)
			case "hidden":
				settings := s.Hub.Settings()
				settings.HiddenChats = []string{writingChat}
				testkit.Must(t, s.Hub.SetSettings(settings))
			case "missing effort":
				_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.effort_id','missing') WHERE id=?", v.ID)
				testkit.Must(t, err)
			}
			db, path := protocolCopy(t, s, true)
			before, err := os.ReadFile(path)
			testkit.Must(t, err)
			_, err = db.Exec("PRAGMA query_only=ON")
			testkit.Must(t, err)
			p, err := PlanProtocolPass(db, s.now(), time.UTC)
			testkit.Must(t, err)
			got := protocolByID(t, p, v.ID)
			if mode == "automatic" && protocolHas(got, "finished", "looks finished?") {
				t.Fatal("active effort became finish hint", got)
			}
			if mode == "automatic" && !protocolHas(got, "follows_work", "remove departed members") {
				t.Fatal(got)
			}
			if mode == "legacy" && !protocolHas(got, "follows_work", "left for you") {
				t.Fatal(got)
			}
			if (mode == "hidden" || mode == "missing effort") && !protocolHas(got, "follows_work", "not checked") {
				t.Fatal(got)
			}
			// Compare against the real automatic rule on a second fixture copy.
			live, _ := protocolCopy(t, s, true)
			h := hub.New(&hub.Repo{DB: live}, &wa.Store{DB: live}, t.TempDir())
			h.Now = s.Hub.Now
			actual := New(h, &efforts.Service{Hub: h})
			actual.Location = time.UTC
			testkit.Must(t, actual.applyFollowsWork(mustRaw(t, actual, v.ID)))
			r := mustRaw(t, actual, v.ID)
			removed := member(r.Chats, writingChat) == nil && member(r.People, model.PersonID(ashaJID)) == nil
			if removed != (mode == "automatic") {
				t.Fatal("live membership disagrees", removed, mode)
			}
			after, err := os.ReadFile(path)
			testkit.Must(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("planner wrote snapshot")
			}
			var text bytes.Buffer
			testkit.Must(t, WriteProtocolText(&text, p))
			for _, secret := range []string{"Asha", "Bela", "Writing class", "Stats project", writingChat, statsChat, ashaJID, belaJID} {
				if strings.Contains(text.String(), secret) {
					t.Fatal("membership leaked", secret)
				}
			}
		})
	}
}

func TestProtocolPureBoundariesAndInputImmutability(t *testing.T) {
	now := testkit.Epoch
	in := protocolInput{record: record{Task: Task{ID: "owner", Title: "Review poster", Status: "active", Origin: "owner", Kind: "long", CreatedAt: now, LastActiveAt: now, LifespanDays: 14}}, active: now, messagesPresent: true, followsAvailable: true}
	for _, tc := range []struct {
		name         string
		edit         func(*protocolInput)
		at           time.Time
		rule, action string
	}{
		{"21 days", func(x *protocolInput) {}, now.Add(21 * 24 * time.Hour), "obsolete", "ends as dropped"},
		{"before 21", func(x *protocolInput) {}, now.Add(21*24*time.Hour - time.Nanosecond), "finished", "no change"},
		{"40 days", func(x *protocolInput) { x.record.LifespanDays = 40; x.record.LifespanSet = true }, now.Add(40 * 24 * time.Hour), "obsolete", "ends as dropped"},
		{"end day remains", func(x *protocolInput) { x.record.EndAt = ptrTime(now) }, now, "finished", "no change"},
		{"next local day", func(x *protocolInput) { x.record.EndAt = ptrTime(now) }, now.AddDate(0, 0, 1), "deadline", "ends as done"},
		{"completed checklist", func(x *protocolInput) {
			x.record.EndCondition = "checklist_done"
			x.record.Checklist = []Step{{ID: "step", Done: true}}
		}, now, "finished", "ends as done"},
		{"paused checklist", func(x *protocolInput) {
			x.record.EndCondition = "checklist_done"
			x.record.Checklist = []Step{{ID: "step", Done: true}}
			x.record.PauseChecklistClose = true
		}, now, "finished", "no change"},
		{"weak effort", func(x *protocolInput) { x.effortStatus = "done" }, now, "finished", "looks finished?"},
		{"hint expires", func(x *protocolInput) {
			x.record.FinishHint = &FinishHint{Why: "private hint text", At: now.Add(-7 * 24 * time.Hour)}
		}, now, "finished", "no change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := in
			tc.edit(&x)
			// JSON of a public view verifies nested pointers too; copy structural values
			// for the unexported snapshot (which encoding/json intentionally omits).
			recordBefore := dbutil.JSON(x.record)
			followsBefore := dbutil.JSON(x.follows)
			p := planProtocolPass([]protocolInput{x}, tc.at, time.UTC)
			if !protocolHas(p.Tasks[0], tc.rule, tc.action) {
				t.Fatal(p.Tasks[0])
			}
			if dbutil.JSON(x.record) != recordBefore || dbutil.JSON(x.follows) != followsBefore {
				t.Fatal("pure planner mutated input")
			}
		})
	}
}
