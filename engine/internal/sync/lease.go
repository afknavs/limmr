package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"limmr/internal/hub"
	"time"
)

type Lease struct {
	EngineID  string    `json:"engine_id"`
	RenewedAt time.Time `json:"renewed_at"`
}

func (s *Service) Pair(peer, holder string) error {
	if peer == "" || peer == s.EngineID || (holder != peer && holder != s.EngineID) {
		return ErrInvalid
	}
	var old Peer
	_ = s.Repo.Get("settings", "sync_peer", &old)
	if old.EngineID != "" && old.EngineID != peer {
		return ErrInvalid
	}
	if old.EngineID == peer {
		return nil
	}
	// Both sides record the same holder so a stale lease from an earlier
	// standalone period can never authorize the non-holder.
	return s.Repo.Apply(hub.Mutation{Bucket: "settings", ID: "sync_peer", Value: Peer{EngineID: peer}},
		hub.Mutation{Bucket: "settings", ID: "automation_lease", Value: Lease{EngineID: holder, RenewedAt: s.Now()}})
}
func (s *Service) Unpair() error {
	return s.Repo.Apply( /* Per-machine pairing state is deliberately not synced. */
		// Using a normal local write makes the fresh lease durable for a future pairing.
		hub.Mutation{Bucket: "settings", ID: "sync_peer", Value: Peer{}}, hub.Mutation{Bucket: "settings", ID: "automation_lease", Value: Lease{EngineID: s.EngineID, RenewedAt: s.Now()}})
}

// MayRunAutomations is also the Y3 auto-keep hook. Errors deny execution.
func (s *Service) MayRunAutomations(ctx context.Context) bool {
	tx, err := s.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE sync_state SET clock=clock WHERE id=1`); err != nil {
		return false
	}
	var raw string
	var peer Peer
	err = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&raw)
	if err == sql.ErrNoRows {
		return tx.Commit() == nil
	}
	if err != nil {
		return false
	}
	if json.Unmarshal([]byte(raw), &peer) != nil {
		return false
	}
	if peer.EngineID == "" {
		return tx.Commit() == nil
	}
	// Only the recorded holder runs automations. There is no takeover on
	// silence: a partitioned peer cannot tell a dead holder from an unreachable
	// one, so authority moves only when the holder writes the handoff itself
	// (GiveLease) or the owner unpairs. At most one paired engine ever holds it.
	var lease Lease
	if err = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw); err != nil {
		return false
	}
	if json.Unmarshal([]byte(raw), &lease) != nil || lease.EngineID != s.EngineID {
		return false
	}
	return tx.Commit() == nil
}

// GiveLease hands automations to the paired peer. Only the current holder may
// do this; it stops at once and the peer starts after its next sync, so the
// handoff leaves a gap rather than an overlap.
func (s *Service) GiveLease() error {
	var peer Peer
	var lease Lease
	if e := s.Repo.Get("settings", "sync_peer", &peer); e != nil || peer.EngineID == "" {
		return ErrInvalid
	}
	if e := s.Repo.Get("settings", "automation_lease", &lease); e != nil || lease.EngineID != s.EngineID {
		return ErrInvalid
	}
	return s.Repo.Put("settings", "automation_lease", Lease{EngineID: peer.EngineID, RenewedAt: s.Now()})
}

// HoldsLease reports whether this engine runs automations (always true unpaired).
func (s *Service) HoldsLease() bool {
	var peer Peer
	var lease Lease
	if s.Repo.Get("settings", "sync_peer", &peer) != nil || peer.EngineID == "" {
		return true
	}
	return s.Repo.Get("settings", "automation_lease", &lease) == nil && lease.EngineID == s.EngineID
}
