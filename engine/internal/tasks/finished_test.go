package tasks

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
)

func finishedFixture(t *testing.T) (*Service, *model.Agent, *time.Time, Task) {
	t.Helper()
	s, a, now := fixture(t)
	v, err := s.Create(nil, Create{Title: "Science fair poster", Goal: "Science fair poster", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	return s, a, now, v
}
func finishMessagePut(t *testing.T, s *Service, now *time.Time, text, author string, at time.Time) model.Message {
	t.Helper()
	m := model.Message{ID: "finish-message", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: text, Timestamp: at, IsFromMe: author != "contact"}
	testkit.Must(t, s.Hub.Messages.PutMessage(m))
	if author != "contact" {
		id := "you"
		if author == "agent" {
			id = "recorded-ai"
		}
		_, err := s.Hub.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", m.ChatJID, m.ID, id, "Fixture AI")
		testkit.Must(t, err)
	}
	if at.After(*now) {
		*now = at
	}
	return m
}

func TestR2CompletionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, text, author string
		done, hint         bool
	}{
		{"owner", "Submitted the poster", "owner", true, false},
		{"contact", "Submitted the poster", "contact", false, true},
		{"recorded AI", "Submitted the poster", "agent", false, true},
		{"question", "submitted?", "owner", false, false},
		{"negation", "not submitted yet", "owner", false, false},
		{"contraction", "haven't submitted the poster", "owner", false, false},
		{"future", "will submit tomorrow", "owner", false, false},
		{"future completed", "will have submitted the poster tomorrow", "owner", false, false},
		{"going to", "going to send the poster", "owner", false, false},
		{"no reference", "done", "owner", false, true},
		{"substring", "undone poster", "owner", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, a, now, v := finishedFixture(t)
			finishMessagePut(t, s, now, tc.text, tc.author, now.Add(time.Minute))
			before, err := s.History(v.ID)
			testkit.Must(t, err)
			var updates, pending int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&updates))
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions'").Scan(&pending))
			stream, cancel := s.Hub.Subscribe()
			defer cancel()
			_, applyErr := s.applyFinished(mustRaw(t, s, v.ID))
			testkit.Must(t, applyErr)
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if (got.Status == "done") != tc.done || (got.FinishHint != nil) != tc.hint || got.NeedsYou {
				t.Fatalf("outcome: %+v", got)
			}
			if tc.hint && (!strings.HasSuffix(got.FinishHint.Why, "Ask the owner before closing it.") || len(got.FinishHint.MessageRefs) != 1) {
				t.Fatal("weak evidence", got.FinishHint)
			}
			history, err := s.History(v.ID)
			testkit.Must(t, err)
			if tc.done {
				if len(history) != len(before)+1 || history[0].Kind != "rule_finished" || history[0].Text != "Ended: you said “Submitted the poster”" || history[0].ChangeID == "" {
					t.Fatal(history)
				}
				var payload struct {
					Rule, Reason string
					Evidence     struct {
						Refs []Ref `json:"message_refs"`
					}
				}
				var raw string
				testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='rule_finished'", v.ID).Scan(&raw))
				testkit.Must(t, json.Unmarshal([]byte(raw), &payload))
				if payload.Rule != "finished" || payload.Reason == "" || len(payload.Evidence.Refs) != 1 || payload.Evidence.Refs[0].MessageID != "finish-message" {
					t.Fatal(raw)
				}
				changes := membershipChanges(t, s, v.ID)
				if len(changes) != 1 || len(changes[0].Before) == 0 {
					t.Fatal(changes)
				}
				restored, err := s.UndoMembershipChange(nil, v.ID, changes[0].ID)
				testkit.Must(t, err)
				if restored.Status != v.Status || restored.CloseReason != "" || restored.ClosedAt != nil || restored.Goal != v.Goal {
					t.Fatal("undo", restored)
				}
				_, applyErr = s.applyFinished(mustRaw(t, s, v.ID))
				testkit.Must(t, applyErr)
				restored, err = s.Get(nil, v.ID)
				testkit.Must(t, err)
				if restored.Status != "active" {
					t.Fatal("undo replayed")
				}
			} else if len(history) != len(before) {
				t.Fatal("hint added history", history)
			}
			// Undo is itself silent; neither finish nor hint wakes attention consumers.
			select {
			case <-stream:
				t.Fatal("automatic broadcast")
			default:
			}
			var afterUpdates, afterPending int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&afterUpdates))
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions'").Scan(&afterPending))
			if updates != afterUpdates || pending != afterPending {
				t.Fatal("attention created")
			}
		})
	}
}

func TestR2ReferencesAndTimeGuards(t *testing.T) {
	for _, mode := range []string{"reply", "file ref", "file hash", "filename only", "before creation", "unlinked", "hidden", "reopened 10 days", "reopened boundary", "done", "dropped"} {
		t.Run(mode, func(t *testing.T) {
			s, _, now, v := finishedFixture(t)
			ref := Ref{ChatJID: testkit.Phone, MessageID: "task-reference"}
			testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: ref.MessageID, ChatJID: ref.ChatJID, Content: "A design draft", Timestamp: now.Add(-time.Minute), MediaType: "document", Filename: "draft.pdf"}))
			_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.evidence_refs',json(?)) WHERE id=?", dbutil.JSON([]Ref{ref}), v.ID)
			testkit.Must(t, err)
			at := now.Add(time.Minute)
			if mode == "before creation" {
				at = now.Add(-time.Second)
			}
			if mode == "reopened 10 days" || mode == "reopened boundary" {
				_, err = s.Update(nil, v.ID, map[string]any{"status": "done"})
				testkit.Must(t, err)
				_, err = s.Reopen(v.ID)
				testkit.Must(t, err)
				days := 10
				if mode == "reopened boundary" {
					days = 14
				}
				*now = now.Add(time.Duration(days) * 24 * time.Hour)
				at = *now
			}
			if mode == "done" || mode == "dropped" {
				_, err = s.Update(nil, v.ID, map[string]any{"status": mode})
				testkit.Must(t, err)
			}
			m := finishMessagePut(t, s, now, "Submitted the poster", "owner", at)
			wantDone := true
			switch mode {
			case "reply":
				m.Content = "done"
				m.QuotedMessageID = ref.MessageID
			case "file ref", "file hash", "filename only":
				m.Content = "done"
				m.MediaType = "document"
				m.Filename = "draft.pdf"
				itemRef := ref
				if mode == "file ref" {
					itemRef = Ref{ChatJID: m.ChatJID, MessageID: m.ID}
				}
				_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", dbutil.JSON([]Item{{ID: "task-file", Kind: "file", Title: "draft.pdf", Source: "chat", AttachmentID: "media:" + itemRef.MessageID, MessageRef: &itemRef}}), v.ID)
				testkit.Must(t, err)
				if mode == "file hash" {
					_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET file_sha256=? WHERE id IN (?,?)", []byte{1, 2, 3}, ref.MessageID, m.ID)
					testkit.Must(t, err)
				}
				if mode == "filename only" {
					wantDone = false
				}
			case "before creation", "reopened 10 days":
				wantDone = false
			case "unlinked":
				m.ChatJID = testkit.Group
				wantDone = false
			case "hidden":
				settings := s.Hub.Settings()
				settings.HiddenChats = []string{testkit.Phone}
				testkit.Must(t, s.Hub.SetSettings(settings))
				wantDone = false
			case "done", "dropped":
				wantDone = mode == "done"
			}
			// Move the existing message without leaving a second linked copy.
			_, err = s.Hub.Messages.DB.Exec("DELETE FROM messages WHERE id=?", m.ID)
			testkit.Must(t, err)
			testkit.Must(t, s.Hub.Messages.PutMessage(m))
			if mode == "file hash" {
				_, err = s.Hub.Messages.DB.Exec("UPDATE messages SET file_sha256=? WHERE id=?", []byte{1, 2, 3}, m.ID)
				testkit.Must(t, err)
			}
			// Attribution is keyed by chat; moved fixture messages need their owner record too.
			if mode == "unlinked" {
				_, err = s.Hub.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", m.ChatJID, m.ID, "you", "You")
				testkit.Must(t, err)
			}
			testkit.Must(t, s.Tick(context.Background()))
			got, err := s.Get(nil, v.ID)
			testkit.Must(t, err)
			if (got.Status == "done") != wantDone {
				t.Fatal("status", got.Status)
			}
			if mode == "dropped" && got.Status != "dropped" {
				t.Fatal("dropped touched")
			}
			if mode == "done" || mode == "dropped" {
				if len(membershipChanges(t, s, v.ID)) != 0 {
					t.Fatal("closed task changed")
				}
			}
		})
	}
}

func TestR2HintsExpireAndOwnerActivityClears(t *testing.T) {
	for _, mode := range []string{"expiry", "owner note", "owner edit", "owner message", "end"} {
		t.Run(mode, func(t *testing.T) {
			s, a, now, v := finishedFixture(t)
			finishMessagePut(t, s, now, "Submitted the poster", "contact", now.Add(time.Minute))
			testkit.Must(t, s.Tick(context.Background()))
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.FinishHint == nil {
				t.Fatal("missing hint")
			}
			initial := *got.FinishHint
			*now = now.Add(time.Hour)
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if !reflect.DeepEqual(got.FinishHint, &initial) {
				t.Fatal("hint refreshed")
			}
			switch mode {
			case "expiry":
				*now = initial.At.Add(7*24*time.Hour - time.Second)
				got, err = s.Get(a, v.ID)
				testkit.Must(t, err)
				if got.FinishHint == nil {
					t.Fatal("expired early")
				}
				*now = initial.At.Add(7 * 24 * time.Hour)
			case "owner note":
				_, err = s.OwnerNote(nil, v.ID, "", "Review a new layout", false)
			case "owner edit":
				_, err = s.Update(nil, v.ID, map[string]any{"goal": "Science fair poster revision"})
			case "owner message":
				finishMessagePut(t, s, now, "A new layout is underway", "owner", *now)
			case "end":
				_, err = s.Update(nil, v.ID, map[string]any{"status": "done"})
			}
			testkit.Must(t, err)
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if mode != "owner message" && got.FinishHint != nil {
				t.Fatal("read retained stale hint")
			}
			testkit.Must(t, s.Tick(context.Background()))
			testkit.Must(t, s.Tick(context.Background()))
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.FinishHint != nil || got.NeedsYou {
				t.Fatal("hint replayed", got.FinishHint)
			}
			listed, err := s.List(a, "all", 100)
			testkit.Must(t, err)
			for _, summary := range listed {
				if summary.ID == v.ID && summary.FinishHint != nil {
					t.Fatal("stale list hint")
				}
			}
		})
	}
}

func TestR2ClosureReasonsAndRuleUndo(t *testing.T) {
	for _, mode := range []string{"owner done", "owner dropped", "agent", "explicit reason", "deadline", "checklist", "create checklist", "merge"} {
		t.Run(mode, func(t *testing.T) {
			s, a, now, v := finishedFixture(t)
			var err error
			reason := ""
			auto := false
			switch mode {
			case "owner done":
				_, err = s.Update(nil, v.ID, map[string]any{"status": "done"})
				reason = "you marked it done"
			case "owner dropped":
				_, err = s.Update(nil, v.ID, map[string]any{"status": "dropped"})
				reason = "you set it aside"
			case "agent":
				_, err = s.Update(a, v.ID, map[string]any{"status": "done"})
				reason = "the AI marked it done"
			case "explicit reason":
				_, err = s.Update(a, v.ID, map[string]any{"status": "done", "reason": "Poster delivered to the fair"})
				reason = "Poster delivered to the fair"
			case "deadline":
				_, err = s.Update(nil, v.ID, map[string]any{"end_at": *now})
				testkit.Must(t, err)
				*now = now.Add(24 * time.Hour)
				err = s.Tick(context.Background())
				reason = "its end time passed"
				auto = true
			case "checklist":
				_, err = s.Update(nil, v.ID, map[string]any{"end_condition": "checklist_done"})
				testkit.Must(t, err)
				var added Task
				added, err = s.Checklist(nil, v.ID, "add", []Step{{Text: "Deliver poster"}})
				testkit.Must(t, err)
				_, err = s.Checklist(nil, v.ID, "done", added.Checklist)
				reason = "every checklist step is done"
				auto = true
			case "create checklist":
				v, err = s.Create(nil, Create{Title: "Another poster", Goal: "Other display", Kind: "long", EndCondition: "checklist_done", Checklist: []Step{{Text: "Deliver", Done: true}}})
				reason = "every checklist step is done"
				auto = true
			case "merge":
				var dst Task
				dst, err = s.Create(nil, Create{Title: "Another display", Kind: "long"})
				testkit.Must(t, err)
				_, err = s.Merge(nil, dst.ID, v.ID)
				reason = "merged into another task"
			}
			testkit.Must(t, err)
			kind := "task_closed"
			if auto {
				kind = "rule_finished"
			}
			var text, payload string
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT text,payload FROM task_events WHERE task_id=? AND kind=?", v.ID, kind).Scan(&text, &payload))
			var data map[string]any
			testkit.Must(t, json.Unmarshal([]byte(payload), &data))
			if !strings.Contains(text, reason) || data["reason"] != reason || data["close_reason"] == "" {
				t.Fatal(text, payload)
			}
			if auto {
				var n int
				testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM task_events WHERE task_id=? AND kind='task_closed'", v.ID).Scan(&n))
				if n != 0 {
					t.Fatal("generic auto closure noise")
				}
				changes := membershipChanges(t, s, v.ID)
				restored, err := s.UndoMembershipChange(nil, v.ID, changes[0].ID)
				testkit.Must(t, err)
				if restored.Status != "active" {
					t.Fatal("undo failed")
				}
				testkit.Must(t, s.Tick(context.Background()))
				restored, err = s.Get(nil, v.ID)
				testkit.Must(t, err)
				if restored.Status != "active" {
					t.Fatal("undo replayed")
				}
			}
		})
	}
}

func mustRaw(t *testing.T, s *Service, id string) record {
	t.Helper()
	r, err := s.raw(id)
	testkit.Must(t, err)
	return r
}

func TestR2EffortHintsStayWeakAndExpire(t *testing.T) {
	for _, status := range []string{"done", "dormant"} {
		t.Run(status, func(t *testing.T) {
			s, a, now, v := finishedFixture(t)
			id := finishEffortFixture(t, s, now)
			_, err := s.Efforts.Update(nil, id, efforts.Update{Status: &status})
			testkit.Must(t, err)
			_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.effort_id',?) WHERE id=?", id, v.ID)
			testkit.Must(t, err)
			beforeRecent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			stream, cancel := s.Hub.Subscribe()
			defer cancel()
			_, err = s.applyFinished(mustRaw(t, s, v.ID))
			testkit.Must(t, err)
			got, err := s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.Status != "active" || got.FinishHint == nil || !strings.Contains(got.FinishHint.Why, status) || got.NeedsYou {
				t.Fatal("effort ended task or raised attention", got)
			}
			list, err := s.List(a, "open", 100)
			testkit.Must(t, err)
			found := false
			for _, summary := range list {
				if summary.ID == v.ID {
					found = summary.FinishHint != nil
				}
			}
			if !found {
				t.Fatal("list missing hint")
			}
			afterRecent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			if !reflect.DeepEqual(beforeRecent, afterRecent) {
				t.Fatal("hint changed Recently")
			}
			select {
			case <-stream:
				t.Fatal("hint broadcast")
			default:
			}
			*now = got.FinishHint.At.Add(7 * 24 * time.Hour)
			_, err = s.applyFinished(mustRaw(t, s, v.ID))
			testkit.Must(t, err)
			_, err = s.applyFinished(mustRaw(t, s, v.ID))
			testkit.Must(t, err)
			got, err = s.Get(a, v.ID)
			testkit.Must(t, err)
			if got.FinishHint != nil {
				t.Fatal("effort hint replayed")
			}
		})
	}
}

// Match the real grouping fixture used by membership and effort-ending tests:
// owner participation, coherent cross-chat work, artefacts and an open request.
func finishEffortFixture(t *testing.T, s *Service, now *time.Time) string {
	t.Helper()
	for _, m := range []model.Message{
		{ID: "fair-1", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Pitch deck opening slides https://notebooklm.google.com/notebook/fair", Timestamp: now.AddDate(0, 0, -5)},
		{ID: "fair-2", ChatJID: testkit.Group, Sender: testkit.Phone, Content: "The pitch deck layout is ready https://docs.google.com/document/d/fair", Timestamp: now.AddDate(0, 0, -3)},
		{ID: "fair-3", ChatJID: testkit.Phone, IsFromMe: true, Content: "I will review the pitch deck https://github.com/example/fair", Timestamp: now.AddDate(0, 0, -2)},
		{ID: "fair-4", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: "Can you review the pitch deck slides? Due tomorrow. https://github.com/example/fair", Timestamp: now.Add(-time.Hour)},
	} {
		testkit.Must(t, s.Hub.Messages.PutMessage(m))
	}
	es, err := s.Efforts.List(nil, efforts.Filter{IncludeUntitled: true})
	testkit.Must(t, err)
	if len(es) != 1 || es[0].Status != "on_you" || len(es[0].MessageRefs) != 4 || len(es[0].Chats) != 2 {
		t.Fatalf("fixture must contain one real owner-involved work effort: %+v", es)
	}
	t.Logf("real effort: status=%s refs=%d chats=%d", es[0].Status, len(es[0].MessageRefs), len(es[0].Chats))
	return es[0].ID
}
