package people_test

import (
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"sync"
	"testing"
)

func TestP8OwnerCircleIDsInvalidatesAndDoesNotShareMaps(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	ids, e := p.OwnerCircleIDs()
	testkit.Must(t, e)
	if !ids[id] {
		t.Fatal("missing named person")
	}
	delete(ids, id)
	ids, e = p.WithHub(h.WithContext(t.Context())).OwnerCircleIDs()
	testkit.Must(t, e)
	if !ids[id] {
		t.Fatal("caller changed cached IDs")
	}
	// SQL writes, not just service mutations, must invalidate the revision cache.
	_, e = h.Repo.DB.Exec("UPDATE objects SET data=json_set(data,'$.business',json('true')) WHERE bucket='people' AND id=?", id)
	testkit.Must(t, e)
	ids, e = p.OwnerCircleIDs()
	testkit.Must(t, e)
	if ids[id] {
		t.Fatal("business visibility remained cached")
	}
	testkit.Must(t, h.Repo.Put("circles", "example", model.Circle{ID: "example", Confirmed: true, People: []model.CirclePerson{{PersonID: id}}}))
	ids, e = p.OwnerCircleIDs()
	testkit.Must(t, e)
	if !ids[id] {
		t.Fatal("explicit circle membership did not invalidate cache")
	}
	testkit.Must(t, h.Repo.Apply(hub.Mutation{Bucket: "people", ID: id, Delete: true}))
	ids, e = p.OwnerCircleIDs()
	testkit.Must(t, e)
	if ids[id] {
		t.Fatal("deleted identity remained cached")
	}
}

func TestP8PeopleCacheCopiesNestedStorage(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	var person model.Person
	testkit.Must(t, h.Repo.Get("people", id, &person))
	person.Details.Phones = []model.LabeledValue{{Value: "+1 555 010 0001", Source: "you"}}
	person.DetailSuggestions = []model.DetailSuggestion{{ID: "suggestion", Value: "example"}}
	testkit.Must(t, h.Repo.Put("people", id, person))
	testkit.Must(t, h.Repo.Put("circles", "design", model.Circle{ID: "design", Name: "Design circle", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Group}}, People: []model.CirclePerson{{PersonID: id}}}))
	testkit.Must(t, h.Repo.Put("relationship_inferences", id, model.RelationshipInference{Status: "inferred", Label: "Example friend"}))
	xs, e := p.PeopleAll(nil, "", true)
	testkit.Must(t, e)
	if len(xs) != 1 || len(xs[0].CircleAffinity) == 0 || len(xs[0].GroupsInCommon) == 0 {
		t.Fatal("incomplete copy fixture")
	}
	xs[0].JIDs[0] = "changed"
	xs[0].Details.Phones[0].Value = "changed"
	xs[0].DetailSuggestions[0].Value = "changed"
	xs[0].CircleAffinity[0].Reasons[0] = "changed"
	xs[0].GroupsInCommon[0].CircleIDs[0] = "changed"
	xs[0].InferredRelationship.Label = "changed"
	again, e := p.PeopleAll(nil, "", true)
	testkit.Must(t, e)
	v := again[0]
	if v.JIDs[0] == "changed" || v.Details.Phones[0].Value == "changed" || v.DetailSuggestions[0].Value == "changed" || v.CircleAffinity[0].Reasons[0] == "changed" || v.GroupsInCommon[0].CircleIDs[0] == "changed" || v.InferredRelationship.Label == "changed" {
		t.Fatal("caller changed nested cached storage")
	}
}

func TestP8OwnerIDsConcurrentRevisionChanges(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 20; k++ {
				ids, e := p.OwnerCircleIDs()
				if e != nil {
					t.Error(e)
					return
				}
				ids["caller-only"] = true
			}
		}()
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("example-%d", i)
		testkit.Must(t, h.Repo.Put("people", id, model.Person{ID: id, Name: "Maya Example"}))
	}
	wg.Wait()
	ids, e := p.OwnerCircleIDs()
	testkit.Must(t, e)
	if len(ids) != 21 || ids["caller-only"] {
		t.Fatal("stale or shared index after writes")
	}
}
