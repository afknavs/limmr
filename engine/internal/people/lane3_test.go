package people_test

import (
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestLane3ConfirmedCircleBlocksStaleDMInference(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "recent-dm", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Synthetic trip planning", Timestamp: testkit.Epoch}))
	before, e := p.PeopleToUnderstand(nil)
	testkit.Must(t, e)
	if len(before) != 1 || before[0].ID != id {
		t.Fatal("unknown contact missing from inference queue fixture")
	}
	v := model.RelationshipInference{Label: "Workshop contact", HowKnown: "Introduced themselves in a direct message", Confidence: .9, Status: "inferred"}
	testkit.Must(t, h.Repo.Put("relationship_inferences", id, v))
	testkit.Must(t, h.Repo.Put("circles", "collective", model.Circle{ID: "collective", Name: "Workshop Collective", Confirmed: true, People: []model.CirclePerson{{PersonID: id, Role: "Coordinator"}}}))
	contacts, e := p.PeopleToUnderstand(nil)
	testkit.Must(t, e)
	for _, c := range contacts {
		if c.ID == id {
			t.Fatal("known circle member offered to DM-only model")
		}
	}
	if e = p.SaveInference(id, v); e != model.ErrConflict {
		t.Fatalf("stale inference accepted: %v", e)
	}
	if e = p.ReviewInference(id, true); e != model.ErrConflict {
		t.Fatalf("stale inference confirmed: %v", e)
	}
	profile, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if profile.Person.InferredRelationship != nil {
		t.Fatal("stored contradictory suggestion projected")
	}
	testkit.Must(t, p.ReviewInference(id, false))
	testkit.Must(t, h.Repo.Put("circles", "collective", model.Circle{ID: "collective", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Group}}}))
	if e = p.SaveInference(id, v); e != model.ErrConflict {
		t.Fatal("group membership in known circle ignored")
	}
}
func TestLane3OwnerAliasCleanupIsSafeAndIdempotent(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	testkit.Must(t, p.KeepOwner(model.Person{ID: "me", Name: "Alex Owner", PushName: "Alex", JIDs: []string{"15550100999@s.whatsapp.net"}}))
	id := model.PersonID(testkit.Phone)
	var peer model.Person
	testkit.Must(t, h.Repo.Get("people", id, &peer))
	peer.PushName = "Alex Owner"
	peer.OtherNames = []string{"  ALEX Owner ", "Alex", "Maya Studio"}
	testkit.Must(t, h.Repo.Put("people", id, peer))
	testkit.Must(t, p.CleanOwnerNames())
	testkit.Must(t, p.CleanOwnerNames())
	var got model.Person
	testkit.Must(t, h.Repo.Get("people", id, &got))
	if got.PushName != "" || len(got.OtherNames) != 1 || got.OtherNames[0] != "Maya Studio" || got.Name != peer.Name || len(got.JIDs) != len(peer.JIDs) {
		t.Fatal("cleanup lost identity or kept owner alias")
	}
	// A refresh of previously polluted old.Name must not reintroduce it as history.
	got.Name = "Alex Owner"
	testkit.Must(t, h.Repo.Put("people", id, got))
	testkit.Must(t, p.PutPerson(model.Person{ID: id, Name: "Maya Rao", JIDs: peer.JIDs}))
	testkit.Must(t, h.Repo.Get("people", id, &got))
	if len(got.OtherNames) != 1 {
		t.Fatal("identity refresh reintroduced owner alias")
	}
	all, e := hub.List[model.Person](h.Repo, "people")
	testkit.Must(t, e)
	if len(all) != 2 {
		t.Fatal("cleanup removed a person")
	}
}
func TestLane3SummaryUndoRestoresAndRejectsStaleUpdate(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	for i := 0; i < 5; i++ {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: string(rune('a' + i)), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Synthetic planning", Timestamp: testkit.Epoch}))
	}
	first, e := p.SavePersonSummary(nil, id, "Discussed the venue.", testkit.Epoch)
	testkit.Must(t, e)
	testkit.Must(t, p.UndoPersonSummary(id, first.WrittenAt))
	var absent any
	if e = h.Repo.Get("person_summaries", id, &absent); e != model.ErrNotFound {
		t.Fatal("first summary not removed")
	}
	first, e = p.SavePersonSummary(nil, id, "Discussed the venue.", testkit.Epoch)
	testkit.Must(t, e)
	h.Now = func() time.Time { return testkit.Epoch.Add(time.Hour) }
	second, e := p.SavePersonSummary(nil, id, "Confirmed the venue.", testkit.Epoch)
	testkit.Must(t, e)
	if e = p.UndoPersonSummary(id, first.WrittenAt); e != model.ErrConflict {
		t.Fatal("stale undo overwrote a newer summary")
	}
	testkit.Must(t, p.UndoPersonSummary(id, second.WrittenAt))
	var restored struct{ Text string }
	testkit.Must(t, h.Repo.Get("person_summaries", id, &restored))
	if restored.Text != first.Text {
		t.Fatal("summary not restored")
	}
}

func TestLane3SummaryUndoFollowsIdentityAlias(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	lid := "900000123@lid"
	oldID, id := model.PersonID(lid), model.PersonID(testkit.Phone)
	testkit.Must(t, p.PutPerson(model.Person{ID: oldID, JIDs: []string{lid}, Name: "Maya alias"}))
	testkit.Must(t, h.Repo.Put("person_summaries", oldID, map[string]any{"person_id": oldID, "text": "New summary", "written_at": testkit.Epoch}))
	testkit.Must(t, h.Repo.Put("person_summary_undo", oldID, map[string]any{"before": map[string]any{"person_id": oldID, "text": "Previous summary"}, "after": testkit.Epoch}))
	testkit.Must(t, p.NormalizeMapping(lid, testkit.Phone))
	testkit.Must(t, p.UndoPersonSummary(id, testkit.Epoch))
	var restored struct {
		PersonID string `json:"person_id"`
		Text     string `json:"text"`
	}
	testkit.Must(t, h.Repo.Get("person_summaries", id, &restored))
	if restored.PersonID != id || restored.Text != "Previous summary" {
		t.Fatal("summary Undo did not follow canonical identity")
	}
	var absent any
	if h.Repo.Get("person_summary_undo", oldID, &absent) != model.ErrNotFound {
		t.Fatal("alias undo record retained")
	}
}
