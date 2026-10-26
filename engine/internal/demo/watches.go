package demo

import (
	"context"
	"limmr/internal/triggers"
	"limmr/internal/watches"
	"time"
)

// Demo mode never launches installed AI CLIs, even for MCP-created handoffs.
type WatchRunner struct{}

func (WatchRunner) Run(ctx context.Context, req triggers.RunRequest) (triggers.RunResult, error) {
	return triggers.RunResult{ExitCode: 0}, ctx.Err()
}

type WatchClock struct{}

func (WatchClock) Now() time.Time                         { return Epoch }
func (WatchClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func SeedWatches(s *watches.Service) error {
	ctx := context.Background()
	var done bool
	if s.Hub.Repo.Get("demo_meta", "watches_v3", &done) == nil {
		return nil
	}
	defs := []triggers.Watch{
		{ID: "w_exam", Title: "Exam news", Goal: "Tell me when anyone posts about the exam", Scope: triggers.Scope{Circles: []string{"university"}}, Match: triggers.Match{Kind: triggers.Keywords, Keywords: []string{"exam", "IA"}}, Action: triggers.Notify},
		{ID: "w_atlas", Title: "Atlas updates", Goal: "Keep me updated about Atlas from Casey and Robin", Scope: triggers.Scope{People: []string{"person-11", "person-12"}}, Match: triggers.Match{Kind: triggers.FromPeople, People: []string{"person-11", "person-12"}}, Action: triggers.Digest},
		{ID: "w_website", Title: "Website changes", Goal: "Help Robin refine the project website", Scope: triggers.Scope{Chats: []string{DM(12)}}, Match: triggers.Match{Kind: triggers.Any}},
	}
	for _, w := range defs {
		w.Owner = "agent-2"
		w.CreatedVia = triggers.Provenance{Agent: "agent-2", GrantText: w.Goal, SessionID: "demo-website"}
		var err error
		if w.ID == "w_website" {
			_, err = s.Engine.StartHandoff(ctx, triggers.Handoff{Watch: w, HandoffConfig: triggers.HandoffConfig{Runner: triggers.RunnerConfig{Kind: "codex", SessionID: "demo-website", Workdir: "/tmp/limmr-demo-website"}, SendGrant: triggers.SendGrant{ChatJID: DM(12)}}})
		} else {
			_, err = s.Engine.CreateWatch(ctx, w)
		}
		if err != nil {
			return err
		}
	}
	for i, text := range []string{"IA date moved to 24 Oct", "Exam reading list updated"} {
		if err := s.Store.PutHit(ctx, triggers.Hit{ID: []string{"exam_hit_1", "exam_hit_2"}[i], WatchID: "w_exam", At: Epoch.Add(-time.Duration(2-i) * time.Minute), Action: triggers.Notify, State: "completed", Messages: []triggers.IncomingMessage{{ID: []string{"exam_msg_1", "exam_msg_2"}[i], ChatJID: Group(3), Sender: DM(7), SenderPersonID: "person-07", Text: text, At: Epoch.Add(-time.Duration(2-i) * time.Minute)}}}); err != nil {
			return err
		}
	}
	if err := s.Store.SetDigest(ctx, "w_atlas", "Casey has the darker samples ready. Robin wants to compare them at the site visit."); err != nil {
		return err
	}
	return s.Hub.Repo.Put("demo_meta", "watches_v3", true)
}
