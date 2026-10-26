package people_test

import (
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
)

func TestZ2OtherNamesAreOwnerEditableAndSurviveReadback(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	person, err := p.UpdateProfile(nil, id, map[string]any{"other_names": []string{"Maya", "Mayu", "Maya"}})
	testkit.Must(t, err)
	if len(person.OtherNames) != 2 {
		t.Fatal("names were not saved and deduplicated")
	}
	detail, err := p.Profile(nil, id)
	testkit.Must(t, err)
	if len(detail.Person.OtherNames) != 2 {
		t.Fatal("names were lost on readback")
	}
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	projected, err := p.Profile(&a, id)
	testkit.Must(t, err)
	if len(projected.Person.OtherNames) != 0 {
		t.Fatal("other saved names leaked to a group-only AI")
	}

	_, err = p.UpdateProfile(&a, id, map[string]any{"other_names": []string{"Unconfirmed"}})
	if err != model.ErrForbidden {
		t.Fatal("AI replaced owner names")
	}
}
