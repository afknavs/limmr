package integration_test

import (
	"context"
	"errors"
	"limmr/internal/model"
	"limmr/internal/wa"
	"strings"
	"testing"
	"time"
)

func TestPeopleStatsAndScope(t *testing.T) {
	r := newRig(t)
	profile, e := r.p.Profile(nil, "maya")
	must(t, e)
	if profile.Stats.DM30 != 3 || profile.Stats.DM365 != 3 || profile.Stats.GroupMessages != 3 || len(profile.Stats.SharedGroups) != 1 {
		t.Fatalf("wrong stats %+v", profile.Stats)
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{groupA}}
	r.agent(t, p)
	profile, e = r.p.Profile(&r.a, "maya")
	must(t, e)
	if len(profile.Person.JIDs) != 0 || profile.Stats.DM30 != 0 || profile.Stats.DM365 != 0 || profile.Stats.LastDM != nil {
		t.Fatal("DM metadata leaked through group visibility")
	}
	if profile.Stats.GroupMessages != 3 {
		t.Fatal("scoped group stats lost")
	}
	must(t, r.p.CacheStats())
}
func TestFactsEvidenceAndRevert(t *testing.T) {
	r := newRig(t)
	message := firstMessage(t, r, groupA)
	f, e := r.p.AddFact(&r.a, model.Fact{Subject: "person", SubjectID: "maya", Text: "Organises sessions", EvidenceMessageIDs: []string{message.ID}})
	must(t, e)
	if f.Source != r.a.ID || f.Status != "active" {
		t.Fatal("fact attribution")
	}
	profile, e := r.p.Profile(&r.a, "maya")
	must(t, e)
	if len(profile.Facts) != 1 {
		t.Fatal("fact not visible")
	}
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	profile, e = r.p.Profile(&r.a, "maya")
	must(t, e)
	if len(profile.Facts) != 0 {
		t.Fatal("excluded evidence fact leaked")
	}
	must(t, r.p.Revert(f.ID))
	profile, e = r.p.Profile(nil, "maya")
	must(t, e)
	if len(profile.Facts) != 0 {
		t.Fatal("reverted fact still active")
	}
	if e = r.p.Revert(f.ID); e != model.ErrConflict {
		t.Fatal("duplicate revert")
	}
}
func TestFactEvidenceRejectsHiddenAndAmbiguousIDs(t *testing.T) {
	for _, kind := range []string{"hidden", "ambiguous", "missing"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			message := firstMessage(t, r, groupB)
			id := message.ID
			if kind == "hidden" {
				p := r.a.Permissions
				p.Chats = model.Scope{Mode: "all_except", JIDs: []string{groupB}}
				r.agent(t, p)
			} else if kind == "ambiguous" {
				message.ChatJID = chatA
				must(t, r.m.PutMessage(message))
			} else {
				id = "missing-id"
			}
			_, e := r.p.AddFact(&r.a, model.Fact{Subject: "person", SubjectID: "maya", Text: "Fact", EvidenceMessageIDs: []string{id}})
			if e != model.ErrNotFound {
				t.Fatal(e)
			}
		})
	}
}
func TestPersonChangeRevertAndEnrichmentPreservation(t *testing.T) {
	r := newRig(t)
	_, e := r.p.Update(&r.a, "maya", map[string]string{"label": "Studio friend", "circle": "Design"})
	must(t, e)
	changes, e := r.p.Changes()
	must(t, e)
	if len(changes) != 1 || changes[0].Source != r.a.ID {
		t.Fatal("change attribution")
	}
	must(t, r.p.Revert(changes[0].ID))
	profile, e := r.p.Profile(nil, "maya")
	must(t, e)
	if profile.Person.Label != "" {
		t.Fatal("person revert failed")
	}
	_, e = r.p.Update(&r.a, "maya", map[string]string{"notes": "Prefers mornings"})
	must(t, e)
	must(t, r.p.PutPerson(model.Person{ID: "maya", JIDs: []string{chatA}, Name: "Maya Updated", UpdatedAt: epoch}))
	profile, e = r.p.Profile(nil, "maya")
	must(t, e)
	if profile.Person.Notes != "" || len(profile.Person.DetailSuggestions) != 1 || profile.Person.DetailSuggestions[0].Field != "notes" || profile.Person.DetailSuggestions[0].Value != "Prefers mornings" {
		t.Fatal("directory refresh clobbered the owner-review note suggestion")
	}
}
func TestPersonRevertDoesNotClobberLaterEdit(t *testing.T) {
	r := newRig(t)
	_, e := r.p.Update(&r.a, "maya", map[string]string{"label": "First"})
	must(t, e)
	xs, e := r.p.Changes()
	must(t, e)
	first := xs[0].ID
	_, e = r.p.Update(&r.a, "maya", map[string]string{"label": "Second"})
	must(t, e)
	if e = r.p.Revert(first); e != model.ErrConflict {
		t.Fatal("revert clobbered newer edit")
	}
}
func TestRelationsAndGraphLimits(t *testing.T) {
	r := newRig(t)
	rel, e := r.p.AddRelation(&r.a, model.Relation{A: "me", B: "maya", Kind: "friend"})
	must(t, e)
	for _, limit := range []int{1, 2, 3, 100} {
		t.Run(string(rune('a'+limit%26)), func(t *testing.T) {
			g, e := r.p.Graph(&r.a, "maya", 4, limit)
			must(t, e)
			if len(g.Nodes) > limit || len(g.Nodes) == 0 || g.Nodes[0].ID != "maya" {
				t.Fatal("graph focus or limit")
			}
			seen := map[string]bool{}
			for _, n := range g.Nodes {
				seen[n.ID] = true
			}
			for _, edge := range g.Edges {
				if !seen[edge.A] || !seen[edge.B] {
					t.Fatal("dangling graph edge")
				}
			}
		})
	}
	must(t, r.p.Revert(rel.ID))
	rels, e := r.p.Relations(&r.a)
	must(t, e)
	if len(rels) != 0 {
		t.Fatal("reverted relation")
	}
}
func TestMembershipRefreshRemovesOldMembers(t *testing.T) {
	r := newRig(t)
	must(t, r.p.ReplaceGroups(context.Background(), []model.Group{{JID: groupA, Name: "Design Club", MemberCount: 1}}, []model.Membership{{Group: groupA, Person: "alex"}}))
	group, e := r.p.Group(nil, groupA)
	must(t, e)
	raw := encode(group)
	if !strings.Contains(raw, "Alex Chen") || strings.Contains(raw, "Maya Rao") {
		t.Fatal("stale membership survived snapshot")
	}
	gs, e := r.p.Groups(nil, "")
	must(t, e)
	if len(gs) != 1 {
		t.Fatal("stale group survived snapshot")
	}
}
func TestLinkStateMachineAndRotatingQR(t *testing.T) {
	src := &wa.Fake{Events: []wa.LinkEvent{{State: "qr", Code: "first", ExpiresIn: 60}, {State: "qr", Code: "second", ExpiresIn: 20}, {State: "syncing", Progress: 100, Chats: 2, Messages: 10}, {State: "ready", Account: &wa.Account{Name: "Demo account", PhoneMasked: "•••• 00"}}}}
	l := wa.NewLink(src)
	ch, done := l.Subscribe()
	defer done()
	must(t, l.Start(context.Background(), "qr", ""))
	codes := []string{}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-ch:
			if e.State == "qr" {
				codes = append(codes, e.Code)
			}
			if e.State == "ready" {
				if strings.Join(codes, ",") != "first,second" {
					t.Fatal("QR rotation lost")
				}
				if l.State().State != "ready" {
					t.Fatal("ready state lost")
				}
				return
			}
		case <-timer.C:
			t.Fatal("link did not finish")
		}
	}
}
func TestLinkCancelAndPhone(t *testing.T) {
	r := newRig(t)
	if e := r.link.Start(context.Background(), "phone", ""); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("accepted no phone")
	}
	ch, done := r.link.Subscribe()
	defer done()
	must(t, r.link.Start(context.Background(), "phone", "15550100001"))
	deadline := time.After(3 * time.Second)
	seen := false
	for !seen {
		select {
		case e := <-ch:
			seen = e.State == "pair_code"
		case <-deadline:
			t.Fatal("missing phone pair code")
		}
	}
	if e := r.link.Unlink(context.Background(), false); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("unlink lacked confirmation")
	}
	must(t, r.link.Unlink(context.Background(), true))
	if r.link.State().State != "unlinked" {
		t.Fatal("cancel state")
	}
}

type slowSource struct{}

func (slowSource) Start(ctx context.Context, m, p string, emit func(wa.LinkEvent)) error {
	<-ctx.Done()
	emit(wa.LinkEvent{State: "ready"})
	return ctx.Err()
}
func (slowSource) Cancel()                      {}
func (slowSource) Unlink(context.Context) error { return nil }
func TestCancelledPairingCannotPublishReady(t *testing.T) {
	l := wa.NewLink(slowSource{})
	must(t, l.Start(context.Background(), "qr", ""))
	if e := l.Start(context.Background(), "qr", ""); e != nil {
		t.Fatal("repeating the running pairing should be a no-op, got", e)
	}
	if e := l.Start(context.Background(), "phone", "15550100"); e != model.ErrConflict {
		t.Fatal("parallel pair with another method allowed")
	}
	l.Cancel()
	if l.State().State != "unlinked" {
		t.Fatal("stale ready after cancellation")
	}
}
