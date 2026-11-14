package tasks

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"limmr/internal/dbutil"
	"limmr/internal/model"
	"limmr/internal/testkit"
	"limmr/internal/wa"
)

func writeProtocolApproval(t *testing.T, s *Service, keeps ...string) {
	t.Helper()
	if keeps == nil {
		keeps = []string{}
	}
	testkit.Must(t, os.WriteFile(filepath.Join(s.Hub.Home, "protocol-v1-approved.json"), []byte(dbutil.JSON(map[string]any{"approved_at": s.now().Format(time.RFC3339), "keep_task_ids": keeps})), 0600))
}
func protocolState(t *testing.T, s *Service) string {
	t.Helper()
	state := map[string]any{}
	for _, table := range []string{"tasks", "task_events", "objects", "sync_state", "sync_log", "sync_meta", "sync_items", "p1_revision", "activity", "agent_updates"} {
		rows, err := s.Hub.Repo.DB.Query("SELECT * FROM " + table + " ORDER BY rowid")
		testkit.Must(t, err)
		cols, err := rows.Columns()
		testkit.Must(t, err)
		all := [][]any{}
		for rows.Next() {
			row, ptrs := make([]any, len(cols)), make([]any, len(cols))
			for i := range row {
				ptrs[i] = &row[i]
			}
			testkit.Must(t, rows.Scan(ptrs...))
			for i, v := range row {
				if b, ok := v.([]byte); ok {
					row[i] = string(b)
				}
			}
			all = append(all, row)
		}
		testkit.Must(t, rows.Err())
		rows.Close()
		state[table] = all
	}
	return dbutil.JSON(state)
}
func TestProtocolApprovalRequiredAndValid(t *testing.T) {
	for _, raw := range []string{"absent", "{", "null", "[]", `{}`, `{"approved_at":"bad","keep_task_ids":[]}`, `{"approved_at":"2026-11-11T12:00:00Z"}`, `{"approved_at":"2026-11-11T12:00:00Z","keep_task_ids":null}`, `{"approved_at":"2026-11-11T12:00:00Z","keep_task_ids":[2]}`, `{"approved_at":"2026-11-11T12:00:00Z","keep_task_ids":[""]}`, `{"approved_at":"2026-11-11T12:00:00Z","keep_task_ids":[],"apply":true}`, `{"approved_at":"2026-11-11T12:00:00Z","keep_task_ids":[]} {}`} {
		t.Run(raw, func(t *testing.T) {
			s, _, now := fixture(t)
			protocolSeed(t, s, *now)
			if raw != "absent" {
				testkit.Must(t, os.WriteFile(filepath.Join(s.Hub.Home, "protocol-v1-approved.json"), []byte(raw), 0600))
			}
			before := protocolState(t, s)
			var logs bytes.Buffer
			testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), log.New(&logs, "", 0)))
			if protocolState(t, s) != before {
				t.Fatal("unapproved pass changed database")
			}
			if strings.Count(logs.String(), "\n") != 1 || !strings.Contains(logs.String(), "skipped") {
				t.Fatal(logs.String())
			}
		})
	}
}
func TestProtocolApplyMatchesPlanRecordByRecord(t *testing.T) {
	s, _, now := fixture(t)
	originals := protocolSeed(t, s, *now)
	db, _ := protocolCopy(t, s, true)
	plan, err := PlanProtocolPass(db, *now, time.UTC, ProtocolOptions{KeepTaskIDs: []string{"unasked", "missing", "unasked"}})
	testkit.Must(t, err)
	if protocolByID(t, plan, "unasked").Outcome != "Stays a task" {
		t.Fatal("keep did not change preview")
	}
	writeProtocolApproval(t, s, "unasked", "missing", "unasked")
	counts := finishTickCounts(t, s)
	recent, err := s.Recently(nil, "")
	testkit.Must(t, err)
	stream, cancel := s.Hub.Subscribe()
	defer cancel()
	var logs bytes.Buffer
	testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), log.New(&logs, "", 0)))
	if !strings.Contains(logs.String(), "ignored 1 unknown keep_task_ids") {
		t.Fatal(logs.String())
	}
	for _, before := range originals {
		want := before
		p := protocolByID(t, plan, before.ID)
		switch before.ID {
		case "auto", "circle":
			if !protocolHas(p, "demote", "back to suggestion") {
				t.Fatal(p)
			}
			want.Status = "suggested"
			want.Suggestion = &Suggestion{DetectedAt: *now}
		case "unasked":
			want.Origin = "owner"
			want.AutoKeptAt = nil
			want.AskedByOwner = &AskedByOwner{OwnerRequest: OwnerRequest{Text: protocolReviewKeepText}, AgentID: "you", At: *now}
		case "deadline":
			if !protocolHas(p, "deadline", "ends as done") {
				t.Fatal(p)
			}
			want.Status, want.CloseReason, want.ClosedAt = "done", "lifespan", now
		case "obsolete", "test":
			if !protocolHas(p, "obsolete", "ends as dropped") {
				t.Fatal(p)
			}
			want.Status, want.CloseReason, want.ClosedAt = "dropped", "obsolete", now
		case "expired":
			if !protocolHas(p, "suggestion_expiry", "expires") {
				t.Fatal(p)
			}
			want.Status, want.CloseReason, want.ClosedAt, want.EndedAsSuggestion = "dropped", "suggestion_expired", now, true
		}
		got := mustRaw(t, s, before.ID)
		if dbutil.JSON(got) != dbutil.JSON(want) {
			t.Errorf("%s record differs\ngot %s\nwant %s", before.ID, dbutil.JSON(got), dbutil.JSON(want))
		}
	}
	var marker string
	testkit.Must(t, s.Hub.Repo.Get("settings", protocolMarker, &marker))
	if marker != now.Format(time.RFC3339Nano) {
		t.Fatal(marker)
	}
	var circle model.Circle
	testkit.Must(t, s.Hub.Repo.Get("circles", "old-circle", &circle))
	if !circle.Archived {
		t.Fatal("migration circle unarchived")
	}
	history, err := s.History("unasked")
	testkit.Must(t, err)
	if len(history) != 1 || history[0].Text != protocolReviewKeepText+" · you" {
		t.Fatal(history)
	}
	var by, storedText string
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT json_extract(payload,'$.by'),text FROM task_events WHERE task_id='unasked' AND kind='protocol_kept'").Scan(&by, &storedText))
	if by != "you" || storedText != protocolReviewKeepText {
		t.Fatal("review keep lost plain text or owner attribution", storedText, by)
	}
	afterRecent, err := s.Recently(nil, "")
	testkit.Must(t, err)
	if !reflect.DeepEqual(counts, finishTickCounts(t, s)) || !reflect.DeepEqual(recent, afterRecent) || len(s.Hub.Messenger.(*wa.Fake).Sent) != 0 {
		t.Fatal("attention, Recently or sends changed")
	}
	if len(stream) != 1 || (<-stream).Tool != "task_changed" {
		t.Fatal("unexpected broadcasts")
	}
	var needs int
	testkit.Must(t, s.Hub.Repo.DB.QueryRow("SELECT count(*) FROM tasks WHERE json_extract(data,'$.needs_you')=1").Scan(&needs))
	if needs != 0 {
		t.Fatal("Needs you created")
	}
	state := protocolState(t, s)
	// A fresh service represents another start, with the approval file retained.
	fresh := New(s.Hub, s.Efforts)
	fresh.Location = time.UTC
	testkit.Must(t, fresh.ApplyApprovedProtocolPass(context.Background(), log.New(&logs, "", 0)))
	if protocolState(t, s) != state || len(stream) != 0 {
		t.Fatal("second startup applied again")
	}
	// Even a now-malformed approval cannot override the stored marker.
	testkit.Must(t, os.WriteFile(filepath.Join(s.Hub.Home, "protocol-v1-approved.json"), []byte("bad"), 0600))
	testkit.Must(t, fresh.ApplyApprovedProtocolPass(context.Background(), nil))
	if protocolState(t, s) != state {
		t.Fatal("marker ignored")
	}
	for _, id := range []string{"auto", "circle"} {
		h, err := s.History(id)
		testkit.Must(t, err)
		if len(h) != 1 || h[0].Text != "Moved back to suggestions: Limmr made this, you never asked for it" || h[0].ChangeID == "" {
			t.Fatal(h)
		}
		_, err = s.UndoMembershipChange(nil, id, h[0].ChangeID)
		testkit.Must(t, err)
		r := mustRaw(t, s, id)
		if r.Status != "active" || r.Origin != "owner" || r.AskedByOwner == nil || r.AskedByOwner.Text != protocolReviewKeepText {
			t.Fatal("undo did not keep previous task", r)
		}
	}
	still, err := PlanProtocolPass(s.Hub.Repo.DB, *now, time.UTC)
	testkit.Must(t, err)
	if protocolHas(protocolByID(t, still, "auto"), "demote", "back to suggestion") {
		t.Fatal("undo failed future owner gate")
	}
	testkit.Must(t, s.Hub.Repo.Get("circles", "old-circle", &circle))
	if !circle.Archived {
		t.Fatal("undo unarchived circle")
	}
}
func TestProtocolApplyRollbackMidPass(t *testing.T) {
	s, _, now := fixture(t)
	protocolSeed(t, s, *now)
	writeProtocolApproval(t, s)
	// 'auto' is updated before 'circle'; fail the latter to prove earlier writes,
	// history, change objects, sync capture and revision updates all roll back.
	_, err := s.Hub.Repo.DB.Exec("CREATE TRIGGER invented_failure BEFORE UPDATE ON tasks WHEN NEW.id='circle' BEGIN SELECT RAISE(ABORT,'invented mid-pass failure'); END")
	testkit.Must(t, err)
	before := protocolState(t, s)
	stream, cancel := s.Hub.Subscribe()
	defer cancel()
	err = s.ApplyApprovedProtocolPass(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "invented mid-pass failure") {
		t.Fatal(err)
	}
	if protocolState(t, s) != before || len(stream) != 0 {
		t.Fatal("partial pass committed")
	}
}
func TestProtocolApplyUndoPreviousStatusAndWindow(t *testing.T) {
	for _, status := range []string{"waiting", "quiet", "done", "dropped"} {
		t.Run(status, func(t *testing.T) {
			s, _, now := fixture(t)
			protocolSeed(t, s, *now)
			_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.status',?) WHERE id='auto'", status)
			testkit.Must(t, err)
			writeProtocolApproval(t, s)
			testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), nil))
			h, err := s.History("auto")
			testkit.Must(t, err)
			*now = now.Add(30*24*time.Hour - time.Nanosecond)
			_, err = s.UndoMembershipChange(nil, "auto", h[0].ChangeID)
			testkit.Must(t, err)
			if mustRaw(t, s, "auto").Status != status {
				t.Fatal("wrong restored status")
			}
			if _, err = s.UndoMembershipChange(nil, "auto", h[0].ChangeID); err == nil {
				t.Fatal("undo consumed twice")
			}
		})
	}
	t.Run("expired", func(t *testing.T) {
		s, _, now := fixture(t)
		protocolSeed(t, s, *now)
		writeProtocolApproval(t, s)
		testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), nil))
		h, err := s.History("auto")
		testkit.Must(t, err)
		*now = now.Add(30 * 24 * time.Hour)
		before := protocolState(t, s)
		if _, err = s.UndoMembershipChange(nil, "auto", h[0].ChangeID); err == nil {
			t.Fatal("expired undo accepted")
		}
		if before != protocolState(t, s) {
			t.Fatal("expired undo changed state")
		}
	})
}

func TestProtocolApplyFinishedAndFollowsWork(t *testing.T) {
	for _, mode := range []string{"owner", "contact", "checklist", "follows"} {
		t.Run(mode, func(t *testing.T) {
			s, _, now, v := finishedFixture(t)
			if mode == "follows" {
				s, v, _, _ = followsFixture(t)
				now = ptrTime(s.now())
			} else if mode == "checklist" {
				_, err := s.Hub.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.end_condition','checklist_done','$.checklist',json(?)) WHERE id=?", dbutil.JSON([]Step{{ID: "step", Text: "Deliver poster", Done: true}}), v.ID)
				testkit.Must(t, err)
			} else {
				finishMessagePut(t, s, now, "Science fair poster is done", mode, now.Add(time.Minute))
			}
			db, _ := protocolCopy(t, s, true)
			plan, err := PlanProtocolPass(db, *now, time.UTC)
			testkit.Must(t, err)
			p := protocolByID(t, plan, v.ID)
			writeProtocolApproval(t, s)
			testkit.Must(t, s.ApplyApprovedProtocolPass(context.Background(), nil))
			got := mustRaw(t, s, v.ID)
			switch mode {
			case "owner", "checklist":
				if !protocolHas(p, "finished", "ends as done") || got.Status != "done" || got.CloseReason != "done" {
					t.Fatal(p, got)
				}
			case "contact":
				if !protocolHas(p, "finished", "looks finished?") || got.FinishHint == nil || got.Status != "active" {
					t.Fatal(p, got)
				}
			case "follows":
				if !protocolHas(p, "follows_work", "remove departed members") || member(got.Chats, writingChat) != nil {
					t.Fatal(p, got)
				}
			}
			if mode != "contact" {
				h, err := s.History(v.ID)
				testkit.Must(t, err)
				_, err = s.UndoMembershipChange(nil, v.ID, h[0].ChangeID)
				testkit.Must(t, err)
			}
		})
	}
}

// Keep these compile-time contracts explicit: both offline DB and a reserved
// transaction feed the identical exported planner.
var _ dbutil.Database = protocolTransaction{}
