package circles

import (
	"database/sql"
	"limmr/internal/model"
	"limmr/internal/tasks"
)

// ReviewSuggestions keeps old records available for restore/history while
// projecting the same task snooze as cards and People markers.
func (s *Service) ReviewSuggestions() ([]model.Suggestion, error) {
	xs, err := s.Hub.Suggestions()
	if err != nil {
		return nil, err
	}
	out := []model.Suggestion{}
	for _, x := range xs {
		if x.TaskID != "" {
			var raw string
			err := s.Hub.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", x.TaskID).Scan(&raw)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
			if err == nil {
				task, err := tasks.ProjectAttentionData(s.Hub, raw)
				if err != nil {
					return nil, err
				}
				if task.LaterUntil != nil && s.Hub.Now().Before(*task.LaterUntil) {
					continue
				}
			}
		}
		if x.CircleID != "" {
			var c model.Circle
			if err := s.Hub.Repo.Get("circles", x.CircleID, &c); err == nil && !c.Live() {
				if x.TaskID == "" {
					continue
				}
				x.CircleID = ""
			}
		}
		out = append(out, x)
	}
	return out, nil
}
