package people_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"limmr/internal/model"
	"limmr/internal/people"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func TestY45CustomContactsAndWhatsAppPhotoPrecedence(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	testkit.Must(t, p.ImportContacts([]people.ContactMatch{{PersonID: id, Photo: jpegFixture(t)}}))
	contact, e := cache.Get(context.Background(), id)
	testkit.Must(t, e)
	if len(fake.calls) != 0 {
		t.Fatal("Contacts photo made a WhatsApp request")
	}
	testkit.Must(t, p.SetPhoto(id, jpegFixture(t)))
	var canonical model.Person
	testkit.Must(t, h.Repo.Get("people", id, &canonical))
	if canonical.Photo.Source != "custom" || len(canonical.Photo.SHA256) != 64 {
		t.Fatal("missing digest reference")
	}
	config, e := jpeg.DecodeConfig(bytes.NewReader(contact))
	testkit.Must(t, e)
	if config.Width != 512 || config.Height != 512 {
		t.Fatal("photo not normalized")
	}
	testkit.Must(t, p.SetPhoto(id, nil))
	testkit.Must(t, h.Repo.Get("people", id, &canonical))
	if canonical.Photo.Source != "contacts" {
		t.Fatal("delete lost Contacts fallback")
	}
	testkit.Must(t, p.ImportContacts(nil))
	data, e := cache.Get(context.Background(), id)
	testkit.Must(t, e)
	if !bytes.Equal(data, fake.data) || len(fake.calls) != 1 {
		t.Fatal("WhatsApp fallback")
	}
	testkit.Must(t, p.SetPhoto(id, jpegFixture(t)))
	demo := &people.AvatarCache{Hub: h}
	if _, e = demo.Get(context.Background(), id); e != nil {
		t.Fatal("custom photo should work without network lookup")
	}
	if e = p.SetPhoto(id, make([]byte, people.MaxAvatarBytes+1)); e != model.ErrInvalid {
		t.Fatal("accepted oversize photo")
	}
	if e = p.SetPhoto(id, []byte("not an image")); e != model.ErrInvalid {
		t.Fatal("accepted invalid photo")
	}
}
func TestY45GroupPhotosShareThrottleAndDemoNeverLooksUp(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	fake := &avatarFake{data: jpegFixture(t)}
	cache := &people.AvatarCache{Hub: h, Lookup: fake}
	_, e := cache.Get(context.Background(), testkit.Group)
	testkit.Must(t, e)
	if len(fake.calls) != 1 || fake.calls[0] != testkit.Group {
		t.Fatal("group photo not queried")
	}
	_, e = cache.Get(context.Background(), model.PersonID(testkit.Phone))
	if e != model.ErrNotFound || len(fake.calls) != 1 {
		t.Fatal("group did not share person throttle")
	}
	if _, e = (&people.AvatarCache{Hub: h}).Get(context.Background(), testkit.Group); e != model.ErrNotFound {
		t.Fatal("demo read remote cache")
	}
	h.Now = func() time.Time { return testkit.Epoch.Add(7 * 24 * time.Hour) }
	_, e = cache.Get(context.Background(), testkit.Group)
	testkit.Must(t, e)
	if len(fake.calls) != 2 {
		t.Fatal("group weekly refresh")
	}
}
func TestY45TwoCommonWordsAndSharedPeersDoNotConnectNonMember(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, h.Repo.Put("circles", "design", model.Circle{ID: "design", Name: "Design studio", Description: "material samples", Confirmed: true, People: []model.CirclePerson{{PersonID: "peer", Source: "you"}}}))
	testkit.Must(t, p.PutMembership(model.Membership{Group: testkit.Group, Person: "peer"}))
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "overlap", ChatJID: testkit.Phone, Content: "material samples", Timestamp: testkit.Epoch}))
	pr, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if len(pr.Person.CircleAffinity) != 0 || pr.Person.PrimaryCircleID != "" {
		t.Fatal("words/peers created a connection")
	}
	var c model.Circle
	testkit.Must(t, h.Repo.Get("circles", "design", &c))
	c.Chats = []model.CircleChat{{ChatJID: testkit.Group}}
	testkit.Must(t, h.Repo.Put("circles", c.ID, c))
	pr, e = p.Profile(nil, id)
	testkit.Must(t, e)
	if len(pr.Person.CircleAffinity) != 1 || len(pr.Person.GroupsInCommon[0].CircleIDs) != 1 {
		t.Fatal("structural group link missing")
	}
}
func TestY45NotesAreReviewableAndLegacyMigrationIsReversible(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	a, _, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	_, e = p.Update(nil, id, map[string]string{"notes": "Prefers mornings"})
	testkit.Must(t, e)
	pr, e := p.Update(&a, id, map[string]string{"notes": "Bring material samples"})
	testkit.Must(t, e)
	if pr.Notes != "Prefers mornings" || len(pr.DetailSuggestions) != 1 || pr.DetailSuggestions[0].Field != "notes" {
		t.Fatal("agent overwrote notes")
	}
	_, e = p.ReviewDetail(id, pr.DetailSuggestions[0].ID, true)
	testkit.Must(t, e)
	pr, e = p.Update(&a, id, map[string]string{"notes": "Maya is female"})
	testkit.Must(t, e)
	if pr.Details.Pronouns != "" || pr.DetailSuggestions[0].Value != "she/her" {
		t.Fatal("gender not a pronouns suggestion")
	}
	var before model.Person
	testkit.Must(t, h.Repo.Get("people", id, &before))
	legacy := before
	legacy.Notes = "Maya is male"
	testkit.Must(t, h.Repo.Put("people", id, legacy))
	testkit.Must(t, h.Repo.Put("changes", "legacy", people.Change{ID: "legacy", At: testkit.Epoch.Add(time.Second), Source: a.ID, Kind: "person", SubjectID: id, Before: &before, After: &legacy}))
	testkit.Must(t, p.MigrateAgentNotes())
	testkit.Must(t, p.MigrateAgentNotes())
	var after model.Person
	testkit.Must(t, h.Repo.Get("people", id, &after))
	if after.Notes != before.Notes {
		t.Fatal("migration did not restore previous note")
	}
	changes, e := p.Changes()
	testkit.Must(t, e)
	migration := ""
	for _, c := range changes {
		if c.Source == "you" && c.Before != nil && c.Before.Notes == legacy.Notes {
			migration = c.ID
		}
	}
	if migration == "" {
		t.Fatal("migration not recorded")
	}
	testkit.Must(t, p.Revert(migration))
	testkit.Must(t, h.Repo.Get("people", id, &after))
	if after.Notes != legacy.Notes {
		t.Fatal("migration not reversible")
	}
}
func TestY45PersonSummariesRequireFiveRecentMessagesAndBoundText(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	if _, e := p.SavePersonSummary(nil, id, "A quiet month.", testkit.Epoch); e != model.ErrInvalid {
		t.Fatal("low message count accepted")
	}
	for i := 0; i < 5; i++ {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprint("recent-", i), ChatJID: testkit.Phone, Timestamp: testkit.Epoch.Add(-time.Duration(i) * time.Hour), Content: "Studio review"}))
	}
	v, e := p.SavePersonSummary(nil, id, "You discussed the studio review. Next comes the material shortlist.", testkit.Epoch)
	testkit.Must(t, e)
	if v.WrittenBy != "This Mac" || v.PersonID != id {
		t.Fatal("summary provenance")
	}
	if _, e = p.SavePersonSummary(nil, id, strings.Repeat("界", 601), testkit.Epoch); e != model.ErrInvalid {
		t.Fatal("long summary accepted")
	}
	a, _, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	a.Permissions.SaveSummaries = false
	if _, e = p.SavePersonSummary(&a, id, "No permission.", testkit.Epoch); e != model.ErrForbidden {
		t.Fatal("summary permission bypass")
	}
}
func TestY45EmailsOwnerContactsAndAISurviveExportAndMerge(t *testing.T) {
	for _, source := range []string{"owner", "contacts", "ai-confirm", "ai-dismiss"} {
		t.Run(source, func(t *testing.T) {
			h, p, _ := testkit.Open(t)
			testkit.Seed(t, h, p)
			lid := "900000099@lid"
			id := model.PersonID(lid)
			testkit.Must(t, p.PutPerson(model.Person{ID: id, JIDs: []string{lid}, Name: "Maya alias"}))
			testkit.Must(t, h.Messages.PutChat(model.Chat{JID: lid, Name: "Maya alias"}))
			email := source + "@example.com"
			switch source {
			case "contacts":
				testkit.Must(t, p.ImportContacts([]people.ContactMatch{{PersonID: id, Emails: []model.LabeledValue{{Label: "work", Value: email}}, Birthday: "04-12", Org: "Sample Studio", Title: "Designer"}}))
			case "owner":
				_, e := p.UpdateProfile(nil, id, map[string]any{"details": map[string]any{"emails": []model.LabeledValue{{Label: "work", Value: email}}}})
				testkit.Must(t, e)
			default:
				a, _, e := h.CreateAgent("Fixture AI", "codex", "local")
				testkit.Must(t, e)
				pr, e := p.UpdateProfile(&a, id, map[string]any{"details": map[string]any{"emails": []model.LabeledValue{{Label: "work", Value: email}}}})
				testkit.Must(t, e)
				_, e = p.ReviewDetail(id, pr.DetailSuggestions[0].ID, source == "ai-confirm")
				testkit.Must(t, e)
			}
			testkit.Must(t, p.NormalizeMapping(lid, testkit.Phone))
			target := model.PersonID(testkit.Phone)
			pr, e := p.Profile(nil, target)
			testkit.Must(t, e)
			want := source != "ai-dismiss"
			found := false
			for _, v := range pr.Person.Details.Emails {
				found = found || v.Value == email
			}
			if found != want {
				t.Fatal("merge lost confirmed email or retained dismissed email")
			}
			export, e := p.Export(nil)
			testkit.Must(t, e)
			raw, e := json.Marshal(export)
			testkit.Must(t, e)
			if strings.Contains(string(raw), email) != want {
				t.Fatal("export email mismatch")
			}
			// A normal JSON person-record roundtrip is the sync storage boundary.
			raw, e = json.Marshal(pr.Person)
			testkit.Must(t, e)
			var replica model.Person
			testkit.Must(t, json.Unmarshal(raw, &replica))
			if len(replica.Details.Emails) != len(pr.Person.Details.Emails) {
				t.Fatal("record roundtrip lost email")
			}
		})
	}
}
func TestY45ContactsRemovalKeepsOwnerValues(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	_, e := p.UpdateProfile(nil, id, map[string]any{"details": map[string]any{"emails": []model.LabeledValue{{Label: "personal", Value: "owner@example.com"}}, "birthday": "01-02"}})
	testkit.Must(t, e)
	testkit.Must(t, p.ImportContacts([]people.ContactMatch{{PersonID: id, Emails: []model.LabeledValue{{Label: "work", Value: "contact@example.com"}}, Birthday: "04-12", Org: "Sample Studio"}}))
	testkit.Must(t, p.ImportContacts(nil))
	pr, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if len(pr.Person.Details.Emails) != 1 || pr.Person.Details.Emails[0].Value != "owner@example.com" || pr.Person.Details.Birthday != "01-02" || pr.Person.Details.Org != "" {
		t.Fatal("removed contact overwrote owner values or left Contacts values")
	}
}

func TestY45GroupOnlyProjectionDoesNotExposePrivatePhotoReferences(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, p.ImportContacts([]people.ContactMatch{{PersonID: id, Photo: jpegFixture(t)}}))
	testkit.Must(t, p.SetPhoto(id, jpegFixture(t)))
	a := model.Agent{ID: "group-only", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	projected, e := p.Profile(&a, id)
	testkit.Must(t, e)
	if projected.Person.Photo != nil || projected.Person.ContactsPhoto != nil {
		t.Fatal("group-only profile leaked private photo references")
	}
	canonical, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if canonical.Person.Photo == nil || canonical.Person.ContactsPhoto == nil {
		t.Fatal("projection altered canonical photo references")
	}
}

func TestY45SuggestionEvidenceIsRecheckedAfterChatScopeChanges(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	a, _, e := h.CreateAgent("Fixture AI", "codex", "local")
	testkit.Must(t, e)
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "private-pronouns", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "Invented profile detail", Timestamp: testkit.Epoch}))
	_, e = p.ProposePronouns(a, id, "she/her", []string{"private-pronouns"})
	testkit.Must(t, e)
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
	projected, e := p.Profile(&a, id)
	testkit.Must(t, e)
	if len(projected.Person.DetailSuggestions) != 0 {
		t.Fatal("suggestion exposed evidence from a now-excluded group")
	}
	canonical, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if len(canonical.Person.DetailSuggestions) != 1 {
		t.Fatal("projection erased owner suggestion")
	}
}

func TestY45PrivacyProjectionCoversSearchAndExport(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	id := model.PersonID(testkit.Phone)
	testkit.Must(t, p.ImportContacts([]people.ContactMatch{{PersonID: id, Photo: jpegFixture(t)}}))
	_, e := p.UpdateProfile(nil, id, map[string]any{"label": "PRIVATE_OWNER_LABEL", "notes": "PRIVATE_OWNER_NOTE"})
	testkit.Must(t, e)
	a := model.Agent{ID: "group-only", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
	for _, q := range []string{"PRIVATE_OWNER_LABEL", testkit.Phone} {
		found, e := p.PeopleAll(&a, q, true)
		testkit.Must(t, e)
		if len(found) != 0 {
			t.Fatal("search matched a redacted owner label or private DM identifier")
		}
	}
	export, e := p.Export(&a)
	testkit.Must(t, e)
	raw, e := json.Marshal(export)
	testkit.Must(t, e)
	for _, secret := range []string{"PRIVATE_OWNER_LABEL", "PRIVATE_OWNER_NOTE", "contacts_photo", "sha256"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("agent export leaked %s", secret)
		}
	}
	settings := h.Settings()
	settings.HiddenChats = []string{testkit.Phone}
	testkit.Must(t, h.SetSettings(settings))
	// The same service has warm projection caches from before hiding.
	found, e := p.PeopleAll(&a, "", true)
	testkit.Must(t, e)
	for _, person := range found {
		if person.ID == id {
			t.Fatal("hidden person remained in cached directory")
		}
	}
	export, e = p.Export(&a)
	testkit.Must(t, e)
	raw, e = json.Marshal(export)
	testkit.Must(t, e)
	if strings.Contains(string(raw), id) {
		t.Fatal("hidden person remained in graph export")
	}
	owner, e := p.Profile(nil, id)
	testkit.Must(t, e)
	if owner.Person.Notes != "PRIVATE_OWNER_NOTE" || owner.Person.ContactsPhoto == nil {
		t.Fatal("privacy projection changed owner knowledge")
	}
}
