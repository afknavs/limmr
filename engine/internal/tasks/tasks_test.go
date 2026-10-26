package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Service, *model.Agent, *time.Time) {
	t.Helper()
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	now := testkit.Epoch
	h.Now = func() time.Time { return now }
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	s := New(h, &efforts.Service{Hub: h})
	s.Location = time.UTC
	return s, &a, &now
}
func create(t *testing.T, s *Service, a *model.Agent, kind string) Task {
	t.Helper()
	v, err := s.Create(a, Create{Title: "Maya's study plan", Kind: kind, Chats: []Entity{{ID: testkit.Phone}}, People: []Entity{{ID: model.PersonID(testkit.Phone), Name: "Maya Rao"}}})
	testkit.Must(t, err)
	return v
}
func TestEffortPrefillAndLinkScanVisibility(t *testing.T) {
	s, a, now := fixture(t)
	ms := []model.Message{
		{ID: "deck-1", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck opening slides https://notebooklm.google.com/notebook/demo", Timestamp: now.AddDate(0, 0, -5)},
		{ID: "deck-2", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "The pitch deck layout is ready https://docs.google.com/document/d/demo", Timestamp: now.AddDate(0, 0, -3)},
		{ID: "deck-3", ChatJID: testkit.Phone, IsFromMe: true, Content: "I will review the pitch deck https://github.com/example/demo", Timestamp: now.AddDate(0, 0, -2)},
		{ID: "deck-4", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Can you review the pitch deck slides? Due tomorrow. https://GITHUB.com/example/demo?utm_source=chat#readme", Timestamp: now.Add(-time.Hour)},
	}
	for _, m := range ms {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	xs, err := s.Efforts.List(nil, efforts.Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 1 {
		t.Fatalf("want one effort, got %d", len(xs))
	}
	v, err := s.Create(a, Create{Title: "Review pitch deck", Kind: "long", EffortID: xs[0].ID})
	testkit.Must(t, err)
	if len(v.Chats) != 2 || len(v.People) == 0 || len(v.Items) != 3 {
		t.Fatalf("prefill missing: chats=%d people=%d links=%d", len(v.Chats), len(v.People), len(v.Items))
	}
	labels := map[string]bool{}
	for _, it := range v.Items {
		labels[it.Label] = true
		if it.Source != "chat" || it.MessageRef == nil {
			t.Fatal("lost chat provenance")
		}
	}
	for _, label := range []string{"NotebookLM", "Google Docs", "GitHub"} {
		if !labels[label] {
			t.Fatal("missing label", label)
		}
	}
	// Collect only messages from creation onwards, while keeping effort's older refs.
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "https://drive.google.com/file/d/demo", Timestamp: now.Add(time.Second)}))
	*now = now.Add(time.Minute)
	testkit.Must(t, s.Tick(context.Background()))
	v, err = s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(v.Items) != 4 {
		t.Fatal("new link not collected")
	}
	set := s.Hub.Settings()
	set.HiddenChats = []string{testkit.Group}
	testkit.Must(t, s.Hub.SetSettings(set))
	listed, err := s.List(a, "all", 100)
	testkit.Must(t, err)
	if len(listed) != 0 {
		t.Fatal("hidden-chat task visible to MCP")
	}
	if _, err = s.Get(a, v.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("hidden task readable")
	}
	control, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(control.Items) != 3 {
		t.Fatal("control must omit hidden chat's link")
	}
	if _, err = s.Create(a, Create{Title: "Hidden", Kind: "long", Chats: []Entity{{ID: testkit.Group}}}); err == nil {
		t.Fatal("hidden task creation accepted")
	}
}
func TestCheckInExactlyOncePerSlotAndDraft(t *testing.T) {
	for _, rule := range []string{"weekly", "every_n_days", "once"} {
		t.Run(rule, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "long")
			day := int(now.Weekday())
			c := CheckIn{Rule: rule, Weekday: &day, EveryDays: 2, AtTime: "12:00", To: testkit.Phone, Note: "Learning and next topic", NextAt: *now}
			_, err := s.Update(a, v.ID, map[string]any{"check_in": c})
			testkit.Must(t, err)
			_, err = s.Draft(a, v.ID, "check_in", "Maya has finished chapter one.")
			testkit.Must(t, err)
			var wg sync.WaitGroup
			errs := make(chan error, 12)
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); errs <- s.Tick(context.Background()) }()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				testkit.Must(t, err)
			}
			suggestions, err := s.Hub.Suggestions()
			testkit.Must(t, err)
			if len(suggestions) != 1 || suggestions[0].Kind != "check_in" || suggestions[0].TaskID != v.ID || suggestions[0].DraftText != "Maya has finished chapter one." {
				t.Fatal("slot draft missing or duplicated")
			}
			var n int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates WHERE kind='task_due' AND agent=?", a.ID).Scan(&n))
			if n != 1 {
				t.Fatal("task_due duplicated")
			}
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if !got.NeedsYou {
				t.Fatal("queued check-in not needs_you")
			}
			if rule == "once" {
				if got.CheckIn != nil {
					t.Fatal("once did not complete")
				}
			} else if got.CheckIn == nil || !got.CheckIn.NextAt.After(*now) {
				t.Fatal("schedule not advanced")
			}
			testkit.Must(t, s.Hub.DismissSuggestion(suggestions[0].ID))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.NeedsYou {
				t.Fatal("dismiss did not clear cue")
			}
			if len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
				t.Fatal("ticker sent a message")
			}
		})
	}
}
func TestScheduleDSTCalendarAndLocalWallClock(t *testing.T) {
	s, _, _ := fixture(t)
	loc, err := time.LoadLocation("America/New_York")
	testkit.Must(t, err)
	s.Location = loc
	for _, day := range []time.Time{time.Date(2026, 3, 7, 9, 0, 0, 0, loc), time.Date(2026, 10, 31, 9, 0, 0, 0, loc)} {
		got := s.next(CheckIn{Rule: "every_n_days", EveryDays: 1, AtTime: "09:00"}, day)
		if got.Hour() != 9 || got.Day() != day.AddDate(0, 0, 1).Day() {
			t.Fatal("calendar drift")
		}
		if got.Sub(day) == 24*time.Hour {
			t.Fatal("fixture did not cross DST")
		}
	}
	sunday := 0
	got := s.next(CheckIn{Rule: "weekly", Weekday: &sunday, AtTime: "09:00"}, time.Date(2026, 3, 1, 9, 0, 0, 0, loc))
	if got.Day() != 8 || got.Hour() != 9 {
		t.Fatal("weekly DST drift")
	}
	got = s.next(CheckIn{Rule: "every_n_days", EveryDays: 1, AtTime: "02:30"}, time.Date(2026, 3, 7, 2, 30, 0, 0, loc))
	if got.Day() != 8 || got.Hour() != 3 || got.Minute() != 30 {
		t.Fatal("nonexistent wall time not normalized forwards")
	}
}
func TestWaitingNudgeDraftTemplateIncomingAndSkip(t *testing.T) {
	for _, draft := range []string{"", "An AI supplied this gentle follow-up."} {
		t.Run(draft, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "long")
			_, err := s.Update(a, v.ID, map[string]any{"waiting": map[string]any{"on": v.People[0], "what": "the syllabus"}})
			testkit.Must(t, err)
			if draft != "" {
				_, err = s.Draft(a, v.ID, "nudge", draft)
				testkit.Must(t, err)
			}
			*now = now.AddDate(0, 0, 3)
			testkit.Must(t, s.Tick(context.Background()))
			testkit.Must(t, s.Tick(context.Background()))
			xs, err := s.Hub.Suggestions()
			testkit.Must(t, err)
			if len(xs) != 1 || xs[0].Kind != "nudge" {
				t.Fatal("nudge not exactly once")
			}
			want := draft
			if want == "" {
				want = "Hi Maya, just checking in on the syllabus. No rush!"
			}
			if xs[0].DraftText != want {
				t.Fatal("wrong draft fallback")
			}
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.Waiting.NudgeState != "queued" || !got.NeedsYou {
				t.Fatal("nudge state not persisted")
			}
			testkit.Must(t, s.Incoming(context.Background(), model.Message{ID: "owner", ChatJID: testkit.Phone, Sender: testkit.Phone, IsFromMe: true, Timestamp: now.Add(time.Second)}))
			got, _ = s.Get(a, v.ID)
			if got.Waiting == nil {
				t.Fatal("owner echo cleared waiting")
			}
			testkit.Must(t, s.Incoming(context.Background(), model.Message{ID: "reply", ChatJID: testkit.Phone, Sender: testkit.Phone, Timestamp: now.Add(time.Second)}))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.Waiting != nil || got.Status != "active" || got.NeedsYou {
				t.Fatal("reply did not clear waiting")
			}
			var n int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates WHERE kind='task_waiting_cleared'").Scan(&n))
			if n != 1 {
				t.Fatal("missing waiting-cleared update")
			}
		})
	}
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.Update(a, v.ID, map[string]any{"waiting": Waiting{On: &v.People[0], What: "notes", Since: now.AddDate(0, 0, -4)}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	got, err := s.SkipNudge(a, v.ID)
	testkit.Must(t, err)
	if got.Waiting.NudgeState != "skipped" || got.NeedsYou {
		t.Fatal("skip did not settle nudge")
	}
	testkit.Must(t, s.Tick(context.Background()))
	xs, _ := s.Hub.Suggestions()
	if len(xs) != 0 {
		t.Fatal("skipped nudge requeued")
	}
}
func TestShortCloseAndRetention(t *testing.T) {
	for _, reason := range []string{"done", "checklist", "lifespan"} {
		t.Run(reason, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "short")
			var got Task
			var err error
			switch reason {
			case "done":
				got, err = s.Update(a, v.ID, map[string]any{"status": "done"})
			case "checklist":
				got, err = s.Checklist(a, v.ID, "add", []Step{{Text: "Confirm appointment"}})
				testkit.Must(t, err)
				got, err = s.Checklist(a, v.ID, "done", got.Checklist)
			case "lifespan":
				*now = now.AddDate(0, 0, 14)
				testkit.Must(t, s.Tick(context.Background()))
				got, err = s.Get(a, v.ID)
			}
			testkit.Must(t, err)
			want := "done"
			if reason == "lifespan" {
				want = "lifespan"
			}
			if got.Status != "done" || got.ClosedAt == nil || got.CloseReason != want {
				t.Fatal("short task failed to close")
			}
			var n int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates WHERE kind='task_closed'").Scan(&n))
			if n != 1 {
				t.Fatal("close update missing")
			}
			*now = now.AddDate(0, 0, 91)
			testkit.Must(t, s.Tick(context.Background()))
			r, err := s.raw(v.ID)
			testkit.Must(t, err)
			if !r.Purged || r.Title != v.Title || r.Goal != "" || len(r.People) != 0 || len(r.Checklist) != 0 {
				t.Fatal("retention kept workspace")
			}
		})
	}
}
func TestSuggestionSendsRecordHistoryAndNoAutomaticSend(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.Update(a, v.ID, map[string]any{"waiting": Waiting{On: &v.People[0], What: "the syllabus", Since: now.AddDate(0, 0, -4)}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	xs, _ := s.Hub.Suggestions()
	if len(xs) != 1 {
		t.Fatal("missing suggestion")
	}
	d, err := s.Hub.SendSuggestion(context.Background(), xs[0].ID, nil)
	testkit.Must(t, err)
	if d.State != "sent" || d.TaskID != v.ID {
		t.Fatal("suggestion did not use normal fake send")
	}
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(got.UpdatesSent) != 1 || got.UpdatesSent[0].Kind != "nudge" || got.Waiting.NudgeState != "sent" || got.NeedsYou {
		t.Fatal("send history/state missing")
	}
	testkit.Must(t, s.RecordSent(d))
	got, _ = s.Get(a, v.ID)
	if len(got.UpdatesSent) != 1 {
		t.Fatal("history not idempotent")
	}
	feed, err := s.Feed(time.Time{})
	testkit.Must(t, err)
	raw, _ := json.Marshal(feed)
	if strings.Contains(string(raw), "Hi Maya") || strings.Contains(string(raw), "text_preview") {
		t.Fatal("message text leaked to feed")
	}
	// The feed exposes event labels rather than stored prose.
	kinds := map[string]bool{}
	for _, e := range feed[0].Events {
		kinds[e.Kind] = true
	}
	for _, kind := range []string{"created", "waiting_set", "nudge_queued", "nudge_sent", "update_sent"} {
		if !kinds[kind] {
			t.Fatal("missing feed event", kind)
		}
	}
}

func TestNudgeOnDeviceFallbackPriorityAndUnavailable(t *testing.T) {
	for _, mode := range []string{"local", "ai", "unavailable", "async"} {
		t.Run(mode, func(t *testing.T) {
			s, a, now := fixture(t)
			v := create(t, s, a, "long")
			_, err := s.Update(a, v.ID, map[string]any{"waiting": Waiting{On: &v.People[0], What: "notes", Since: now.AddDate(0, 0, -4)}})
			testkit.Must(t, err)
			calls := 0
			s.NudgeDraft = func(context.Context, Task) (string, error) {
				calls++
				if mode == "unavailable" {
					return "", errors.New("unavailable")
				}
				return "An on-device draft.", nil
			}
			if mode == "ai" {
				_, err = s.Draft(a, v.ID, "nudge", "An AI draft.")
				testkit.Must(t, err)
			}
			if mode == "async" {
				s.NudgeDraft = nil
			}
			testkit.Must(t, s.Tick(context.Background()))
			if mode == "async" || mode == "ai" {
				_, err = s.OnDeviceNudge(v.ID, "An on-device draft.")
				testkit.Must(t, err)
			}
			xs, err := s.Hub.Suggestions()
			testkit.Must(t, err)
			if len(xs) != 1 {
				t.Fatal("missing draft")
			}
			want := "An on-device draft."
			if mode == "ai" {
				want = "An AI draft."
				if calls != 0 {
					t.Fatal("model called despite AI draft")
				}
			}
			if mode == "unavailable" {
				want = "Hi Maya, just checking in on notes. No rush!"
			}
			if xs[0].DraftText != want {
				t.Fatal("draft priority mismatch")
			}
		})
	}
}

func TestWorkspaceNotesAndFilesDoNotMultiplyOnTicks(t *testing.T) {
	s, a, _ := fixture(t)
	v := create(t, s, a, "long")
	for _, item := range []Item{{Kind: "note", Title: "Review", Text: "Memory exercises"}, {Kind: "file", Title: "Practice sheet", AttachmentID: "invented-attachment"}} {
		var err error
		v, err = s.Attach(a, v.ID, item)
		testkit.Must(t, err)
	}
	count := len(v.Items)
	for i := 0; i < 3; i++ {
		testkit.Must(t, s.Tick(context.Background()))
	}
	got, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(got.Items) != count {
		t.Fatal("tick duplicated workspace items")
	}
	ids := map[string]bool{}
	for _, item := range got.Items {
		if ids[item.ID] {
			t.Fatal("duplicate workspace identity")
		}
		ids[item.ID] = true
	}
}

func TestOnDeviceCannotReplaceLateAIDraftOrSettledNudge(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	_, err := s.Update(a, v.ID, map[string]any{"waiting": Waiting{On: &v.People[0], What: "notes", Since: now.AddDate(0, 0, -4)}})
	testkit.Must(t, err)
	testkit.Must(t, s.Tick(context.Background()))
	_, err = s.Draft(a, v.ID, "nudge", "A newer AI draft.")
	testkit.Must(t, err)
	_, err = s.OnDeviceNudge(v.ID, "Late local result")
	testkit.Must(t, err)
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	if r.DraftNudge != "A newer AI draft." {
		t.Fatal("local worker replaced AI draft")
	}
	_, err = s.SkipNudge(nil, v.ID)
	testkit.Must(t, err)
	var before model.Suggestion
	testkit.Must(t, s.Hub.Repo.Get("suggestions", r.Waiting.NudgeSuggestionID, &before))
	_, _ = s.OnDeviceNudge(v.ID, "Late after skip")
	var after model.Suggestion
	testkit.Must(t, s.Hub.Repo.Get("suggestions", r.Waiting.NudgeSuggestionID, &after))
	if after.DraftText != before.DraftText || after.State != "dismissed" {
		t.Fatal("settled draft changed")
	}
}
