package demo_test

import (
	"context"
	"encoding/json"
	"limmr/internal/circles"
	"limmr/internal/demo"
	"limmr/internal/health"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/triggers"
	"limmr/internal/wa"
	"limmr/internal/watches"
	"testing"
	"time"
)

func TestRichDemoAndIdempotentRestart(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	for i := 0; i < 2; i++ {
		ps, e := p.People(nil, "")
		testkit.Must(t, e)
		gs, e := p.Groups(nil, "")
		testkit.Must(t, e)
		as, e := h.Agents()
		testkit.Must(t, e)
		c, m := h.Messages.Counts()
		acts, e := h.Activity(nil, hub.ActivityFilter{Limit: 500})
		testkit.Must(t, e)
		if len(ps) != 13 || len(gs) != 13 || len(as) != 5 || c != 30 || m != 79 || h.PendingCount() != 0 || len(acts) != 8 {
			t.Fatal("demo counts drifted")
		}
		messages, err := h.Messages.Messages(wa.Query{Chat: demo.Group(5), Limit: 20}, []string{demo.Group(5)})
		testkit.Must(t, err)
		foundSticker := false
		for _, message := range messages {
			if message.MediaType == "sticker" {
				encoded, err := json.Marshal(message)
				testkit.Must(t, err)
				var fields map[string]json.RawMessage
				testkit.Must(t, json.Unmarshal(encoded, &fields))
				if _, present := fields["content"]; present {
					t.Fatal("captionless demo sticker must omit content on the wire")
				}
				foundSticker = true
			}
		}
		if !foundSticker {
			t.Fatal("demo group needs a captionless sticker")
		}
		warnings := 0
		hs := &health.Service{Hub: h, Link: l, Home: h.Home, Demo: true}
		for _, check := range hs.Checks(context.Background()) {
			if check.State == "warn" {
				warnings++
			}
		}
		if warnings != 1 || l.State().State != "ready" {
			t.Fatal("demo state drifted")
		}
		testkit.Must(t, demo.Seed(h, p, l))
	}
}

func TestCircleDemoRelationshipsAndBriefEvidence(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	s := &circles.Service{Hub: h, People: p}
	cs, e := s.List(nil)
	testkit.Must(t, e)
	sg, e := h.Suggestions()
	testkit.Must(t, e)
	proposals, e := s.Proposals()
	testkit.Must(t, e)
	if len(cs) != 7 || len(sg) != 3 || len(proposals) != 4 {
		t.Fatal("circle demo invariants")
	}
	roles := map[string]map[string]string{}
	for _, c := range cs {
		roles[c.ID] = map[string]string{}
		for _, person := range c.People {
			roles[c.ID][person.PersonID] = person.Role
		}
		b, e := s.Latest(nil, c.ID)
		testkit.Must(t, e)
		if b == nil || len(b.Upcoming) == 0 || len(b.PeopleNotes) == 0 || len(b.Evidence) == 0 {
			t.Fatal("incomplete demo brief")
		}
		for _, id := range b.Evidence {
			found := false
			for _, j := range circles.JIDs(c) {
				ms, e := h.Messages.Messages(wa.Query{Chat: j, ID: id, Limit: 1}, []string{j})
				testkit.Must(t, e)
				if len(ms) == 1 {
					found = true
				}
			}
			if !found {
				t.Fatal("invented evidence id missing from fixture")
			}
		}
	}
	if roles["university"]["person-04"] != "classmate" || roles["atlas"]["person-04"] != "materials collaborator" {
		t.Fatal("classmate/project overlap missing")
	}
	if roles["university"]["person-05"] != "club member" || roles["flatmates"]["person-05"] != "flatmate" {
		t.Fatal("club/flatmate overlap missing")
	}
	atlas, e := s.Circle(nil, "atlas")
	testkit.Must(t, e)
	if len(atlas.Topics) != 2 {
		t.Fatal("project topics missing")
	}
	plan, e := s.Plan()
	testkit.Must(t, e)
	if len(plan.Circles) != 7 {
		t.Fatal("demo refresh plan")
	}
	if s.Settings().Enabled || s.Settings().Time != "06:00" {
		t.Fatal("nightly default changed")
	}
}

func TestDemoWatchesContractAndFakeSessionWake(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	clock := &demoClock{at: demo.Epoch}
	s, err := watches.New(h, p, watches.Options{Runner: demo.WatchRunner{}, Clock: clock, MCPURL: "http://127.0.0.1:18800/mcp"})
	testkit.Must(t, err)
	defer s.Engine.Close()
	testkit.Must(t, demo.SeedWatches(s))
	testkit.Must(t, demo.SeedWatches(s))
	ctx := context.Background()
	xs, err := s.List(ctx, true)
	testkit.Must(t, err)
	if len(xs) != 3 {
		t.Fatal("demo watch seed duplicated or missing")
	}
	exam, err := s.Detail(ctx, "w_exam")
	testkit.Must(t, err)
	if exam.Title != "Exam news" || exam.Where != "University" || exam.NewHits != 2 || exam.Latest == nil || exam.Latest.Sender != "Dr. Morgan" {
		t.Fatal("exam watch contract drifted")
	}
	atlas, err := s.Record(ctx, "w_atlas")
	testkit.Must(t, err)
	if len(atlas.Scope.People) != 2 || atlas.Action != triggers.Digest {
		t.Fatal("Atlas is not a two-person digest")
	}
	website, err := s.Detail(ctx, "w_website")
	testkit.Must(t, err)
	if website.Kind != "handoff" || website.Status != triggers.Active || website.Runner == nil || website.Runner.Kind != "codex" {
		t.Fatal("website handoff missing")
	}
	testkit.Must(t, s.Handle(ctx, wa.StoredEvent{Message: model.Message{ID: "demo-live-message", ChatJID: demo.DM(12), Sender: demo.DM(12), Content: "Please update the website layout", Timestamp: demo.Epoch}}))
	clock.at = clock.at.Add(time.Minute)
	testkit.Must(t, s.Engine.FlushDue(ctx))
	deadline := time.Now().Add(3 * time.Second)
	for {
		runs, err := s.Store.Runs(ctx, "w_website")
		testkit.Must(t, err)
		if len(runs) == 1 && runs[0].State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("demo fake runner did not finish")
		}
		time.Sleep(time.Millisecond * 5)
	}
	if s.Check().State != "ok" {
		t.Fatal("demo watch health degraded")
	}
}

type demoClock struct{ at time.Time }

func (c *demoClock) Now() time.Time                         { return c.at }
func (c *demoClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func TestUnderstandingDemoContract(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	s := &circles.Service{Hub: h, People: p}
	out, e := s.Map()
	testkit.Must(t, e)
	m := out.(circles.UnderstandingMap)
	people, circleCount, topics := 0, 0, 0
	nodes := map[string]circles.MapNode{}
	for _, n := range m.Nodes {
		nodes[n.ID] = n
		if n.Weight < 0 || n.Weight > 1 || n.Initials == "" || n.Circles == nil {
			t.Fatal("node contract")
		}
		switch n.Kind {
		case "person":
			people++
		case "circle":
			circleCount++
		case "topic":
			topics++
		}
	}
	if people != 13 || circleCount != 11 || topics != 2 {
		t.Fatalf("demo map nodes people=%d circles=%d topics=%d", people, circleCount, topics)
	}
	if len(nodes["person-04"].Circles) < 3 || len(nodes["person-05"].Circles) < 3 {
		t.Fatal("overlap missing")
	}
	pending, byAI, thisWeek := 0, 0, 0
	for _, edge := range m.Edges {
		if edge.ID == "" || edge.Origin.By == "" || edge.Strength < 0 || edge.Strength > 1 || nodes[edge.Source].ID == "" || nodes[edge.Target].ID == "" {
			t.Fatal("edge contract")
		}
		if edge.State == "suggested" {
			pending++
		}
		if edge.Origin.ByKind == "agent" {
			byAI++
		}
		if edge.Origin.At.After(demo.Epoch.Add(-7 * 24 * time.Hour)) {
			thisWeek++
		}
	}
	if pending != 4 || byAI != m.Summary.ByAI || m.Summary.Connections != len(m.Edges) || thisWeek != m.Summary.ThisWeek {
		t.Fatal("summary or pending edges")
	}
	aiEvents := map[string]bool{}
	days := map[string]bool{}
	for _, ev := range m.Events {
		if ev.By == "Codex" || ev.By == "Claude Code" || ev.By == "ChatGPT" {
			aiEvents[ev.By] = true
		}
		days[ev.At.Format("2006-01-02")] = true
	}
	if len(m.Events) < 12 || len(m.Events) > 24 || len(aiEvents) != 3 || len(days) < 8 || !m.Events[0].At.After(demo.Epoch.Add(-time.Hour)) {
		t.Fatal("two-week understanding story")
	}
	out, e = s.MapSince(m.Events[0].At)
	testkit.Must(t, e)
	incremental := out.(circles.UnderstandingMap)
	if len(incremental.Events) != 0 || len(incremental.Nodes) != len(m.Nodes) || len(incremental.Edges) != len(m.Edges) || incremental.Summary != m.Summary {
		t.Fatal("incremental map contract")
	}
	out, e = s.Presence()
	testkit.Must(t, e)
	as := out.(map[string]any)["agents"].([]circles.AgentPresence)
	if len(as) != 1 || as[0].Name != "Codex" || as[0].State != "reading" || as[0].Target != "University" || len(as[0].TargetIDs) != 1 || as[0].TargetIDs[0] != "university" {
		t.Fatal("demo presence")
	}
}

func TestRound10DemoAccessBatchAndSetup(t *testing.T) {
	h, p, l := testkit.Open(t)
	testkit.Must(t, demo.Seed(h, p, l))
	s := &circles.Service{Hub: h, People: p}
	a, e := h.Agent("acme-assistant")
	testkit.Must(t, e)
	if a.Name != "Acme Assistant" || a.Transport != "remote" || a.Access.Unsorted || len(a.Access.Circles) != 1 || a.Access.Circles[0] != "work" || a.Access.Until == nil || !a.Access.Until.Equal(demo.Epoch.AddDate(0, 0, 30)) {
		t.Fatal("Acme grant")
	}
	chats, _, e := h.Visible(&a)
	testkit.Must(t, e)
	if len(chats) != 3 {
		t.Fatal("Acme chat projection")
	}
	for _, c := range chats {
		if c.JID == demo.Group(1) || c.JID == demo.DM(2) || c.JID == demo.DM(3) {
			t.Fatal("Family leaked")
		}
	}
	ended, e := h.Agent("ended-assistant")
	testkit.Must(t, e)
	if !ended.Access.Ended(h.Now()) {
		t.Fatal("ended demo")
	}
	proposals, e := s.Proposals()
	testkit.Must(t, e)
	if len(proposals) != 4 {
		t.Fatal("four-circle batch")
	}
	for _, p := range proposals {
		if p.CreatedBy != "agent-3" || p.BatchID == "" || p.BatchID != proposals[0].BatchID {
			t.Fatal("ChatGPT batch")
		}
	}
	status, e := s.Setup(nil, true)
	testkit.Must(t, e)
	if status.Stage != "sorting" || status.Circles.Proposed != 4 {
		t.Fatal("demo setup")
	}
}
