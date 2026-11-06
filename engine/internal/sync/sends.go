package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"limmr/internal/attachments"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type QueuedSend struct {
	ObservedBy string         `json:"observed_by,omitempty"`
	ID         string         `json:"id"`
	Origin     string         `json:"origin"`
	Approval   model.Approval `json:"approval"`
	Phase      string         `json:"phase"`
	Epoch      uint64         `json:"epoch"`
	Holder     string         `json:"holder"`
	AgentName  string         `json:"agent_name"`
	Parts      int            `json:"parts"`
	At         time.Time      `json:"at"`
}
type SendFile struct {
	SendID string `json:"send_id"`
	Index  int    `json:"index"`
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}
type SendAck struct {
	ID     string `json:"id"`
	Epoch  uint64 `json:"epoch"`
	Holder string `json:"holder"`
}

func safeID(id string) bool {
	return id != "" && len(id) <= 128 && filepath.Base(id) == id && id != "." && id != ".." && !strings.ContainsAny(id, "/\\")
}
func phaseRank(p string) int {
	switch p {
	case "queued":
		return 0
	case "claimed":
		return 1
	case "sent", "uncertain", "expired":
		return 2
	}
	return -1
}

// Bind wraps the real messenger at the last common dispatch boundary. Tests bind
// a fake. CLI-only sync never constructs a WhatsApp client.
func (s *Service) Bind(h *hub.Service) { s.Hub = h; s.Messenger = h.Messenger; h.Messenger = s }
func (s *Service) Send(ctx context.Context, a model.Approval) (string, error) {
	if !safeID(a.ID) || a.MessageID == "" {
		return "", model.ErrInvalid
	}
	var existing QueuedSend
	if s.Repo.Get("send_queue", a.ID, &existing) == nil {
		if existing.Phase == "sent" {
			return existing.Approval.MessageID, nil
		}
		return "", hub.ErrSendQueued
	}
	q := QueuedSend{ID: a.ID, Origin: s.EngineID, Approval: a, Phase: "queued", At: s.Now()}
	if s.Hub != nil {
		if agent, e := s.Hub.Agent(a.AgentID); e == nil {
			q.AgentName = agent.Name
		}
	}
	mutations := []hub.Mutation{}
	if a.Attachment != nil {
		if !safeID(a.Attachment.ID) {
			return "", model.ErrInvalid
		}
		b, e := os.ReadFile(filepath.Join(s.Home, "outbox", a.Attachment.ID))
		if e != nil {
			return "", e
		}
		if dbutil.Digest(string(b)) != a.Attachment.SHA256 || int64(len(b)) != a.Attachment.Size {
			return "", model.ErrInvalid
		}
		if e = attachments.CheckSize(int64(len(b)), attachments.MIME(b, a.Attachment.Name)); e != nil {
			return "", e
		}
		// Bound the retained pending queue; terminal identity rows remain forever.
		all, e := hub.List[QueuedSend](s.Repo, "send_queue")
		if e != nil {
			return "", e
		}
		total := int64(len(b))
		for _, x := range all {
			if phaseRank(x.Phase) < 2 && x.Approval.Attachment != nil {
				total += x.Approval.Attachment.Size
			}
		}
		if total > hub.MaxUploadReserved {
			return "", fmt.Errorf("Queued files exceed 256 MiB. Wait for existing sends to finish or send a link.")
		}
		for offset := 0; offset < len(b); offset += hub.MaxChunkDecoded {
			part := b[offset:min(offset+hub.MaxChunkDecoded, len(b))]
			f := SendFile{a.ID, q.Parts, part, dbutil.Digest(string(part))}
			mutations = append(mutations, hub.Mutation{Bucket: "send_files", ID: a.ID + "." + strconv.Itoa(q.Parts), Value: f})
			q.Parts++
		}
	}
	mutations = append(mutations, hub.Mutation{Bucket: "send_queue", ID: q.ID, Value: q})
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	result, e := tx.Exec(`INSERT OR IGNORE INTO objects VALUES('send_queue',?,?)`, q.ID, dbutil.JSON(q))
	if e != nil {
		return "", e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return "", e
	}
	if n == 0 {
		return "", hub.ErrSendQueued
	}
	for _, m := range mutations {
		if m.Bucket == "send_queue" {
			continue
		}
		if _, e = tx.Exec(`INSERT OR IGNORE INTO objects VALUES(?,?,?)`, m.Bucket, m.ID, dbutil.JSON(m.Value)); e != nil {
			return "", e
		}
	}
	if e = tx.Commit(); e != nil {
		return "", e
	}
	if s.paired() || !s.standaloneReady() {
		return "", hub.ErrSendQueued
	}
	// Standalone keeps the existing synchronous behavior, using the same ledger.
	if e := s.claim(&q); e != nil {
		return "", e
	}
	e = s.transmit(ctx, &q)
	if e != nil {
		return "", e
	}
	return q.Approval.MessageID, nil
}
func (s *Service) paired() bool {
	var p Peer
	return s.Repo.Get("settings", "sync_peer", &p) == nil && p.EngineID != ""
}
func (s *Service) queueFile(q QueuedSend) error {
	a := q.Approval.Attachment
	if a == nil {
		if q.Parts != 0 {
			return ErrInvalid
		}
		return nil
	}
	if !safeID(a.ID) || a.Size < 1 || a.Size > attachments.MaxDocument || q.Parts < 1 || q.Parts > 256 {
		return ErrInvalid
	}
	b := make([]byte, 0, int(a.Size))
	for i := 0; i < q.Parts; i++ {
		var f SendFile
		if e := s.Repo.Get("send_files", q.ID+"."+strconv.Itoa(i), &f); e != nil {
			return e
		}
		if f.SendID != q.ID || f.Index != i || len(f.Data) > hub.MaxChunkDecoded || dbutil.Digest(string(f.Data)) != f.SHA256 || int64(len(b)+len(f.Data)) > a.Size {
			return ErrInvalid
		}
		b = append(b, f.Data...)
	}
	if int64(len(b)) != a.Size || dbutil.Digest(string(b)) != a.SHA256 {
		return ErrInvalid
	}
	if e := attachments.CheckSize(a.Size, attachments.MIME(b, a.Name)); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Join(s.Home, "outbox"), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Join(s.Home, "outbox"), ".queued-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e == nil {
		e = closed
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(s.Home, "outbox", a.ID)); e != nil {
		return e
	}
	return s.Repo.Put("attachments", a.ID, a)
}
func (s *Service) claim(q *QueuedSend) error {
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var peer Peer
	var raw string
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&raw); e == nil {
		if json.Unmarshal([]byte(raw), &peer) != nil {
			return ErrInvalid
		}
	}
	var lease Lease
	if peer.EngineID == "" && !s.standaloneReadyTx(tx) {
		return model.ErrConflict
	}
	if peer.EngineID != "" {
		if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw); e != nil {
			return e
		}
		if json.Unmarshal([]byte(raw), &lease) != nil || lease.EngineID != s.EngineID || !s.Now().Before(lease.ExpiresAt) || peer.Paused {
			return model.ErrConflict
		}
	}
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_queue' AND id=?`, q.ID).Scan(&raw); e != nil {
		return e
	}
	var old QueuedSend
	if json.Unmarshal([]byte(raw), &old) != nil || old.Phase != "queued" {
		return model.ErrConflict
	}
	q.Phase = "claimed"
	q.Epoch = lease.Epoch
	q.Holder = s.EngineID
	if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='send_queue' AND id=?`, dbutil.JSON(q), q.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) transmit(ctx context.Context, q *QueuedSend) error {
	// CAS and epoch check are the final fence; claimed rows block handover on both
	// sides. An ambiguous network outcome is terminal, never automatically retried.
	tx, e := s.Repo.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE sync_state SET clock=clock WHERE id=1`); e != nil {
		return e
	}
	var raw string
	var current QueuedSend
	if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_queue' AND id=?`, q.ID).Scan(&raw); e != nil {
		return e
	}
	if json.Unmarshal([]byte(raw), &current) != nil || current.Phase != "claimed" || current.Holder != s.EngineID {
		return model.ErrConflict
	}
	if !s.pairedTx(tx) && !s.standaloneReadyTx(tx) {
		return model.ErrConflict
	}
	if s.pairedTx(tx) {
		var peer Peer
		if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='sync_peer'`).Scan(&raw); e != nil {
			return e
		}
		if json.Unmarshal([]byte(raw), &peer) != nil || peer.Paused {
			return model.ErrConflict
		}
		var l Lease
		if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw); e != nil {
			return e
		}
		if json.Unmarshal([]byte(raw), &l) != nil || l.EngineID != s.EngineID || l.Epoch != q.Epoch || !s.Now().Before(l.ExpiresAt) {
			return model.ErrConflict
		}
		var ack SendAck
		{
			if e = tx.QueryRow(`SELECT data FROM objects WHERE bucket='send_acks' AND id=?`, q.ID).Scan(&raw); e != nil {
				return e
			}
			if json.Unmarshal([]byte(raw), &ack) != nil || ack.Epoch != q.Epoch || ack.Holder != s.EngineID {
				return model.ErrConflict
			}
		}
	}
	// Persist uncertainty BEFORE crossing the external boundary. This is an
	// at-most-once attempt, including process death before/after server acceptance.
	q.Phase = "uncertain"
	if _, e = tx.Exec(`UPDATE objects SET data=? WHERE bucket='send_queue' AND id=?`, dbutil.JSON(q), q.ID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if s.Messenger == nil {
		return model.ErrConflict
	}
	if _, e = s.Repo.DB.Exec(`INSERT OR IGNORE INTO send_authors VALUES(?,?,?,?)`, q.Approval.ChatJID, q.Approval.MessageID, q.Approval.AgentID, q.AgentName); e != nil {
		return e
	}
	parent := ctx
	ctx = model.WithUploadProgress(ctx, func(p model.UploadProgress) error {
		q.Approval.Upload = &p
		if err := s.Repo.Put("send_queue", q.ID, q); err != nil {
			return err
		}
		return model.ReportUpload(parent, p)
	})
	id, e := s.Messenger.Send(ctx, q.Approval)
	if e == nil {
		if id != q.Approval.MessageID {
			return hub.ErrSendUncertain
		}
		q.Phase = "sent"
		at := s.Now()
		q.Approval.SentAt = &at
		q.Approval.Status = "sent"
	}
	if err := s.Repo.Put("send_queue", q.ID, q); err != nil {
		return err
	}
	if e != nil {
		return hub.ErrSendUncertain
	}
	return nil
}

// Coordinate runs after sync and on the engine timer. Import, acknowledge,
// claim, and delivery are restart-safe; replay never repeats an external call.
func (s *Service) Coordinate(ctx context.Context) error {
	if e := s.processLeaseRequest(); e != nil {
		return e
	}
	all, e := hub.List[QueuedSend](s.Repo, "send_queue")
	if e != nil {
		return e
	}
	for _, q := range all {
		if e = ctx.Err(); e != nil {
			return e
		}
		s.observeSendEcho(&q)
		if q.Phase == "sent" || q.Phase == "uncertain" || q.Phase == "expired" {
			if s.Hub != nil {
				if e = s.Hub.ApplyCoordinatedResult(q.Approval, q.Phase); e != nil {
					return e
				}
			}
			continue
		}
		var l Lease
		_ = s.Repo.Get("settings", "automation_lease", &l)
		if s.paired() && q.Phase == "claimed" && q.Holder != s.EngineID && l.EngineID == q.Holder && l.Epoch == q.Epoch {
			ack := SendAck{q.ID, q.Epoch, q.Holder}
			if e = s.Repo.Put("send_acks", q.ID, ack); e != nil {
				return e
			}
			continue
		}
		if s.Messenger == nil {
			continue
		}
		if s.paired() {
			if l.EngineID != s.EngineID || !s.Now().Before(l.ExpiresAt) {
				continue
			}
		} else if q.Origin != s.EngineID || !s.standaloneReady() {
			continue
		}
		if !q.Approval.ExpiresAt.IsZero() && !s.Now().Before(q.Approval.ExpiresAt) {
			q.Phase = "expired"
			q.Holder = s.EngineID
			q.Epoch = l.Epoch
			if e = s.Repo.Put("send_queue", q.ID, q); e != nil {
				return e
			}
			continue
		}
		if e = s.queueFile(q); e != nil {
			continue
		} // More file pages may still be arriving.
		if q.Phase == "queued" {
			if e = s.claim(&q); e != nil {
				continue
			}
		}
		if q.Holder != s.EngineID {
			continue
		}
		if s.paired() {
			var ack SendAck
			if s.Repo.Get("send_acks", q.ID, &ack) != nil || ack.Epoch != q.Epoch || ack.Holder != s.EngineID {
				continue
			}
		}
		_ = s.transmit(ctx, &q)
		if phaseRank(q.Phase) == 2 && s.Hub != nil {
			if e = s.Hub.ApplyCoordinatedResult(q.Approval, q.Phase); e != nil {
				return e
			}
		}
	}
	return nil
}
