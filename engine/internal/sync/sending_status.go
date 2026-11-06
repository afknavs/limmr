package sync

import "time"

// SendingStatus is safe for whoami: no tasks, chat names, merge reviews or
// per-device credentials are exposed to an ordinary AI profile.
func (s *Service) SendingStatus() map[string]any {
	var p Peer
	var l Lease
	_ = s.Repo.Get("settings", "sync_peer", &p)
	_ = s.Repo.Get("settings", "automation_lease", &l)
	paired := p.EngineID != ""
	side := "standalone"
	summary := "This device holds sending; no peer is paired."
	active := s.standaloneReady()
	if !paired && !active {
		summary = "Sending is paused after unpairing. Wait for the former lease to expire and confirm that the previous sending device has stopped."
	}
	if paired {
		side = "peer"
		summary = "The paired device holds sending. New sends wait for both devices to sync."
		if l.EngineID == s.EngineID {
			side = "this_device"
			summary = "This device holds sending. New sends wait for the peer to acknowledge them."
		}
		active = l.Epoch > 0 && s.Now().Before(l.ExpiresAt) && !p.Paused
		if !active {
			summary = "Sending is paused; both devices queue new sends. Sync to renew the lease."
		}
		var request struct {
			Epoch uint64 `json:"epoch"`
		}
		if side == "peer" && s.Repo.Get("send_control", s.EngineID, &request) == nil && request.Epoch == l.Epoch {
			summary = "Sending handover requested. The current holder must sync before this device can send."
		}
	}
	var last any
	if !p.LastSyncAt.IsZero() {
		last = p.LastSyncAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{"holder_side": side, "summary": summary, "last_synced_at": last, "lease_expires_at": l.ExpiresAt, "lease_epoch": l.Epoch, "lease_active": active, "peer_ack_required": paired}
}
