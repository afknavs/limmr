package hub

import (
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"time"
)

func (s *Service) CurrentSince() time.Time {
	return s.Now().UTC().AddDate(0, 0, -s.Settings().CurrentDays)
}
func (s *Service) Current(c model.Chat) bool {
	return c.LastMessageTime != nil && !c.LastMessageTime.Before(s.CurrentSince())
}

// ResetCircles removes only circle context and records the owner action atomically.
// People, their edits/evidence, chats, credentials and the activity log survive.
func (s *Service) ResetCircles() error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	s.shared().activityMu.Lock()
	defer s.shared().activityMu.Unlock()
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, bucket := range []string{"circles", "proposals", "proposal_batches", "dismissed_proposals", "briefs", "cursors", "catch_pages", "done_asks", "understanding_changes", "auto_circle_changes", "circle_additions"} {
		if _, e = tx.Exec("DELETE FROM objects WHERE bucket=?", bucket); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("DELETE FROM objects WHERE bucket='suggestions' AND COALESCE(json_extract(data,'$.circle_id'),'')<>''"); e != nil {
		return e
	}
	a := model.Activity{At: s.Now().UTC(), AgentID: "you", Tool: "reset_circles", Kind: "people_write", ChatJIDs: []string{}, MessageIDs: []string{}, Note: "Started circles over. Chats and people kept."}
	r, e := tx.Exec("INSERT INTO activity(at,data) VALUES(?,?)", a.At, dbutil.JSON(a))
	if e != nil {
		return e
	}
	a.ID, _ = r.LastInsertId()
	if e = tx.Commit(); e != nil {
		return e
	}
	for ch := range s.shared().watchers {
		select {
		case ch <- a:
		default:
		}
	}
	return nil
}
