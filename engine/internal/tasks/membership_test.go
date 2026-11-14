package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
)

func membershipChanges(t *testing.T, s *Service, id string) []MembershipChange {
	t.Helper()
	xs, err := hub.List[MembershipChange](s.Hub.Repo, "changes")
	testkit.Must(t, err)
	out := []MembershipChange{}
	for _, x := range xs {
		if x.TaskID == id {
			out = append(out, x)
		}
	}
	return out
}
func assertSource(t *testing.T, task Task, id, source string, people bool) {
	t.Helper()
	if task.Membership == nil {
		t.Fatal("missing provenance")
	}
	m := task.Membership.Chats
	if people {
		m = task.Membership.People
	}
	if m[id] != source {
		t.Fatalf("%s provenance=%q want %q", id, m[id], source)
	}
}
func TestMembershipCreationWriters(t *testing.T) {
	for _, writer := range []string{"owner", "agent", "detector", "circle_conversion"} {
		t.Run(writer, func(t *testing.T) {
			s, a, now := fixture(t)
			in := Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, People: []Entity{{ID: model.PersonID(testkit.Phone)}}}
			var task Task
			var err error
			source := writer
			switch writer {
			case "owner":
				task, err = s.Create(nil, in)
			case "agent":
				task, err = s.Create(a, in)
				source = "agent:" + a.ID
			case "detector":
				task, err = s.Suggest(nil, in, Suggestion{DetectedAt: *now})
			case "circle_conversion":
				c := model.Circle{ID: "fair", Name: "Science fair", Kind: "project", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}, People: []model.CirclePerson{{PersonID: model.PersonID(testkit.Phone)}}}
				testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
				task, err = s.ConvertCircle(nil, c.ID, in)
			}
			testkit.Must(t, err)
			assertSource(t, task, testkit.Phone, source, false)
			assertSource(t, task, model.PersonID(testkit.Phone), source, true)
			stored, err := s.raw(task.ID)
			testkit.Must(t, err)
			assertSource(t, stored.Task, testkit.Phone, source, false)
		})
	}
}
func TestMembershipEffortAndLegacyInference(t *testing.T) {
	s, a, now := fixture(t)
	for _, m := range []model.Message{
		{ID: "fair-1", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck opening slides https://notebooklm.google.com/notebook/fair", Timestamp: now.AddDate(0, 0, -5)},
		{ID: "fair-2", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "The pitch deck layout is ready https://docs.google.com/document/d/fair", Timestamp: now.AddDate(0, 0, -3)},
		{ID: "fair-3", ChatJID: testkit.Phone, IsFromMe: true, Content: "I will review the pitch deck https://github.com/example/fair", Timestamp: now.AddDate(0, 0, -2)},
		{ID: "fair-4", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Can you review the pitch deck slides? Due tomorrow. https://github.com/example/fair", Timestamp: now.Add(-time.Hour)},
	} {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	es, err := s.Efforts.List(nil, efforts.Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(es) != 1 {
		t.Fatalf("efforts=%d", len(es))
	}
	v, err := s.Create(a, Create{Title: "Prepare science fair", Kind: "long", EffortID: es[0].ID})
	testkit.Must(t, err)
	for _, x := range v.Chats {
		assertSource(t, v, x.ID, "effort:"+es[0].ID, false)
	}
	for _, x := range v.People {
		assertSource(t, v, x.ID, "effort:"+es[0].ID, true)
	}
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	r.Membership = nil
	r.People = append(r.People, Entity{ID: "mira", Name: "Mira Bell"})
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), r.ID)
	testkit.Must(t, err)
	before := dbutil.JSON(r)
	got, err := s.Get(nil, r.ID)
	testkit.Must(t, err)
	assertSource(t, got, testkit.Phone, "effort:"+es[0].ID, false)
	assertSource(t, got, "mira", "legacy", true)
	after, err := s.raw(r.ID)
	testkit.Must(t, err)
	if dbutil.JSON(after) != before {
		t.Fatal("legacy read rewrote stored JSON")
	}
	plain, err := s.Create(nil, Create{Title: "Choose stage lighting", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}, DifferentBecause: "Separate lighting only outcome"})
	testkit.Must(t, err)
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_remove(data,'$.membership_provenance') WHERE id=?", plain.ID)
	testkit.Must(t, err)
	got, err = s.Get(nil, plain.ID)
	testkit.Must(t, err)
	assertSource(t, got, testkit.Phone, "legacy", false)
}
func TestMembershipBackfillAndProjectionSources(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Suggest(a, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	testkit.Must(t, s.BackfillChatPeople(context.Background()))
	stored, err := s.raw(v.ID)
	testkit.Must(t, err)
	assertSource(t, stored.Task, model.PersonID(testkit.Phone), "backfill", true)
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	assertSource(t, v, model.PersonID(testkit.Phone), "backfill", true)
	testkit.Must(t, s.BackfillCircles(context.Background()))
	again, err := s.raw(v.ID)
	testkit.Must(t, err)
	assertSource(t, again.Task, model.PersonID(testkit.Phone), "backfill", true)
}
func TestAttachEvidenceNewOnlyAtomicHistoryAndUndo(t *testing.T) {
	s, _, now := fixture(t)
	testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", Name: "You"}))
	v, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	ref := Ref{ChatJID: testkit.Group, MessageID: "fair-ref"}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: ref.MessageID, ChatJID: ref.ChatJID, Sender: testkit.Phone, Content: "Science fair evidence", Timestamp: *now}))
	got, err := s.AttachEvidence(v.ID, []Entity{{ID: testkit.Phone}, {ID: testkit.Group}, {ID: testkit.Group}}, []Entity{{ID: model.PersonID(testkit.Phone)}}, []Ref{ref, ref}, "detector", "a matching suggestion")
	testkit.Must(t, err)
	if len(got.Chats) != 2 || len(got.EvidenceRefs) != 1 {
		t.Fatal("attachment not deduplicated")
	}
	assertSource(t, got, testkit.Phone, "owner", false)
	assertSource(t, got, testkit.Group, "detector", false)
	changes := membershipChanges(t, s, v.ID)
	if len(changes) != 1 || len(changes[0].Chats) != 1 || len(changes[0].People) != 1 || len(changes[0].AddedRefs) != 1 || changes[0].Chats[0].Entity != nil {
		t.Fatalf("bad before-state: %+v", changes)
	}
	history, err := s.History(v.ID)
	testkit.Must(t, err)
	if history[0].ChangeID != changes[0].ID || !strings.Contains(history[0].Text, "from a matching suggestion") {
		t.Fatal("missing history/change link", history)
	}
	_, err = s.AttachEvidence(v.ID, got.Chats, got.People, []Ref{ref}, "merge", "a matching suggestion")
	testkit.Must(t, err)
	if len(membershipChanges(t, s, v.ID)) != 1 {
		t.Fatal("no-op recorded another change")
	}
	// Invalid evidence must roll back membership and the event/change together.
	_, err = s.AttachEvidence(v.ID, nil, []Entity{{ID: "me"}}, []Ref{{ChatJID: testkit.Phone, MessageID: "missing"}}, "detector", "a matching suggestion")
	if err == nil {
		t.Fatal("bad evidence accepted")
	}
	r, err := s.raw(v.ID)
	testkit.Must(t, err)
	if len(r.Chats) != 2 || len(membershipChanges(t, s, v.ID)) != 1 {
		t.Fatal("partial attachment persisted")
	}
	_, err = s.Update(nil, v.ID, map[string]any{"title": "Build science fair display"})
	testkit.Must(t, err)
	*now = now.Add(29 * 24 * time.Hour)
	_, err = s.UndoMembershipChange(nil, v.ID, changes[0].ID)
	testkit.Must(t, err)
	r, err = s.raw(v.ID)
	testkit.Must(t, err)
	if len(r.Chats) != 1 || len(r.People) != 0 || len(r.EvidenceRefs) != 0 || r.Title != "Build science fair display" {
		t.Fatal("undo restored unrelated task state or missed members", r.Task)
	}
	if _, err = s.UndoMembershipChange(nil, v.ID, changes[0].ID); !errors.Is(err, model.ErrConflict) {
		t.Fatal("double undo allowed", err)
	}
}
func TestOwnerRemovalStickyAllWritersAndUndo(t *testing.T) {
	s, a, now := fixture(t)
	testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", Name: "You"}))
	person := model.PersonID(testkit.Phone)
	v, err := s.Suggest(a, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}, {ID: testkit.Group}}, People: []Entity{{ID: person, Name: "Maya Rao"}}}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	if _, err = s.RemoveMembers(a, v.ID, nil, []string{person}); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("agent removed member")
	}
	if _, err = s.RemoveMembers(nil, v.ID, []string{testkit.Group}, []string{"me"}); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("removed me")
	}
	stored, err := s.raw(v.ID)
	testkit.Must(t, err)
	if len(stored.Chats) != 2 {
		t.Fatal("me rejection partially removed chats")
	}
	got, err := s.RemoveMembers(nil, v.ID, []string{testkit.Group}, []string{person})
	testkit.Must(t, err)
	if member(got.People, person) != nil || member(got.Chats, testkit.Group) != nil {
		t.Fatal("removed row still projected")
	}
	changes := membershipChanges(t, s, v.ID)
	if len(changes) != 1 || changes[0].Kind != "owner_remove" || changes[0].People[0].Entity == nil || changes[0].People[0].Source != "agent:"+a.ID {
		t.Fatal("removal before-state missing", changes)
	}
	testkit.Must(t, s.BackfillChatPeople(context.Background()))
	testkit.Must(t, s.BackfillCircles(context.Background()))
	_, err = s.AttachEvidence(v.ID, []Entity{{ID: testkit.Group}}, []Entity{{ID: person}}, nil, "effort:fair", "the fair effort")
	testkit.Must(t, err)
	src, err := s.Create(nil, Create{Title: "Choose exhibit furniture", Kind: "long", Chats: []Entity{{ID: testkit.Group}}, People: []Entity{{ID: person}}, DifferentBecause: "Separate furniture selection work"})
	testkit.Must(t, err)
	_, err = s.Merge(nil, v.ID, src.ID)
	testkit.Must(t, err)
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if member(got.People, person) != nil || member(got.Chats, testkit.Group) != nil {
		t.Fatal("writer resurrected removed member")
	}
	if len(membershipChanges(t, s, v.ID)) != 1 {
		t.Fatal("sticky attachment created a change")
	}
	if _, err = s.UndoMembershipChange(a, v.ID, changes[0].ID); !errors.Is(err, model.ErrForbidden) {
		t.Fatal("agent undid owner removal")
	}
	// Unrelated membership added after removal must survive its undo.
	_, err = s.AttachEvidence(v.ID, nil, []Entity{{ID: "me"}}, nil, "owner", "the owner request")
	testkit.Must(t, err)
	_, err = s.UndoMembershipChange(nil, v.ID, changes[0].ID)
	testkit.Must(t, err)
	got, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if member(got.People, person) == nil || member(got.People, "me") == nil || member(got.Chats, testkit.Group) == nil {
		t.Fatal("undo did not restore scoped members")
	}
	assertSource(t, got, person, "agent:"+a.ID, true)
	if model.Contains(got.RemovedMembers.People, person) || model.Contains(got.RemovedMembers.Chats, testkit.Group) {
		t.Fatal("undo left tombstones")
	}
	history, err := s.History(v.ID)
	testkit.Must(t, err)
	if !strings.HasPrefix(history[0].Text, "Undone: Removed") {
		t.Fatal("undo history missing", history[0])
	}
}
func TestOwnerRemovalProjectionOnlyAndUndoExpiry(t *testing.T) {
	for _, days := range []int{29, 30, 31} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			s, _, now := fixture(t)
			person := model.PersonID(testkit.Phone)
			v, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
			testkit.Must(t, err)
			got, err := s.Get(nil, v.ID)
			testkit.Must(t, err)
			if member(got.People, person) == nil {
				t.Fatal("missing projection fixture")
			}
			_, err = s.RemoveMembers(nil, v.ID, nil, []string{person})
			testkit.Must(t, err)
			changes := membershipChanges(t, s, v.ID)
			if len(changes) != 1 || changes[0].People[0].Entity != nil {
				t.Fatal("projection-only before state incorrect")
			}
			*now = now.Add(time.Duration(days) * 24 * time.Hour)
			_, err = s.UndoMembershipChange(nil, v.ID, changes[0].ID)
			if days < 30 {
				testkit.Must(t, err)
			} else if !errors.Is(err, model.ErrConflict) {
				t.Fatal("expired undo accepted", err)
			}
			got, err = s.Get(nil, v.ID)
			testkit.Must(t, err)
			if (member(got.People, person) != nil) != (days < 30) {
				t.Fatal("projection resurrected removed participant or failed undo")
			}
		})
	}
}
func TestMergeAndAttachSourceVocabulary(t *testing.T) {
	s, _, _ := fixture(t)
	dst, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long"})
	testkit.Must(t, err)
	src, err := s.Create(nil, Create{Title: "Choose exhibit furniture", Kind: "long", Chats: []Entity{{ID: testkit.Group}}, People: []Entity{{ID: model.PersonID(testkit.Phone)}}})
	testkit.Must(t, err)
	got, err := s.Merge(nil, dst.ID, src.ID)
	testkit.Must(t, err)
	assertSource(t, got, testkit.Group, "merge", false)
	assertSource(t, got, model.PersonID(testkit.Phone), "merge", true)
	for _, source := range []string{"owner", "agent:example", "effort:fair", "detector", "merge", "circle_conversion", "backfill", "follows", "legacy"} {
		t.Run(source, func(t *testing.T) {
			v, err := s.Create(nil, Create{Title: "Stage lighting plan", Kind: "long"})
			testkit.Must(t, err)
			got, err := s.AttachEvidence(v.ID, []Entity{{ID: testkit.Phone}}, nil, nil, source, "the linked work")
			testkit.Must(t, err)
			assertSource(t, got, testkit.Phone, source, false)
		})
	}
}
