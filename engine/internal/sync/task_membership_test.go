package sync_test

import (
	"encoding/json"
	"limmr/internal/dbutil"
	"limmr/internal/tasks"
	"reflect"
	"testing"
	"time"
)

func TestTaskMembershipProvenanceSyncRoundTrip(t *testing.T) {
	a, b := newEngine(t), newEngine(t)
	pair(t, a, b)
	task(t, a, "fair", "Science fair plan", nil)
	var original tasks.Task
	var raw string
	must(t, a.h.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id='fair'").Scan(&raw))
	must(t, json.Unmarshal([]byte(raw), &original))
	original.Membership = &tasks.Membership{Chats: map[string]string{original.Chats[0].ID: "effort:fair"}, People: map[string]string{}}
	for i, source := range []string{"owner", "agent:example", "detector", "merge", "circle_conversion", "backfill", "follows", "legacy"} {
		id := []string{"mira", "maya", "robin", "noor", "lee", "ravi", "kai", "sam"}[i]
		original.People = append(original.People, tasks.Entity{ID: id, Name: id})
		original.Membership.People[id] = source
	}
	original.RemovedMembers = tasks.RemovedMembers{Chats: []string{"120363000099@g.us"}, People: []string{"removed-mira"}}
	original.EvidenceRefs = []tasks.Ref{{ChatJID: original.Chats[0].ID, MessageID: "fair-evidence"}}
	_, err := a.h.Repo.DB.Exec("UPDATE tasks SET data=? WHERE id=?", dbutil.JSON(original), original.ID)
	must(t, err)
	exchange(t, a, b)
	read := func(e *engine) tasks.Task {
		t.Helper()
		var v tasks.Task
		var raw string
		must(t, e.h.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id='fair'").Scan(&raw))
		must(t, json.Unmarshal([]byte(raw), &v))
		return v
	}
	assert := func(v tasks.Task) {
		t.Helper()
		if !reflect.DeepEqual(v.Membership, original.Membership) || !reflect.DeepEqual(v.RemovedMembers, original.RemovedMembers) || !reflect.DeepEqual(v.EvidenceRefs, original.EvidenceRefs) {
			t.Fatalf("membership metadata lost after sync: %+v", v)
		}
	}
	assert(read(b))
	b.now = b.now.Add(time.Hour)
	must(t, b.clock())
	_, err = b.tasks.Update(nil, "fair", map[string]any{"title": "Build the fair exhibit"})
	must(t, err)
	exchange(t, b, a)
	assert(read(a))
	assert(read(b))
}
