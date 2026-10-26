package efforts

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestFileOnlyMessagesRemainDecodableInTimeline(t *testing.T) {
	s, _ := seed(t)
	for i := 0; i < 3; i++ {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("document-%d", i), ChatJID: testkit.Phone, Sender: testkit.Phone, Filename: "pitch deck.pdf", MediaType: "document", IsFromMe: i == 1, Timestamp: testkit.Epoch.Add(-time.Duration(2-i) * time.Minute)}))
	}
	xs, e := s.List(nil, Filter{Query: "pitch deck.pdf"})
	testkit.Must(t, e)
	if len(xs) != 1 {
		t.Fatal("file effort missing")
	}
	detail, e := s.Get(nil, xs[0].ID)
	testkit.Must(t, e)
	data, e := json.Marshal(detail)
	testkit.Must(t, e)
	var wire struct {
		Timeline []struct {
			Message map[string]any `json:"message"`
		} `json:"timeline"`
	}
	testkit.Must(t, json.Unmarshal(data, &wire))
	files := 0
	for _, row := range wire.Timeline {
		if row.Message["filename"] == "pitch deck.pdf" {
			files++
			if text, ok := row.Message["content"].(string); !ok || text != "" {
				t.Fatal("missing required empty content field")
			}
		}
	}
	if files != 3 {
		t.Fatal("file evidence lost")
	}
}

func seed(t testing.TB) (*Service, []model.Message) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	ms := []model.Message{
		{ID: "deck-1", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck opening slides", Timestamp: testkit.Epoch.Add(-5 * 24 * time.Hour)},
		{ID: "noise", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Coffee and sunshine", Timestamp: testkit.Epoch.Add(-4 * 24 * time.Hour)},
		{ID: "deck-2", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "The pitch deck layout is ready", Timestamp: testkit.Epoch.Add(-3 * 24 * time.Hour)},
		{ID: "deck-3", ChatJID: testkit.Phone, IsFromMe: true, Content: "I will review the pitch deck", Timestamp: testkit.Epoch.Add(-2 * 24 * time.Hour)},
		{ID: "deck-4", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Can you review the pitch deck slides? Due tomorrow.", Timestamp: testkit.Epoch.Add(-time.Hour)},
	}
	for _, m := range ms {
		testkit.Must(t, h.Messages.PutMessage(m))
	}
	return &Service{Hub: h}, ms
}
func TestCrossChatGroupingSearchTimelineAndIsolation(t *testing.T) {
	s, ms := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || len(xs[0].MessageRefs) != 4 || len(xs[0].Chats) != 2 || xs[0].Status != "on_you" || xs[0].Due == nil {
		t.Fatalf("unexpected effort shape: %+v", xs)
	}
	for _, q := range []string{"Maya", "Design Club", "layout", "slides"} {
		xs, e := s.List(nil, Filter{Query: q})
		testkit.Must(t, e)
		if len(xs) != 1 {
			t.Fatalf("search %q", q)
		}
	}
	testkit.Must(t, s.Hub.Record(model.Activity{AgentID: "example", Tool: "save_summary", Kind: "summary", ChatJIDs: []string{testkit.Phone}, MessageIDs: []string{ms[0].ID}, Note: "Invented review"}))
	d, e := s.Get(nil, xs[0].ID)
	testkit.Must(t, e)
	if len(d.Timeline) != 5 || d.Timeline[0].Action == nil {
		t.Fatal("timeline lost action ordering")
	}
	testkit.Must(t, s.Hub.Record(model.Activity{AgentID: "example", Tool: "save_summary", Kind: "summary", ChatJIDs: []string{testkit.Phone}, Note: "The presentation slides are ready for review."}))
	d, e = s.Get(nil, xs[0].ID)
	testkit.Must(t, e)
	if len(d.AgentActionRefs) != 2 {
		t.Fatal("summary action without IDs was lost")
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Group}
	testkit.Must(t, s.Hub.SetSettings(settings))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || len(xs[0].Chats) != 1 || len(xs[0].MessageRefs) != 3 {
		t.Fatal("owner hidden chat leaked")
	}
	a := model.Agent{ID: "restricted", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	xs, e = s.List(&a, Filter{})
	testkit.Must(t, e)
	if len(xs) != 0 {
		t.Fatal("hidden effort disclosed")
	}
}
func TestStatusRules(t *testing.T) {
	cases := []struct {
		text string
		mine bool
		days int
		want string
	}{
		{"Can you review the slides?", false, 0, "on_you"}, {"Can you review the slides?", true, 0, "waiting_on_them"},
		{"Slide layout is progressing", false, 0, "moving"}, {"Slides submitted", false, 0, "done"},
		{"Slides submitted!", false, 0, "done"}, {"Slides not done", false, 0, "moving"}, {"Can you mark the slides done?", false, 0, "on_you"},
		{"Slides agreed", false, 8, "done"}, {"Slides progressing", false, 15, "dormant"},
	}
	for _, c := range cases {
		t.Run(c.want+fmt.Sprint(c.mine), func(t *testing.T) {
			m := model.Message{ChatJID: testkit.Phone, Content: c.text, IsFromMe: c.mine, Timestamp: testkit.Epoch.AddDate(0, 0, -c.days)}
			if got := status([]model.Message{m}, testkit.Epoch); got != c.want {
				t.Fatalf("%s != %s", got, c.want)
			}
		})
	}
	s, _ := seed(t)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "closure", ChatJID: testkit.Phone, Content: "done", Timestamp: testkit.Epoch}))
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || xs[0].Status != "done" {
		t.Fatal("short closure was lost")
	}
}
func TestEditsMergeSplitAttachSurviveAndStayPrivate(t *testing.T) {
	s, ms := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	id := xs[0].ID
	title, summary, st := "Pitch review", "Review the opening tomorrow.", "waiting_on_them"
	agent := model.Agent{ID: "agent", Name: "Example AI", Permissions: model.Defaults(false)}
	_, e = s.Update(&agent, id, Update{Title: &title, Summary: &summary, Status: &st})
	testkit.Must(t, e)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new", ChatJID: testkit.Phone, Content: "More slides for the pitch deck", Timestamp: testkit.Epoch}))
	xs, e = (&Service{Hub: s.Hub}).List(nil, Filter{})
	testkit.Must(t, e)
	if xs[0].Title != title || xs[0].Status != st || xs[0].CreatedBy != "Example AI" {
		t.Fatal("edits lost after new message/restart")
	}
	part, e := s.Regroup(nil, "split", id, "", []Ref{ref(ms[0]), ref(ms[2])})
	testkit.Must(t, e)
	if len(part.MessageRefs) != 2 {
		t.Fatal("split lost refs")
	}
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	xs, e = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	if len(xs) != 2 {
		t.Fatal("split failed")
	}
	merged, e := s.Regroup(nil, "merge", id, part.ID, nil)
	testkit.Must(t, e)
	if len(merged.MessageRefs) != 5 {
		t.Fatalf("merge refs: %d", len(merged.MessageRefs))
	}
	_, e = s.Regroup(nil, "attach", id, "", []Ref{ref(ms[1])})
	testkit.Must(t, e)
	agent.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
	limited, e := s.List(&agent, Filter{})
	testkit.Must(t, e)
	for _, x := range limited {
		if x.Title == title || strings.Contains(x.Summary, summary) {
			t.Fatal("cross-scope edit text leaked")
		}
	}
	hide := true
	_, e = s.Update(nil, id, Update{Hidden: &hide})
	testkit.Must(t, e)
	hide = false
	_, e = s.Update(nil, id, Update{Hidden: &hide})
	testkit.Must(t, e)
}
func TestCircleUpdatesOrderAndSeen(t *testing.T) {
	s, _ := seed(t)
	c := model.Circle{ID: "design", Name: "Design", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}, {ChatJID: testkit.Group}}}
	testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	updates, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(updates) != 1 || updates[0].Status != "on_you" || updates[0].Line == "" {
		t.Fatal("concrete request summary is missing")
	}
	st := "moving"
	_, e = s.Update(nil, updates[0].EffortID, Update{Status: &st, Due: ptr("")})
	testkit.Must(t, e)
	testkit.Must(t, s.Seen())
	updates, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(updates) != 0 {
		t.Fatal("seen circles repeated")
	}
}

func TestCircleUpdatesDueRecencyLimitAndUnconfirmed(t *testing.T) {
	s, _ := seed(t)
	testkit.Must(t, s.Hub.Repo.Put("circles", "confirmed", model.Circle{ID: "confirmed", Name: "Studio", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}))
	testkit.Must(t, s.Hub.Repo.Put("circles", "unconfirmed", model.Circle{ID: "unconfirmed", Name: "Proposed", Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}))
	for i, topic := range []string{"pottery", "budget", "rehearsal", "cycling", "ceramics"} {
		for j := 0; j < 3; j++ {
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("ordering-%d-%d", i, j), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: topic + " supplies. Due " + []string{"2026-10-16", "2026-10-18", "2026-10-18"}[j] + ".", IsFromMe: j == 1, Timestamp: testkit.Epoch.Add(-time.Duration(20-i*2-j) * time.Minute)}))
		}
		xs, e := s.List(nil, Filter{Query: topic})
		testkit.Must(t, e)
		if len(xs) != 1 {
			t.Fatal("fixture grouping")
		}
		st := "moving"
		due := ""
		if i == 0 {
			due = "2026-10-16T00:00:00Z"
		}
		if i == 1 {
			due = "2026-10-18T00:00:00Z"
		}
		_, e = s.Update(nil, xs[0].ID, Update{Title: &topic, Status: &st, Due: &due})
		testkit.Must(t, e)
	}
	xs, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(xs) != 4 || xs[0].Status != "on_you" || xs[1].Title != "pottery" || xs[2].Title != "budget" || xs[3].Title != "ceramics" {
		t.Fatal("priority/due/recency/cap ordering")
	}
	for _, x := range xs {
		if x.Circle.ID != "confirmed" {
			t.Fatal("unconfirmed circle surfaced")
		}
	}
}

func TestSeenAcknowledgementPreservesConcurrentArrival(t *testing.T) {
	s, _ := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	st := "moving"
	_, e = s.Update(nil, xs[0].ID, Update{Status: &st, Due: ptr("")})
	testkit.Must(t, e)
	testkit.Must(t, s.Hub.Repo.Put("circles", "studio", model.Circle{ID: "studio", Name: "Studio", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}))
	before, e := s.CircleUpdates()
	testkit.Must(t, e)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "concurrent", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck layout is ready", Timestamp: testkit.Epoch}))
	testkit.Must(t, s.Seen(map[string]time.Time{"studio": before[0].LastAt}))
	after, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(after) != 1 || !after[0].LastAt.Equal(testkit.Epoch) {
		t.Fatal("concurrent arrival marked seen")
	}
	testkit.Must(t, s.Seen(map[string]time.Time{"studio": testkit.Epoch}))
	testkit.Must(t, s.Seen(map[string]time.Time{"studio": before[0].LastAt}))
	after, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(after) != 0 {
		t.Fatal("older acknowledgement moved seen backwards")
	}
}
func TestTwentyThousandMessageGroupingShape(t *testing.T) {
	ms := make([]model.Message, 20000)
	for i := range ms {
		person := i % 300
		ms[i] = model.Message{ID: fmt.Sprint(i), ChatJID: fmt.Sprintf("1555%07d@s.whatsapp.net", person), Sender: fmt.Sprintf("1555%07d@s.whatsapp.net", person), Content: fmt.Sprintf("project%d slides layout review", person), Timestamp: testkit.Epoch.Add(-time.Duration(20000-i) * time.Minute)}
	}
	start := time.Now()
	cs := group(ms)
	elapsed := time.Since(start)
	if len(cs) != 300 {
		t.Fatalf("%d clusters", len(cs))
	}
	t.Logf("20k messages / 300 people: %s", elapsed)
	// The 200ms contract is for an incremental arrival, measured end-to-end below.
	// Cold grouping is logged here without a scheduler-sensitive wall-clock gate.
}

func TestIncrementalHistoryAndRevisionInvalidation(t *testing.T) {
	s, ms := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	id := xs[0].ID
	// A chronological arrival extends the same index and stable ID.
	extra := model.Message{ID: "arrived", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck submitted", Timestamp: testkit.Epoch}
	testkit.Must(t, s.Hub.Messages.PutMessage(extra))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || xs[0].ID != id || xs[0].Status != "done" || len(xs[0].MessageRefs) != 5 {
		t.Fatal("incremental arrival lost effort")
	}
	// An edited/deleted row forces reconstruction, never stale text or refs.
	extra.Content = "Pitch deck: can you check?"
	testkit.Must(t, s.Hub.Messages.PutMessage(extra))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if xs[0].Status != "on_you" {
		t.Fatal("message edit was cached")
	}
	_, e = s.Hub.Messages.DB.Exec("DELETE FROM messages WHERE id=?", extra.ID)
	testkit.Must(t, e)
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs[0].MessageRefs) != 4 {
		t.Fatal("deleted message retained")
	}
	quoted := model.Message{ID: "quote", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "This needs another look", QuotedMessageID: ms[0].ID, Timestamp: testkit.Epoch}
	testkit.Must(t, s.Hub.Messages.PutMessage(quoted))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs[0].MessageRefs) != 5 {
		t.Fatal("quote chain lost")
	}
}

func TestProjectedEffortCannotOverwriteOrExposeHiddenEdit(t *testing.T) {
	s, _ := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	id := xs[0].ID
	title, summary := "Private group decision", "A private group detail."
	_, e = s.Update(nil, id, Update{Title: &title, Summary: &summary})
	testkit.Must(t, e)
	a := model.Agent{ID: "limited", Name: "Example AI", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
	projected, e := s.List(&a, Filter{})
	testkit.Must(t, e)
	if len(projected) != 1 || projected[0].ID != id {
		t.Fatal("fixture must preserve earliest visible ID")
	}
	publicTitle := "Visible title"
	_, e = s.Update(&a, id, Update{Title: &publicTitle})
	if e != model.ErrNotFound {
		t.Fatal("projected write reached hidden metadata")
	}
	_, e = s.Regroup(&a, "attach", id, "", projected[0].MessageRefs[:1])
	if e != model.ErrNotFound {
		t.Fatal("projected attach reached hidden metadata")
	}
	full, e := s.Get(nil, id)
	testkit.Must(t, e)
	if full.Title != title || full.Summary != summary {
		t.Fatal("hidden owner edit overwritten")
	}
}

func TestAttachWinsOverPreviousAssignmentsAndRepeatedSplitIsFresh(t *testing.T) {
	s, ms := seed(t)
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	id := xs[0].ID
	title := "Owner title"
	_, e = s.Update(nil, id, Update{Title: &title})
	testkit.Must(t, e)
	part, e := s.Regroup(nil, "split", id, "", []Ref{ref(ms[0])})
	testkit.Must(t, e)
	// Attach a reference held by the source's older explicit edit.
	part, e = s.Regroup(nil, "attach", part.ID, "", []Ref{ref(ms[2])})
	testkit.Must(t, e)
	if len(part.MessageRefs) != 2 {
		t.Fatal("attach lost to an older assignment")
	}
	_, e = s.Regroup(nil, "merge", id, part.ID, nil)
	testkit.Must(t, e)
	next, e := s.Regroup(nil, "split", id, "", []Ref{ref(ms[0])})
	testkit.Must(t, e)
	if next.ID == part.ID {
		t.Fatal("split reused an ID under a fixed clock")
	}
}

func TestIncrementalTwentyThousandMessageBudget(t *testing.T) {
	h, _, _ := testkit.Open(t)
	for i := 0; i < 300; i++ {
		jid := fmt.Sprintf("1555%07d@s.whatsapp.net", i)
		person := model.Person{ID: fmt.Sprintf("fixture-person-%d", i), Name: fmt.Sprintf("Example person %d", i), JIDs: []string{jid}, UpdatedAt: testkit.Epoch}
		testkit.Must(t, h.Repo.Put("people", person.ID, person))
	}
	tx, e := h.Messages.DB.Begin()
	testkit.Must(t, e)
	for i := 0; i < 300; i++ {
		_, e = tx.Exec("INSERT INTO chats(jid,name) VALUES(?,?)", fmt.Sprintf("1555%07d@s.whatsapp.net", i), fmt.Sprintf("Example person %d", i))
		testkit.Must(t, e)
	}
	stmt, e := tx.Prepare("INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me) VALUES(?,?,?,?,?,0)")
	testkit.Must(t, e)
	for i := 0; i < 20000; i++ {
		jid := fmt.Sprintf("1555%07d@s.whatsapp.net", i%300)
		_, e = stmt.Exec(fmt.Sprint(i), jid, jid, fmt.Sprintf("project%d slides draft", i%300), testkit.Epoch.Add(-time.Duration(20000-i)*time.Minute))
		testkit.Must(t, e)
	}
	testkit.Must(t, stmt.Close())
	testkit.Must(t, tx.Commit())
	svc := &Service{Hub: h}
	coldStart := time.Now()
	xs, e := svc.List(nil, Filter{Limit: 500})
	t.Logf("cold 20k load: %s", time.Since(coldStart))
	testkit.Must(t, e)
	if len(xs) != 300 {
		t.Fatal("fixture purposes drifted")
	}
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "arrival", ChatJID: "15550000000@s.whatsapp.net", Sender: "15550000000@s.whatsapp.net", Content: "project0 slides draft", Timestamp: testkit.Epoch}))
	start := time.Now()
	xs, e = svc.List(nil, Filter{Limit: 500})
	testkit.Must(t, e)
	elapsed := time.Since(start)
	if len(xs) != 300 {
		t.Fatal("incremental purpose count changed")
	}
	t.Logf("incremental 20k messages / 300 people: %s", elapsed)
	if elapsed >= 200*time.Millisecond {
		t.Errorf("incremental recompute exceeds 200ms: %s", elapsed)
	}
}
