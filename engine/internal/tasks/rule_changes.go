package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"limmr/internal/dbutil"
	"limmr/internal/model"
)

// saveRuleChange is the single history/change writer for automatic rules. The
// snapshot is captured before any mutation (including map/slice edits). Rule
// changes bypass normal status events, attention refreshes and owner broadcasts.
func (s *Service) saveRuleChange(tx *sql.Tx, r *record, before json.RawMessage, rule, what, reason string, evidence any, change MembershipChange) error {
	if strings.TrimSpace(rule) == "" || strings.TrimSpace(what) == "" || strings.TrimSpace(reason) == "" || len(before) == 0 {
		return model.ErrInvalid
	}
	change.Kind, change.Rule = "rule_"+rule, rule
	change.Reason = strings.TrimSpace(reason)
	change.Text = strings.TrimSpace(what) + ": " + strings.TrimSpace(reason)
	change.Before, change.Evidence = before, evidence
	r.silentChange = true
	return s.saveMembershipChange(tx, r, change)
}

func (s *Service) restoreRuleChange(tx *sql.Tx, r *record, change MembershipChange) error {
	var before record
	if err := json.Unmarshal(change.Before, &before); err != nil {
		return err
	}
	if before.ID != r.ID {
		return model.ErrConflict
	}
	// Duplicate undo is scoped to the additions. Preserve later edits to the
	// destination, and refuse to overwrite a suggestion edited since the merge.
	if change.Kind != "rule_duplicate" && change.Kind != "rule_follows_work" {
		*r = before
		if change.Rule == "protocol_demote" {
			s.markProtocolKept(r)
		}
	}
	if len(change.SuggestionBefore) == 0 {
		return nil
	}
	var suggestion record
	if err := json.Unmarshal(change.SuggestionBefore, &suggestion); err != nil {
		return err
	}
	var current string
	if err := tx.QueryRow("SELECT data FROM tasks WHERE id=?", suggestion.ID).Scan(&current); err != nil {
		return model.ErrConflict
	}
	if current != string(change.SuggestionAfter) {
		return model.ErrConflict
	}
	suggestion.UpdatedAt = s.now()
	_, err := tx.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(suggestion), suggestion.ID)
	return err
}

func duplicateOpen(t Task) bool {
	return t.Status == "active" || t.Status == "waiting" || t.Status == "quiet"
}

// Uses precisely the create matcher, with explicit status/visibility filtering.
func (s *Service) duplicateTarget(tx *sql.Tx, candidate Task, visible func(Task) bool, suggestions bool) (*record, error) {
	rows, err := tx.Query("SELECT data FROM tasks ORDER BY CASE json_extract(data,'$.status') WHEN 'suggested' THEN 1 ELSE 0 END,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var r record
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		if r.ID != candidate.ID && !r.Purged && (duplicateOpen(r.Task) || suggestions && r.Status == "suggested") && !s.deadlinePassed(r.Task) && visible(r.Task) && duplicate(candidate, r.Task) {
			return &r, nil
		}
	}
	return nil, rows.Err()
}

func duplicateReason(candidate, target Task) (string, []string) {
	matched := []string{}
	for _, ch := range candidate.Chats {
		if member(target.Chats, ch.ID) != nil {
			matched = append(matched, "same chat")
			break
		}
	}
	for _, p := range candidate.People {
		if p.ID != "me" && member(target.People, p.ID) != nil {
			matched = append(matched, "same person")
			break
		}
	}
	if candidate.EffortID != "" && candidate.EffortID == target.EffortID {
		matched = append(matched, "same effort")
	}
	if similar(candidate.Title, target.Title) {
		matched = append(matched, "similar title")
	}
	if similar(candidate.Goal, target.Goal) || similar(candidate.Title, target.Goal) || similar(candidate.Goal, target.Title) {
		matched = append(matched, "similar goal")
	}
	return fmt.Sprintf("%s as %q", strings.Join(matched, " and "), target.Title), matched
}

func taskMessageRefs(t Task) []Ref {
	refs := append([]Ref{}, t.EvidenceRefs...)
	for _, ref := range t.SubjectEvidence {
		if !hasRef(refs, ref) {
			refs = append(refs, ref)
		}
	}
	for _, item := range t.Items {
		if item.MessageRef != nil && !hasRef(refs, *item.MessageRef) {
			refs = append(refs, *item.MessageRef)
		}
	}
	return refs
}

// mergeKeptSuggestion shares AttachEvidence's transactional core. Both records,
// the before-state and the single history line commit or roll back together.
func (s *Service) mergeKeptSuggestion(tx *sql.Tx, src, dst *record, refs []Ref, hidden func(string) bool, ask *AskedByOwner) error {
	before := json.RawMessage(dbutil.JSON(dst))
	suggestionBefore := json.RawMessage(dbutil.JSON(src))
	reason, matched := duplicateReason(src.Task, dst.Task)
	change, err := s.attachEvidence(dst, src.Chats, src.People, refs, "merge", hidden, true)
	if err != nil {
		return err
	}
	now := s.now()
	src.Status, src.CloseReason, src.MergedInto = "dropped", "merged", dst.ID
	src.ClosedAt, src.UpdatedAt = &now, now
	src.EndedAsSuggestion = true
	src.NeedsYou, src.NeedsYouReason = false, ""
	src.silentChange = true
	change.SuggestionBefore = suggestionBefore
	change.SuggestionAfter = json.RawMessage(dbutil.JSON(src))
	change.OwnerRequest = ask
	reason = ownerRequestLine(reason, ask)
	evidence := map[string]any{"matched_fields": matched, "suggestion_id": src.ID, "message_refs": refs, "owner_request": ask}
	if err := s.saveRuleChange(tx, dst, before, "duplicate", "Attached suggestion to existing task", reason, evidence, change); err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(dst), dst.ID)
	return err
}

// attachDetected applies R1 to a duplicate already found by covered. Recheck
// under the writer reservation to avoid attaching to work that just ended.
func (s *Service) attachDetected(candidate Task, targetID string) error {
	hidden := s.Hub.HiddenChatAccess()
	_, err := s.mutate(nil, targetID, func(dst *record, tx *sql.Tx) error {
		dst.silentChange = true
		if dst.Purged || !(duplicateOpen(dst.Task) || dst.Status == "suggested") || s.deadlinePassed(dst.Task) || !duplicate(candidate, dst.Task) {
			return model.ErrConflict
		}
		before := json.RawMessage(dbutil.JSON(dst))
		reason, matched := duplicateReason(candidate, dst.Task)
		var chats, people []Entity
		if candidate.EffortID != "" && candidate.EffortID == dst.EffortID {
			chats, people = candidate.Chats, candidate.People
		}
		change, err := s.attachEvidence(dst, chats, people, taskMessageRefs(candidate), "merge", hidden, true)
		if err != nil {
			return err
		}
		if len(change.Chats)+len(change.People)+len(change.AddedRefs) == 0 {
			return nil
		}
		evidence := map[string]any{"matched_fields": matched, "effort_id": candidate.EffortID, "message_refs": taskMessageRefs(candidate)}
		return s.saveRuleChange(tx, dst, before, "duplicate", "Attached detected evidence", reason, evidence, change)
	})
	return err
}
