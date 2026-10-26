package efforts

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestE7FormattedBankAndOwnerMenuCommands(t *testing.T) {
	s := qualitySeed(t, nil)
	texts := []string{
		"Click below for other *Account Services* or Type *Hi* to go back to the *Main Menu*.",
		"Looking for something else? 🔍 Check all options in Main Menu",
		"Please *type* any Cedar Bank related query or select from the options below",
		"Get a Cedar credit card with instant approval and rewards today",
	}
	ms := []model.Message{}
	for day := 0; day < 6; day++ {
		for i, text := range texts {
			m := e7Messages(testkit.Phone, text)[0]
			m.ID = fmt.Sprintf("bank-%d-%d", day, i)
			m.IsFromMe = false
			m.Timestamp = testkit.Epoch.AddDate(0, 0, -6+day).Add(time.Duration(i) * time.Minute)
			ms = append(ms, m)
			owner := m
			owner.ID += "-command"
			owner.IsFromMe = true
			owner.Content = []string{"Hi", "1", "2", "Hi"}[i]
			owner.Timestamp = owner.Timestamp.Add(time.Second)
			ms = append(ms, owner)
		}
	}
	if xs := e7List(t, s, ms); len(xs) != 0 {
		t.Fatal("formatted bank/promos or owner menu commands formed an effort")
	}
	_, total, err := s.Unnamed(nil, 100)
	testkit.Must(t, err)
	if total != 0 {
		t.Fatal("bank entered naming queue")
	}
	q := newQuality(nil, nil, nil, "")
	q.business[testkit.Phone] = true
	if q.addressed(model.Message{ChatJID: testkit.Phone, IsFromMe: true, Content: "Hi"}) {
		t.Fatal("owner command counted as bot-chat involvement")
	}
	for _, text := range []string{"_Main Menu_", "~Type Hi~", "`select from the options`", "👇 *Click below*", "Reply with your selection", "Press 1 for services"} {
		if !automatedMenu(text) {
			t.Errorf("missed formatted menu shape %q", text)
		}
	}
}
func TestE7ShortNormalizedTemplatesAndNumericMenuRatio(t *testing.T) {
	for _, tc := range []struct {
		name             string
		n, total         int
		numeric, wantBot bool
	}{
		{"short repeat majority", 4, 7, false, true},
		{"minority is human", 4, 9, false, false},
		{"numeric sixty percent", 3, 5, true, true},
		{"numeric below sixty", 2, 5, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			if tc.numeric {
				testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: testkit.Phone, Name: "+1 (555) 010-0999"}))
			}
			for i := 0; i < tc.total; i++ {
				extras := []string{"Please review the festival budget proposal", "A river map marks the northern entrance", "Transport will follow the coastal road", "The pottery exhibition opens after lunch", "We can meet outside the observatory"}
				text := extras[(i-tc.n+len(extras))%len(extras)]
				if i < tc.n {
					text = []string{"*Cedar rewards offer is ready for approval* 🎁", "_Cedar rewards offer is ready for approval_ 🔍", "~Cedar rewards offer is ready for approval~", "`Cedar rewards offer is ready for approval`"}[i%4]
				}
				m := e7Messages(testkit.Phone, text)[0]
				m.ID = fmt.Sprint("template-", i)
				m.IsFromMe = false
				m.Timestamp = testkit.Epoch.AddDate(0, 0, -tc.total+i)
				testkit.Must(t, s.Hub.Messages.PutMessage(m))
			}
			owner := e7Messages(testkit.Phone, "Hi")[0]
			owner.ID = "owner-menu-command"
			owner.IsFromMe = true
			owner.Timestamp = testkit.Epoch
			testkit.Must(t, s.Hub.Messages.PutMessage(owner))
			c, err := s.snapshot(nil)
			testkit.Must(t, err)
			if got := c.groups.rules.business[testkit.Phone]; got != tc.wantBot {
				t.Fatalf("bot=%v want=%v", got, tc.wantBot)
			}
			if tc.wantBot && len(c.efforts) != 0 {
				t.Fatal("template sender formed effort")
			}
		})
	}
	if automationTemplate(automationText("👍 *A short sentence*")) != "" {
		t.Fatal("short replies counted as templates")
	}
}

func e7Job(parts [][]string, days []int) []model.Message {
	ms := []model.Message{}
	for i, part := range parts {
		for j, text := range part {
			m := e7Messages(testkit.Phone, text)[0]
			m.ID = fmt.Sprintf("job-%d-%d", i, j)
			m.IsFromMe = j == 1
			m.Timestamp = testkit.Epoch.AddDate(0, 0, -days[i]).Add(time.Duration(j-3) * time.Minute)
			ms = append(ms, m)
		}
	}
	return ms
}
func TestE7LiveShapedDirectJobsOverSixWeeks(t *testing.T) {
	jobs := []struct {
		name  string
		parts [][]string
		days  []int
	}{
		{"co-lead workshop", [][]string{
			{"Please review the first Oriole workshop outline", "Oriole workshop teaching outline is ready", "First Oriole workshop outline confirmed"},
			{"Please review the Oriole club collaboration proposal", "Oriole club collaboration proposal is ready", "Oriole club collaboration proposal confirmed"},
			{"Please send the proposal email to Professor Inez for the Oriole collaboration", "Professor Inez proposal email for Oriole needs revision", "Oriole proposal email to Professor Inez confirmed"},
			{"Latest: speak to the HOD and club about the department proposal", "Department proposal needs a discussion with the HOD", "Department proposal to the HOD confirmed"},
		}, []int{42, 28, 14, 1}},
		{"house walkthrough", [][]string{
			{"Please review the photoreal house renders", "Photoreal house renders lighting is ready", "Photoreal house renders confirmed"},
			{"Update: the walkthrough brief and zip are ready", "Please review the walkthrough brief and zip", "Walkthrough brief and zip confirmed"},
		}, []int{20, 1}},
		{"client pitch", [][]string{
			{"Please review the client pitch pricing in the demo", "Client pitch pricing in the demo is ready", "Client pitch pricing in the demo confirmed"},
			{"Quick update on the client pitch decision", "Please review the client pitch update", "Client pitch update is confirmed"},
			{"Latest: official client links for the pitch", "Please review the official client pitch links https://example.test/cedar-pitch", "Official client pitch links confirmed"},
		}, []int{35, 18, 1}},
	}
	for _, job := range jobs {
		t.Run(job.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			testkit.Must(t, s.Hub.Repo.Put("people", "professor", model.Person{ID: "professor", Name: "Inez", JIDs: []string{"15550909999@s.whatsapp.net"}}))
			ms := e7Job(job.parts, job.days)
			if job.name == "house walkthrough" {
				ms[3].MediaType = "document"
				ms[3].Filename = "Willow-Walkthrough.zip"
			}
			xs := e7List(t, s, ms)
			if len(xs) != 1 || !exactRound2(xs[0], ms) {
				t.Fatalf("%s fragmented or lost evidence: efforts=%d", job.name, len(xs))
			}
			if !xs[0].FirstAt.Equal(ms[0].Timestamp) || !xs[0].LastAt.Equal(ms[len(ms)-1].Timestamp) {
				t.Fatal("lost project time bounds")
			}
			cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if !reflect.DeepEqual(xs, cold) {
				t.Fatal("project differs after restart")
			}
		})
	}
}
func TestE7DifferentJobsWithSameFriendStaySeparate(t *testing.T) {
	s := qualitySeed(t, nil)
	workshop := e7Job([][]string{{"Please review the estuary workshop proposal", "Estuary workshop proposal is ready", "Estuary workshop proposal confirmed"}}, []int{14})
	house := e7Job([][]string{{"Latest: please review the cedar house walkthrough", "Cedar house walkthrough lighting is ready", "Cedar house walkthrough confirmed"}}, []int{13})
	for i := range house {
		house[i].ID = "house-" + house[i].ID
	}
	xs := e7List(t, s, append(workshop, house...))
	if len(xs) != 2 {
		t.Fatalf("different jobs merged: %d", len(xs))
	}
	for _, e := range xs {
		if !exactRound2(e, workshop) && !exactRound2(e, house) {
			t.Fatal("jobs contaminated one another")
		}
	}
}
func TestE7DirectIntervalsAndTwentyOneDayWindow(t *testing.T) {
	for _, spec := range []struct {
		name       string
		gap        int
		interleave bool
		want       int
	}{
		{"within window", 21, false, 1}, {"outside window", 22, false, 2}, {"interleaving", 40, true, 1},
	} {
		t.Run(spec.name, func(t *testing.T) {
			s := qualitySeed(t, nil)
			ms := e7Job([][]string{{"Please review the oriole workshop agenda", "Oriole workshop agenda is ready", "Oriole workshop agenda confirmed"}, {"Please review the oriole club proposal", "Oriole club proposal is ready", "Oriole club proposal confirmed"}}, []int{spec.gap, 0})
			if spec.interleave {
				ms[2].Timestamp = testkit.Epoch.Add(-time.Minute)
			}
			xs := e7List(t, s, ms)
			if len(xs) != spec.want {
				t.Fatalf("got %d want %d", len(xs), spec.want)
			}
		})
	}
}

func TestE7MachineNamesCannotResurrectBroadcasts(t *testing.T) {
	for _, by := range []string{"This Mac", "this-mac", "naming queue", "heuristics", "Limmr", "you", "Explicit Fixture AI"} {
		t.Run(by, func(t *testing.T) {
			s := qualitySeed(t, nil)
			testkit.Must(t, s.Hub.Repo.Put("groups", testkit.Group, model.Group{JID: testkit.Group, MemberCount: 60}))
			ms := e7Messages(testkit.Group, "Association hour meeting today at one in the east venue", "Association hour meeting is at one in the east venue", "Association hour meeting today at one in the east venue")
			refs := []Ref{}
			for i := range ms {
				ms[i].IsFromMe = false
				ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -30-i)
				refs = append(refs, ref(ms[i]))
			}
			putRound2(t, s, ms)
			title := "Association hour"
			testkit.Must(t, s.Hub.Repo.Put("effort_edits", "legacy-named", override{ID: "legacy-named", Refs: refs, By: by, Update: Update{Title: &title}}))
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			explicit := by == "you" || by == "Explicit Fixture AI"
			if explicit {
				if len(xs) != 1 || xs[0].Status != "dormant" {
					t.Fatal("deliberate name did not survive")
				}
			} else if len(xs) != 0 {
				t.Fatal("automatic name resurrected broadcast")
			}
			cold, err := (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if !reflect.DeepEqual(xs, cold) {
				t.Fatal("provenance differs after restart")
			}
		})
	}
}
func TestE7EditsDoNotChangeLegacyMachineTitleOrigin(t *testing.T) {
	for _, action := range []string{"pin", "attach"} {
		t.Run(action, func(t *testing.T) {
			s := qualitySeed(t, nil)
			testkit.Must(t, s.Hub.Repo.Put("groups", testkit.Group, model.Group{JID: testkit.Group, MemberCount: 8}))
			ms := e7Messages(testkit.Group, "budget for the proposal", "budget is for proposal", "budget and a proposal")
			for i := range ms {
				ms[i].IsFromMe = false
				ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -3+i)
			}
			e7List(t, s, ms)
			queue, total, err := s.Unnamed(nil, 10)
			testkit.Must(t, err)
			if total != 1 {
				t.Fatal("missing unnamed work")
			}
			title := "Association budget"
			_, err = s.Update(nil, queue[0].ID, Update{Title: &title, OnlyUnnamed: true, ExpectedRevision: queue[0].Revision})
			testkit.Must(t, err)
			// Emulate a legacy edit with no separately stored title origin.
			var edit override
			testkit.Must(t, s.Hub.Repo.Get("effort_edits", queue[0].ID, &edit))
			edit.TitleOrigin = ""
			testkit.Must(t, s.Hub.Repo.Put("effort_edits", queue[0].ID, edit))
			var named Effort
			if action == "pin" {
				pinned := true
				named, err = s.Update(nil, queue[0].ID, Update{Pinned: &pinned})
			} else {
				named, err = s.Regroup(nil, "attach", queue[0].ID, "", []Ref{ref(ms[0])})
			}
			testkit.Must(t, err)
			if explicitTitle(named) {
				t.Fatal("edit converted machine name into deliberate rename")
			}
			testkit.Must(t, s.Hub.Repo.Get("effort_edits", queue[0].ID, &edit))
			if edit.TitleOrigin != "machine" {
				t.Fatal("machine origin not persisted")
			}
			testkit.Must(t, s.Hub.Repo.Put("groups", testkit.Group, model.Group{JID: testkit.Group, MemberCount: 60}))
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if len(xs) != 0 {
				t.Fatal("edited machine name escaped involvement gate")
			}
		})
	}
}
func TestE7AccountSharingArgumentIsNotWork(t *testing.T) {
	s := qualitySeed(t, nil)
	ms := e7Messages(testkit.Phone, "Please don't use my account for your project", "Use Claude instead for that project", "Your project doesn't work with my account", "Please don't use my account", "Use Claude instead", "It doesn't work")
	for i := range ms {
		ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -6+i)
	}
	if xs := e7List(t, s, ms); len(xs) != 0 {
		t.Fatal("account-sharing argument became work")
	}
	title := "Account access"
	refs := []Ref{}
	for _, m := range ms {
		refs = append(refs, ref(m))
	}
	testkit.Must(t, s.Hub.Repo.Put("effort_edits", "old-argument", override{ID: "old-argument", Refs: refs, By: "This Mac", Update: Update{Title: &title}}))
	xs, err := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("old machine name resurrected argument")
	}
	text := strings.Join([]string{ms[0].Content, ms[1].Content}, " ")
	real := model.Message{Timestamp: testkit.Epoch}
	// A decision token is only evidence, never an instruction to share credentials.
	real.Content = text + " Separate account setup confirmed"
	if !taskEvidence([]model.Message{real}) {
		t.Fatal("explicit decision was dropped with banter")
	}
}

func TestE7DirectThirdPartyAndURLContinuity(t *testing.T) {
	for _, kind := range []string{"third party", "counterpart only", "counterpart metadata missing", "same URL", "different URLs"} {
		t.Run(kind, func(t *testing.T) {
			s := qualitySeed(t, nil)
			testkit.Must(t, s.Hub.Repo.Put("people", "inez", model.Person{ID: "inez", Name: "Inez", JIDs: []string{"15550901111@s.whatsapp.net"}}))
			if kind == "counterpart metadata missing" {
				testkit.Must(t, s.Hub.Repo.Delete("people", model.PersonID(testkit.Phone)))
			}
			parts := [][]string{{"Please review the copper exhibition layout", "Copper exhibition layout is ready", "Copper exhibition layout confirmed"}, {"Please prepare the estuary grant application", "Estuary grant application is ready", "Estuary grant application confirmed"}}
			for i := range parts {
				for j := range parts[i] {
					switch kind {
					case "third party":
						parts[i][j] += " for Inez"
					case "counterpart only", "counterpart metadata missing":
						parts[i][j] += " Maya"
					case "same URL":
						parts[i][j] += " https://example.test/shared-goal"
					case "different URLs":
						parts[i][j] += fmt.Sprintf(" https://example.test/goal-%d", i)
					}
				}
			}
			ms := e7Job(parts, []int{14, 1})
			xs := e7List(t, s, ms)
			want := 1
			if kind == "counterpart only" || kind == "counterpart metadata missing" || kind == "different URLs" {
				want = 2
			}
			if len(xs) != want {
				t.Fatalf("got %d efforts want %d", len(xs), want)
			}
			if want == 1 && !exactRound2(xs[0], ms) {
				t.Fatal("lost project evidence")
			}
		})
	}
}
func TestE7ArgumentCannotBridgeGenuineAssistantProject(t *testing.T) {
	s := qualitySeed(t, nil)
	argument := e7Job([][]string{{"Please don't use my account for your Claude project", "Use Claude instead for your project", "The Claude project doesn't work with my account"}}, []int{14})
	real := e7Job([][]string{{"Please review the Claude website design", "Claude website design is ready", "Claude website design confirmed"}}, []int{1})
	for i := range real {
		real[i].ID = "website-" + real[i].ID
	}
	xs := e7List(t, s, append(argument, real...))
	if len(xs) != 1 || !exactRound2(xs[0], real) {
		t.Fatal("argument suppressed or polluted genuine work")
	}
}
