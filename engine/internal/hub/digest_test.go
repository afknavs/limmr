package hub_test

import (
	"encoding/json"
	"errors"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func digestFixture(t *testing.T) (*hub.Service, *circles.Service, model.Agent) {
	t.Helper()
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	c := &circles.Service{Hub: h, People: p}
	a, _, err := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, err)
	for _, circle := range []model.Circle{
		{ID: "design", Name: "Design Club", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Group}}},
		{ID: "friends", Name: "Friends", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}},
	} {
		testkit.Must(t, h.Repo.Put("circles", circle.ID, circle))
	}
	return h, c, a
}

func TestD1DigestAccessHiddenBriefsAndZeroReads(t *testing.T) {
	h, c, a := digestFixture(t)
	now := h.Now()
	a.Permissions.Access = model.Access{Circles: []string{"design"}}
	testkit.Must(t, h.UpdateAgent(a))
	other, _, err := h.CreateAgent("Other AI", "custom", "local")
	testkit.Must(t, err)
	for _, m := range []model.Message{
		{ID: "visible", ChatJID: testkit.Group, Sender: testkit.Phone, Content: strings.Repeat("界", 200), Timestamp: now.Add(-time.Hour)},
		{ID: "private", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "PRIVATE_CANARY", Timestamp: now},
		{ID: "deleted", ChatJID: testkit.Group, Content: "DELETED_CANARY", Timestamp: now},
	} {
		testkit.Must(t, h.Messages.PutMessage(m))
	}
	_, err = h.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id='deleted'", now)
	testkit.Must(t, err)
	for _, b := range []model.Brief{
		{CircleID: "design", Version: 1, ChatJIDs: []string{testkit.Group}, WrittenAt: now.Add(-time.Hour), OpenAsks: []model.OpenAsk{{From: "Maya Rao", Text: "Review the sketch?", ChatJID: testkit.Group, At: now.Add(-time.Hour)}}, Upcoming: []model.Upcoming{{Date: "2026-10-17", Text: "Sketch due"}}},
		{CircleID: "friends", Version: 1, ChatJIDs: []string{testkit.Phone}, WrittenAt: now, OpenAsks: []model.OpenAsk{{Text: "PRIVATE_ASK", ChatJID: testkit.Phone, At: now}}, Upcoming: []model.Upcoming{{Date: "2026-10-18", Text: "PRIVATE_DUE"}}},
	} {
		testkit.Must(t, h.Repo.Put("briefs", b.CircleID, b))
	}
	for _, v := range []model.Activity{
		{AgentID: a.ID, At: now.Add(-time.Hour), Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 4},
		{AgentID: a.ID, At: now.Add(-50 * time.Minute), Kind: "search", Tool: "search_messages", ChatJIDs: []string{testkit.Group}, Count: 2},
		{AgentID: a.ID, At: now, Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 0},
		{AgentID: a.ID, At: now, Kind: "read", Tool: "list_chats", ChatJIDs: []string{testkit.Group}, Count: 90},
		{AgentID: a.ID, At: now, Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group, testkit.Phone}, Count: 900},
		{AgentID: other.ID, At: now, Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 99},
		{AgentID: a.ID, At: now, Kind: "sent", ChatJIDs: []string{testkit.Group}, MessageIDs: []string{"outbound"}, Note: "The sketch is ready"},
		{AgentID: a.ID, At: now, Kind: "sent", ChatJIDs: []string{testkit.Phone}, Note: "PRIVATE_SEND"},
	} {
		testkit.Must(t, h.Record(v))
	}
	testkit.Must(t, h.Repo.Put("drafts", "outbound", model.Draft{ChatJID: testkit.Group, MessageID: "outbound", Status: "delivered"}))
	d, err := h.GetDigest(&a, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	if len(d.CircleUpdates) != 1 || d.CircleUpdates[0].CircleID != "design" || d.CircleUpdates[0].NewMessages != 1 || len([]rune(d.CircleUpdates[0].LatestLine)) != 160 || len(d.CircleUpdates[0].ActivePeople) != 1 {
		t.Fatalf("updates: %+v", d.CircleUpdates)
	}
	if len(d.AgentWork) != 1 || d.AgentWork[0].Messages != 6 || d.AgentWork[0].Sessions != 1 {
		t.Fatalf("work: %+v", d.AgentWork)
	}
	if len(d.WaitingOnYou) != 1 || d.WaitingOnYou[0].Who != "Maya Rao" || d.WaitingOnYou[0].Chat != "Design Club" || len(d.Due) != 1 {
		t.Fatalf("asks/due: %+v %+v", d.WaitingOnYou, d.Due)
	}
	if len(d.Sends) != 1 || d.Sends[0].Status != "delivered" || d.Sends[0].Text != "The sketch is ready" {
		t.Fatalf("sends: %+v", d.Sends)
	}
	b, _ := json.Marshal(d)
	if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "DELETED") || strings.Contains(string(b), other.ID) {
		t.Fatal("out-of-scope data leaked")
	}
	if _, err = h.GetDigest(&a, hub.DigestOptions{CircleID: "friends"}, c); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("invisible circle: %v", err)
	}
	// Completed asks stay completed through the existing brief presentation.
	testkit.Must(t, c.DoneAsk("design", circles.AskID("design", "Review the sketch?", testkit.Group)))
	d, err = h.GetDigest(&a, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	if len(d.WaitingOnYou) != 0 {
		t.Fatal("completed ask resurfaced")
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Group}
	testkit.Must(t, h.SetSettings(settings))
	d, err = h.GetDigest(&a, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	if len(d.CircleUpdates)+len(d.AgentWork)+len(d.Sends)+len(d.Due)+len(d.WaitingOnYou) != 0 {
		t.Fatal("hidden chat leaked through digest")
	}
	owner, err := h.GetDigest(nil, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	for _, u := range owner.CircleUpdates {
		if u.CircleID == "design" {
			t.Fatal("owner digest included hidden chat")
		}
	}
	// A mixed brief cannot expose hidden free text even to the owner digest.
	testkit.Must(t, h.Repo.Put("briefs", "mixed", model.Brief{CircleID: "friends", Version: 2, ChatJIDs: []string{testkit.Group, testkit.Phone}, WrittenAt: now, Upcoming: []model.Upcoming{{Text: "MIXED_CANARY", Date: "2026-10-19"}}}))
	owner, err = h.GetDigest(nil, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	b, _ = json.Marshal(owner)
	if strings.Contains(string(b), "MIXED_CANARY") {
		t.Fatal("mixed brief leaked hidden text")
	}
}

func TestD1DigestWindowsAndCursorIsolation(t *testing.T) {
	h, c, a := digestFixture(t)
	now := testkit.Epoch
	today := now.Truncate(24 * time.Hour)
	for _, item := range []struct {
		id string
		at time.Time
	}{
		{"older", now.AddDate(0, 0, -8)}, {"week-start", now.AddDate(0, 0, -7)}, {"yesterday", today.Add(-time.Second)}, {"midnight", today}, {"now", now}, {"future", now.Add(time.Hour)},
	} {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: item.id, ChatJID: testkit.Group, Content: item.id, Timestamp: item.at}))
		testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, At: item.at, Kind: "read", Tool: "list_messages", ChatJIDs: []string{testkit.Group}, Count: 1}))
	}
	d, err := h.GetDigest(&a, hub.DigestOptions{Period: "week", CircleID: "design"}, c)
	testkit.Must(t, err)
	if len(d.CircleUpdates) != 1 || d.CircleUpdates[0].NewMessages != 4 || d.AgentWork[0].Messages != 4 {
		t.Fatalf("week: %+v", d)
	}
	d, err = h.GetDigest(&a, hub.DigestOptions{CircleID: "design"}, c)
	testkit.Must(t, err)
	if d.CircleUpdates[0].NewMessages != 2 || d.AgentWork[0].Messages != 2 {
		t.Fatalf("today: %+v", d)
	}
	cursor := d.Cursor
	// Recreate the service to prove the implicit watermark is persisted.
	h2 := hub.New(h.Repo, h.Messages, h.Home)
	h2.Now = func() time.Time { return now.Add(2 * time.Hour) }
	c2 := &circles.Service{Hub: h2, People: c.People}
	d, err = h2.GetDigest(&a, hub.DigestOptions{Period: "since_last", CircleID: "design"}, c2)
	testkit.Must(t, err)
	if d.CircleUpdates[0].NewMessages != 1 || d.CircleUpdates[0].LatestLine != "future" || d.AgentWork[0].Messages != 1 {
		t.Fatalf("since last: %+v", d)
	}
	d, err = h2.GetDigest(&a, hub.DigestOptions{Period: "since_last", CircleID: "design"}, c2)
	testkit.Must(t, err)
	if len(d.CircleUpdates)+len(d.AgentWork) != 0 {
		t.Fatal("cursor repeated data at boundary")
	}
	d, err = h2.GetDigest(&a, hub.DigestOptions{Period: "since_last", CircleID: "design", Cursor: cursor}, c2)
	testkit.Must(t, err)
	if d.CircleUpdates[0].NewMessages != 1 {
		t.Fatal("explicit cursor failed to replay")
	}
	other, _, err := h.CreateAgent("Second AI", "custom", "local")
	testkit.Must(t, err)
	for _, o := range []hub.DigestOptions{{Period: "bad"}, {Period: "today", Cursor: cursor}, {Period: "since_last", Cursor: "invalid"}, {Period: "since_last", CircleID: "friends", Cursor: cursor}} {
		if _, err = h.GetDigest(&a, o, c); !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid options accepted: %+v, %v", o, err)
		}
	}
	if _, err = h2.GetDigest(&other, hub.DigestOptions{Period: "since_last", CircleID: "design", Cursor: cursor}, c2); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("cross-agent cursor accepted")
	}
	if _, err = h.GetDigest(&a, hub.DigestOptions{Period: "since_last", CircleID: "design", Cursor: d.Cursor}, c); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("future cursor accepted")
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, h.SetSettings(settings))
	// Explicit all-circle cursors are bound to the visible set, not just agent ID.
	all, err := h.GetDigest(&a, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	settings.HiddenChats = nil
	testkit.Must(t, h.SetSettings(settings))
	if _, err = h.GetDigest(&a, hub.DigestOptions{Period: "since_last", Cursor: all.Cursor}, c); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("changed-scope cursor accepted")
	}
}

func TestD1DigestCapabilitiesAndEmptyReads(t *testing.T) {
	h, c, a := digestFixture(t)
	testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Kind: "read", Tool: "list_messages", Count: 0, ChatJIDs: []string{testkit.Group}}))
	d, err := h.GetDigest(&a, hub.DigestOptions{}, c)
	testkit.Must(t, err)
	if len(d.AgentWork) != 0 {
		t.Fatal("zero-read row")
	}
	a.Permissions.ReadMessages = false
	if _, err = h.GetDigest(&a, hub.DigestOptions{}, c); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("message capability bypass")
	}
	a.Permissions.ReadMessages = true
	a.Permissions.ReadPeople = false
	if _, err = h.GetDigest(&a, hub.DigestOptions{}, c); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("people capability bypass")
	}
	a.Permissions.ReadPeople = true
	a.Permissions.Access.Until = &testkit.Epoch
	if _, err = h.GetDigest(&a, hub.DigestOptions{}, c); err == nil {
		t.Fatal("expired access bypass")
	}
}

func TestD1DigestCompleteCountsAndSendsBeyondReadPage(t *testing.T) {
	h, c, a := digestFixture(t)
	// Count in SQL rather than accidentally using the read tool's 50/1000-row page.
	_, err := h.Messages.DB.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1100)
		INSERT INTO messages(id,chat_jid,sender,content,timestamp,is_from_me)
		SELECT 'bulk-'||x,?,?,'A sketch update',?,0 FROM n`, testkit.Group, testkit.Phone, testkit.Epoch)
	testkit.Must(t, err)
	for i := 0; i < 75; i++ {
		testkit.Must(t, h.Record(model.Activity{AgentID: a.ID, Kind: "sent", ChatJIDs: []string{testkit.Group}, Note: strings.Repeat("界", 200)}))
	}
	d, err := h.GetDigest(&a, hub.DigestOptions{CircleID: "design"}, c)
	testkit.Must(t, err)
	if len(d.CircleUpdates) != 1 || d.CircleUpdates[0].NewMessages != 1100 || len(d.Sends) != 75 {
		t.Fatal("digest silently used a UI page or truncated sends")
	}
	for _, send := range d.Sends {
		if send.Status != "sent" || len([]rune(send.Text)) != 160 {
			t.Fatal("missing fallback status or text cap")
		}
	}
}
