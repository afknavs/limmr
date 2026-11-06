package tasks

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
)

func TestN1CircleSuggestionsFromChatAndPersonOwnerUndo(t *testing.T) {
	s, a, _ := fixture(t)
	for _, c := range []model.Circle{{Confirmed: true, ID: "chat-circle", Name: "Design Club", Chats: []model.CircleChat{{ChatJID: testkit.Group}}}, {Confirmed: true, ID: "person-circle", Name: "Study", People: []model.CirclePerson{{PersonID: model.PersonID(testkit.Phone)}}}} {
		testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	}
	v, err := s.Create(a, Create{Title: "Exam preparation", Kind: "long", Chats: []Entity{{ID: testkit.Group}}, People: []Entity{{ID: model.PersonID(testkit.Phone)}}})
	testkit.Must(t, err)
	if len(v.CircleIDs) != 1 || v.CircleIDs[0] != "chat-circle" || len(v.CircleSuggestions) != 1 {
		t.Fatal("linked chat must outrank weak person matches")
	}
	personTask, err := s.Create(a, Create{Title: "Personal study", Kind: "long", People: []Entity{{ID: model.PersonID(testkit.Phone)}}})
	testkit.Must(t, err)
	if len(personTask.CircleIDs) != 1 || personTask.CircleIDs[0] != "person-circle" {
		t.Fatal("missing person-only suggestion")
	}
	_, err = s.Update(nil, v.ID, map[string]any{"circle_ids": []string{}})
	testkit.Must(t, err)
	testkit.Must(t, s.BackfillCircles(context.Background()))
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(v.CircleIDs) != 0 || len(v.CircleSuggestions) != 0 {
		t.Fatal("owner undo overwritten")
	}
	listed, err := s.List(nil, "all", 500)
	testkit.Must(t, err)
	for _, row := range listed {
		if row.ID == v.ID && len(row.CircleIDs) != 0 {
			t.Fatal("read-time inference restored undone circle")
		}
	}
	_, err = s.Update(nil, v.ID, map[string]any{"circle_ids": []string{"person-circle"}})
	testkit.Must(t, err)
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(v.CircleIDs) != 1 || !v.CirclesEdited {
		t.Fatal("owner placement not saved")
	}
}
