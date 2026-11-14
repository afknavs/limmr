package tasks

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/wa"
)

var finishedWords = regexp.MustCompile(`\b(done|submitted|sent|paid|finished|completed|handed\s+in|booked)\b`)
var unfinishedWords = regexp.MustCompile(`\b(not|never|no|haven't|hasn't|hadn't|isn't|wasn't|weren't|didn't|don't|doesn't|cannot|can't|won't|will|going\s+to|gonna|plan\s+to|planning\s+to|need\s+to|about\s+to|should|would|could)\b`)

func saysFinished(text string) bool {
	text = strings.ReplaceAll(strings.ToLower(text), "’", "'")
	return !strings.ContainsAny(text, "?？") && !unfinishedWords.MatchString(text) && finishedWords.MatchString(text)
}

func ptrTime(at time.Time) *time.Time { return &at }
func (s *Service) ownerTaskActivity(r *record) {
	r.FinishHint = nil
	r.FinishHintAfter = s.now()
}
func (s *Service) currentFinishHint(t Task) *FinishHint {
	if ended(t) || t.Status == "suggested" || t.FinishHint == nil || !s.now().Before(t.FinishHint.At.Add(7*24*time.Hour)) {
		return nil
	}
	return t.FinishHint
}

func (s *Service) finishTask(tx *sql.Tx, r *record, before json.RawMessage, reason, closeReason string, evidence any) error {
	closeTask(r, s.now(), closeReason)
	// Retire existing review drafts silently, just as the ordinary close path does.
	if _, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','dismissed') WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", r.ID); err != nil {
		return err
	}
	return s.saveRuleChange(tx, r, before, "finished", "Ended", reason, evidence, MembershipChange{})
}

type finishMessage struct {
	message model.Message
	refers  bool
}

// Use the duplicate matcher's tokenizer, excluding grammatical/completion words
// so a title such as "Get it done" cannot make an unrelated "done" strong.
func taskTokenReference(t Task, text string) bool {
	words := coverWords(t.Title + " " + t.Goal)
	generic := coverWords("done submitted sent paid finished completed handed booked you have has had was were are get got been being will tomorrow today please already yet")
	for word := range coverWords(text) {
		if words[word] && !generic[word] {
			return true
		}
	}
	return false
}

// A filename is not file identity. Compare the stored message ref or actual
// content hash, including uploaded task items, without opening any file.
func (s *Service) taskFileReference(t Task, m model.Message) bool {
	if m.MediaType == "" || m.MediaType == "reaction" {
		return false
	}
	var hash []byte
	_ = s.Hub.Messages.DB.QueryRow("SELECT file_sha256 FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", m.ChatJID, m.ID).Scan(&hash)
	for _, it := range t.Items {
		if it.Kind != "file" {
			continue
		}
		if it.MessageRef != nil {
			ref := it.MessageRef
			if ref.ChatJID == m.ChatJID && ref.MessageID == m.ID {
				return true
			}
			if s.Hub.HiddenChat(ref.ChatJID) {
				continue
			}
			var other []byte
			_ = s.Hub.Messages.DB.QueryRow("SELECT file_sha256 FROM messages WHERE chat_jid=? AND id=? AND deleted_at IS NULL", ref.ChatJID, ref.MessageID).Scan(&other)
			if len(hash) > 0 && bytes.Equal(hash, other) {
				return true
			}
		}
		if len(hash) > 0 && it.AttachmentID != "" {
			var attachment model.Attachment
			if s.Hub.Repo.Get("attachments", it.AttachmentID, &attachment) == nil && strings.EqualFold(attachment.SHA256, hex.EncodeToString(hash)) {
				return true
			}
		}
	}
	return false
}

func (s *Service) finishMessages(t Task) ([]finishMessage, time.Time, error) {
	out := []finishMessage{}
	var ownerAt time.Time
	for _, chat := range t.Chats {
		if model.Contains(t.RemovedMembers.Chats, chat.ID) || s.Hub.HiddenChat(chat.ID) {
			continue
		}
		// Pagination avoids making an old task's result depend on the last 1000 messages.
		for offset := 0; ; offset += 1000 {
			ms, err := s.Hub.Messages.Messages(wa.Query{Chat: chat.ID, After: t.CreatedAt.Format(time.RFC3339Nano), Limit: 1000, Offset: offset}, []string{chat.ID})
			if err != nil {
				return nil, ownerAt, err
			}
			for _, m := range ms {
				if m.DeletedAt != nil || !m.Timestamp.After(t.CreatedAt) || m.Timestamp.After(s.now()) {
					continue
				}
				m, err = s.Hub.Attribute(m)
				if err != nil {
					return nil, ownerAt, err
				}
				if m.Author.Type == "owner" && m.Timestamp.After(ownerAt) {
					ownerAt = m.Timestamp
				}
				if !saysFinished(m.Content) {
					continue
				}
				refers := taskTokenReference(t, m.Content)
				for _, ref := range taskMessageRefs(t) {
					if ref.ChatJID == m.ChatJID && ref.MessageID == m.QuotedMessageID && m.QuotedMessageID != "" {
						refers = true
					}
				}
				refers = refers || s.taskFileReference(t, m)
				out = append(out, finishMessage{m, refers})
			}
			if len(ms) < 1000 {
				break
			}
		}
	}
	return out, ownerAt, nil
}

// evaluateFinished is shared with the protocol planner. It changes only the
// supplied in-memory record's hint fields and returns strong completion evidence.
func evaluateFinished(r *record, messages []finishMessage, ownerAt time.Time, effortStatus string, now time.Time) *model.Message {
	previousHintAfter := r.FinishHintAfter
	cutoff := r.FinishHintAfter
	if ownerAt.After(cutoff) {
		cutoff = ownerAt
		r.FinishHintAfter = ownerAt
		if r.FinishHint != nil && ownerAt.After(r.FinishHint.At) {
			r.FinishHint = nil
		}
	}
	if r.FinishHint != nil && !now.Before(r.FinishHint.At.Add(7*24*time.Hour)) {
		r.FinishHint = nil
		cutoff = now
		r.FinishHintAfter = cutoff
	}
	var weak *model.Message
	for _, candidate := range messages {
		m := candidate.message
		// Owner reopen and rule undo are sticky for fourteen days. Evidence from
		// before that correction is never replayed once the window elapses.
		if m.Author.Type == "owner" && candidate.refers && (r.ReopenedAt == nil || !now.Before(r.ReopenedAt.Add(14*24*time.Hour)) && m.Timestamp.After(*r.ReopenedAt)) {
			return &m
		}
		if (m.Author.Type != "owner" && candidate.refers || m.Author.Type == "owner" && !candidate.refers) && !m.Timestamp.Before(cutoff) && (weak == nil || m.Timestamp.After(weak.Timestamp)) {
			// Equal timestamps are allowed for the current owner message, but not an
			// already acknowledged/expired message from a previous hint.
			if m.Timestamp.After(previousHintAfter) {
				copy := m
				weak = &copy
			}
		}
	}
	if effortStatus == "" {
		r.FinishHintEffort = ""
	}
	if r.FinishHint == nil {
		if weak != nil {
			why := "Someone else said this work is finished. Ask the owner before closing it."
			if weak.Author.Type == "owner" {
				why = "You used a completion word without a clear task reference. Ask the owner before closing it."
			}
			r.FinishHint = &FinishHint{Why: why, MessageRefs: []Ref{{ChatJID: weak.ChatJID, MessageID: weak.ID}}, At: now}
			r.FinishHintAfter = weak.Timestamp
		} else if effortStatus != "" && r.FinishHintEffort != effortStatus {
			r.FinishHint = &FinishHint{Why: "The linked effort is " + effortStatus + ". Ask the owner before closing it.", MessageRefs: []Ref{}, At: now}
		}
	}
	r.FinishHintEffort = effortStatus
	return nil
}

func checklistFinished(t Task, paused bool) bool {
	if !open(t) || paused || t.EndCondition != "checklist_done" || len(t.Checklist) == 0 {
		return false
	}
	for _, step := range t.Checklist {
		if !step.Done {
			return false
		}
	}
	return true
}

func suggestionExpired(t Task, now time.Time) bool {
	return t.Status == "suggested" && t.Suggestion != nil && !now.Before(t.Suggestion.DetectedAt.AddDate(0, 0, 14))
}

// Only done/dormant efforts are weak completion evidence, never moving/on_you.
func finishedEffortStatus(status string) string {
	if status == "done" || status == "dormant" {
		return status
	}
	return ""
}

// Attribution and effort reads happen outside the single-connection hub writer.
// Recheck the snapshot inside the transaction; a concurrent edit waits for next tick.
func (s *Service) applyFinished(old record) (bool, error) {
	var reopened string
	if err := s.Hub.Repo.DB.QueryRow("SELECT coalesce(max(at),'') FROM task_events WHERE task_id=? AND kind='reopened'", old.ID).Scan(&reopened); err != nil {
		return false, err
	}
	reopenedAt, _ := time.Parse(time.RFC3339Nano, reopened)
	messages, ownerAt, err := s.finishMessages(old.Task)
	if err != nil {
		return false, err
	}
	effortStatus := ""
	if old.EffortID != "" && s.Efforts != nil {
		effort, e := s.Efforts.Get(nil, old.EffortID)
		if e == nil {
			effortStatus = finishedEffortStatus(effort.Status)
		}
	}
	finished := false
	_, err = s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
		r.silentChange = true
		if !duplicateOpen(r.Task) || dbutil.JSON(r) != dbutil.JSON(old) {
			return nil
		}
		before := json.RawMessage(dbutil.JSON(r))
		if !reopenedAt.IsZero() && (r.ReopenedAt == nil || reopenedAt.After(*r.ReopenedAt)) {
			r.ReopenedAt = ptrTime(reopenedAt)
		}
		if m := evaluateFinished(r, messages, ownerAt, effortStatus, s.now()); m != nil {
			finished = true
			return s.finishTask(tx, r, before, "you said “"+short(m.Content, 80)+"”", "done", map[string]any{"message_refs": []Ref{{ChatJID: m.ChatJID, MessageID: m.ID}}})
		}
		return nil
	})
	return finished, err
}
