package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"limmr/internal/model"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

func (s *Service) DraftSendKey(ctx context.Context, a model.Agent, kind, chat, text, voice, reply, attachment, key string, options ...SendOptions) (model.Draft, error) {
	return s.draftSend(ctx, a, kind, chat, text, voice, reply, attachment, true, nil, key, options...)
}
func (s *Service) keyedDraft(agent, key string) (model.Draft, error) {
	var raw string
	e := s.Repo.DB.QueryRow(`SELECT data FROM objects WHERE bucket='drafts' AND json_extract(data,'$.agent_id')=? AND json_extract(data,'$.key')=? AND json_extract(data,'$.created_at')>? ORDER BY json_extract(data,'$.created_at') LIMIT 1`, agent, key, s.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&raw)
	if e != nil { // Distinguish absence from storage errors.
		if e == sql.ErrNoRows {
			return model.Draft{}, model.ErrNotFound
		}
		return model.Draft{}, e
	}
	var d model.Draft
	e = json.Unmarshal([]byte(raw), &d)
	return d, e
}

// Outbox retention is run at engine startup and on every upload. No polling loop.
func (s *Service) PruneOutbox() {
	dir := filepath.Join(s.Home, "outbox")
	xs, e := os.ReadDir(dir)
	if e != nil {
		return
	}
	cutoff := s.Now().Add(-7 * 24 * time.Hour)
	for _, x := range xs {
		if x.IsDir() || x.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, e := x.Info()
		if e == nil && info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(dir, x.Name())) == nil {
				_ = s.Repo.Delete("attachments", x.Name())
				_ = s.Repo.Delete("attachment_owners", x.Name())
			}
		}
	}
}

// Receipt events only update outbound records; this never acknowledges or marks a chat read.
func (s *Service) RecordReceipt(chat, sender string, ids []string, status string, at time.Time) error {
	if status != "delivered" && status != "read" {
		return nil
	}
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	for _, id := range ids {
		for _, bucket := range []string{"drafts", "approvals"} {
			var raw string
			if e := s.Repo.DB.QueryRow(`SELECT data FROM objects WHERE bucket=? AND json_extract(data,'$.chat_jid')=? AND json_extract(data,'$.message_id')=? LIMIT 1`, bucket, chat, id).Scan(&raw); e != nil {
				if e == sql.ErrNoRows {
					continue
				}
				return e
			}
			var d model.Draft
			if bucket == "approvals" {
				var ap model.Approval
				if e := json.Unmarshal([]byte(raw), &ap); e != nil {
					return e
				}
				d = approvalDraft(ap)
			} else if e := json.Unmarshal([]byte(raw), &d); e != nil {
				return e
			}
			if d.SentAt == nil {
				continue
			}
			var seen map[string]string
			_ = s.Repo.Get("send_receipts", d.ID, &seen)
			if seen == nil {
				seen = map[string]string{}
			}
			if seen[sender] == "read" || seen[sender] == status {
				continue
			}
			seen[sender] = status
			d.DeliveredCount = len(seen)
			d.ReadCount = 0
			for _, v := range seen {
				if v == "read" {
					d.ReadCount++
				}
			}
			stamp := at.UTC()
			if stamp.Before(*d.SentAt) {
				stamp = *d.SentAt
			}
			if d.DeliveredAt == nil || stamp.Before(*d.DeliveredAt) {
				d.DeliveredAt = &stamp
			}
			if status == "read" && (d.ReadAt == nil || stamp.Before(*d.ReadAt)) {
				d.ReadAt = &stamp
			}
			d.Status = "delivered"
			if d.ReadCount > 0 {
				d.Status = "read"
			}
			var value any = d
			if bucket == "approvals" {
				var ap model.Approval
				if e := json.Unmarshal([]byte(raw), &ap); e != nil {
					return e
				}
				ap.Status = d.Status
				ap.DeliveredAt = d.DeliveredAt
				ap.ReadAt = d.ReadAt
				ap.DeliveredCount = d.DeliveredCount
				ap.ReadCount = d.ReadCount
				value = ap
			}
			if e := s.Repo.Apply(Mutation{Bucket: bucket, ID: d.ID, Value: value}, Mutation{Bucket: "send_receipts", ID: d.ID, Value: seen}); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) SendStatus(a model.Agent, message, approval, key string) (model.Draft, error) {
	count := 0
	for _, v := range []string{message, approval, key} {
		if v != "" {
			count++
		}
	}
	if count != 1 || utf8.RuneCountInString(key) > 128 {
		return model.Draft{}, model.ErrInvalid
	}
	var d model.Draft
	var e error
	if approval != "" {
		e = s.Repo.Get("drafts", approval, &d)
		if e == model.ErrNotFound {
			var ap model.Approval
			e = s.Repo.Get("approvals", approval, &ap)
			if e == nil {
				d = approvalDraft(ap)
			}
		}
	} else if key != "" {
		d, e = s.keyedDraft(a.ID, key)
	} else {
		var raw, bucket string
		e = s.Repo.DB.QueryRow(`SELECT bucket,data FROM objects WHERE bucket IN ('drafts','approvals') AND json_extract(data,'$.agent_id')=? AND json_extract(data,'$.message_id')=? LIMIT 1`, a.ID, message).Scan(&bucket, &raw)
		if e == nil {
			if bucket == "approvals" {
				var ap model.Approval
				e = json.Unmarshal([]byte(raw), &ap)
				d = approvalDraft(ap)
			} else {
				e = json.Unmarshal([]byte(raw), &d)
			}
		}
	}
	if e != nil || d.AgentID != a.ID || !s.CanChat(&a, d.ChatJID) || a.RunChat != "" && a.RunChat != d.ChatJID {
		return model.Draft{}, model.ErrNotFound
	}
	if d.Status == "" {
		d.Status = "queued"
		if d.State == "sent" || d.State == "failed" {
			d.Status = d.State
		}
	}
	if d.State == "denied" || d.State == "expired" || d.Status == "queued" && d.State != "sending" && !s.Now().Before(d.ExpiresAt) {
		d.Status = "failed"
	}
	return d, nil
}

func approvalDraft(a model.Approval) model.Draft {
	return model.Draft{AsDocument: a.AsDocument, ID: a.ID, AgentID: a.AgentID, ChatJID: a.ChatJID, FinalText: a.Text, Status: a.Status, MessageID: a.MessageID, ApprovalID: a.ID, State: a.State, SentAt: a.SentAt, DeliveredAt: a.DeliveredAt, ReadAt: a.ReadAt, DeliveredCount: a.DeliveredCount, ReadCount: a.ReadCount, CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, Attachment: a.Attachment, ReplyTo: a.ReplyTo, Kind: a.Kind}
}
