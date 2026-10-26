package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"limmr/internal/model"
	"strconv"
	"strings"
	"time"
)

func clockTime(v string) bool { _, err := time.Parse("15:04", v); return err == nil && len(v) == 5 }

// Construct each slot from a calendar date in the configured local zone. Never add 24h across DST.
func localSlot(day time.Time, clock string, loc *time.Location) time.Time {
	h, _ := strconv.Atoi(strings.Split(clock, ":")[0])
	m := 0
	if len(clock) == 5 {
		m, _ = strconv.Atoi(clock[3:])
	}
	y, mo, d := day.In(loc).Date()
	at := time.Date(y, mo, d, h, m, 0, 0, loc)
	// Go may normalize a nonexistent spring-forward hour backwards; move forward through the gap.
	if at.Hour() < h {
		at = at.Add(time.Hour)
	}
	return at
}
func (s *Service) next(c CheckIn, after time.Time) time.Time {
	if !clockTime(c.AtTime) {
		return time.Time{}
	}
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	day := after.In(loc)
	switch c.Rule {
	case "weekly":
		if c.Weekday == nil || *c.Weekday < 0 || *c.Weekday > 6 {
			return time.Time{}
		}
		for n := 0; n <= 7; n++ {
			d := day.AddDate(0, 0, n)
			if int(d.Weekday()) == *c.Weekday {
				at := localSlot(d, c.AtTime, loc)
				if at.After(after) {
					return at
				}
			}
		}
	case "every_n_days":
		if c.EveryDays < 1 || c.EveryDays > 60 {
			return time.Time{}
		}
		return localSlot(day.AddDate(0, 0, c.EveryDays), c.AtTime, loc)
	case "once":
		at := localSlot(day, c.AtTime, loc)
		if !at.After(after) {
			at = localSlot(day.AddDate(0, 0, 1), c.AtTime, loc)
		}
		return at
	}
	return time.Time{}
}
func (s *Service) refreshNeeds(tx *sql.Tx, r *record) error {
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.state')='pending'", r.ID).Scan(&n); err != nil {
		return err
	}
	r.NeedsYou = open(r.Task) && (n > 0 || r.FlagReason != "" || r.EffortNeeds)
	r.NeedsYouReason = ""
	if r.NeedsYou {
		switch {
		case r.FlagReason != "":
			r.NeedsYouReason = r.FlagReason
		case n > 0:
			r.NeedsYouReason = "An update is ready for your review"
		default:
			r.NeedsYouReason = "Your effort needs attention"
		}
	}
	return nil
}
func (s *Service) suggestion(tx *sql.Tx, r *record, key, kind, chat, text, why string) (string, error) {
	draftSource := "ai"
	if kind == "nudge" {
		if r.DraftNudge == "" {
			draftSource = "template"
		}
	}

	circle := ""
	if len(r.CircleIDs) > 0 {
		circle = r.CircleIDs[0]
	}
	id := r.ID + ":" + key
	x := model.Suggestion{DraftSource: draftSource, ID: id, TaskID: r.ID, Kind: kind, ChatJID: chat, CircleID: circle, DraftText: text, Body: text, Variations: []string{text}, Why: why, CreatedBy: r.CreatedBy.AgentID, CreatedAt: s.now(), ExpiresAt: s.now().AddDate(0, 0, 90), State: "pending"}
	if s.Hub.MayRunAutomations != nil {
		// A slot is shared, while each offline engine's immutable claim is distinct.
		slot := r.ID + ":" + key
		var existing string
		err := tx.QueryRow(`SELECT json_extract(e.payload,'$.suggestion.id') FROM task_events e JOIN sync_meta m ON m.bucket='task_events' AND m.id=e.id WHERE e.kind='slot_claim' AND json_extract(e.payload,'$.slot')=? ORDER BY m.hlc,m.origin,e.id LIMIT 1`, slot).Scan(&existing)
		if err == nil {
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
		var origin string
		if err = tx.QueryRow(`SELECT engine_id FROM sync_state WHERE id=1`).Scan(&origin); err != nil {
			return "", err
		}
		id += ":" + origin
		x.ID = id
		payload := dbutil.JSON(map[string]any{"slot": slot, "chat_jid": chat, "draft": text, "suggestion": x})
		if _, err = tx.Exec(`INSERT INTO task_events VALUES(?,?,?,?,?,?)`, id+":claim", r.ID, s.now().UTC().Format(time.RFC3339Nano), "slot_claim", "Scheduled draft claimed", payload); err != nil {
			return "", err
		}
	}
	_, err := tx.Exec("INSERT OR IGNORE INTO objects VALUES('suggestions',?,?)", id, dbutil.JSON(x))
	return id, err
}
func (s *Service) Tick(ctx context.Context) error {
	if s.Hub.MayRunAutomations != nil && !s.Hub.MayRunAutomations(ctx) {
		return ctx.Err()
	}
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	if err := s.detect(ctx); err != nil {
		return err
	}
	xs, err := s.records()
	if err != nil {
		return err
	}
	for _, old := range xs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if old.Purged {
			continue
		}
		if old.Status == "suggested" {
			if old.Suggestion != nil && !s.now().Before(old.Suggestion.DetectedAt.AddDate(0, 0, 14)) {
				if _, err = s.resolve(nil, old.ID, "suggestion_expired", ""); err != nil {
					return err
				}
			}
			continue
		}
		if !open(old.Task) {
			if old.ClosedAt != nil && !s.now().Before(old.ClosedAt.AddDate(0, 0, 90)) {
				if err = s.purge(old); err != nil {
					return err
				}
			}
			continue
		}
		scanned := old
		if err = s.scan(&scanned, nil); err != nil {
			return err
		}
		effortNeeds := old.EffortNeeds
		if old.EffortID != "" && s.Efforts != nil {
			if e, eerr := s.Efforts.Get(nil, old.EffortID); eerr == nil {
				effortNeeds = e.Status == "on_you"
			}
		}
		nudgeChat := s.waitChat(old)
		fallback := ""
		if old.Waiting != nil && old.Waiting.NudgeState == "none" && old.DraftNudge == "" && !s.now().Before(old.Waiting.Since.AddDate(0, 0, old.Waiting.NudgeAfterDays)) && s.NudgeDraft != nil {
			var draftErr error
			fallback, draftErr = s.NudgeDraft(ctx, old.Task)
			if draftErr != nil {
				fallback = ""
			}
			if !length(fallback, 2000) {
				fallback = ""
			}
		}
		_, err = s.mutate(nil, old.ID, func(r *record, tx *sql.Tx) error {
			now := s.now()
			if !open(r.Task) {
				return nil
			}
			beforeItems := len(r.Items)
			for _, it := range scanned.Items {
				r.Items = appendItem(r.Items, it)
			}
			if len(r.Items) > beforeItems {
				r.UpdatedAt = now
			}
			if r.Kind == "short" && !now.Before(r.UpdatedAt.AddDate(0, 0, r.LifespanDays)) {
				closeTask(r, now, "lifespan")
				r.UpdatedAt = now
				return nil
			}
			if !effortNeeds {
				r.EffortAcknowledged = false
			}
			r.EffortNeeds = effortNeeds && !r.EffortAcknowledged
			if c := r.CheckIn; c != nil && !c.NextAt.After(now) {
				slot := c.NextAt.UTC().Format(time.RFC3339Nano)
				draft := ""
				if r.DraftSlot == slot {
					draft = r.DraftCheckIn
				}
				key := "check_in:" + slot
				if _, err := s.suggestion(tx, r, key, "check_in", c.To, draft, fmt.Sprintf("Update for %s is due", r.Title)); err != nil {
					return err
				}
				if err := s.event(tx, *r, key, "task_due", "Check-in due", map[string]any{"note": c.Note, "progress": r.Progress}); err != nil {
					return err
				}
				c.LastAt = &now
				if c.Rule == "once" {
					r.CheckIn = nil
				} else {
					at := c.NextAt
					for !at.After(now) {
						next := s.next(*c, at)
						if !next.After(at) {
							return model.ErrInvalid
						}
						at = next
					}
					c.NextAt = at
				}
				r.DraftCheckIn = ""
				r.DraftSlot = ""
				r.UpdatedAt = now
			}
			if w := r.Waiting; w != nil && w.NudgeState == "none" && !now.Before(w.Since.AddDate(0, 0, w.NudgeAfterDays)) {
				chat := ""
				if old.Waiting != nil && w.Since.Equal(old.Waiting.Since) && w.On != nil && old.Waiting.On != nil && w.On.ID == old.Waiting.On.ID {
					chat = nudgeChat
				}
				if chat != "" {
					draft := r.DraftNudge
					if draft == "" {
						draft = fallback
					}
					if draft == "" {
						name := "there"
						if w.On != nil && len(strings.Fields(w.On.Name)) > 0 {
							name = strings.Fields(w.On.Name)[0]
						}
						draft = fmt.Sprintf("Hi %s, just checking in on %s. No rush!", name, short(w.What, 120))
					}
					key := "nudge:" + w.Since.AddDate(0, 0, w.NudgeAfterDays).UTC().Format(time.RFC3339Nano)
					id, err := s.suggestion(tx, r, key, "nudge", chat, draft, "A gentle follow-up is ready")
					if err != nil {
						return err
					}
					if r.DraftNudge == "" && fallback != "" {
						if _, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.draft_source','this-mac') WHERE bucket='suggestions' AND id=?", id); err != nil {
							return err
						}
					}
					w.NudgeState = "queued"
					w.NudgeSuggestionID = id
					if err = s.event(tx, *r, key, "nudge_queued", "Follow-up queued", nil); err != nil {
						return err
					}
					r.UpdatedAt = now
				}
			}
			before := r.NeedsYou
			if err := s.refreshNeeds(tx, r); err != nil {
				return err
			}
			if before != r.NeedsYou {
				r.UpdatedAt = now
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if s.Accessory != nil {
		xs, err = s.records()
		if err != nil {
			return err
		}
		s.Accessory.Sync(ctx, xs, s.Hub.Settings().AccessoryCue, s.now())
	}
	return nil
}

// Resolve canonical people/JID aliases before opening the task write transaction.
func (s *Service) waitChat(r record) string {
	if r.Waiting == nil || r.Waiting.On == nil {
		return ""
	}
	ids := map[string]bool{r.Waiting.On.ID: true}
	var p model.Person
	if s.Hub.Repo.Get("people", r.Waiting.On.ID, &p) == nil {
		for _, jid := range p.JIDs {
			ids[jid] = true
			if canonical, err := s.Hub.ResolveChat(jid); err == nil {
				ids[canonical] = true
			}
		}
	}
	for _, ch := range r.Chats {
		if ids[ch.ID] || model.PersonID(ch.ID) == r.Waiting.On.ID {
			return ch.ID
		}
	}
	// If the task only involves a shared group, follow up in that task group.
	for _, ch := range r.Chats {
		if strings.HasSuffix(ch.ID, "@g.us") {
			return ch.ID
		}
	}
	return ""
}

func (s *Service) Incoming(ctx context.Context, m model.Message) error {
	if m.IsFromMe && m.DeletedAt == nil && s.Hub.MayRunAutomations != nil {
		if err := s.observeSentSlot(ctx, m); err != nil {
			return err
		}
	}
	if m.IsFromMe || m.DeletedAt != nil {
		return nil
	}
	xs, err := s.records()
	if err != nil {
		return err
	}
	person := model.PersonID(m.Sender)
	if m.Sender == "" {
		person = model.PersonID(m.ChatJID)
	}
	// Canonical people_jids includes LID aliases and owner-confirmed identities.
	jid := m.Sender
	if jid == "" {
		jid = m.ChatJID
	}
	_ = s.Hub.Repo.DB.QueryRow("SELECT person_id FROM people_jids WHERE jid=? LIMIT 1", jid).Scan(&person)
	for _, r := range xs {
		w := r.Waiting
		if !open(r.Task) || w == nil || w.On == nil || w.On.ID != person && w.On.ID != jid || !m.Timestamp.After(w.Since) {
			continue
		}
		inChat := false
		for _, ch := range r.Chats {
			inChat = inChat || ch.ID == m.ChatJID
		}
		if !inChat {
			continue
		}
		_, err = s.mutate(nil, r.ID, func(r *record, tx *sql.Tx) error {
			if r.Waiting == nil || r.Waiting.Since != w.Since {
				return nil
			}
			if err := dismiss(tx, r.Waiting.NudgeSuggestionID); err != nil {
				return err
			}
			r.Waiting = nil
			r.Status = "active"
			r.UpdatedAt = s.now()
			if err := s.event(tx, *r, "waiting:"+w.Since.Format(time.RFC3339Nano), "task_waiting_cleared", "Reply received; waiting cleared", nil); err != nil {
				return err
			}
			return s.refreshNeeds(tx, r)
		})
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (s *Service) purge(old record) error {
	tx, err := s.Hub.Repo.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r := record{Task: Task{ID: old.ID, Title: old.Title, CreatedAt: old.CreatedAt, UpdatedAt: old.UpdatedAt, ClosedAt: old.ClosedAt}, Purged: true}
	if _, err = tx.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(r), old.ID); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM task_events WHERE task_id=?", old.ID); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=?", old.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ValidateSend(a model.Agent, id, chat string) error {
	t, err := s.Get(&a, id)
	if err != nil {
		return err
	}
	for _, ch := range t.Chats {
		if ch.ID == chat {
			return nil
		}
	}
	return model.ErrNotFound
}
func (s *Service) RecordSent(d model.Draft) error {
	if d.TaskID == "" {
		return nil
	}
	_, err := s.mutate(nil, d.TaskID, func(r *record, tx *sql.Tx) error {
		for _, u := range r.UpdatesSent {
			if u.ChatJID == d.ChatJID && u.MessageID == d.MessageID {
				return nil
			}
		}
		kind := "other"
		if d.SuggestionID == "" {
			var suggestionID string
			err := tx.QueryRow("SELECT id FROM objects WHERE bucket='suggestions' AND json_extract(data,'$.task_id')=? AND json_extract(data,'$.chat_jid')=? AND json_extract(data,'$.state')='pending' AND (json_extract(data,'$.kind')='check_in' OR (json_extract(data,'$.kind')='nudge' AND json_extract(data,'$.draft_text')=?)) ORDER BY json_extract(data,'$.created_at'),id LIMIT 1", r.ID, d.ChatJID, d.FinalText).Scan(&suggestionID)
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			d.SuggestionID = suggestionID
		}
		if d.SuggestionID != "" {
			var k string
			_ = tx.QueryRow("SELECT json_extract(data,'$.kind') FROM objects WHERE bucket='suggestions' AND id=?", d.SuggestionID).Scan(&k)
			if k == "check_in" || k == "nudge" {
				kind = k
			}
		}
		now := s.now()
		r.UpdatesSent = append(r.UpdatesSent, SentUpdate{At: now, ChatJID: d.ChatJID, MessageID: d.MessageID, TextPreview: short(d.FinalText, 120), Kind: kind, By: d.AgentID})
		r.UpdatedAt = now
		if kind == "nudge" && r.Waiting != nil {
			r.Waiting.NudgeState = "sent"
		}
		if d.SuggestionID != "" {
			if _, err := tx.Exec("UPDATE objects SET data=json_set(data,'$.state','sent') WHERE bucket='suggestions' AND id=?", d.SuggestionID); err != nil {
				return err
			}
		}
		r.FlagReason = ""
		r.EffortNeeds = false
		r.EffortAcknowledged = true
		label := "Update sent"
		if kind == "check_in" {
			label = "Check-in sent"
		}
		if err := s.event(tx, *r, "sent:"+d.ChatJID+":"+d.MessageID, "update_sent", label, map[string]string{"by": d.AgentID}); err != nil {
			return err
		}
		if kind == "nudge" {
			if err := s.event(tx, *r, "nudge_sent:"+d.MessageID, "nudge_sent", "Follow-up sent", nil); err != nil {
				return err
			}
		}
		return s.refreshNeeds(tx, r)
	})
	return err
}
func (s *Service) SuggestionDone(x model.Suggestion) error {
	if x.TaskID == "" {
		return nil
	}
	_, err := s.mutate(nil, x.TaskID, func(r *record, tx *sql.Tx) error {
		if x.Kind == "nudge" && r.Waiting != nil && r.Waiting.NudgeSuggestionID == x.ID {
			r.Waiting.NudgeState = "skipped"
		}
		r.FlagReason = ""
		r.EffortNeeds = false
		r.EffortAcknowledged = true
		r.UpdatedAt = s.now()
		return s.refreshNeeds(tx, r)
	})
	return err
}

// Both linked devices observe owner-sent messages. Matching never sends or marks read.
func (s *Service) observeSentSlot(ctx context.Context, m model.Message) error {
	tx, err := s.Hub.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id,task_id,payload FROM task_events WHERE kind='slot_claim' AND json_extract(payload,'$.chat_jid')=? AND coalesce((SELECT json_extract(o.data,'$.draft_text') FROM objects o WHERE o.bucket='suggestions' AND o.id=json_extract(task_events.payload,'$.suggestion.id')),json_extract(payload,'$.draft'))=? AND julianday(at)<=julianday(?)`, m.ChatJID, m.Content, m.Timestamp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	type claim struct{ id, task, payload string }
	xs := []claim{}
	for rows.Next() {
		var x claim
		if err = rows.Scan(&x.id, &x.task, &x.payload); err != nil {
			break
		}
		xs = append(xs, x)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range xs {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO task_events VALUES(?,?,?,?,?,?)`, x.id+":sent", x.task, m.Timestamp.UTC().Format(time.RFC3339Nano), "slot_sent", "Scheduled draft observed sent", x.payload); err != nil {
			return err
		}
		var payload struct {
			Suggestion struct {
				ID string `json:"id"`
			} `json:"suggestion"`
		}
		if err = json.Unmarshal([]byte(x.payload), &payload); err != nil {
			return err
		}
		id := payload.Suggestion.ID
		if _, err = tx.Exec(`UPDATE objects SET data=json_set(data,'$.state','sent') WHERE bucket='suggestions' AND id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
