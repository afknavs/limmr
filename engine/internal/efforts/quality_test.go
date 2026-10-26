package efforts

import (
	"encoding/json"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"strings"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }
func qualitySeed(t *testing.T, texts []string) *Service {
	t.Helper()
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	for i, text := range texts {
		testkit.Must(t, h.Messages.PutMessage(model.Message{ID: fmt.Sprintf("quality-%d", i), ChatJID: testkit.Phone, Sender: testkit.Phone, IsFromMe: i == 1, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(len(texts)-i) * time.Hour)}))
	}
	return &Service{Hub: h}
}
func TestQualityExcludedMessagesCannotFormOrAttach(t *testing.T) {
	cases := []struct {
		name, text, media string
		change            func(*testing.T, *Service, *model.Message)
		reason            string
	}{
		{"otp", "Your verification code is 628415", "", nil, "otp"},
		{"numeric-code", "628415", "", nil, "otp"},
		{"pin", "Use code 6284 to sign in", "", nil, "otp"},
		{"bank", "Bank transaction alert: debit balance", "", nil, "otp"},
		{"business", "Festival budget proposal", "", func(t *testing.T, s *Service, m *model.Message) {
			testkit.Must(t, s.Hub.Repo.Put("people", "business", model.Person{ID: "business", JIDs: []string{testkit.Phone}, Business: true}))
		}, "otp"},
		{"short-code", "Festival budget proposal", "", func(t *testing.T, _ *Service, m *model.Message) { m.Sender = "12345@s.whatsapp.net" }, "otp"},
		{"self", "Festival budget proposal", "", func(t *testing.T, s *Service, m *model.Message) {
			testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", JIDs: []string{testkit.Phone}}))
		}, "self"},
		{"self-alias", "Festival budget proposal", "", func(t *testing.T, s *Service, m *model.Message) {
			testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", JIDs: []string{"900001@lid"}}))
			_, e := s.Hub.Messages.DB.Exec("INSERT OR REPLACE INTO lid_aliases(lid,pn) VALUES(?,?)", "900001@lid", testkit.Phone)
			testkit.Must(t, e)
		}, "self"},
		{"you-circle", "Festival budget proposal", "", func(t *testing.T, s *Service, m *model.Message) {
			testkit.Must(t, s.Hub.Repo.Put("circles", "you", model.Circle{ID: "you", Name: "You", Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}))
		}, "self"},
		{"media", "<Media omitted>", "", nil, "media"}, {"photo", "image omitted", "image", nil, "media"},
		{"sticker", "Festival budget proposal", "sticker", nil, "media"}, {"reaction", "Festival budget proposal", "reaction", nil, "media"},
		{"system", "Festival budget proposal", "system", nil, "media"},
		{"deleted", "Festival budget proposal", "", func(t *testing.T, _ *Service, m *model.Message) { m.DeletedAt = ptr(testkit.Epoch) }, "media"},
		{"deleted-placeholder", "This message was deleted", "", nil, "media"},
		{"link-only", "https://example.test/festival/budget https://example.test/proposal", "", nil, "media"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			for i := 0; i < 3; i++ {
				m := model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: tc.text, MediaType: tc.media, Timestamp: testkit.Epoch.AddDate(0, 0, -i)}
				if tc.change != nil {
					tc.change(t, s, &m)
				}
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
				if m.DeletedAt != nil {
					_, e := s.Hub.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id=? AND chat_jid=?", *m.DeletedAt, m.ID, m.ChatJID)
					testkit.Must(t, e)
				}
			}
			xs, e := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, e)
			if len(xs) != 0 {
				t.Fatal("excluded source formed effort")
			}
			a, e := s.Audit()
			testkit.Must(t, e)
			if a.MessagesFiltered[tc.reason] != 3 {
				t.Fatalf("wrong aggregate reason: %+v", a)
			}
		})
	}
	s := qualitySeed(t, []string{"Festival budget proposal", "Festival budget proposal", "Festival budget proposal"})
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "otp-attach", ChatJID: testkit.Phone, Content: "Verification code 837215", Timestamp: testkit.Epoch}))
	_, e = s.Regroup(nil, "attach", xs[0].ID, "", []Ref{{testkit.Phone, "otp-attach"}})
	if e != model.ErrNotFound {
		t.Fatal("explicit attach bypassed filter")
	}
}
func TestQualityThresholdAndNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		texts []string
		want  int
	}{
		{"one-term", []string{"pottery", "pottery", "pottery"}, 0},
		{"two-messages", []string{"budget proposal", "budget proposal"}, 0},
		{"unshared", []string{"budget proposal", "budget festival", "proposal venue"}, 0},
		{"names", []string{"Maya Rao hai yaar image", "Maya Rao hai yaar image", "Maya Rao hai yaar image"}, 0},
		{"hinglish", []string{"haan bhai accha yaar", "haan bhai accha yaar", "haan bhai accha yaar"}, 0},
		{"short-tokens", []string{"ab cd ef", "ab cd ef", "ab cd ef"}, 0},
		{"supported", []string{"Festival budget proposal", "Festival budget proposal", "Festival budget proposal"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, tc.texts)
			xs, e := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, e)
			if len(xs) != tc.want {
				t.Fatalf("got %d", len(xs))
			}
		})
	}
	// One sender within one day is insufficient; across two days it is evidence.
	s := qualitySeed(t, nil)
	for i := 0; i < 3; i++ {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprint(i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "festival budget", Timestamp: testkit.Epoch.Add(-time.Duration(i) * time.Hour)}))
	}
	xs, e := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	if len(xs) != 0 {
		t.Fatal("one-day monologue accepted")
	}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "another-day", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "festival budget", Timestamp: testkit.Epoch.AddDate(0, 0, -1)}))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 {
		t.Fatal("two-day evidence lost")
	}
}
func TestQualityInternalUntilNamedAndNoFiller(t *testing.T) {
	s := qualitySeed(t, []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"})
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 0 {
		t.Fatal("non-phrase title surfaced")
	}
	xs, e = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	if len(xs) != 1 || xs[0].Title != "" || xs[0].Summary != "" {
		t.Fatal("internal inference missing or filler generated")
	}
	a, e := s.Audit()
	testkit.Must(t, e)
	if a.Internal != 1 || a.NamingQueued != 1 || a.CandidatesFiltered["untitled"] != 0 {
		t.Fatal("audit missed internal purpose")
	}
	title := "Festival budget"
	named, e := s.Update(nil, xs[0].ID, Update{Title: &title, OnlyUnnamed: true, ExpectedLastAt: &xs[0].LastAt})
	testkit.Must(t, e)
	if named.CreatedBy != "This Mac" {
		t.Fatal("local attribution missing")
	}
	renamed := "Another purpose"
	_, e = s.Update(nil, xs[0].ID, Update{Title: &renamed, OnlyUnnamed: true, ExpectedLastAt: &xs[0].LastAt})
	testkit.Must(t, e)
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || xs[0].Title != title {
		t.Fatal("local naming overwrote existing name")
	}
	for _, junk := range []string{"Image with Maya", "OTP code", "Shared work", "   "} {
		if validate(Update{Title: &junk}) == nil {
			t.Fatal("accepted junk title")
		}
	}
	if concreteSummary([]model.Message{{Content: "Festival budget", Timestamp: testkit.Epoch}}, "moving", nil) != "" {
		t.Fatal("generic progress text")
	}
}
func TestQualityExplicitDeadlines(t *testing.T) {
	at := time.Date(2026, 10, 19, 20, 30, 0, 0, time.FixedZone("fixture", 19800))
	for _, tc := range []struct{ text, want string }{
		{"Please finish by Thursday", "2026-10-22T00:00:00+05:30"},
		{"Submit by Thursday at 5 pm", "2026-10-22T17:00:00+05:30"},
		{"The deadline is 21 Oct", "2026-10-21T00:00:00+05:30"},
		{"Due 2026-10-23", "2026-10-23T00:00:00+05:30"},
		{"Review tomorrow 5 pm", "2026-10-20T17:00:00+05:30"},
		{"Due tomorrow at 17:30", "2026-10-20T17:30:00+05:30"},
		{"Due today", "2026-10-19T00:00:00+05:30"},
		{"Thursday is my favourite weekday", ""}, {"We discussed Monday during the call", ""},
		{"It happened due to Monday traffic", ""}, {"Give credit where credit is due", ""},
		{"Overdue library books on Thursday", ""}, {"Due to the rain tomorrow we may postpone", ""},
		{"Nothing is due; Monday was sunny", ""}, {"The budget proposal is ready", ""},
		{"Due 2026-02-30", ""},
	} {
		t.Run(tc.text, func(t *testing.T) {
			d := due([]model.Message{{Content: tc.text, Timestamp: at}})
			got := ""
			if d != nil {
				got = d.Format(time.RFC3339)
			}
			if got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
	s := qualitySeed(t, []string{"festival budget", "festival budget", "festival budget"})
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	_, e = s.Update(nil, xs[0].ID, Update{Due: ptr("2026-10-22T00:00:00Z")})
	if e != model.ErrInvalid {
		t.Fatal("agent fabricated deadline")
	}
}
func TestQualityHomeGlobalCapDedupeAndDormancy(t *testing.T) {
	s := qualitySeed(t, nil)
	for i := 0; i < 6; i++ {
		for j := 0; j < 3; j++ {
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("purpose-%d-%d", i, j), ChatJID: testkit.Phone, Sender: testkit.Phone, IsFromMe: j == 1, Content: fmt.Sprintf("purpose%d research", i), Timestamp: testkit.Epoch.Add(-time.Duration(i*4+j) * time.Hour)}))
		}
	}
	for i := 0; i < 3; i++ {
		c := model.Circle{ID: fmt.Sprint(i), Name: fmt.Sprintf("Studio %d", i), Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}
		testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	}
	rows, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 4 {
		t.Fatalf("global cap %d", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.EffortID] {
			t.Fatal("duplicate purpose across circles")
		}
		seen[r.EffortID] = true
	}
	testkit.Must(t, s.Seen())
	rows, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 0 {
		t.Fatal("unchanged moving efforts remained")
	}
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	st := "dormant"
	for _, x := range xs {
		_, e = s.Update(nil, x.ID, Update{Status: &st})
		testkit.Must(t, e)
	}
	rows, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 0 {
		t.Fatal("dormant efforts on home")
	}
	report, e := s.Audit()
	testkit.Must(t, e)
	data, e := json.Marshal(report)
	testkit.Must(t, e)
	for _, secret := range []string{"Maya", "Studio", "purpose0", testkit.Phone} {
		if strings.Contains(string(data), secret) {
			t.Fatal("audit contains identifying content")
		}
	}
}

func TestQualityDedupeAcrossUnrelatedPeopleAndCircles(t *testing.T) {
	s := qualitySeed(t, nil)
	other := "15550100002@s.whatsapp.net"
	testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: other, Name: "Noor Chen"}))
	for i, chat := range []string{testkit.Phone, other} {
		for j := 0; j < 3; j++ {
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("duplicate-%d-%d", i, j), ChatJID: chat, Sender: chat, Content: "festival budget proposal", Timestamp: testkit.Epoch.Add(-time.Duration((i*3+j)*24) * time.Hour)}))
		}
		c := model.Circle{ID: fmt.Sprint(i), Name: fmt.Sprintf("Circle %d", i), Confirmed: true, Chats: []model.CircleChat{{ChatJID: chat}}}
		testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	}
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || len(xs[0].MessageRefs) != 6 || len(xs[0].CircleIDs) != 2 {
		t.Fatalf("cross-circle duplicate remained: %d", len(xs))
	}
	rows, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 1 {
		t.Fatal("purpose repeated on Home")
	}
	testkit.Must(t, s.Seen(map[string]time.Time{rows[0].Circle.ID: rows[0].LastAt}))
	rows, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 0 {
		t.Fatal("seen purpose rotated back through another circle")
	}

}
func TestQualityHomeUrgentOrDueSoonAfterSeen(t *testing.T) {
	s := qualitySeed(t, []string{"Festival budget proposal", "Festival budget proposal", "Festival budget proposal. Due tomorrow."})
	c := model.Circle{ID: "studio", Name: "Studio", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}
	testkit.Must(t, s.Hub.Repo.Put("circles", c.ID, c))
	testkit.Must(t, s.Seen())
	rows, e := s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 1 {
		t.Fatal("explicit upcoming due disappeared after seen")
	}
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	_, e = s.Update(nil, xs[0].ID, Update{Due: ptr(""), Status: ptr("on_you")})
	testkit.Must(t, e)
	rows, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 1 {
		t.Fatal("on-you disappeared after seen")
	}
	_, e = s.Update(nil, xs[0].ID, Update{Status: ptr("moving")})
	testkit.Must(t, e)
	rows, e = s.CircleUpdates()
	testkit.Must(t, e)
	if len(rows) != 0 {
		t.Fatal("unchanged ordinary purpose remained")
	}
	if dueSoon(ptr(testkit.Epoch.AddDate(0, 0, 8)), testkit.Epoch) || dueSoon(ptr(testkit.Epoch.Add(-time.Minute)), testkit.Epoch) {
		t.Fatal("out-of-window deadline treated as due soon")
	}
}

func TestQualityLateOffCoreEvidenceDoesNotMergeCandidates(t *testing.T) {
	s := qualitySeed(t, []string{"festival budget proposal", "festival budget proposal", "festival budget proposal"})
	other := "15550100002@s.whatsapp.net"
	testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: other, Name: "Noor Chen"}))
	for j := 0; j < 3; j++ {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("separate-%d", j), ChatJID: other, Sender: other, IsFromMe: j == 1, Content: "festival budget proposal venue", Timestamp: testkit.Epoch.Add(-time.Duration(j+4) * time.Hour)}))
	}
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 2 {
		t.Fatal("fixture needs two candidates")
	}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "bridge", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "venue", QuotedMessageID: "quality-0", Timestamp: testkit.Epoch}))
	xs, e = s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 2 || len(xs[0].MessageRefs)+len(xs[1].MessageRefs) != 7 {
		t.Fatal("off-core quote bridged two existing purposes")
	}
}
func TestQualityStaleLocalGenerationCannotOverwriteArrival(t *testing.T) {
	s := qualitySeed(t, []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"})
	xs, e := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	old := xs[0]
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "new-evidence", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "budget and proposal", Timestamp: testkit.Epoch}))
	_, e = s.Update(nil, old.ID, Update{Title: ptr("Festival budget"), OnlyUnnamed: true, ExpectedLastAt: &old.LastAt})
	testkit.Must(t, e)
	detail, e := s.Get(nil, old.ID)
	testkit.Must(t, e)
	if detail.Title != "" {
		t.Fatal("stale generation overwrote new evidence")
	}
	if cleanSummary("No progress for two weeks; decide whether to restart.", "moving") != "" {
		t.Fatal("legacy filler survived")
	}
}

func TestRandomRepeatedFragmentsAreDiscarded(t *testing.T) {
	s := qualitySeed(t, []string{"zorb flerm", "zorb flerm", "zorb flerm"})
	xs, e := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	// E7 requires work evidence before even entering the naming queue.
	if len(xs) != 0 {
		t.Fatal("random fragments entered the effort queue")
	}
	shown, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(shown) != 0 {
		t.Fatal("random fragment surfaced in Activity")
	}
}

func TestQualityMixedCodesAndHumanBankDiscussion(t *testing.T) {
	q := qualityRules{}
	for _, m := range []model.Message{{Content: "Your login code is AB9CD2"}, {Sender: "VK-NBANK", Content: "A recurring account update"}} {
		if q.reason(m) != "otp" {
			t.Fatal("verification source was accepted")
		}
	}
	if q.reason(model.Message{Content: "Please verify the festival budget at the river bank"}) != "" {
		t.Fatal("human topic mistaken for a bank alert")
	}
}

func TestDeadlineEvidenceOutsideTimelineAndDirectoryFallback(t *testing.T) {
	s := qualitySeed(t, []string{"festival budget proposal. Due tomorrow.", "festival budget proposal", "festival budget proposal"})
	for i := 0; i < 55; i++ {
		testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: fmt.Sprintf("later-%d", i), ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "festival budget proposal", Timestamp: testkit.Epoch.Add(-time.Duration(55-i) * time.Minute)}))
	}
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 || xs[0].Due == nil {
		t.Fatal("deadline fixture missing")
	}
	_, e = s.Update(nil, xs[0].ID, Update{Due: ptr(xs[0].Due.Format(time.RFC3339))})
	testkit.Must(t, e)
	// During directory sync a DM may have a display name before its Person exists.
	s = qualitySeed(t, []string{"Maya Rao", "Maya Rao", "Maya Rao"})
	testkit.Must(t, s.Hub.Repo.Delete("people", model.PersonID(testkit.Phone)))
	xs, e = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	if len(xs) != 0 {
		t.Fatal("chat display names became topic terms")
	}
	testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: testkit.Phone, Name: "You"}))
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: "self-by-name", ChatJID: testkit.Phone, Content: "festival budget proposal", Timestamp: testkit.Epoch}))
	report, e := s.Audit()
	testkit.Must(t, e)
	if report.MessagesFiltered["self"] != 4 {
		t.Fatal("self chat display-name fallback missed")
	}
}

func TestNewOwnerAliasInvalidatesExistingInference(t *testing.T) {
	s := qualitySeed(t, []string{"festival budget proposal", "festival budget proposal", "festival budget proposal"})
	testkit.Must(t, s.Hub.Repo.Put("people", "me", model.Person{ID: "me", JIDs: []string{"900000001@lid"}}))
	xs, e := s.List(nil, Filter{})
	testkit.Must(t, e)
	if len(xs) != 1 {
		t.Fatal("fixture needs an initially unrelated chat")
	}
	_, e = s.Hub.Messages.DB.Exec("INSERT INTO lid_aliases(lid,pn) VALUES(?,?)", "900000001@lid", testkit.Phone)
	testkit.Must(t, e)
	xs, e = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, e)
	if len(xs) != 0 {
		t.Fatal("newly resolved self alias reused stale efforts")
	}
}
