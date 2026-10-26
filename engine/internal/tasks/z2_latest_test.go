package tasks

import (
	"context"
	"errors"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"testing"
	"time"
)

func TestZ2LatestOrderingNamesMediaAndPrivacy(t *testing.T) {
	s, a, now := fixture(t)
	v, err := s.Create(a, Create{Title: "Review slides", Kind: "long", Chats: []Entity{{ID: testkit.Phone}, {ID: testkit.Group}}})
	testkit.Must(t, err)
	p := model.Person{ID: model.PersonID(testkit.Phone), JIDs: []string{testkit.Phone}, Label: "Maya saved", Name: "Maya Rao", PushName: "Mayu"}
	testkit.Must(t, s.Hub.Repo.Put("people", p.ID, p))
	for i, m := range []model.Message{
		{ID: "old", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Old"},
		{ID: "a", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Review the deck"},
		{ID: "b", ChatJID: testkit.Phone, IsFromMe: true, Content: "I will review it"},
		{ID: "c", ChatJID: testkit.Group, Sender: "15550100999@s.whatsapp.net", MediaType: "sticker"},
	} {
		m.Timestamp = now.Add(time.Duration(i+1) * time.Minute)
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(v.Latest) != 3 || v.Latest[0].MessageID != "c" || v.Latest[1].MessageID != "b" || v.Latest[2].MessageID != "a" {
		t.Fatal("latest must be three newest messages across all linked chats")
	}
	if v.Latest[0].Sender != "Someone in the chat" || v.Latest[0].Text != "Sticker" || v.Latest[1].Sender != "You" || v.Latest[2].Sender != "Maya saved" || v.Latest[2].Chat == "" {
		t.Fatal("missing names, owner or media label")
	}
	for _, tc := range []struct{ label, name, push, want string }{
		{"", "Maya Rao", "Mayu", "Maya Rao"}, {"", "", "Mayu", "Mayu"}, {"+1 (555) 0100", "15550100100", "+15550100100", "Someone in the chat"},
	} {
		p.Label = tc.label
		p.Name = tc.name
		p.PushName = tc.push
		testkit.Must(t, s.Hub.Repo.Put("people", p.ID, p))
		v, err = s.Get(nil, v.ID)
		testkit.Must(t, err)
		if v.Latest[2].Sender != tc.want {
			t.Fatalf("wrong display-name precedence: %q", v.Latest[2].Sender)
		}
	}
	private, err := s.Get(a, v.ID)
	testkit.Must(t, err)
	if len(private.Latest) != 0 {
		t.Fatal("latest leaked into agent response")
	}
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{testkit.Group}
	testkit.Must(t, s.Hub.SetSettings(settings))
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	for _, m := range v.Latest {
		if m.ChatJID == testkit.Group {
			t.Fatal("hidden chat leaked")
		}
	}
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, s.Hub.SetSettings(settings))
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	if len(v.Latest) != 1 || v.Latest[0].MessageID != "c" {
		t.Fatal("hidden person's DM or group messages leaked")
	}
}

func TestZ2SummaryAttemptIsPersistentConditionalAndAttributed(t *testing.T) {
	s, a, now := fixture(t)
	v := create(t, s, a, "long")
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Please review the deck", Timestamp: now.Add(time.Hour)}))
	v, err := s.Get(nil, v.ID)
	testkit.Must(t, err)
	m := v.Latest[0]
	attempt := SummaryAttempt{ChatJID: m.ChatJID, MessageID: m.MessageID, Timestamp: m.Timestamp}
	fields := map[string]any{"summary_attempt": attempt}
	_, err = s.Update(a, v.ID, fields)
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatal("agent claimed a local writer attempt")
	}
	_, err = s.Update(nil, v.ID, fields)
	testkit.Must(t, err)
	restarted := New(s.Hub, s.Efforts)
	_, err = restarted.Update(nil, v.ID, fields)
	if !errors.Is(err, model.ErrConflict) {
		t.Fatal("repeat claim allowed after restart")
	}
	fields["summary"] = Summary{Text: "Maya needs a deck review. You will send notes.", UpdatedBy: "This Mac"}
	saved, err := restarted.Update(nil, v.ID, fields)
	testkit.Must(t, err)
	if saved.Summary.UpdatedBy != "This Mac" {
		t.Fatal("lost local writer credit")
	}
	_, err = restarted.Update(nil, v.ID, fields)
	if !errors.Is(err, model.ErrConflict) {
		t.Fatal("overwrote a supplied summary")
	}
	// An empty/failed result can only be attempted again after a newer message.
	_, err = s.Update(nil, v.ID, map[string]any{"summary": Summary{}})
	testkit.Must(t, err)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "newer", ChatJID: testkit.Phone, Content: "Deck updated", Timestamp: now.Add(2 * time.Hour)}))
	_, err = s.Update(nil, v.ID, map[string]any{"summary_attempt": attempt})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale input accepted")
	}
	v, err = s.Get(nil, v.ID)
	testkit.Must(t, err)
	m = v.Latest[0]
	attempt = SummaryAttempt{ChatJID: m.ChatJID, MessageID: m.MessageID, Timestamp: m.Timestamp}
	_, err = s.Update(nil, v.ID, map[string]any{"summary_attempt": attempt})
	testkit.Must(t, err)
	_, err = s.Update(nil, v.ID, map[string]any{"summary": Summary{Text: "Owner wrote this"}})
	testkit.Must(t, err)
	_, err = s.Update(nil, v.ID, map[string]any{"summary_attempt": attempt, "summary": Summary{Text: "Late model", UpdatedBy: "This Mac"}})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatal("late model overwrote owner")
	}
}

func TestZ2SurfacingLinksKnownChatPeople(t *testing.T) {
	for _, tc := range []struct {
		name        string
		group       bool
		count, want int
	}{{"DM", false, 1, 1}, {"small group", true, 3, 3}, {"big group", true, 4, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			s, a, now := fixture(t)
			a.Preset = "own"
			testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
			jid := "15550100333@s.whatsapp.net"
			if tc.group {
				jid = "120363003333@g.us"
			}
			for i := 0; i < tc.count; i++ {
				id := fmt.Sprintf("known-%d", i)
				j := fmt.Sprintf("1555010033%d@s.whatsapp.net", i)
				if !tc.group {
					j = jid
				}
				testkit.Must(t, s.Hub.Repo.Put("people", id, model.Person{ID: id, JIDs: []string{j}, Name: fmt.Sprintf("Maya %c", 'A'+i)}))
				if tc.group {
					testkit.Must(t, s.Hub.Repo.Put("memberships", id, model.Membership{Group: jid, Person: id}))
				}
			}
			testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", Name: "You", JIDs: []string{"15550100000@s.whatsapp.net"}}))
			testkit.Must(t, s.Hub.Repo.Put("memberships", "owner", model.Membership{Group: jid, Person: "me"}))
			testkit.Must(t, s.Hub.Repo.Put("memberships", "unknown", model.Membership{Group: jid, Person: "unknown-number"}))
			surfacingChat(t, s, a, *now, jid, "Design Club 2026", "CV", tc.group, tc.count+2, true)
			testkit.Must(t, s.Tick(context.Background()))
			xs, err := s.List(nil, "all", 100)
			testkit.Must(t, err)
			if len(xs) != 1 {
				t.Fatalf("want a surfaced task, got %d", len(xs))
			}
			v, err := s.Get(nil, xs[0].ID)
			testkit.Must(t, err)
			if len(v.People) != tc.want {
				t.Fatalf("got %d people want %d", len(v.People), tc.want)
			}
			for _, p := range v.People {
				if p.ID == "me" || p.ID == "unknown-number" {
					t.Fatal("linked owner or unknown")
				}
			}
		})
	}
}

func TestZ2ChatPeopleBackfillIsScopedAndIdempotent(t *testing.T) {
	s, a, _ := fixture(t)
	for _, tc := range []struct {
		status, origin                string
		hasSuggestion, existing, want bool
	}{{"active", "agent", true, false, true}, {"suggested", "agent", true, false, true}, {"done", "agent", true, false, false}, {"active", "owner", true, false, false}, {"active", "agent", false, false, false}, {"active", "agent", true, true, false}} {
		v := create(t, s, a, "long")
		r, err := s.raw(v.ID)
		testkit.Must(t, err)
		r.Status = tc.status
		r.Origin = tc.origin
		r.People = nil
		if tc.existing {
			r.People = []Entity{{ID: "already", Name: "Robin Bell"}}
		}
		if tc.hasSuggestion {
			r.Suggestion = &Suggestion{}
		}
		_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), r.ID)
		testkit.Must(t, err)
		testkit.Must(t, s.BackfillChatPeople(context.Background()))
		testkit.Must(t, s.BackfillChatPeople(context.Background()))
		v, err = s.Get(nil, v.ID)
		testkit.Must(t, err)
		events := 0
		for _, e := range v.History {
			if e.Text == "Linked people from the chat" {
				events++
			}
		}
		want := 0
		if tc.want {
			want = 1
		}
		if events != want {
			t.Fatalf("backfill events=%d want %d", events, want)
		}
		if tc.want && (len(v.People) != 1 || v.People[0].ID != model.PersonID(testkit.Phone)) {
			t.Fatal("missing backfilled DM person")
		}
	}
}
