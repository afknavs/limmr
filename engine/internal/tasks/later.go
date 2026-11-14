package tasks

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"time"
)

// DefaultLater uses calendar arithmetic in the Mac's zone, including DST days.
func (s *Service) DefaultLater() time.Time {
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	now := s.now().In(loc)
	day := now
	if now.Hour() >= 4 {
		day = day.AddDate(0, 0, 1)
	}
	return localSlot(day, "09:00", loc)
}

func (s *Service) Later(id string, until *time.Time) (Task, error) {
	at := s.DefaultLater()
	if until != nil {
		at = *until
	}
	if !at.After(s.now()) {
		return Task{}, model.ErrInvalid
	}
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if !open(r.Task) {
			return model.ErrConflict
		}
		if r.LaterUntil != nil && r.LaterUntil.Equal(at) {
			return nil
		}
		now := s.now()
		r.LaterAt, r.LaterUntil = &now, &at
		r.UpdatedAt = now
		return s.event(tx, *r, dbutil.ID(), "task_later", "Left for later until "+at.Format("Mon 15:04"), nil)
	})
}
func (s *Service) BringBack(id string) (Task, error) {
	return s.mutate(nil, id, func(r *record, tx *sql.Tx) error {
		if r.LaterUntil == nil {
			return nil
		}
		r.LaterUntil = nil
		r.UpdatedAt = s.now()
		return s.event(tx, *r, dbutil.ID(), "task_brought_back", "Brought back", nil)
	})
}

// attentionAt is the single expiry/snooze rule for every task attention surface.
// Stored NeedsYou remains the underlying ask so expiry of a snooze needs no write.
func attentionAt(r record, now time.Time) Task {
	t := r.Task
	at := r.CreatedAt
	if r.NeedsYouAt != nil {
		at = *r.NeedsYouAt
	}
	if t.Status == "suggested" || ended(t) || !at.IsZero() && !now.Before(at.Add(model.NeedsYouTTL)) || t.LaterUntil != nil && now.Before(*t.LaterUntil) {
		t.NeedsYou, t.NeedsYouReason = false, ""
	}
	return t
}
func (s *Service) projectAttention(r record) Task {
	// New review items may arrive between runtime ticks. Project them immediately;
	// refreshNeeds persists the cleared snooze on the next mutation.
	if r.LaterAt != nil && r.LaterUntil != nil {
		var n int
		err := s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending' AND julianday(json_extract(data,'$.created_at'))>julianday(?)", r.ID, *r.LaterAt).Scan(&n)
		if err == nil && n > 0 {
			r.LaterUntil = nil
			r.NeedsYou = true
		}
	}
	return attentionAt(r, s.now())
}

func ProjectAttentionData(h *hub.Service, raw string) (Task, error) {
	var r record
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return Task{}, err
	}
	return (&Service{Hub: h}).projectAttention(r), nil
}
