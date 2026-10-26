package sync_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	syncer "limmr/internal/sync"
	"limmr/internal/tasks"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBriefCollisionRenumbersBothSources(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	put(t, a, "briefs", "study:1", map[string]any{"circle_id": "study", "version": 1, "summary": "Maya read the first chapter"})
	b.now = b.now.Add(time.Minute)
	must(t, b.clock())
	put(t, b, "briefs", "study:1", map[string]any{"circle_id": "study", "version": 1, "summary": "Riya prepared exercises"})
	pair(t, a, b)
	exchange(t, a, b)
	same(t, a, b)
	for _, e := range []*engine{a, b} {
		var first, second map[string]any
		must(t, e.h.Repo.Get("briefs", "study:000000001", &first))
		must(t, e.h.Repo.Get("briefs", "study:000000002", &second))
		if first["summary"] != "Maya read the first chapter" || second["summary"] != "Riya prepared exercises" {
			t.Fatal("brief lost or renumbered in reverse order")
		}
	}
	exchange(t, a, b)
	same(t, a, b)
}

func TestTransactionalCaptureAndScope(t *testing.T) {
	e := newEngine(t)
	var before int
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&before))
	tx, err := e.h.Repo.DB.Begin()
	must(t, err)
	_, err = tx.Exec(`INSERT INTO objects VALUES('people','rollback','{"id":"rollback","label":"Maya"}')`)
	must(t, err)
	_, err = tx.Exec(`INSERT INTO tasks VALUES('rollback-task','{"id":"rollback-task","checklist":[{"id":"step","text":"Read"}]}')`)
	must(t, err)
	must(t, tx.Rollback())
	var n int
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&n))
	if n != before {
		t.Fatal("rolled-back write leaked a log")
	}
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_meta WHERE id IN ('rollback','rollback-task')`).Scan(&n))
	if n != 0 {
		t.Fatal("rolled-back metadata survived")
	}
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_items WHERE task_id='rollback-task'`).Scan(&n))
	if n != 0 {
		t.Fatal("rolled-back items survived")
	}
	for _, bucket := range []string{"agents", "drafts", "activity", "groups", "effort_edits", "normalized_pairs", "settings"} {
		var value any = map[string]any{"name": "Invented local state"}
		if bucket == "agents" {
			value = model.Agent{ID: "local-only", Name: "Invented local AI", Kind: "custom", Transport: "local", CreatedAt: e.now, Enabled: true, Preset: "own", Permissions: model.Preset("own")}
		}
		if bucket == "drafts" {
			value = map[string]any{"id": "local-only", "agent_id": "local-only", "chat": "15550000001@s.whatsapp.net", "text": "Invented draft", "status": "pending"}
		}
		put(t, e, bucket, "local-only", value)
	}
	put(t, e, "memberships", "wa-group-maya", map[string]any{"group": "15550000010@g.us", "person": "maya", "admin": false})
	must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&n))
	if n != before {
		t.Fatal("machine-local state escaped scope")
	}
	// Raw SQL writes are captured too, and relations retain a hidden removed row.
	_, err = e.h.Repo.DB.Exec(`INSERT INTO objects VALUES('relations','friend','{"id":"friend","a":"me","b":"maya"}');DELETE FROM objects WHERE bucket='relations' AND id='friend'`)
	must(t, err)
	var status string
	must(t, e.h.Repo.DB.QueryRow(`SELECT json_extract(data,'$.status') FROM objects WHERE bucket='relations' AND id='friend'`).Scan(&status))
	if status != "removed" {
		t.Fatal("relation hard deleted")
	}
	xs, err := hub.List[map[string]any](e.h.Repo, "relations")
	must(t, err)
	if len(xs) != 0 {
		t.Fatal("removed relation still visible")
	}
}

func TestTaskEventCollisionOrderAndReplay(t *testing.T) {
	for _, tie := range []bool{false, true} {
		t.Run(fmt.Sprintf("equal_at_%t", tie), func(t *testing.T) {
			a, b := newEngine(t), newEngine(t)
			winner, loser := a, b
			if tie && winner.s.EngineID > loser.s.EngineID {
				winner, loser = loser, winner
			}
			for _, e := range []*engine{winner, loser} {
				task(t, e, "study", "Study", nil)
				at, text := e.now, "Earliest observation"
				if e == loser {
					text = "Losing observation"
					if !tie {
						at = at.Add(time.Minute)
					}
				}
				_, err := e.h.Repo.DB.Exec(`INSERT INTO task_events VALUES('study:slot','study',?,'task_due',?,?)`, at.UTC().Format(time.RFC3339Nano), text, dbutil.JSON(map[string]any{"observation": text}))
				must(t, err)
			}
			pair(t, a, b)
			// Exchange in both directions and replay the winning/losing histories.
			exchange(t, loser, winner)
			exchange(t, winner, loser)
			same(t, a, b)
			for _, e := range []*engine{a, b} {
				var text, payload, origin string
				must(t, e.h.Repo.DB.QueryRow(`SELECT e.text,e.payload,m.origin FROM task_events e JOIN sync_meta m ON m.bucket='task_events' AND m.id=e.id WHERE e.id='study:slot'`).Scan(&text, &payload, &origin))
				if text != "Earliest observation" || !strings.Contains(payload, text) || origin != winner.s.EngineID {
					t.Fatal("same-id event did not retain the winning payload and origin")
				}
			}
		})
	}
}

func TestForgetScrubsReplicatedKnowledgeAndHistory(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	put(t, a, "people", "maya", model.Person{ID: "maya", Name: "Maya", Notes: "Private owner note"})
	put(t, a, "facts", "studio", model.Fact{ID: "studio", Subject: "person", SubjectID: "maya", Text: "Private studio fact"})
	put(t, a, "facts", "old", model.Fact{ID: "old", Subject: "person", SubjectID: "maya", Text: "Private old fact"})
	put(t, a, "relations", "friend", model.Relation{ID: "friend", A: "me", B: "maya", Kind: "friend", Note: "Private relation note"})
	put(t, a, "memberships", "placement", map[string]any{"person": "maya", "role": "Private placement"})
	put(t, a, "circles", "studio", model.Circle{ID: "studio", Name: "Studio", Confirmed: true, People: []model.CirclePerson{{PersonID: "maya", Role: "Private circle role"}}, Topics: []model.Topic{{ID: "topic", People: []string{"maya"}}}})
	must(t, a.h.Repo.Delete("facts", "old"))
	pair(t, a, b)
	exchange(t, a, b)
	old, err := b.s.Pull(ctx, syncer.PullRequest{PeerEngineID: a.s.EngineID, SyncVersion: 1, Schema: 10})
	must(t, err)
	must(t, a.p.Forget("maya"))
	exchange(t, a, b)
	// Replaying a pre-forget page cannot restore payloads or circle references.
	_, err = a.s.Push(ctx, syncer.PushRequest{Changes: old.Changes, FromEngineID: b.s.EngineID, SyncVersion: 1, Schema: 10})
	must(t, err)
	exchange(t, a, b)
	same(t, a, b)
	for _, e := range []*engine{a, b} {
		for bucket, id := range map[string]string{"people": "maya", "facts": "studio", "relations": "friend", "memberships": "placement"} {
			var value any
			if err := e.h.Repo.Get(bucket, id, &value); !errors.Is(err, model.ErrNotFound) {
				t.Fatalf("forgotten %s still readable: %v", bucket, err)
			}
		}
		exported, err := e.p.Export(nil)
		must(t, err)
		peer := a.s.EngineID
		if e == a {
			peer = b.s.EngineID
		}
		page, err := e.s.Pull(ctx, syncer.PullRequest{PeerEngineID: peer, SyncVersion: 1, Schema: 10})
		must(t, err)
		rows, err := e.h.Repo.DB.Query(`SELECT data FROM objects UNION ALL SELECT data FROM sync_log UNION ALL SELECT data FROM sync_deleted`)
		must(t, err)
		var retained strings.Builder
		for rows.Next() {
			var raw string
			must(t, rows.Scan(&raw))
			retained.WriteString(raw)
		}
		must(t, rows.Err())
		rows.Close()
		for _, view := range []string{dbutil.JSON(exported), dbutil.JSON(page), retained.String()} {
			if strings.Contains(view, "Private ") {
				t.Fatal("forgotten payload remains readable in an export or retained store")
			}
		}
		var raw string
		must(t, e.h.Repo.DB.QueryRow(`SELECT data FROM objects WHERE bucket='relations' AND id='friend'`).Scan(&raw))
		if raw != `{"forgotten":true,"status":"removed"}` {
			t.Fatal("relation retained personal data")
		}
	}
}

func TestUnknownBucketAndFailedApplyRollbackEntireBatch(t *testing.T) {
	e := newEngine(t)
	valid := syncer.Change{Bucket: "people", ID: "maya", HLC: e.now.UnixMilli() + 100, Origin: "invented-peer", Op: "upsert", Data: json.RawMessage(`{"id":"maya","label":"Maya"}`)}
	for _, bad := range []syncer.Change{
		{Bucket: "future_bucket", ID: "unknown", HLC: valid.HLC, Origin: valid.Origin, Op: "upsert", Data: json.RawMessage(`{}`)},
		{Bucket: "briefs", ID: "invalid-brief", HLC: valid.HLC + 1, Origin: valid.Origin, Op: "upsert", Data: json.RawMessage(`{"version":0}`)},
	} {
		before := knowledge(t, e)
		var count, after int
		must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&count))
		_, err := e.s.Push(ctx, syncer.PushRequest{Changes: []syncer.Change{valid, bad}, FromEngineID: valid.Origin, SyncVersion: 1, Schema: 10})
		if err == nil {
			t.Fatal("bad batch accepted")
		}
		if bad.Bucket == "future_bucket" && !errors.Is(err, syncer.ErrUpdateNeeded) {
			t.Fatal(err)
		}
		must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_log`).Scan(&after))
		if count != after || !reflect.DeepEqual(before, knowledge(t, e)) {
			t.Fatal("partial batch committed")
		}
		var active int
		must(t, e.h.Repo.DB.QueryRow(`SELECT count(*) FROM sync_state WHERE remote_hlc IS NOT NULL OR suppress<>0`).Scan(&active))
		if active != 0 {
			t.Fatal("apply context escaped rollback")
		}
	}
}

func TestPagingReplayAndClockSurviveRestart(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	// More than 500 changes must span pages, and a repeated page must be stale.
	for i := 0; i < 510; i++ {
		id := dbutil.ID()
		put(t, a, "people", id, map[string]any{"id": id, "label": "Maya"})
	}
	page, err := a.s.Pull(ctx, syncer.PullRequest{PeerEngineID: b.s.EngineID, SyncVersion: 1, Schema: 10})
	must(t, err)
	if len(page.Changes) != 500 || !page.More {
		t.Fatal("page bound not enforced")
	}
	q := syncer.PushRequest{Changes: page.Changes, FromEngineID: a.s.EngineID, SyncVersion: 1, Schema: 10}
	_, err = b.s.Push(ctx, q)
	must(t, err)
	r, err := b.s.Push(ctx, q)
	must(t, err)
	if r.Applied != 0 || r.Stale != 500 {
		t.Fatalf("replay %+v", r)
	}
	pair(t, a, b)
	exchange(t, a, b)
	same(t, a, b)
	var last int64
	must(t, a.h.Repo.DB.QueryRow(`SELECT max(hlc) FROM sync_log`).Scan(&last))
	original := a.s.EngineID
	reopened, err := hub.Open(filepath.Join(a.home, "hub.db"))
	must(t, err)
	defer reopened.Close()
	svc, err := syncer.New(reopened, a.home, time.Now)
	must(t, err)
	if svc.EngineID != original {
		t.Fatal("engine id changed on reopen")
	}
	_, err = reopened.DB.Exec(`UPDATE sync_state SET wall_ms=1`)
	must(t, err)
	must(t, reopened.Put("people", "after-restart", map[string]any{"id": "after-restart", "label": "Riya"}))
	var next int64
	must(t, reopened.DB.QueryRow(`SELECT hlc FROM sync_meta WHERE bucket='people' AND id='after-restart'`).Scan(&next))
	if next <= last {
		t.Fatal("clock moved backwards")
	}
}

func TestY3SuggestedTasksAndDismissalsSync(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	task(t, a, "candidate", "Study", nil)
	_, err := a.h.Repo.DB.Exec(`UPDATE tasks SET data=json_set(data,'$.status','suggested') WHERE id='candidate'`)
	must(t, err)
	put(t, a, "task_dismissals", "message-seed", map[string]any{"id": "message-seed", "reason": "not_a_task"})
	pair(t, a, b)
	exchange(t, a, b)
	same(t, a, b)
	var v tasks.Task
	var raw string
	must(t, b.h.Repo.DB.QueryRow(`SELECT data FROM tasks WHERE id='candidate'`).Scan(&raw))
	must(t, json.Unmarshal([]byte(raw), &v))
	if v.Status != "suggested" {
		t.Fatal("suggested status lost")
	}
	_, err = b.h.Repo.DB.Exec(`UPDATE tasks SET data=json_set(data,'$.status','dropped','$.close_reason','not_a_task') WHERE id='candidate'`)
	must(t, err)
	exchange(t, a, b)
	same(t, a, b)
	must(t, a.h.Repo.DB.QueryRow(`SELECT json_extract(data,'$.close_reason') FROM tasks WHERE id='candidate'`).Scan(&raw))
	if raw != "not_a_task" {
		t.Fatal("drop decision lost")
	}
}

func TestBlobAndHandlerBounds(t *testing.T) {
	e := newEngine(t)
	dir := filepath.Join(e.home, "avatars", "custom")
	must(t, os.MkdirAll(dir, 0700))
	var imageBytes bytes.Buffer
	must(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	pngData := imageBytes.Bytes()
	sum := sha256.Sum256(pngData)
	hash := hex.EncodeToString(sum[:])
	must(t, os.WriteFile(filepath.Join(dir, hash+".png"), pngData, 0600))
	req := httptest.NewRequest(http.MethodPost, "/sync/blob", strings.NewReader(`{"sha256":"`+hash+`"}`))
	w := httptest.NewRecorder()
	e.s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), pngData) {
		t.Fatalf("blob HTTP %d", w.Code)
	}
	if _, err := e.s.Blob(ctx, "../../hub.db"); err == nil {
		t.Fatal("invalid blob path accepted")
	}
	bad := sha256.Sum256([]byte("not an image"))
	badHash := hex.EncodeToString(bad[:])
	must(t, os.WriteFile(filepath.Join(dir, badHash), []byte("not an image"), 0600))
	if _, err := e.s.Blob(ctx, badHash); err == nil {
		t.Fatal("non-image accepted")
	}
	must(t, os.Remove(filepath.Join(dir, hash+".png")))
	must(t, os.Symlink(filepath.Join(e.home, "hub.db"), filepath.Join(dir, hash+".png")))
	if _, err := e.s.Blob(ctx, hash); err == nil {
		t.Fatal("symlink accepted")
	}
	for _, path := range []string{"/sync/push", "/sync/pull"} {
		body := "{" + strings.Repeat(" ", syncer.MaxBytes+1) + "}"
		w = httptest.NewRecorder()
		e.s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		if w.Code == http.StatusOK {
			t.Fatal("oversize request accepted")
		}
	}
	w = httptest.NewRecorder()
	e.s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sync/pull", strings.NewReader(`{"peer_engine_id":"invented","sync_version":2,"schema":10}`)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"error":"update_needed"`) {
		t.Fatal("update response mismatch")
	}
}

func TestPhotoBytesFollowPersonReference(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	dir := filepath.Join(a.home, "avatars", "custom")
	must(t, os.MkdirAll(dir, 0700))
	var buf bytes.Buffer
	must(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	sum := sha256.Sum256(buf.Bytes())
	hash := hex.EncodeToString(sum[:])
	must(t, os.WriteFile(filepath.Join(dir, hash), buf.Bytes(), 0600))
	put(t, a, "people", "maya", map[string]any{"id": "maya", "label": "Maya", "photo": map[string]any{"sha256": hash, "updated_at": a.now}})
	pair(t, a, b)
	exchange(t, a, b)
	got, err := b.s.Blob(ctx, hash)
	must(t, err)
	if !bytes.Equal(got, buf.Bytes()) {
		t.Fatal("photo bytes not fetched")
	}
	fi, err := os.Stat(filepath.Join(b.home, "avatars", "custom", hash))
	must(t, err)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("photo mode")
	}
}
