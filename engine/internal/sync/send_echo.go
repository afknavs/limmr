package sync

import "limmr/internal/wa"

// Passive local storage evidence only: this does not fetch media, mark messages
// read, or make a WhatsApp request. Both linked devices can observe the same ID.
func (s *Service) observeSendEcho(q *QueuedSend) bool {
	if s.Hub == nil || s.Hub.Messages == nil || q.Phase != "uncertain" && q.Phase != "claimed" {
		return false
	}
	messages, e := s.Hub.Messages.Messages(wa.Query{Chat: q.Approval.ChatJID, ID: q.Approval.MessageID, Limit: 1}, []string{q.Approval.ChatJID})
	if e != nil || len(messages) != 1 || !messages[0].IsFromMe {
		return false
	}
	q.Phase = "sent"
	q.ObservedBy = s.EngineID
	q.Approval.Status = "sent"
	at := messages[0].Timestamp
	q.Approval.SentAt = &at
	return s.Repo.Put("send_queue", q.ID, q) == nil
}
