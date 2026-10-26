package demo

import (
	"context"
	"limmr/internal/tasks"
)

func SeedTasks(s *tasks.Service) error {
	var done bool
	if s.Hub.Repo.Get("demo_meta", "tasks_v1", &done) == nil {
		return seedTaskSurfacing(s)
	}
	a, err := s.Hub.Agent("agent-2")
	if err != nil {
		return err
	}
	day := int(Epoch.Weekday())
	_, err = s.Create(&a, tasks.Create{Title: "Maya's study plan", Goal: "Make revision material and track learning", Kind: "long", Chats: []tasks.Entity{{ID: DM(4)}}, People: []tasks.Entity{{ID: "person-04", Name: "Maya Rao"}}, Checklist: []tasks.Step{{Text: "Collect syllabus", Done: true}, {Text: "Prepare revision notes"}}, CheckIn: &tasks.CheckIn{Rule: "weekly", Weekday: &day, AtTime: "17:00", To: DM(4), Note: "Learning progress and next topic", NextAt: Epoch.AddDate(0, 0, 7)}})
	if err != nil {
		return err
	}
	_, err = s.Create(&a, tasks.Create{Title: "Book a dentist appointment", Kind: "short", Chats: []tasks.Entity{{ID: DM(7)}}, Checklist: []tasks.Step{{Text: "Choose an appointment time"}}})
	if err != nil {
		return err
	}
	t, err := s.Create(&a, tasks.Create{Title: "Wait for the project syllabus", Kind: "long", Chats: []tasks.Entity{{ID: DM(4)}}, People: []tasks.Entity{{ID: "person-04", Name: "Maya Rao"}}})
	if err != nil {
		return err
	}
	_, err = s.Update(&a, t.ID, map[string]any{"waiting": tasks.Waiting{On: &tasks.Entity{ID: "person-04", Name: "Maya Rao"}, What: "the project syllabus", Since: Epoch.AddDate(0, 0, -4), NudgeAfterDays: 3}})
	if err != nil {
		return err
	}
	if err = s.Tick(context.Background()); err != nil {
		return err
	}
	if err = seedTaskSurfacing(s); err != nil {
		return err
	}
	return s.Hub.Repo.Put("demo_meta", "tasks_v1", true)
}

func seedTaskSurfacing(s *tasks.Service) error {
	var done bool
	if s.Hub.Repo.Get("demo_meta", "task_surfacing_v1", &done) == nil {
		return nil
	}
	for _, v := range []struct {
		title, signal string
		chat          int
	}{
		{"Print the revision guide", "You discussed exam material on three different days", 6},
		{"Prepare the fundraiser application", "The application has a deadline", 9},
	} {
		if _, err := s.Suggest(nil, tasks.Create{Title: v.title, Kind: "long", Chats: []tasks.Entity{{ID: DM(v.chat)}}}, tasks.Suggestion{Signals: []string{v.signal}, Confidence: 0.7, DetectedAt: Epoch, EffortIDs: []string{}}); err != nil {
			return err
		}
	}
	a, err := s.Hub.Agent("agent-2")
	if err != nil {
		return err
	}
	a.Preset = "own"
	if _, err = s.SuggestForAgent(context.Background(), &a, tasks.Create{Title: "Review the family CV", Kind: "long", Chats: []tasks.Entity{{ID: DM(10)}}}, tasks.Suggestion{Signals: []string{"Atlas has sent 4 messages here in the last 14 days"}, Confidence: 0.75, DetectedAt: Epoch, EffortIDs: []string{}}); err != nil {
		return err
	}
	return s.Hub.Repo.Put("demo_meta", "task_surfacing_v1", true)
}
