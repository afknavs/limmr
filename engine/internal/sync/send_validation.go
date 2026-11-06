package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/attachments"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"reflect"
)

func immutableApproval(a model.Approval) model.Approval {
	a.Upload = nil
	a.SentAt = nil
	a.Status = ""
	return a
}

// Coordination records use semantic ordering before HLC ordering. An old clock
// or a delayed page must never resurrect a queued send or an earlier lease epoch.
func (s *Service) coordinationChange(tx *sql.Tx, c Change) (bool, error) {
	var raw string
	if c.Bucket == "settings" && c.ID == "automation_lease" {
		var next, old Lease
		if c.Op != "upsert" || json.Unmarshal(c.Data, &next) != nil || next.EngineID == "" {
			return false, ErrInvalid
		}
		if tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw) == nil {
			if json.Unmarshal([]byte(raw), &old) != nil {
				return false, ErrInvalid
			}
			if next.Epoch < old.Epoch {
				return false, nil
			}
			if next.Epoch == old.Epoch && next.EngineID != old.EngineID {
				return false, nil
			}
			if next.Epoch > old.Epoch && old.Epoch > 0 {
				if next.Epoch != old.Epoch+1 {
					return false, ErrInvalid
				}
				recovery := next.OwnerRecovery && c.Origin == next.EngineID && !next.PriorExpiresAt.IsZero() && !next.RenewedAt.Before(next.PriorExpiresAt.Add(ClockSkew))
				if c.Origin != old.EngineID && !recovery {
					return false, ErrInvalid
				}
				if e := s.fenceLocalClaims(tx, next.Epoch); e != nil {
					return false, e
				}
			}
		}
	}
	switch c.Bucket {
	case "send_queue":
		var q, old QueuedSend
		if c.Op != "upsert" || json.Unmarshal(c.Data, &q) != nil || !safeID(q.ID) || q.ID != c.ID || q.ID != q.Approval.ID || q.Approval.MessageID == "" || q.Origin == "" || phaseRank(q.Phase) < 0 || q.Parts < 0 || q.Parts > 256 {
			return false, ErrInvalid
		}
		if q.Approval.Attachment != nil {
			a := q.Approval.Attachment
			if !safeID(a.ID) || a.Size < 1 || a.Size > attachments.MaxDocument || q.Parts < 1 {
				return false, ErrInvalid
			}
		}
		if e := tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_queue' AND id=?`, c.ID).Scan(&raw); e == nil {
			if json.Unmarshal([]byte(raw), &old) != nil {
				return false, ErrInvalid
			}
			if old.Origin != q.Origin || old.AgentName != q.AgentName || old.Parts != q.Parts || !reflect.DeepEqual(immutableApproval(old.Approval), immutableApproval(q.Approval)) {
				return false, ErrInvalid
			}
			reset := old.Phase == "claimed" && q.Phase == "queued" && q.Epoch > old.Epoch
			if phaseRank(old.Phase) > phaseRank(q.Phase) && !reset || old.Phase == "sent" && q.Phase != "sent" {
				return false, nil
			}
			if old.Epoch > q.Epoch {
				return false, nil
			}
		}
		if q.ObservedBy != "" && (q.Phase != "sent" || q.ObservedBy != c.Origin) {
			return false, ErrInvalid
		}
		if q.Phase != "queued" && c.Origin != q.Holder && !(q.Phase == "sent" && q.ObservedBy == c.Origin) {
			return false, ErrInvalid
		}
	case "send_files":
		var f SendFile
		if c.Op != "upsert" || json.Unmarshal(c.Data, &f) != nil || !safeID(f.SendID) || f.Index < 0 || f.Index >= 256 || len(f.Data) == 0 || len(f.Data) > hub.MaxChunkDecoded || dbutil.Digest(string(f.Data)) != f.SHA256 || c.ID != f.SendID+"."+fmt.Sprint(f.Index) {
			return false, ErrInvalid
		}
		if tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_files' AND id=?`, c.ID).Scan(&raw) == nil && raw != string(c.Data) {
			var old SendFile
			_ = json.Unmarshal([]byte(raw), &old)
			if old.SHA256 != f.SHA256 {
				return false, ErrInvalid
			}
		}
	case "send_acks":
		var ack SendAck
		var q QueuedSend
		if c.Op != "upsert" || json.Unmarshal(c.Data, &ack) != nil || !safeID(ack.ID) || ack.ID != c.ID || ack.Holder == "" || c.Origin == ack.Holder {
			return false, ErrInvalid
		}
		if tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_queue' AND id=?`, ack.ID).Scan(&raw) != nil {
			return false, ErrInvalid
		}
		if json.Unmarshal([]byte(raw), &q) != nil || q.Epoch != ack.Epoch || q.Holder != ack.Holder {
			return false, nil
		}
	case "send_control":
		var req struct {
			Requester string `json:"requester"`
			Epoch     uint64 `json:"epoch"`
		}
		if c.Op != "upsert" || json.Unmarshal(c.Data, &req) != nil || req.Requester != c.ID || req.Requester != c.Origin || req.Epoch == 0 {
			return false, ErrInvalid
		}
	}
	return true, nil
}
