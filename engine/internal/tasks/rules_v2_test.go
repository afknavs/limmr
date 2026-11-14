package tasks

import (
	"context"
	"database/sql"
	"errors"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskRulesV2Ending(t *testing.T) {
	for _, zone := range []string{"Asia/Kolkata", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			s, a, now := fixture(t)
			loc, err := time.LoadLocation(zone)
			testkit.Must(t, err)
			s.Location = loc
			end := time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
			*now = end.Add(time.Hour)
			v, err := s.Create(a, Create{Title: "Hold the workshop", Goal: "Participants finish the workshop", Kind: "long", EndAt: &end})
			testkit.Must(t, err)
			testkit.Must(t, s.Tick(context.Background()))
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if ended(got) {
				t.Fatal("ended before next local day")
			}
			*now = time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if !ended(got) || got.NeedsYou {
				t.Fatal("did not archive at local midnight")
			}
			xs, err := s.List(a, "open", 100)
			testkit.Must(t, err)
			if len(xs) != 0 {
				t.Fatal("ended task still active")
			}
			*now = now.AddDate(0, 0, 100)
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.Goal != v.Goal {
				t.Fatal("ended history purged")
			}
		})
	}
	s, a, _ := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.NeedsYou(a, v.ID, "Pick an option")
	testkit.Must(t, err)
	v, err = s.Update(a, v.ID, map[string]any{"status": "dropped", "close_reason": "failed", "reason": "The application was rejected"})
	testkit.Must(t, err)
	if v.NeedsYou || v.CloseReason != "failed" || v.ClosedAt == nil {
		t.Fatal("failure did not end immediately")
	}
	if _, err = s.Update(a, v.ID, map[string]any{"status": "active"}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("agent reopened ended task")
	}
	if _, err = s.Update(nil, v.ID, map[string]any{"waiting": Waiting{What: "new request"}}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("waiting implicitly reopened")
	}
}
func TestTaskRulesV2Followup(t *testing.T) {
	s, a, _ := fixture(t)
	old := create(t, s, a, "long")
	old, err := s.Attach(a, old.ID, Item{Kind: "note", Text: "Original participants and material"})
	testkit.Must(t, err)
	in := Create{Title: "Email the participants", Goal: "Participants receive the recap", Kind: "short", Follows: old.ID}
	if _, err = s.Create(a, in); !errors.Is(err, model.ErrConflict) {
		t.Fatal("followed unfinished task")
	}
	_, err = s.Update(a, old.ID, map[string]any{"status": "done"})
	testkit.Must(t, err)
	fresh, err := s.Create(a, in)
	testkit.Must(t, err)
	got, err := s.Get(a, fresh.ID)
	testkit.Must(t, err)
	if got.Follows != old.ID || got.FollowsContext == nil || len(got.FollowsContext.Items) == 0 || len(got.FollowsContext.Chats) == 0 || len(got.FollowsContext.People) == 0 {
		t.Fatal("missing linked context")
	}
	prior, err := s.Get(a, old.ID)
	testkit.Must(t, err)
	if !ended(prior) {
		t.Fatal("follow-up reopened old task")
	}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	got, err = s.Get(a, fresh.ID)
	testkit.Must(t, err)
	if got.FollowsContext != nil {
		t.Fatal("link bypassed current access")
	}
}
func TestTaskRulesV2DuplicatesAndMerge(t *testing.T) {
	s, a, _ := fixture(t)
	in := Create{Title: "Review career options", Goal: "Choose the next career step", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}}
	first, err := s.Create(a, in)
	testkit.Must(t, err)
	_, err = s.Create(a, in)
	var conflict *CreationConflict
	if !errors.As(err, &conflict) || len(conflict.Candidates) != 1 || conflict.Candidates[0].ID != first.ID {
		t.Fatal("missing duplicate candidates", err)
	}
	for _, reason := range []string{"too short", strings.Repeat("word ", 201)} {
		in.DifferentBecause = reason
		if _, err = s.Create(a, in); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("invalid duplicate override accepted", err)
		}
	}
	in.DifferentBecause = "Separate outcome for another deadline"
	in.Title = "Career options review"
	second, err := s.Create(a, in)
	testkit.Must(t, err)
	var payload string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='created'", second.ID).Scan(&payload))
	if !strings.Contains(payload, in.DifferentBecause) {
		t.Fatal("override reason lost from history")
	}
	first, err = s.Attach(a, first.ID, Item{Kind: "note", Text: "First file history"})
	testkit.Must(t, err)
	second, err = s.Attach(a, second.ID, Item{Kind: "note", Text: "Second file history"})
	testkit.Must(t, err)
	_, err = s.OwnerNote(nil, second.ID, "", "Owner's correction", false)
	testkit.Must(t, err)
	merged, err := s.Merge(a, first.ID, second.ID)
	testkit.Must(t, err)
	src, err := s.Get(a, second.ID)
	testkit.Must(t, err)
	if len(merged.Items) != 2 || len(merged.OwnerNotes) != 1 || src.MergedInto != first.ID || src.Status != "dropped" {
		t.Fatal("merge lost workspace or source archive")
	}
	var n int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM task_events WHERE task_id=? AND id LIKE 'merge:%'", first.ID).Scan(&n))
	if n == 0 {
		t.Fatal("merge lost source history")
	}
	if _, err = s.Merge(a, first.ID, second.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatal("repeat merge duplicated history")
	}
}
func TestTaskRulesV2ConcurrentCreate(t *testing.T) {
	s, a, _ := fixture(t)
	in := Create{Title: "Prepare the recap", Goal: "Share a recap", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}}
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Create(a, in); errs <- err }()
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			var c *CreationConflict
			if !errors.As(err, &c) {
				t.Fatal(err)
			}
			conflicts++
		}
	}
	if successes != 1 || conflicts != 3 {
		t.Fatal(successes, conflicts)
	}
}
func TestTaskRulesV2TitlesAndAttribution(t *testing.T) {
	s, a, now := fixture(t)
	if _, err := s.Create(a, Create{Title: "This title has far too many words", Kind: "long"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("long title accepted")
	}
	subject := &Entity{ID: model.PersonID(testkit.Phone), Name: "Untrusted supplied label"}
	in := Create{Title: "Plan the exam", Goal: "Prepare for the exam", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, Subject: subject}
	if _, err := s.Create(a, in); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("unsupported attribution accepted")
	}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "evidence", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Please help with my exam", Timestamp: *now}))
	in.SubjectEvidence = []Ref{{ChatJID: testkit.Phone, MessageID: "evidence"}}
	v, err := s.Create(a, in)
	testkit.Must(t, err)
	if v.Subject.Name == "Untrusted supplied label" {
		t.Fatal("uncanonical subject label")
	}
	_, err = s.Update(nil, v.ID, map[string]any{"subject": nil, "owner_correction": true})
	testkit.Must(t, err)
	if _, err = s.Update(a, v.ID, map[string]any{"subject": subject, "subject_evidence": in.SubjectEvidence}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("owner correction overwritten")
	}
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.Subject != nil || !got.SubjectEdited {
		t.Fatal("owner clearing not sticky")
	}
}
func TestTaskRulesV2CoverPin(t *testing.T) {
	s, a, now := fixture(t)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "poster", ChatJID: testkit.Phone, Sender: testkit.Phone, MediaType: "image", Filename: "Poster.png", Content: "Workshop recap poster for participants", Timestamp: now.Add(-time.Hour)}))
	coverOriginal(t, s, testkit.Phone, "poster", 1200, 1600)
	v, err := s.Create(a, Create{Title: "Workshop recap", Goal: "Send the workshop recap", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	if v.Cover == nil || v.CoverItemID == "" {
		t.Fatal("relevant cover not pinned")
	}
	pinned := v.CoverItemID
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new", ChatJID: testkit.Phone, Sender: testkit.Phone, MediaType: "image", Filename: "New.png", Content: "Workshop recap poster for participants", Timestamp: now.Add(time.Minute)}))
	coverOriginal(t, s, testkit.Phone, "new", 1200, 1600)
	*now = now.Add(2 * time.Minute)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.CoverItemID != pinned {
		t.Fatal("new image replaced pinned cover")
	}
	var newest string
	for _, it := range got.Items {
		if it.MessageRef != nil && it.MessageRef.MessageID == "new" {
			newest = it.ID
		}
	}
	got, err = s.SetCover(a, v.ID, newest)
	testkit.Must(t, err)
	if got.CoverItemID != newest {
		t.Fatal("explicit set-cover ignored")
	}
	blank, err := s.Create(a, Create{Title: "Arrange transport", Goal: "Book the bus", Kind: "long"})
	testkit.Must(t, err)
	if !strings.HasPrefix(blank.CoverItemID, "default:") {
		t.Fatal("missing pinned default")
	}
	candidate := s.coverCandidate(a, blank)
	if candidate == nil || candidate.ItemID != defaultCoverID(blank.ID) {
		t.Fatal("unstable default")
	}
	preview, err := s.CoverPreview(a, blank.ID)
	testkit.Must(t, err)
	if !regularFile(preview["path"].(string)) {
		t.Fatal("default pixels unavailable")
	}
}
func TestTaskRulesV2CircleConversionAndDryRun(t *testing.T) {
	s, a, now := fixture(t)
	circle := model.Circle{ID: "finite", Name: "Workshop", Kind: "project", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}, People: []model.CirclePerson{{PersonID: model.PersonID(testkit.Phone)}}}
	testkit.Must(t, s.Hub.Repo.Put("circles", circle.ID, circle))
	end := now.AddDate(0, 0, -1)
	v, err := s.Create(a, Create{Title: "Workshop recap", Kind: "long", EndAt: &end, Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	legacy := v
	legacy.ID = "legacy-duplicate"
	_, err = s.Hub.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", legacy.ID, dbutil.JSON(record{Task: legacy}))
	testkit.Must(t, err)
	db := s.Hub.Repo.DB.(*sql.DB)
	before := dumpAuditFixture(t, db)
	report, err := AuditDatabase(db, *now, time.UTC)
	testkit.Must(t, err)
	if len(report.AutoArchive) != 2 || len(report.Duplicates) != 1 || len(report.TaskCircles) != 1 {
		t.Fatalf("audit missing candidates: %+v", report)
	}
	if before != dumpAuditFixture(t, db) {
		t.Fatal("audit changed data")
	}
	converted, err := s.ConvertCircle(a, circle.ID, Create{Title: "Hold workshop", Goal: "Complete the workshop", Kind: "long"})
	testkit.Must(t, err)
	var saved model.Circle
	testkit.Must(t, s.Hub.Repo.Get("circles", circle.ID, &saved))
	if !saved.Archived || saved.ConvertedTaskID != converted.ID || len(converted.Chats) != 1 || len(converted.People) != 1 {
		t.Fatal("conversion lost data")
	}
}
func dumpAuditFixture(t *testing.T, db *sql.DB) string {
	t.Helper()
	var all string
	testkit.Must(t, db.QueryRow("SELECT group_concat(data) FROM (SELECT data FROM tasks UNION ALL SELECT data FROM objects ORDER BY data)").Scan(&all))
	return all
}
func TestTaskRulesV2NeedsYouExpiry(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.NeedsYou(a, v.ID, "Pick the venue")
	testkit.Must(t, err)
	*now = now.Add(model.NeedsYouTTL - time.Second)
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if !got.NeedsYou {
		t.Fatal("expired early")
	}
	*now = now.Add(time.Second)
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.NeedsYou {
		t.Fatal("seven-day request retained")
	}
	_, err = s.NeedsYou(a, v.ID, "A new follow-up")
	testkit.Must(t, err)
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if !got.NeedsYou {
		t.Fatal("follow-up did not renew")
	}
	_, err = s.NeedsYou(a, v.ID, "")
	testkit.Must(t, err)
	got, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if got.NeedsYou {
		t.Fatal("answered request retained")
	}
}
