package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"time"
)

const LeaseDuration = 30 * time.Minute

// ClockSkew is also the minimum wait after expiry for owner recovery.
const ClockSkew = time.Minute

type Lease struct {
	EngineID       string    `json:"engine_id"`
	RenewedAt      time.Time `json:"renewed_at"`
	Epoch          uint64    `json:"epoch"`
	ExpiresAt      time.Time `json:"expires_at"`
	PriorExpiresAt time.Time `json:"prior_expires_at,omitempty"`
	OwnerRecovery  bool      `json:"owner_recovery,omitempty"`
}

func (s *Service) Pair(peer, holder string) error {
	if holder == s.EngineID && !s.standaloneReady() {
		return model.ErrConflict
	}
	if peer == "" || peer == s.EngineID || (holder != peer && holder != s.EngineID) {
		return ErrInvalid
	}
	var old Peer
	_ = s.Repo.Get("settings", "sync_peer", &old)
	if old.EngineID != "" {
		if old.EngineID != peer {
			return ErrInvalid
		}
		return nil
	}
	return s.Repo.Apply(hub.Mutation{Bucket: "settings", ID: "sending_retirement", Value: retirement{}}, hub.Mutation{Bucket: "settings", ID: "sync_peer", Value: Peer{EngineID: peer}}, hub.Mutation{Bucket: "settings", ID: "automation_lease", Value: Lease{EngineID: holder, RenewedAt: s.Now(), Epoch: 1, ExpiresAt: s.Now().Add(LeaseDuration)}})
}
func (s *Service) Unpair() error {
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM objects WHERE bucket='send_queue' AND json_extract(data,'$.phase') IN ('claimed','uncertain')`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return model.ErrConflict
	}
	var oldRaw string
	var old Lease
	var peer Peer
	_ = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&oldRaw)
	_ = json.Unmarshal([]byte(oldRaw), &old)
	_ = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&oldRaw)
	_ = json.Unmarshal([]byte(oldRaw), &peer)
	if peer.EngineID == "" {
		return tx.Commit()
	}
	var retired retirement
	if peer.EngineID != "" && old.EngineID != s.EngineID {
		retired = retirement{Required: true, NotBefore: old.ExpiresAt.Add(ClockSkew)}
		if old.ExpiresAt.IsZero() {
			retired.NotBefore = s.Now().Add(LeaseDuration + ClockSkew)
		}
	}
	lease := Lease{EngineID: s.EngineID, RenewedAt: s.Now(), ExpiresAt: s.Now().Add(LeaseDuration), Epoch: 1}
	for id, value := range map[string]any{"sync_peer": Peer{}, "automation_lease": lease, "sending_retirement": retired} {
		if _, e = tx.Exec(`INSERT INTO objects VALUES('settings',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data`, id, dbutil.JSON(value)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Service) pairedTx(tx *sql.Tx) bool {
	var raw string
	var p Peer
	return tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&raw) == nil && json.Unmarshal([]byte(raw), &p) == nil && p.EngineID != ""
}
func (s *Service) MayRunAutomations(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if !s.paired() {
		return s.standaloneReady()
	}
	var p Peer
	var l Lease
	return s.Repo.Get("settings", "sync_peer", &p) == nil && !p.Paused && s.Repo.Get("settings", "automation_lease", &l) == nil && l.EngineID == s.EngineID && s.Now().Before(l.ExpiresAt)
}
func (s *Service) HoldsLease() bool { return s.MayRunAutomations(context.Background()) }
func (s *Service) noActiveSends() error {
	var n int
	e := s.Repo.DB.QueryRow(`SELECT count(*) FROM objects WHERE bucket='send_queue' AND json_extract(data,'$.phase') IN ('claimed','uncertain')`).Scan(&n)
	if e != nil {
		return e
	}
	if n > 0 {
		return model.ErrConflict
	}
	return nil
}
func (s *Service) moveLease(recoverStopped bool) error {
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var raw string
	var p Peer
	var l Lease
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&raw); e != nil {
		return e
	}
	if json.Unmarshal([]byte(raw), &p) != nil || p.EngineID == "" {
		return ErrInvalid
	}
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw); e != nil {
		return e
	}
	if json.Unmarshal([]byte(raw), &l) != nil {
		return ErrInvalid
	}
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM objects WHERE bucket='send_queue' AND json_extract(data,'$.phase') IN ('claimed','uncertain')`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return model.ErrConflict
	}
	next := p.EngineID
	if recoverStopped {
		if l.EngineID == s.EngineID || l.ExpiresAt.IsZero() || s.Now().Before(l.ExpiresAt.Add(ClockSkew)) {
			return model.ErrConflict
		}
		next = s.EngineID
	} else if l.EngineID != s.EngineID {
		return model.ErrConflict
	}
	prior := l.ExpiresAt
	l = Lease{EngineID: next, Epoch: l.Epoch + 1, RenewedAt: s.Now(), ExpiresAt: s.Now().Add(LeaseDuration), OwnerRecovery: recoverStopped}
	if recoverStopped {
		l.PriorExpiresAt = prior
	}
	if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='settings' AND id='automation_lease'`, dbutil.JSON(l)); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) GiveLease() error { return s.moveLease(false) }

// RecoverLease is owner-only. It requires the explicit statement that the former
// holder has been stopped, expiry plus skew, and no unresolved send attempts.
func (s *Service) RecoverLease(previousHolderStopped bool) error {
	if !s.paired() {
		return s.recoverStandalone(previousHolderStopped)
	}
	if !previousHolderStopped {
		return model.ErrForbidden
	}
	return s.moveLease(true)
}
func (s *Service) RequestLease() error {
	var l Lease
	if s.Repo.Get("settings", "automation_lease", &l) != nil || !s.paired() {
		return model.ErrConflict
	}
	if l.EngineID == s.EngineID {
		return nil
	}
	return s.Repo.Put("send_control", s.EngineID, map[string]any{"requester": s.EngineID, "epoch": l.Epoch})
}
func (s *Service) processLeaseRequest() error {
	var l Lease
	if s.Repo.Get("settings", "automation_lease", &l) != nil || l.EngineID != s.EngineID {
		return nil
	}
	var p Peer
	_ = s.Repo.Get("settings", "sync_peer", &p)
	var req struct {
		Requester string `json:"requester"`
		Epoch     uint64 `json:"epoch"`
	}
	if s.Repo.Get("send_control", p.EngineID, &req) == nil && req.Requester == p.EngineID && req.Epoch == l.Epoch {
		if e := s.GiveLease(); e != nil {
			return nil
		}
	}
	return nil
}

// Renew only after a completed authenticated exchange. Silence never authorizes
// renewal or takeover. An old epoch cannot overwrite a handoff on rejoin.
func (s *Service) renewLease() error {
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var raw string
	var l Lease
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw); e != nil {
		return e
	}
	if json.Unmarshal([]byte(raw), &l) != nil {
		return ErrInvalid
	}
	if l.EngineID != s.EngineID || s.Now().Sub(l.RenewedAt) < 5*time.Minute {
		return tx.Commit()
	}
	l.RenewedAt = s.Now()
	l.ExpiresAt = s.Now().Add(LeaseDuration)
	if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='settings' AND id='automation_lease'`, dbutil.JSON(l)); e != nil {
		return e
	}
	return tx.Commit()
}
