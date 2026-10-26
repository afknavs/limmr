package efforts

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestE6TitleQuality(t *testing.T) {
	for _, tc := range []struct {
		title string
		good  bool
	}{
		{"Cedar website", true}, {"Saffron exam", true}, {"Flight tickets", true},
		{"Zorblax event", true}, {"Flerm session", true}, {"Zorblax", true},
		{"Zorbing plan", true}, {"Flermed venue", true}, {"Funding plan", true},
		{"Training session", true}, {"Reading session", true}, {"First aid training", true},
		{"Review article specific findings", true}, {"Design review", true},
		{"Zorblax first event", true}, {"Café meeting", true},
		{"Taken venue", false}, {"Onwards venue", false}, {"Taking venue", false},
		{"Already plan", false}, {"Please event", false}, {"Make website", false},
		{"Planning garden", false}, {"Organized event", false}, {"Held meeting", false}, {"Booked venue", false},
		{"The cedar website", false}, {"Our plan", false}, {"First event", false},
		{"Next session", false}, {"New meeting", false}, {"Annual event", false},
		{"Venue", false}, {"Main event", false}, {"Special session", false},
		{"event", false}, {"session", false}, {"meeting", false}, {"plan", false},
		{"Events", false}, {"Meeting plan", false}, {"Plan 2", false},
		{"", false}, {"   ", false}, {"123", false}, {"Image with Maya", false},
		{"OTP code", false}, {"Shared work", false},
		{strings.Repeat("z", 41) + " plan", false},
	} {
		t.Run(tc.title, func(t *testing.T) {
			if got := goodTitle(tc.title); got != tc.good {
				t.Errorf("goodTitle(%q)=%v want %v", tc.title, got, tc.good)
			}
			if err := validate(Update{Title: &tc.title}); (err == nil) != tc.good {
				t.Errorf("update title validation disagrees for %q", tc.title)
			}
		})
	}
}

func TestE6InferredTitleShapes(t *testing.T) {
	for _, tc := range []struct{ body, file, want, source string }{
		{"Taken venue", "", "", "naming_queue"},
		{"Planning garden", "", "", "naming_queue"},
		{"Onwards venue", "", "", "naming_queue"},
		{"First event", "", "", "naming_queue"},
		{"Zorblax first event", "", "Zorblax first event", "request_phrase"},
		{"Taken zorblax venue", "", "Zorblax venue", "request_phrase"},
		{"Onwards flerm website", "", "Flerm website", "request_phrase"},
		{"Zorbing plan", "", "Zorbing plan", "request_phrase"},
		{"flerm zorb", "", "", "naming_queue"},
		{"Taken venue", "Taken-Zorblax-Plan.pdf", "Zorblax plan", "document_name"},
		{"First event", "First-Event.pdf", "", "naming_queue"},
	} {
		t.Run(tc.body+tc.file, func(t *testing.T) {
			ms := []model.Message{{Content: "Please review " + tc.body, MediaType: "document", Filename: tc.file}, {Content: tc.body + " agreed"}}
			title, source := titleWithSource(ms, terms(tc.body))
			if title != tc.want || source != tc.source {
				t.Fatalf("title=%q source=%s want=%q/%s", title, source, tc.want, tc.source)
			}
		})
	}
	// An invalid explicit phrase must not displace a valid corroborated phrase.
	ms := []model.Message{{Content: "Taken venue?"}, {Content: "Taken venue"}, {Content: "Zorblax website"}, {Content: "Zorblax website"}}
	if title, _ := titleWithSource(ms, terms("taken venue zorblax website")); title != "Zorblax website" {
		t.Fatal("bad request phrase displaced a good noun phrase")
	}
}

func TestE6DuplicateTitleQualifiers(t *testing.T) {
	for _, tc := range []struct{ name, chat, file, want string }{
		{"topic", "Zorblax Club", "", "Cedar proposal · zorblax"},
		{"artefact", "", "Flerm-Budget.pdf", "Cedar proposal · Flerm budget"},
		{"month", "Maya Rao", "Maya-Rao-Budget.pdf", "Cedar proposal · Oct 2026"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			es := duplicateTitleFixture()
			es[1].Chats = []Entity{{"invented@g.us", tc.chat}}
			es[1].MessageRefs = []Ref{{"invented@g.us", "file"}}
			c := cached{messages: map[string]model.Message{key(es[1].MessageRefs[0]): {MediaType: "document", Filename: tc.file}}}
			q := qualityRules{names: words("maya rao")}
			dedupeTitles(es, c, q)
			if es[0].Title != "Cedar proposal" || es[1].Title != tc.want {
				t.Fatalf("unexpected titles: %q, %q", es[0].Title, es[1].Title)
			}
			if d, g := titleCounts(es); d != 0 || g != 0 {
				t.Fatalf("quality counts: duplicates=%d generic=%d", d, g)
			}
		})
	}
	// A person's DM label is never used, even when directory metadata is absent.
	es := duplicateTitleFixture()
	es[1].Chats = []Entity{{testkit.Phone, "Invented Fullname"}}
	dedupeTitles(es, cached{}, qualityRules{})
	if es[1].Title != "Cedar proposal · Oct 2026" {
		t.Fatal("DM identity became a qualifier")
	}
	// A same-month collision without another safe qualifier goes to naming.
	es = append(duplicateTitleFixture(), duplicateTitleFixture()[1])
	es[2].ID = "third"
	dedupeTitles(es, cached{}, qualityRules{})
	if es[2].Title != "" || es[2].titleSource != "naming_queue" {
		t.Fatal("ambiguous duplicate was exposed or arbitrarily numbered")
	}
	// Resolution is independent of map iteration / input order.
	reversed := duplicateTitleFixture()
	reversed[0], reversed[1] = reversed[1], reversed[0]
	dedupeTitles(reversed, cached{}, qualityRules{})
	if reversed[1].Title != "Cedar proposal" || reversed[0].Title != "Cedar proposal · Oct 2026" {
		t.Fatal("duplicate winner depends on input order")
	}
	// The on-device naming model uses persisted assignments too; those remain
	// automatic labels and need the same collision handling as inferred phrases.
	es = duplicateTitleFixture()
	for i := range es {
		es[i].CreatedBy, es[i].titleSource, es[i].baseTitleSource = "This Mac", "assigned_title", "assigned_title"
	}
	dedupeTitles(es, cached{}, qualityRules{})
	if es[1].Title != "Cedar proposal · Oct 2026" || es[1].titleSource != "assigned_title" {
		t.Fatal("on-device generated name bypassed dedupe")
	}
}

func duplicateTitleFixture() []Effort {
	es := []Effort{}
	for i := 0; i < 2; i++ {
		es = append(es, Effort{ID: fmt.Sprint(i), Title: "Cedar proposal", titleSource: "request_phrase", baseTitle: "Cedar proposal", baseTitleSource: "request_phrase", FirstAt: time.Date(2026, 10, 1+i, 0, 0, 0, 0, time.UTC)})
	}
	return es
}

func TestE6DuplicateTitlesColdIncrementalAndScope(t *testing.T) {
	s := qualitySeed(t, nil)
	chats := []string{"15550100001@s.whatsapp.net", "15550100002@s.whatsapp.net", "15550100003@s.whatsapp.net"}
	var all []Effort
	for i, chat := range chats {
		testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: chat, Name: "Invented Fullname"}))
		for j, text := range []string{"Please review the cedar proposal", "Cedar proposal approved", "Cedar proposal agreed"} {
			// Different body topics intentionally produce the same request label.
			// Identical topics across chats already consolidate under E5.
			text += " " + []string{"lichen canopy", "seabed reef", "quartz geode"}[i]
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("duplicate-%d-%d", i, j), ChatJID: chat, Sender: chat, IsFromMe: j == 1, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(20-i*4-j) * time.Hour)}))
		}
		var err error
		all, err = s.List(nil, Filter{IncludeUntitled: true})
		testkit.Must(t, err)
		cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
		testkit.Must(t, err)
		if !reflect.DeepEqual(all, cold) || len(all) != i+1 {
			t.Fatalf("duplicate resolution changed grouping or differs on cold load: i=%d warm=%+v cold=%+v", i, all, cold)
		}
		for _, e := range all {
			if len(e.MessageRefs) != 3 || len(e.Chats) != 1 {
				t.Fatal("title dedupe merged distinct efforts")
			}
		}
	}
	named, err := s.List(nil, Filter{})
	testkit.Must(t, err)
	_, queued, err := s.Unnamed(nil, 100)
	testkit.Must(t, err)
	if len(named) != 2 || queued != 1 {
		t.Fatal("unresolved duplicate did not reach naming queue")
	}
	a := model.Agent{ID: "isolated", Permissions: model.Defaults(false)}
	a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{chats[1]}}
	visible, err := s.List(&a, Filter{})
	testkit.Must(t, err)
	if len(visible) != 1 || visible[0].Title != "Cedar proposal" {
		t.Fatal("inaccessible title collision affected scoped inference")
	}
	// Removing an earlier collision must restore the next effort's base title.
	settings := s.Hub.Settings()
	settings.HiddenChats = []string{chats[0]}
	testkit.Must(t, s.Hub.SetSettings(settings))
	visible, err = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if !reflect.DeepEqual(visible, cold) || len(visible) != 2 || visible[1].Title != "Cedar proposal" {
		t.Fatal("title was not restored after collision disappeared")
	}
	// Owner edits stay authoritative, with residual collisions reported as counts.
	for _, e := range visible {
		_, err = s.Update(nil, e.ID, Update{Title: ptr("Flerm event")})
		testkit.Must(t, err)
	}
	audit, err := s.Audit()
	testkit.Must(t, err)
	if audit.DuplicateTitles != 1 || audit.GenericTitles != 0 {
		t.Fatal("explicit duplicate was not counted by the audit")
	}
}

func TestE6TitleAuditCountsOnlyAndExplicitEdits(t *testing.T) {
	es := duplicateTitleFixture()
	es = append(es, Effort{Title: "  CEDAR--proposal "}, Effort{Title: "First event"}, Effort{})
	if d, g := titleCounts(es); d != 2 || g != 1 {
		t.Fatalf("counts=%d/%d want=2/1", d, g)
	}
	// Explicit names are preserved, so any intentional collision remains auditable.
	for i := range es[:3] {
		es[i].titleSource = "assigned_title"
	}
	dedupeTitles(es, cached{}, qualityRules{})
	if d, _ := titleCounts(es); d != 2 {
		t.Fatal("explicit names silently changed")
	}
	s := qualitySeed(t, []string{"Cedar proposal", "Cedar proposal", "Cedar proposal"})
	xs, err := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	// Simulate a legacy edit made before the new validator existed.
	testkit.Must(t, s.Hub.Repo.Put("effort_edits", xs[0].ID, override{ID: xs[0].ID, Refs: xs[0].MessageRefs, Update: Update{Title: ptr("First event")}}))
	audit, err := s.Audit()
	testkit.Must(t, err)
	if audit.GenericTitles != 1 || audit.DuplicateTitles != 0 || audit.NamingQueued != 1 {
		t.Fatalf("legacy quality counts wrong: %+v", audit)
	}
	data, err := json.Marshal(audit)
	testkit.Must(t, err)
	for _, private := range []string{"Cedar", "First event", xs[0].ID, testkit.Phone} {
		if strings.Contains(string(data), private) {
			t.Fatal("title audit exposed identifying content")
		}
	}
}
