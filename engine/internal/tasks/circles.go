package tasks

import (
	"context"
	"database/sql"
	"limmr/internal/hub"
	"limmr/internal/model"
)

// Matches remain attributed suggestions until the owner edits placement. An empty
// owner choice is intentional and must survive restarts and later inference.
func (s *Service) suggestCircles(a *model.Agent, t *Task) error {
	if t.CirclesEdited || len(t.CircleIDs) > 0 {
		return nil
	}
	circles, err := hub.List[model.Circle](s.Hub.Repo, "circles")
	if err != nil {
		return err
	}
	chatMatch := false
	for _, c := range circles {
		if c.Archived || !s.Hub.CircleAccess(a, c) {
			continue
		}
		for _, ch := range c.Chats {
			for _, linked := range t.Chats {
				if ch.ChatJID == linked.ID && !s.Hub.HiddenChat(ch.ChatJID) {
					chatMatch = true
				}
			}
		}
	}
	t.CircleSuggestions = nil
	for _, c := range circles {
		if c.Archived || !s.Hub.CircleAccess(a, c) || model.Contains(t.CircleIDs, c.ID) {
			continue
		}
		reason := ""
		for _, ch := range c.Chats {
			for _, linked := range t.Chats {
				if ch.ChatJID == linked.ID && !s.Hub.HiddenChat(ch.ChatJID) {
					reason = "A linked chat is in this circle"
				}
			}
		}
		if reason == "" && !chatMatch {
			for _, p := range c.People {
				for _, linked := range t.People {
					if linked.ID != "me" && p.PersonID == linked.ID {
						reason = "A linked person is in this circle"
					}
				}
			}
		}
		if reason != "" {
			t.CircleIDs = append(t.CircleIDs, c.ID)
			t.CircleSuggestions = append(t.CircleSuggestions, CircleSuggestion{CircleID: c.ID, Reason: reason})
		}
	}
	return nil
}
func (s *Service) BackfillCircles(ctx context.Context) error {
	rs, err := s.records()
	if err != nil {
		return err
	}
	for _, old := range rs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if old.Purged || old.CirclesEdited {
			continue
		}
		candidate := old.Task
		if err := s.suggestCircles(nil, &candidate); err != nil {
			return err
		}
		if len(candidate.CircleIDs) == len(old.CircleIDs) {
			continue
		}
		_, err = s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
			if r.CirclesEdited {
				return nil
			}
			r.CircleIDs = candidate.CircleIDs
			r.CircleSuggestions = candidate.CircleSuggestions
			r.UpdatedAt = s.now()
			return s.event(tx, *r, "circle_suggestions", "circles_suggested", "Suggested task circles", map[string]string{"by": "This Mac"})
		})
		if err != nil {
			return err
		}
	}
	return nil
}
