package people_test

import (
	"context"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestIdentityMergePreservesReferencesAndRevert(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	lid := "900000001@lid"
	oldID := model.PersonID(lid)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, p.PutPerson(model.Person{ID: oldID, JIDs: []string{lid}, Name: "Maya alias"}))
	_, err := p.Update(nil, oldID, map[string]string{"notes": "Prefers mornings"})
	testkit.Must(t, err)
	changes, err := p.Changes()
	testkit.Must(t, err)
	edit := changes[0].ID
	_, err = p.UpdateProfile(nil, oldID, map[string]any{"details": map[string]any{"location": "Bengaluru"}})
	testkit.Must(t, err)
	f, err := p.AddFact(nil, model.Fact{Subject: "person", SubjectID: oldID, Text: "Organises sessions"})
	testkit.Must(t, err)
	rel, err := p.AddRelation(nil, model.Relation{A: "me", B: oldID, Kind: "friend"})
	testkit.Must(t, err)
	testkit.Must(t, p.PutMembership(model.Membership{Group: testkit.Group, Person: oldID, Admin: true}))
	testkit.Must(t, h.Messages.NormalizeMapping(lid, testkit.Phone))
	testkit.Must(t, p.NormalizeMapping(lid, testkit.Phone))
	testkit.Must(t, p.NormalizeMapping(lid, testkit.Phone))
	ps, err := p.People(nil, "")
	testkit.Must(t, err)
	if len(ps) != 1 || ps[0].ID != id || ps[0].Notes != "Prefers mornings" || len(ps[0].JIDs) != 2 {
		t.Fatal("identity enrichment/aliases not merged")
	}
	profile, err := p.Profile(nil, id)
	testkit.Must(t, err)
	if profile.Person.Details.Location != "Bengaluru" || len(profile.Facts) != 1 || profile.Facts[0].SubjectID != id || len(profile.Relations) != 1 || profile.Relations[0].B != id {
		t.Fatal("provenance references not migrated")
	}
	ms, err := hub.List[model.Membership](h.Repo, "memberships")
	testkit.Must(t, err)
	if len(ms) != 1 || !ms[0].Admin || ms[0].Person != id {
		t.Fatal("membership merge lost admin")
	}
	testkit.Must(t, p.Revert(edit))
	testkit.Must(t, p.Revert(f.ID))
	testkit.Must(t, p.Revert(rel.ID))
	profile, err = p.Profile(nil, id)
	testkit.Must(t, err)
	if profile.Person.Notes != "" || len(profile.Facts) != 0 || len(profile.Relations) != 0 {
		t.Fatal("merged changes cannot be reverted")
	}
}
func TestRevertPreservesRefreshAndDisjointEdits(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	_, err := p.Update(nil, id, map[string]string{"label": "Studio friend"})
	testkit.Must(t, err)
	cs, err := p.Changes()
	testkit.Must(t, err)
	edit := cs[0].ID
	_, err = p.Update(nil, id, map[string]string{"notes": "Morning sessions"})
	testkit.Must(t, err)
	testkit.Must(t, p.PutPerson(model.Person{ID: id, JIDs: []string{testkit.Phone}, Name: "Updated contact", PushName: "Maya"}))
	testkit.Must(t, p.Revert(edit))
	profile, err := p.Profile(nil, id)
	testkit.Must(t, err)
	if profile.Person.Name != "Updated contact" || profile.Person.Notes != "Morning sessions" || profile.Person.Label != "" {
		t.Fatal("revert overwrote independent changes")
	}
}
func TestStatsWindowBoundariesAndScope(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	for i, days := range []int{0, 30, 31, 365, 366} {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: string(rune('a' + i)), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Invented", Timestamp: testkit.Epoch.AddDate(0, 0, -days)}))
	}
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "group-message", ChatJID: testkit.Group, Sender: "15550100001", Content: "Invented", Timestamp: testkit.Epoch.Add(-time.Hour)}))
	pr, err := p.Profile(nil, model.PersonID(testkit.Phone))
	testkit.Must(t, err)
	if pr.Stats.DM30 != 2 || pr.Stats.DM365 != 4 || pr.Stats.GroupMessages != 1 || !pr.Stats.LastDM.Equal(testkit.Epoch) {
		t.Fatalf("wrong window counts: %+v", pr.Stats)
	}
	testkit.Must(t, p.CacheStats())
	a := model.Agent{Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	pr, err = p.Profile(&a, pr.Person.ID)
	testkit.Must(t, err)
	if pr.Stats.DM30 != 0 || pr.Stats.LastDM != nil || len(pr.Person.JIDs) != 0 || pr.Stats.GroupMessages != 1 {
		t.Fatal("DM stats escaped scope")
	}
}
func TestGroupSnapshotFailureIsAtomic(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	err := p.ReplaceGroups(context.Background(), []model.Group{{JID: "duplicate"}, {JID: "duplicate"}}, nil)
	if err == nil {
		t.Fatal("duplicate snapshot accepted")
	}
	gs, err := p.Groups(nil, "")
	testkit.Must(t, err)
	if len(gs) != 1 || gs[0].JID != testkit.Group {
		t.Fatal("old snapshot was lost")
	}
}

func TestGroupOnlyWriteReturnsProjectedProfile(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	_, err := p.Update(nil, id, map[string]string{"notes": "PRIVATE_DM_NOTE"})
	testkit.Must(t, err)
	a := model.Agent{ID: "fixture-agent", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	out, err := p.Update(&a, id, map[string]string{"role": "Organiser"})
	testkit.Must(t, err)
	if len(out.JIDs) != 0 || out.Notes != "" || out.Role != "" {
		t.Fatal("write response leaked canonical personal fields")
	}
	canonical, err := p.Profile(nil, id)
	testkit.Must(t, err)
	if canonical.Person.Role != "" || len(canonical.Person.DetailSuggestions) != 1 || canonical.Person.DetailSuggestions[0].Value != "Organiser" || canonical.Person.Notes != "PRIVATE_DM_NOTE" {
		t.Fatal("projected write damaged canonical profile")
	}
}
func TestRecentGroupMessagesAnimateMembershipEdges(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	testkit.Must(t, h.Messages.PutChat(model.Chat{JID: testkit.Group, LastMessageTime: &testkit.Epoch}))
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "recent", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Invented", Timestamp: testkit.Epoch.Add(-time.Hour)}))
	graph, err := p.Graph(nil, "", 4, 300)
	testkit.Must(t, err)
	found := false
	for _, edge := range graph.Edges {
		if edge.Kind == "member" && (edge.A == testkit.Group || edge.B == testkit.Group) {
			found = true
			if !edge.Recent {
				t.Fatal("recent message not reflected on group edge")
			}
		}
	}
	if !found {
		t.Fatal("no group edges")
	}
}

func TestGroupOnlyUnnamedPersonDoesNotRevealPhoneFallback(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, h.Repo.Put("people", id, model.Person{ID: id, JIDs: []string{testkit.Phone}, Name: "15550100001", PushName: "15550100001"}))
	a := model.Agent{Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	_, err := p.Profile(&a, id)
	if err != model.ErrNotFound {
		t.Fatal("unnamed person surfaced")
	}
}

func TestStoredPushNameResolvesLegacyNumber(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, h.Repo.Put("people", id, model.Person{ID: id, JIDs: []string{testkit.Phone}, Name: "15550100001", PushName: "Maya Rao"}))
	ps, e := p.People(nil, "")
	testkit.Must(t, e)
	if len(ps) != 1 || ps[0].Name != "Maya Rao" {
		t.Fatal("stored push name did not replace numeric fallback")
	}
}
