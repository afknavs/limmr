package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

const writingChat = "120363000061@g.us"
const statsChat = "120363000062@g.us"
const ashaJID = "15550100061@s.whatsapp.net"
const belaJID = "15550100062@s.whatsapp.net"

// Build actual owner-involved efforts, explicitly merge them, then split the
// September refs away from the November effort. No invented effort IDs/caches.
func followsFixture(t *testing.T) (*Service, Task, string, []Ref) {
	t.Helper()
	s, _, now := fixture(t)
	*now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	for _, p := range []model.Person{{ID: model.PersonID(ashaJID), Name: "Asha", JIDs: []string{ashaJID}}, {ID: model.PersonID(belaJID), Name: "Bela", JIDs: []string{belaJID}}} {
		testkit.Must(t, s.Hub.Repo.Put("people", p.ID, p))
	}
	writingRefs := []Ref{}
	for _, v := range []struct {
		chat, name, sender, topic, url string
		at                             time.Time
	}{
		{writingChat, "Writing class", ashaJID, "Writing class essay draft", "https://docs.google.com/document/d/essay", time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)},
		{statsChat, "Stats project", belaJID, "Statistics research report", "https://docs.google.com/document/d/statistics", now.Add(-24 * time.Hour)},
	} {
		testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: v.chat, Name: v.name}))
		for i, text := range []string{"Opening outline for ", "Review the layout for ", "I will review ", "Can you review "} {
			id := fmt.Sprintf("%s-%d", v.chat, i)
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: id, ChatJID: v.chat, Sender: v.sender, IsFromMe: i == 2, Content: text + v.topic + " " + v.url, Timestamp: v.at.Add(time.Duration(i) * time.Hour)}))
			if v.chat == writingChat {
				writingRefs = append(writingRefs, Ref{ChatJID: v.chat, MessageID: id})
			}
		}
	}
	es, err := s.Efforts.List(nil, efforts.Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(es) != 2 {
		t.Fatalf("fixture requires TWO real efforts, got %+v", es)
	}
	var writing, stats string
	for _, e := range es {
		if len(e.MessageRefs) != 4 || len(e.Chats) != 1 || len(e.People) != 1 {
			t.Fatalf("malformed real effort: %+v", e)
		}
		if e.Chats[0].ID == writingChat {
			writing = e.ID
		} else if e.Chats[0].ID == statsChat {
			stats = e.ID
		}
	}
	if writing == "" || stats == "" {
		t.Fatal("real efforts missing")
	}
	combined, err := s.Efforts.Regroup(nil, "merge", stats, writing, nil)
	testkit.Must(t, err)
	if len(combined.Chats) != 2 || len(combined.People) != 2 || len(combined.MessageRefs) != 8 {
		t.Fatalf("merge fixture: %+v", combined)
	}
	v, err := s.Create(nil, Create{Title: "Prepare statistics report", Kind: "long", EffortID: stats})
	testkit.Must(t, err)
	// Imported task items deliberately protect their source. Remove historical
	// imports through the owner operation to exercise an unreferenced stale member.
	for _, it := range v.Items {
		_, err = s.RemoveItem(nil, v.ID, it.ID)
		testkit.Must(t, err)
	}
	split, err := s.Efforts.Regroup(nil, "split", stats, "", writingRefs)
	testkit.Must(t, err)
	current, err := s.Efforts.Get(nil, stats)
	testkit.Must(t, err)
	if len(split.Chats) != 1 || split.Chats[0].ID != writingChat || len(current.Chats) != 1 || current.Chats[0].ID != statsChat || len(current.People) != 1 || current.People[0].ID != model.PersonID(belaJID) {
		t.Fatalf("split fixture: old=%+v current=%+v", split, current)
	}
	t.Logf("real split: Writing class=%s (%d refs), Stats project=%s (%d refs)", split.ID, len(split.MessageRefs), stats, len(current.MessageRefs))
	return s, mustRaw(t, s, v.ID).Task, split.ID, writingRefs
}

func TestR4SplitTickSilentIdempotentAndScopedUndo(t *testing.T) {
	s, v, _, _ := followsFixture(t)
	ownerPerson := model.Person{ID: "owner-friend", Name: "Cora"}
	stepPerson := model.Person{ID: "step-friend", Name: "Dara"}
	for _, p := range []model.Person{ownerPerson, stepPerson, {ID: "me", Name: "You"}} {
		testkit.Must(t, s.Hub.Repo.Put("people", p.ID, p))
	}
	_, err := s.AttachEvidence(v.ID, nil, []Entity{{ID: ownerPerson.ID}}, nil, "owner", "your choice")
	testkit.Must(t, err)
	_, err = s.AttachEvidence(v.ID, nil, []Entity{{ID: stepPerson.ID}, {ID: "me"}}, nil, "effort:"+v.EffortID, "original effort")
	testkit.Must(t, err)
	_, err = s.Checklist(nil, v.ID, "add", []Step{{Text: "Ask Dara to check the numbers"}})
	testkit.Must(t, err)
	_, err = s.AttachEvidence(v.ID, []Entity{{ID: testkit.Group}}, nil, nil, "agent:example", "agent context")
	testkit.Must(t, err)
	before := mustRaw(t, s, v.ID)
	history, err := s.History(v.ID)
	testkit.Must(t, err)
	recent, err := s.Recently(nil, "")
	testkit.Must(t, err)
	counts := finishTickCounts(t, s)
	stream, cancel := s.Hub.Subscribe()
	defer cancel()
	testkit.Must(t, s.Tick(context.Background()))
	got := mustRaw(t, s, v.ID)
	if member(got.Chats, writingChat) != nil || member(got.People, model.PersonID(ashaJID)) != nil {
		t.Fatal("stale effort members retained", got)
	}
	for _, id := range []string{statsChat, testkit.Group} {
		if member(got.Chats, id) == nil {
			t.Fatal("protected chat lost", id)
		}
	}
	for _, id := range []string{model.PersonID(belaJID), ownerPerson.ID, stepPerson.ID, "me"} {
		if member(got.People, id) == nil {
			t.Fatal("protected person lost", id)
		}
	}
	afterHistory, err := s.History(v.ID)
	testkit.Must(t, err)
	if len(afterHistory) != len(history)+1 || afterHistory[0].Text != "Removed Writing class and Asha: they belong to other work" || afterHistory[0].Kind != "rule_follows_work" {
		t.Fatal("history", afterHistory)
	}
	var change MembershipChange
	testkit.Must(t, s.Hub.Repo.Get("changes", afterHistory[0].ChangeID, &change))
	if change.Rule != "follows_work" || len(change.Before) == 0 || len(change.Chats) != 1 || len(change.People) != 1 {
		t.Fatal("missing undo/evidence", change)
	}
	var payload map[string]any
	var raw string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='rule_follows_work'", v.ID).Scan(&raw))
	testkit.Must(t, json.Unmarshal([]byte(raw), &payload))
	if payload["rule"] != "follows_work" || payload["evidence"] == nil {
		t.Fatal(raw)
	}
	afterRecent, err := s.Recently(nil, "")
	testkit.Must(t, err)
	if !reflect.DeepEqual(recent, afterRecent) || !reflect.DeepEqual(counts, finishTickCounts(t, s)) || got.NeedsYou != before.NeedsYou || got.NeedsYouReason != before.NeedsYouReason || len(stream) != 0 || len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
		t.Fatal("R4 produced attention or sent", got.NeedsYou, before.NeedsYou, len(stream))
	}
	testkit.Must(t, s.Tick(context.Background()))
	again := mustRaw(t, s, v.ID)
	if dbutil.JSON(got) != dbutil.JSON(again) {
		t.Fatal("second tick changed task")
	}
	againHistory, err := s.History(v.ID)
	testkit.Must(t, err)
	if !reflect.DeepEqual(afterHistory, againHistory) {
		t.Fatal("second tick added history")
	}
	_, err = s.Update(nil, v.ID, map[string]any{"title": "Revised statistics outcome"})
	testkit.Must(t, err)
	restored, err := s.UndoMembershipChange(nil, v.ID, change.ID)
	testkit.Must(t, err)
	if restored.Title != "Revised statistics outcome" || member(restored.Chats, writingChat) == nil || member(restored.People, model.PersonID(ashaJID)) == nil {
		t.Fatal("scoped undo", restored)
	}
	testkit.Must(t, s.Tick(context.Background()))
	restored = mustRaw(t, s, v.ID).Task
	if member(restored.Chats, writingChat) == nil || member(restored.People, model.PersonID(ashaJID)) == nil {
		t.Fatal("owner undo not sticky")
	}
}

func TestR4ReferencesProvenanceAndEligibility(t *testing.T) {
	for _, mode := range []string{"item", "subject evidence", "waiting", "check-in", "checklist ID", "substring", "owner", "agent", "merge", "detector promotion", "other effort", "hidden", "gone", "alias", "alias cycle", "suggested", "done", "dropped", "quiet"} {
		t.Run(mode, func(t *testing.T) {
			s, v, split, refs := followsFixture(t)
			r := mustRaw(t, s, v.ID)
			keep := true
			switch mode {
			case "item":
				r.Items = []Item{{ID: "historical", Kind: "note", MessageRef: &refs[0]}}
			case "subject evidence":
				r.SubjectEvidence = []Ref{refs[0]}
			case "waiting":
				r.Waiting = &Waiting{On: &Entity{ID: model.PersonID(ashaJID)}, What: "Writing class needs review", Since: s.now(), NudgeAfterDays: 10, NudgeState: "none"}
			case "check-in":
				r.CheckIn = &CheckIn{Rule: "once", To: writingChat, Note: "Ask Asha", NextAt: s.now().Add(24 * time.Hour)}
			case "checklist ID":
				r.Checklist = []Step{{ID: "ask", Text: "Ask " + model.PersonID(ashaJID) + " in " + writingChat}}
			case "substring":
				r.Checklist = []Step{{ID: "ask", Text: "Ask Ashanti about rewriting classes"}}
				keep = false
			case "owner", "agent", "merge":
				source := mode
				if mode == "agent" {
					source = "agent:example"
				}
				r.Membership.Chats[writingChat] = source
				r.Membership.People[model.PersonID(ashaJID)] = source
			case "detector promotion":
				r.Membership.Chats[writingChat] = "detector"
				r.Membership.People[model.PersonID(ashaJID)] = "detector"
				keep = false
			case "other effort":
				r.Membership.Chats[writingChat] = "effort:unrelated"
				r.Membership.People[model.PersonID(ashaJID)] = "effort:unrelated"
			case "hidden":
				set := s.Hub.Settings()
				set.HiddenChats = []string{writingChat}
				testkit.Must(t, s.Hub.SetSettings(set))
			case "gone":
				r.EffortID = "missing-without-alias"
			case "alias":
				alias := "merged-old-effort"
				testkit.Must(t, s.Hub.Repo.Put("effort_edits", alias, map[string]any{"id": alias, "replaced_by": v.EffortID}))
				r.EffortID = alias
				r.Membership.Chats[writingChat] = "effort:" + alias
				r.Membership.People[model.PersonID(ashaJID)] = "effort:" + alias
				keep = false
			case "alias cycle":
				r.EffortID = "cycle"
				testkit.Must(t, s.Hub.Repo.Put("effort_edits", "cycle", map[string]string{"replaced_by": "cycle"}))
			case "suggested", "done", "dropped":
				r.Status = mode
			case "quiet":
				r.Status = mode
				keep = false
			}
			_ = split
			_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), r.ID)
			testkit.Must(t, err)
			before := dbutil.JSON(r)
			testkit.Must(t, s.applyFollowsWork(r))
			got := mustRaw(t, s, r.ID)
			if (member(got.Chats, writingChat) != nil) != keep || (member(got.People, model.PersonID(ashaJID)) != nil) != keep {
				t.Fatalf("keep=%v got=%+v", keep, got)
			}
			if keep && before != dbutil.JSON(got) {
				t.Fatal("protected/no-op case rewrote task")
			}
		})
	}
}

func TestR4LegacyCandidatesPureAndOwnerAddHistory(t *testing.T) {
	s, v, _, _ := followsFixture(t)
	testkit.Must(t, s.Hub.Repo.Put("people", "owner-extra", model.Person{ID: "owner-extra", Name: "Cora"}))
	_, err := s.AttachEvidence(v.ID, nil, []Entity{{ID: "owner-extra"}}, nil, "owner", "your choice")
	testkit.Must(t, err)
	r := mustRaw(t, s, v.ID)
	r.Membership = nil // step 2 inference must not turn stale legacy into auto-removable.
	_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), r.ID)
	testkit.Must(t, err)
	before := dbutil.JSON(r)
	history, err := s.History(r.ID)
	testkit.Must(t, err)
	snapshot, err := s.FollowsWorkSnapshot(r.Task)
	testkit.Must(t, err)
	snapBefore := dbutil.JSON(snapshot)
	got := FollowsWorkCandidates(snapshot)
	if len(got) != 2 || got[0].Member.ID != writingChat || got[1].Member.ID != model.PersonID(ashaJID) || got[0].Why == "" {
		t.Fatal("legacy candidates", got)
	}
	if dbutil.JSON(snapshot) != snapBefore || !reflect.DeepEqual(got, FollowsWorkCandidates(snapshot)) {
		t.Fatal("candidate function changed input")
	}
	testkit.Must(t, s.applyFollowsWork(r))
	if before != dbutil.JSON(mustRaw(t, s, r.ID)) {
		t.Fatal("legacy auto-cleaned")
	}
	afterHistory, err := s.History(r.ID)
	testkit.Must(t, err)
	if !reflect.DeepEqual(history, afterHistory) {
		t.Fatal("candidate reporting wrote history")
	}
	// A reference protects legacy too; no effort_id means no candidates at all.
	snapshot.ProtectedPeople[model.PersonID(ashaJID)] = true
	if len(FollowsWorkCandidates(snapshot)) != 1 {
		t.Fatal("legacy reference ignored")
	}
	snapshot.Task.EffortID = ""
	if len(FollowsWorkCandidates(snapshot)) != 0 {
		t.Fatal("non-effort legacy listed")
	}
}
