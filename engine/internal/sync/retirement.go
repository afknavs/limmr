package sync

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"time"
)

// Per-device state, deliberately excluded from knowledge sync. Clearing a
// pairing must never turn a partitioned non-holder into a standalone sender.
type retirement struct {
	Required     bool      `json:"required"`
	NotBefore    time.Time `json:"not_before"`
	OwnerStopped bool      `json:"owner_stopped"`
}

func (s *Service) standaloneReady() bool {
	var r retirement
	e := s.Repo.Get("settings", "sending_retirement", &r)
	return e == model.ErrNotFound || e == nil && (!r.Required || r.OwnerStopped && !s.Now().Before(r.NotBefore))
}
func (s *Service) standaloneReadyTx(tx *sql.Tx) bool {
	var raw string
	var r retirement
	e := tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sending_retirement'`).Scan(&raw)
	return e == sql.ErrNoRows || e == nil && json.Unmarshal([]byte(raw), &r) == nil && (!r.Required || r.OwnerStopped && !s.Now().Before(r.NotBefore))
}
func (s *Service) recoverStandalone(stopped bool) error {
	if !stopped {
		return model.ErrForbidden
	}
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var raw string
	var r retirement
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sending_retirement'`).Scan(&raw); e != nil {
		return e
	}
	if json.Unmarshal([]byte(raw), &r) != nil || !r.Required || s.Now().Before(r.NotBefore) {
		return model.ErrConflict
	}
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM objects WHERE bucket='send_queue' AND json_extract(data,'$.phase') IN ('claimed','uncertain')`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return model.ErrConflict
	}
	r.OwnerStopped = true
	if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='settings' AND id='sending_retirement'`, dbutil.JSON(r)); e != nil {
		return e
	}
	return tx.Commit()
}
