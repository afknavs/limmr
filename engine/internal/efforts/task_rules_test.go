package efforts

import (
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestTaskRulesEffortAttentionExpiryAndHistory(t *testing.T) {
	s, _ := seed(t)
	xs, err := s.List(nil, Filter{})
	testkit.Must(t, err)
	e := xs[0]
	if e.Status != "on_you" {
		t.Fatal("missing request")
	}
	now := e.LastAt.Add(model.NeedsYouTTL - time.Second)
	s.Hub.Now = func() time.Time { return now }
	before, err := s.Get(nil, e.ID)
	testkit.Must(t, err)
	if before.Status != "on_you" {
		t.Fatal("expired early")
	}
	now = now.Add(time.Second)
	after, err := s.Get(nil, e.ID)
	testkit.Must(t, err)
	if after.Status != "dormant" || len(after.Timeline) != len(before.Timeline) {
		t.Fatal("expiry lost history or attention remained")
	}
	xs, err = s.List(nil, Filter{Status: "on_you"})
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("expired request on attention list")
	}
	// Restoring time demonstrates read projection never overwrote the request.
	now = e.LastAt.Add(time.Second)
	after, err = s.Get(nil, e.ID)
	testkit.Must(t, err)
	if after.Status != "on_you" {
		t.Fatal("request overwritten")
	}
	_, err = s.Hub.Repo.DB.Exec("INSERT INTO tasks VALUES(?,?)", "ended", dbutil.JSON(map[string]any{"id": "ended", "effort_id": e.ID, "status": "done"}))
	testkit.Must(t, err)
	after, err = s.Get(nil, e.ID)
	testkit.Must(t, err)
	if after.Status != "done" || len(after.Timeline) != len(before.Timeline) {
		t.Fatal("ended task attention remained")
	}
}

func TestTaskRulesAnsweredEffortLeavesAttentionAndFollowupRenews(t *testing.T) {
	s, _ := seed(t)
	xs, err := s.List(nil, Filter{})
	testkit.Must(t, err)
	id := xs[0].ID
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "answer", ChatJID: testkit.Phone, IsFromMe: true, Content: "Pitch deck slides submitted", Timestamp: testkit.Epoch}))
	got, err := s.Get(nil, id)
	testkit.Must(t, err)
	if got.Status == "on_you" {
		t.Fatal("answered request still needs owner")
	}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "followup", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Can you review the pitch deck slides again?", Timestamp: testkit.Epoch.Add(time.Minute)}))
	s.Hub.Now = func() time.Time { return testkit.Epoch.Add(2 * time.Minute) }
	got, err = s.Get(nil, id)
	testkit.Must(t, err)
	if got.Status != "on_you" || len(got.MessageRefs) < 6 {
		t.Fatal("follow-up did not renew with history")
	}
}
