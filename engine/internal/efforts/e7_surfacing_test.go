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

func e7Messages(chat string, texts ...string) []model.Message {
	ms := []model.Message{}
	for i, text := range texts {
		ms = append(ms, model.Message{ID: fmt.Sprintf("e7-%d", i), ChatJID: chat, Sender: testkit.Phone, IsFromMe: i%2 == 1, Content: text, Timestamp: testkit.Epoch.Add(-time.Duration(len(texts)-i) * time.Hour)})
	}
	return ms
}
func e7List(t *testing.T, s *Service, ms []model.Message) []Effort {
	t.Helper()
	putRound2(t, s, ms)
	xs, err := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	return xs
}
func TestE7BotsNeverSeedJoinOrBecomeOnYou(t *testing.T) {
	for _, shape := range []string{"menu", "repeated template", "numeric name", "business", "alias"} {
		t.Run(shape, func(t *testing.T) {
			s := qualitySeed(t, nil)
			ms := e7Messages(testkit.Phone, "Please review the festival budget proposal", "Festival budget proposal requires your response", "Festival budget proposal is due tomorrow")
			for i := range ms {
				ms[i].IsFromMe = false
				ms[i].Timestamp = testkit.Epoch.AddDate(0, 0, -3+i)
			}
			switch shape {
			case "menu", "alias":
				menu := ms[0]
				menu.ID = "menu"
				menu.Content = "Type Hi to go back to the Main Menu. Please select from the options below."
				ms = append(ms, menu)
				if shape == "alias" {
					menu.Sender = "900099@lid"
					ms[len(ms)-1] = menu
					_, err := s.Hub.Messages.DB.Exec("INSERT INTO lid_aliases(lid,pn) VALUES(?,?)", menu.Sender, testkit.Phone)
					testkit.Must(t, err)
				}
			case "repeated template":
				ms = nil
				for i := 0; i < 10; i++ {
					m := e7Messages(testkit.Phone, "Please review the festival budget proposal and confirm your subscription to our exclusive finance community today.")[0]
					m.ID = fmt.Sprint(i)
					m.Timestamp = testkit.Epoch.AddDate(0, 0, -i)
					ms = append(ms, m)
				}
			case "numeric name":
				testkit.Must(t, s.Hub.Messages.PutChat(model.Chat{JID: testkit.Phone, Name: "15550100999"}))
			case "business":
				testkit.Must(t, s.Hub.Repo.Put("people", "business", model.Person{ID: "business", JIDs: []string{testkit.Phone}, Business: true}))
			}
			if shape == "numeric name" {
				for _, m := range ms {
					testkit.Must(t, s.Hub.Messages.PutMessage(m))
				}
			} else {
				putRound2(t, s, ms)
			}
			xs, err := s.List(nil, Filter{IncludeUntitled: true})
			testkit.Must(t, err)
			if len(xs) != 0 {
				t.Fatalf("bot seeded %d efforts", len(xs))
			}
		})
	}
	// A menu arriving after an apparently real task retracts the old effort and
	// queue, including when it was explicitly named before recognition.
	s := qualitySeed(t, nil)
	ms := e7Messages(testkit.Phone, "Festival budget proposal?", "Festival budget proposal agreed", "Festival budget proposal due tomorrow")
	xs := e7List(t, s, ms)
	if len(xs) != 1 {
		t.Fatal("missing baseline")
	}
	title := "Festival budget"
	_, err := s.Update(nil, xs[0].ID, Update{Title: &title})
	testkit.Must(t, err)
	bot := ms[0]
	bot.ID = "later-menu"
	bot.Timestamp = testkit.Epoch
	bot.Content = "Please select from the options below to return to the main menu"
	testkit.Must(t, s.Hub.Messages.PutMessage(bot))
	xs, err = s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("cached or assigned bot effort survived")
	}
	_, total, err := s.Unnamed(nil, 100)
	testkit.Must(t, err)
	if total != 0 {
		t.Fatal("bot entered naming queue")
	}
	_, err = s.Regroup(nil, "attach", "missing", "", []Ref{ref(bot)})
	if err != model.ErrNotFound {
		t.Fatal("unexpected attach result")
	}
	cold, err := (&Service{Hub: s.Hub}).Audit()
	testkit.Must(t, err)
	warm, err := s.Audit()
	testkit.Must(t, err)
	if !reflect.DeepEqual(cold, warm) {
		t.Fatal("bot cache audit differs")
	}
}
func TestE7LargeGroupsRequireThreadInvolvement(t *testing.T) {
	for _, shape := range []string{"broadcast", "mention", "reply", "name request", "name without request", "owner thread", "unrelated owner", "other mention", "small group", "observed large group"} {
		t.Run(shape, func(t *testing.T) {
			s := qualitySeed(t, nil)
			settings := s.Hub.Settings()
			settings.Owner = "Avery Vale"
			testkit.Must(t, s.Hub.SetSettings(settings))
			size := 60
			if shape == "small group" || shape == "observed large group" {
				size = 8
			}
			testkit.Must(t, s.Hub.Repo.Put("groups", testkit.Group, model.Group{JID: testkit.Group, MemberCount: size}))
			ms := e7Messages(testkit.Group, "Please review the lantern workshop proposal today", "Lantern workshop proposal venue is ready", "Lantern workshop proposal is due tomorrow")
			for i := range ms {
				ms[i].IsFromMe = false
				ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -2+i)
			}
			want := 1
			switch shape {
			case "broadcast", "name without request", "other mention", "unrelated owner", "observed large group":
				want = 0
			}
			switch shape {
			case "mention":
				ms[0].Content = "@owner " + ms[0].Content
			case "reply":
				owner := ms[0]
				owner.ID = "owner-original"
				owner.Content = "Who has the latest copy?"
				owner.IsFromMe = true
				owner.Timestamp = owner.Timestamp.Add(-time.Hour)
				ms = append([]model.Message{owner}, ms...)
				ms[1].QuotedMessageID = owner.ID
			case "name request":
				ms[0].Content = "Avery, " + ms[0].Content
			case "name without request":
				ms[1].Content += " Avery attended yesterday"
			case "owner thread":
				ms[1].IsFromMe = true
			case "unrelated owner":
				m := ms[0]
				m.ID = "unrelated-owner"
				m.IsFromMe = true
				m.Content = "I left my umbrella near the cafeteria"
				ms = append(ms, m)
			case "other mention":
				ms[0].Content = "@someoneelse " + ms[0].Content
			case "observed large group":
				for i := 0; i < 9; i++ {
					m := ms[0]
					m.ID = fmt.Sprint("spectator-", i)
					m.Sender = fmt.Sprintf("155509%05d@s.whatsapp.net", i)
					m.Content = "hello"
					ms = append(ms, m)
				}
			}
			xs := e7List(t, s, ms)
			if len(xs) != want {
				t.Fatalf("got %d efforts want %d", len(xs), want)
			}
			if want == 1 && shape == "mention" {
				if xs[0].Status == "on_you" {
					t.Fatal("later update is not a new ask")
				}
			}
		})
	}
}
func TestE7FragmentedGoalsMergeWithoutGlue(t *testing.T) {
	for _, job := range []struct {
		goal  string
		parts []string
	}{
		{"lantern", []string{"workshop proposal", "workshop collaboration", "workshop slides", "workshop venue", "workshop schedule"}},
		{"juniper", []string{"client pitch", "client pricing", "client slides", "client demo", "client proposal"}},
		{"willow", []string{"house walkthrough", "house measurements", "house rendering"}},
	} {
		t.Run(job.goal, func(t *testing.T) {
			s := qualitySeed(t, nil)
			ms := []model.Message{}
			for i, part := range job.parts {
				for j, text := range []string{"Please review the " + job.goal + " " + part, "Quick update: " + job.goal + " " + part + " is ready for review", "Also, " + job.goal + " " + part + " is confirmed"} {
					m := e7Messages(testkit.Phone, text)[0]
					m.ID = fmt.Sprintf("part-%d-%d", i, j)
					m.IsFromMe = j == 1
					m.Timestamp = testkit.Epoch.Add(-time.Duration(30-i*3-j) * time.Hour)
					ms = append(ms, m)
				}
			}
			xs := e7List(t, s, ms)
			if len(xs) != 1 || !exactRound2(xs[0], ms) {
				t.Fatalf("fragmented %s: efforts=%d", job.goal, len(xs))
			}
			if !xs[0].FirstAt.Equal(ms[0].Timestamp) || !xs[0].LastAt.Equal(ms[len(ms)-1].Timestamp) {
				t.Fatal("lost time bounds")
			}
		})
	}
	// Acknowledgements, including quotes to other acknowledgements, cannot
	// introduce new terms/people or link unrelated direct chats.
	s := qualitySeed(t, nil)
	ms := []model.Message{}
	for chat := 0; chat < 3; chat++ {
		for i, text := range []string{"Yeah for sure", "yeah sure", "yeah that is for sure", "👍 😂", "sure", "don't mute me pls", "can I take a pimple patch"} {
			m := e7Messages(fmt.Sprintf("155502%05d@s.whatsapp.net", chat), text)[0]
			m.ID = fmt.Sprint(i)
			ms = append(ms, m)
		}
	}
	if xs := e7List(t, s, ms); len(xs) != 0 {
		t.Fatal("filler or banter formed an effort")
	}
}
func TestE7QuietAssignmentDismissAndAgeWindow(t *testing.T) {
	s := qualitySeed(t, nil)
	testkit.Must(t, s.Hub.Repo.Put("circles", "study", model.Circle{ID: "study", Name: "Study", Confirmed: true, Chats: []model.CircleChat{{ChatJID: testkit.Phone}}}))
	ms := e7Messages(testkit.Phone, "Please review the estuary assignment outline", "Estuary assignment outline references are ready", "Estuary assignment outline needs another review")
	for i := range ms {
		ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -30)
	}
	xs := e7List(t, s, ms)
	if len(xs) != 1 || xs[0].Status != "dormant" {
		t.Fatal("quiet assignment lost or active")
	}
	updates, err := s.CircleUpdates()
	testkit.Must(t, err)
	if len(updates) != 0 {
		t.Fatal("quiet assignment surfaced on Home")
	}
	hide := true
	_, err = s.Update(nil, xs[0].ID, Update{Hidden: &hide})
	testkit.Must(t, err)
	xs, err = (&Service{Hub: s.Hub}).List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("dismiss did not persist")
	}
	s = qualitySeed(t, nil)
	for i := range ms {
		ms[i].Timestamp = ms[i].Timestamp.AddDate(0, 0, -61)
	}
	if xs = e7List(t, s, ms); len(xs) != 0 {
		t.Fatal("older than 90 days retained")
	}
}
func TestE7AuditRemainsCountsOnly(t *testing.T) {
	s := qualitySeed(t, nil)
	e7List(t, s, e7Messages(testkit.Phone, "Please review the estuary assignment outline", "Estuary assignment outline is ready", "Estuary assignment outline confirmed"))
	audit, err := s.Audit()
	testkit.Must(t, err)
	data, err := json.Marshal(audit)
	testkit.Must(t, err)
	for _, secret := range []string{"estuary", "assignment", testkit.Phone, "e7-", "Maya"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("audit contains identifying content")
		}
	}
}

func TestE7SharedArtefactAndCrossChatFollowup(t *testing.T) {
	for _, shape := range []string{"shared artefact", "same people followup", "unrelated people"} {
		t.Run(shape, func(t *testing.T) {
			s := qualitySeed(t, nil)
			ms := e7Messages(testkit.Phone, "Please review the lantern workshop proposal due tomorrow", "Lantern workshop proposal opening is ready", "Lantern workshop proposal requires review")
			next := e7Messages(testkit.Group, "Also, lantern slides rehearsal needs a review", "Lantern slides rehearsal opening is ready", "Lantern slides rehearsal confirmed")
			for i := range next {
				next[i].Timestamp = next[i].Timestamp.Add(30 * time.Minute)
			}
			if shape == "shared artefact" {
				ms[0].MediaType = "document"
				ms[0].Filename = "Lantern-Workshop-Proposal.pdf"
				next[0].Content = "Please review the copper exhibition layout"
				next[0].MediaType = "document"
				next[0].Filename = ms[0].Filename
				next[1].Content = "Copper exhibition layout is ready"
				next[2].Content = "Copper exhibition layout confirmed"
			}
			if shape == "unrelated people" {
				for i := range next {
					next[i].Sender = "15550209999@s.whatsapp.net"
					next[i].IsFromMe = i == 1
				}
			}
			all := append(ms, next...)
			xs := e7List(t, s, all)
			want := 1
			if shape == "unrelated people" {
				want = 2
			}
			if len(xs) != want {
				t.Fatalf("got %d efforts want %d", len(xs), want)
			}
			if want == 1 && !exactRound2(xs[0], all) {
				t.Fatal("lost fragment evidence")
			}
		})
	}
}

func TestE7FillerCannotBecomeAQuotedBridge(t *testing.T) {
	s := qualitySeed(t, nil)
	ms := e7Messages(testkit.Phone, "Please review the estuary survey due tomorrow", "Estuary survey measurements are ready", "Estuary survey approved")
	xs := e7List(t, s, ms)
	if len(xs) != 1 {
		t.Fatal("missing real task")
	}
	ack := ms[2]
	ack.ID = "ack"
	ack.Content = "Yeah for sure"
	ack.QuotedMessageID = ms[0].ID
	ack.Timestamp = testkit.Epoch.Add(-20 * time.Minute)
	bridge := ack
	bridge.ID = "bridge"
	bridge.Content = "Please prepare the cobalt poster for the exhibition"
	bridge.QuotedMessageID = ack.ID
	bridge.Timestamp = testkit.Epoch.Add(-10 * time.Minute)
	testkit.Must(t, s.Hub.Messages.PutMessage(ack))
	testkit.Must(t, s.Hub.Messages.PutMessage(bridge))
	xs, err := s.List(nil, Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(xs) != 1 {
		t.Fatal("filler created another effort")
	}
	for _, r := range xs[0].MessageRefs {
		if r.MessageID == bridge.ID {
			t.Fatal("filler admitted a new task topic")
		}
	}
}

func TestE7BroadcastCannotSuppressSameGoalInDirectChat(t *testing.T) {
	s := qualitySeed(t, nil)
	testkit.Must(t, s.Hub.Repo.Put("groups", testkit.Group, model.Group{JID: testkit.Group, MemberCount: 60}))
	dm := e7Messages(testkit.Phone, "Please review the estuary assignment outline", "Estuary assignment outline is ready", "Estuary assignment outline requires review")
	broadcast := e7Messages(testkit.Group, "Estuary assignment outline is due tomorrow", "Estuary assignment outline is ready", "Estuary assignment outline requires review")
	for i := range broadcast {
		broadcast[i].IsFromMe = false
		broadcast[i].Timestamp = broadcast[i].Timestamp.Add(-time.Hour)
	}
	xs := e7List(t, s, append(append([]model.Message{}, broadcast...), dm...))
	if len(xs) != 1 || !exactRound2(xs[0], dm) {
		t.Fatal("uninvolved broadcast suppressed or polluted direct work")
	}
}
