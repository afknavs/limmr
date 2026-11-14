package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/testkit"
)

func duplicateFixture(t *testing.T, status string) (*Service, *model.Agent, *time.Time, Task, Task, Ref) {
	t.Helper()
	s, a, now := fixture(t)
	testkit.Must(t, s.Hub.Repo.Put("people", "mira", model.Person{ID: "mira", Name: "Mira Bell"}))
	dst, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
	testkit.Must(t, err)
	if status != "active" {
		_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.status',?) WHERE id=?", status, dst.ID)
		testkit.Must(t, err)
	}
	ref := Ref{ChatJID: testkit.Group, MessageID: "fair-evidence"}
	testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: ref.MessageID, ChatJID: ref.ChatJID, Sender: testkit.Phone, Content: "The science fair display needs a plan", Timestamp: *now}))
	src, err := s.Suggest(nil, Create{Title: dst.Title, Kind: "long", Chats: []Entity{{ID: testkit.Phone}, {ID: testkit.Group}}, People: []Entity{{ID: "mira"}}, DifferentBecause: "Fixture for duplicate review", evidenceRefs: []Ref{ref}}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	return s, a, now, dst, src, ref
}

func TestR1KeepDuplicateSilentAtomicAndUndo(t *testing.T) {
	for _, status := range []string{"active", "waiting", "quiet"} {
		t.Run(status, func(t *testing.T) {
			s, a, now, dst, src, ref := duplicateFixture(t, status)
			originalSource, err := s.raw(src.ID)
			testkit.Must(t, err)
			beforeRecent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			beforeHistory, err := s.History(dst.ID)
			testkit.Must(t, err)
			sourceHistory, err := s.History(src.ID)
			testkit.Must(t, err)
			var beforeUpdates, afterUpdates, beforePending, afterPending int
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&beforeUpdates))
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions'").Scan(&beforePending))
			stream, cancel := s.Hub.Subscribe()
			defer cancel()
			got, err := s.Keep(nil, src.ID)
			testkit.Must(t, err)
			if got.ID != dst.ID || !got.AttachedToExisting || got.Status != status || got.NeedsYou || !hasRef(got.EvidenceRefs, ref) {
				t.Fatalf("wrong attachment result: %+v", got)
			}
			select {
			case <-stream:
				t.Fatal("automatic rule broadcast activity")
			default:
			}
			stored, err := s.raw(dst.ID)
			testkit.Must(t, err)
			if len(stored.Chats) != 2 || len(stored.People) != 1 || stored.AttachedToExisting {
				t.Fatal("members/result flag stored incorrectly", stored.Task)
			}
			assertSource(t, stored.Task, testkit.Phone, "owner", false)
			assertSource(t, stored.Task, testkit.Group, "merge", false)
			assertSource(t, stored.Task, "mira", "merge", true)
			closed, err := s.raw(src.ID)
			testkit.Must(t, err)
			if closed.Status != "dropped" || closed.CloseReason != "merged" || closed.MergedInto != dst.ID || !closed.EndedAsSuggestion {
				t.Fatal("suggestion not merged", closed.Task)
			}
			history, err := s.History(dst.ID)
			testkit.Must(t, err)
			if len(history) != len(beforeHistory)+1 || history[0].Kind != "rule_duplicate" || history[0].ChangeID == "" || !strings.Contains(history[0].Text, ": same chat") || !strings.Contains(history[0].Text, "similar title") {
				t.Fatal("rule history", history)
			}
			afterSourceHistory, err := s.History(src.ID)
			testkit.Must(t, err)
			if len(afterSourceHistory) != len(sourceHistory) {
				t.Fatal("extra suggestion closing/status events")
			}
			changes := membershipChanges(t, s, dst.ID)
			if len(changes) != 1 || changes[0].Kind != "rule_duplicate" || len(changes[0].Before) == 0 || len(changes[0].SuggestionBefore) == 0 || len(changes[0].Chats) != 1 || len(changes[0].People) != 1 {
				t.Fatal("missing scoped before-state", changes)
			}
			var payload struct {
				Rule     string
				Evidence map[string]any
			}
			var raw string
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='rule_duplicate'", dst.ID).Scan(&raw))
			testkit.Must(t, json.Unmarshal([]byte(raw), &payload))
			if payload.Rule != "duplicate" || payload.Evidence["suggestion_id"] != src.ID || payload.Evidence["matched_fields"] == nil {
				t.Fatal("missing rule/evidence payload", raw)
			}
			afterRecent, err := s.Recently(nil, "")
			testkit.Must(t, err)
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&afterUpdates))
			testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM objects WHERE bucket='suggestions'").Scan(&afterPending))
			if !reflect.DeepEqual(beforeRecent, afterRecent) || beforeUpdates != afterUpdates || beforePending != afterPending {
				t.Fatal("rule surfaced outside history", beforeRecent, afterRecent)
			}
			if _, err := s.UndoMembershipChange(a, dst.ID, changes[0].ID); !errors.Is(err, model.ErrForbidden) {
				t.Fatal("agent undid rule", err)
			}
			_, err = s.Update(nil, dst.ID, map[string]any{"title": "Build science fair display"})
			testkit.Must(t, err)
			*now = now.Add(29 * 24 * time.Hour)
			_, err = s.UndoMembershipChange(nil, dst.ID, changes[0].ID)
			testkit.Must(t, err)
			stored, err = s.raw(dst.ID)
			testkit.Must(t, err)
			if len(stored.Chats) != 1 || len(stored.People) != 0 || len(stored.EvidenceRefs) != 0 || stored.Title != "Build science fair display" {
				t.Fatal("undo changed unrelated state or retained additions", stored.Task)
			}
			restored, err := s.raw(src.ID)
			testkit.Must(t, err)
			restored.UpdatedAt = originalSource.UpdatedAt
			if dbutil.JSON(restored) != dbutil.JSON(originalSource) {
				t.Fatal("suggestion snapshot not restored")
			}
			if _, err := s.UndoMembershipChange(nil, dst.ID, changes[0].ID); !errors.Is(err, model.ErrConflict) {
				t.Fatal("consumed undo accepted", err)
			}
		})
	}
}

func TestR1KeepNoDuplicatePromotes(t *testing.T) {
	s, _, now := fixture(t)
	src, err := s.Suggest(nil, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}}, Suggestion{DetectedAt: *now})
	testkit.Must(t, err)
	got, err := s.Keep(nil, src.ID)
	testkit.Must(t, err)
	if got.ID != src.ID || got.Status != "active" || got.AttachedToExisting || len(membershipChanges(t, s, got.ID)) != 0 {
		t.Fatal("ordinary keep changed", got)
	}
}

func TestR1KeepFailureRollsBackBothRecords(t *testing.T) {
	s, _, _, dst, src, _ := duplicateFixture(t, "active")
	_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.evidence_refs',json(?)) WHERE id=?", `[{"chat_jid":"`+testkit.Group+`","message_id":"missing"}]`, src.ID)
	testkit.Must(t, err)
	if _, err := s.Keep(nil, src.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("missing evidence accepted", err)
	}
	d, err := s.raw(dst.ID)
	testkit.Must(t, err)
	source, err := s.raw(src.ID)
	testkit.Must(t, err)
	if len(d.Chats) != 1 || len(d.People) != 0 || source.Status != "suggested" || len(membershipChanges(t, s, dst.ID)) != 0 {
		t.Fatal("partial merge escaped rollback")
	}
}

func TestR1RuleUndoThirtyDayBoundary(t *testing.T) {
	for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			s, _, now, dst, src, _ := duplicateFixture(t, "active")
			_, err := s.Keep(nil, src.ID)
			testkit.Must(t, err)
			change := membershipChanges(t, s, dst.ID)[0]
			*now = change.At.Add(30*24*time.Hour + offset)
			_, err = s.UndoMembershipChange(nil, dst.ID, change.ID)
			if offset < 0 {
				testkit.Must(t, err)
			} else if !errors.Is(err, model.ErrConflict) {
				t.Fatal("expired undo accepted", err)
			}
		})
	}
}

func TestR1DetectorEffortMembershipBoundary(t *testing.T) {
	for _, status := range []string{"active", "suggested", "quiet"} {
		for _, sameEffort := range []bool{true, false} {
			t.Run(status+map[bool]string{true: "/same", false: "/different"}[sameEffort], func(t *testing.T) {
				s, _, now := fixture(t)
				testkit.Must(t, s.Hub.Repo.Put("people", "mira", model.Person{ID: "mira", Name: "Mira Bell"}))
				dst, err := s.Create(nil, Create{Title: "Science fair plan", Kind: "long", Chats: []Entity{{ID: testkit.Phone}}})
				testkit.Must(t, err)
				_, err = s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.effort_id','effort-fair','$.status',?) WHERE id=?", status, dst.ID)
				testkit.Must(t, err)
				ref := Ref{ChatJID: testkit.Group, MessageID: "new-fair-message"}
				testkit.Must(t, s.Hub.Messages.PutMessage(model.Message{ID: ref.MessageID, ChatJID: ref.ChatJID, Sender: testkit.Phone, Content: "Science fair evidence", Timestamp: *now}))
				effortID := "effort-fair"
				if !sameEffort {
					effortID = "effort-other"
				}
				candidate := Task{Title: dst.Title, Goal: dst.Goal, EffortID: effortID, Chats: []Entity{{ID: testkit.Phone}, {ID: testkit.Group}}, People: []Entity{{ID: "mira", Name: "Mira Bell"}}, EvidenceRefs: []Ref{ref}}
				before, err := s.Recently(nil, "")
				testkit.Must(t, err)
				blocked, err := s.covered(candidate)
				testkit.Must(t, err)
				if !blocked {
					t.Fatal("duplicate became another suggestion")
				}
				got, err := s.raw(dst.ID)
				testkit.Must(t, err)
				if !hasRef(got.EvidenceRefs, ref) || (member(got.People, "mira") != nil) != sameEffort || (member(got.Chats, testkit.Group) != nil) != sameEffort || got.Status != status || got.NeedsYou {
					t.Fatal("cross-effort membership leaked or evidence lost", got.Task)
				}
				after, err := s.Recently(nil, "")
				testkit.Must(t, err)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("detector created recent activity")
				}
				blocked, err = s.covered(candidate)
				testkit.Must(t, err)
				if !blocked || len(membershipChanges(t, s, dst.ID)) != 1 {
					t.Fatal("repeated detection multiplied changes")
				}
				_, err = s.UndoMembershipChange(nil, dst.ID, membershipChanges(t, s, dst.ID)[0].ID)
				testkit.Must(t, err)
				got, err = s.raw(dst.ID)
				testkit.Must(t, err)
				if len(got.EvidenceRefs) != 0 || len(got.Chats) != 1 || len(got.People) != 0 {
					t.Fatal("detector undo failed", got.Task)
				}
			})
		}
	}
}

func TestU2DetectorSharedChatSuppressesSeparateSuggestion(t *testing.T) {
	s, a, now := fixture(t)
	a.Preset = "own"
	testkit.Must(t, s.Hub.Repo.Put("agents", a.ID, a))
	_, err := s.Create(nil, Create{Title: "Choose stage lighting", Kind: "long", Chats: []Entity{{ID: testkit.Group}}})
	testkit.Must(t, err)
	surfacingChat(t, s, a, *now, testkit.Group, "Study material", "exam material is due", true, 3, true)
	testkit.Must(t, s.Tick(context.Background()))
	xs, err := s.List(nil, "suggested", 100)
	testkit.Must(t, err)
	if len(xs) != 0 {
		t.Fatal("open-task chat suggested again", xs)
	}
}

func TestR1DetectorDismissalWinsBeforeAttachment(t *testing.T) {
	s, _, now, dst, src, ref := duplicateFixture(t, "active")
	_, err := s.Dismiss(nil, src.ID, "")
	testkit.Must(t, err)
	blocked, err := s.covered(Task{Title: src.Title, Chats: src.Chats, EvidenceRefs: []Ref{ref}, Suggestion: &Suggestion{DetectedAt: *now}})
	testkit.Must(t, err)
	got, err := s.raw(dst.ID)
	testkit.Must(t, err)
	if !blocked || len(got.EvidenceRefs) != 0 || len(membershipChanges(t, s, dst.ID)) != 0 {
		t.Fatal("dismissal attached evidence")
	}
}

func TestR1UndoPreservesLaterRemovalAndRefusesEditedSuggestion(t *testing.T) {
	s, _, _, dst, src, _ := duplicateFixture(t, "active")
	_, err := s.Keep(nil, src.ID)
	testkit.Must(t, err)
	change := membershipChanges(t, s, dst.ID)[0]
	_, err = s.RemoveMembers(nil, dst.ID, nil, []string{"mira"})
	testkit.Must(t, err)
	_, err = s.UndoMembershipChange(nil, dst.ID, change.ID)
	testkit.Must(t, err)
	got, err := s.raw(dst.ID)
	testkit.Must(t, err)
	if !model.Contains(got.RemovedMembers.People, "mira") {
		t.Fatal("undo removed later owner tombstone")
	}
	// A second merge can be undone only while its archived suggestion is untouched.
	_, err = s.Keep(nil, src.ID)
	testkit.Must(t, err)
	changes := membershipChanges(t, s, dst.ID)
	var second MembershipChange
	for _, x := range changes {
		if x.Kind == "rule_duplicate" {
			second = x
		}
	}
	_, err = s.Update(nil, src.ID, map[string]any{"title": "Edited archived suggestion"})
	testkit.Must(t, err)
	if _, err = s.UndoMembershipChange(nil, dst.ID, second.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatal("undo overwrote edited suggestion", err)
	}
}

// Exercise the actual suggestion lifecycle against effort read projection, not
// just synthetic terminal records in the efforts package.
func TestR1SuggestionLifecycleDoesNotFinishEffort(t *testing.T) {
	for _, ending := range []string{"dismiss", "expire", "merge"} {
		t.Run(ending, func(t *testing.T) {
			s, _, now := fixture(t)
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
			if len(es) != 1 || es[0].Status != "on_you" {
				t.Fatal("missing effort request", es)
			}
			effort := es[0]
			detectedAt := *now
			if ending == "expire" {
				detectedAt = now.Add(-14 * 24 * time.Hour)
			}
			src, err := s.Suggest(nil, Create{Title: "Review pitch deck", Kind: "long", EffortID: effort.ID}, Suggestion{DetectedAt: detectedAt})
			testkit.Must(t, err)
			switch ending {
			case "dismiss":
				_, err = s.Dismiss(nil, src.ID, "")
			case "expire":
				err = s.Tick(context.Background())
			case "merge":
				var dst Task
				dst, err = s.Create(nil, Create{Title: src.Title, Kind: "long", EffortID: effort.ID, DifferentBecause: "Existing requested work fixture"})
				if err == nil {
					var got Task
					got, err = s.Keep(nil, src.ID)
					if err == nil && (got.ID != dst.ID || !got.AttachedToExisting || len(got.EvidenceRefs) != len(effort.MessageRefs)) {
						t.Fatal("effort keep lost evidence", got)
					}
				}
			}
			testkit.Must(t, err)
			source, err := s.raw(src.ID)
			testkit.Must(t, err)
			if source.Status != "dropped" || !source.EndedAsSuggestion {
				t.Fatal("lifecycle did not end as suggestion", source.Task)
			}
			got, err := s.Efforts.Get(nil, effort.ID)
			testkit.Must(t, err)
			if got.Status != effort.Status {
				t.Fatal("suggestion ending finished effort", ending, got.Status)
			}
		})
	}
}
