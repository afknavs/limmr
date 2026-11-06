package hub

import (
	"context"
	"errors"
	"limmr/internal/model"
)

var ErrSendUncertain = errors.New("delivery is uncertain; check send_status and reconcile the original message identity before requesting another send")

var ErrSendQueued = errors.New("send queued for the sending device")

// ApplyCoordinatedResult keeps passive receipt progression and records task and
// activity effects once on the originating engine; credentials never replicate.
func (s *Service) ApplyCoordinatedResult(a model.Approval, state string) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var d model.Draft
	if s.Repo.Get("drafts", a.ID, &d) == nil {
		alreadySent := d.State == "sent"
		d.MessageID = a.MessageID
		if !alreadySent {
			d.Status = state
		}
		d.State = state
		d.Upload = a.Upload
		d.SentAt = a.SentAt
		if e := s.Repo.Put("drafts", d.ID, d); e != nil {
			return e
		}
		if state == "sent" && !alreadySent {
			if s.TaskSent != nil {
				if e := s.TaskSent(d); e != nil {
					return e
				}
			}
			if e := s.recordOutbound(context.Background(), d); e != nil {
				return e
			}
			return s.recordUnlocked(model.Activity{AgentID: d.AgentID, Tool: "send_draft", Kind: "sent", ChatJIDs: []string{d.ChatJID}, MessageIDs: []string{d.MessageID}, Count: 1})
		}
		return nil
	}
	var old model.Approval
	if s.Repo.Get("approvals", a.ID, &old) == nil {
		alreadySent := old.State == "sent"
		old.MessageID = a.MessageID
		old.State = state
		if !alreadySent {
			old.Status = state
		}
		old.SentAt = a.SentAt
		old.Upload = a.Upload
		return s.Repo.Put("approvals", old.ID, old)
	}
	return nil
}
