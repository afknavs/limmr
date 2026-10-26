package integration_test

import (
	"context"
	"encoding/json"
	"limmr/internal/efforts"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"strings"
	"testing"
	"time"
)

func newTask(t *testing.T, r *rig) tasks.Task {
	t.Helper()
	out, err := r.call(t, "task", map[string]any{"action": "create", "title": "Prepare Maya's study material", "goal": "Track her learning", "kind": "long", "chats": []tasks.Entity{{ID: chatA}}, "people": []tasks.Entity{{ID: "maya", Name: "Maya Rao"}}, "checklist": []tasks.Step{{Text: "Read the syllabus"}}})
	must(t, err)
	return out.(tasks.Task)
}
func TestTaskMCPActionsPermissionVisibilityAndSendHistory(t *testing.T) {
	r := newRig(t)
	v := newTask(t, r)
	for _, args := range []map[string]any{
		{"action": "get", "id": v.ID},
		{"action": "update", "id": v.ID, "summary": map[string]any{"text": "Maya has started revision."}},
		{"action": "attach", "id": v.ID, "item": map[string]any{"kind": "note", "title": "Next topic", "text": "Practice memory exercises"}},
		{"action": "checklist", "id": v.ID, "op": "done", "steps": []map[string]any{{"id": v.Checklist[0].ID}}},
		{"action": "needs_you", "id": v.ID, "reason": "Review the next topic"},
	} {
		_, err := r.call(t, "task", args)
		must(t, err)
	}
	// Other given AIs can read, but cannot update someone else's task.
	other, _, err := r.h.CreateAgent("Given AI", "custom", "local")
	must(t, err)
	other.Preset = "given"
	must(t, r.h.UpdateAgent(other))
	_, err = r.mc.Call(context.Background(), other.ID, "task", map[string]any{"action": "update", "id": v.ID, "title": "Unapproved"})
	if err == nil {
		t.Fatal("given AI changed another creator's task")
	}
	_, err = r.mc.Call(context.Background(), other.ID, "task", map[string]any{"action": "get", "id": v.ID})
	must(t, err)
	// Send uses the existing owner-instruction/circle policy and fake Messenger.
	out, err := r.call(t, "send_message", map[string]any{"recipient": chatA, "text": "Synthetic progress update", "owner_instruction": "Send Maya this update", "task_id": v.ID})
	must(t, err)
	d := out.(model.Draft)
	if d.State != "sent" || d.TaskID != v.ID {
		t.Fatal("task send did not preserve binding")
	}
	got, err := r.mc.TaskService().Get(&r.a, v.ID)
	must(t, err)
	if len(got.UpdatesSent) != 1 || got.UpdatesSent[0].MessageID != d.MessageID {
		t.Fatal("send_message not in task history")
	}
	_, err = r.call(t, "send_message", map[string]any{"recipient": chatB, "text": "Wrong chat", "owner_instruction": "Send this update", "task_id": v.ID})
	if err == nil || len(r.fake.Sent) != 1 {
		t.Fatal("cross-task chat binding bypassed")
	}
	perms := r.a.Permissions
	perms.SaveSummaries = false
	r.agent(t, perms)
	_, err = r.call(t, "task", map[string]any{"action": "update", "id": v.ID, "goal": "forbidden"})
	if err == nil {
		t.Fatal("write capability ignored")
	}
	_, err = r.call(t, "task", map[string]any{"action": "get", "id": v.ID})
	must(t, err)
	perms.ReadMessages = false
	r.agent(t, perms)
	_, err = r.call(t, "task", map[string]any{"action": "get", "id": v.ID})
	if err == nil {
		t.Fatal("read capability ignored")
	}
	perms.ReadMessages = true
	perms.SaveSummaries = true
	r.agent(t, perms)
	set := r.h.Settings()
	set.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(set))
	_, err = r.call(t, "task", map[string]any{"action": "get", "id": v.ID})
	if err == nil {
		t.Fatal("hidden task readable")
	}
	status(t, r.request(t, "GET", "/v1/tasks/"+v.ID, r.api.ControlToken, nil, nil), 200)
}
func TestTasksControlHomeFeedAndTaskUpdates(t *testing.T) {
	r := newRig(t)
	ws, _, _ := setupWatches(t, r)
	v := newTask(t, r)
	svc := r.mc.TaskService()
	day := int(epoch.Weekday())
	_, err := r.call(t, "task", map[string]any{"action": "update", "id": v.ID, "check_in": tasks.CheckIn{Rule: "weekly", Weekday: &day, AtTime: "12:00", NextAt: epoch, To: chatA, Note: "Learning progress"}})
	must(t, err)
	_, err = r.call(t, "task", map[string]any{"action": "draft_check_in", "id": v.ID, "text": "PRIVATE_DRAFT_CANARY"})
	must(t, err)
	must(t, svc.Tick(context.Background()))
	updates, err := ws.GetUpdates(context.Background(), r.a.ID, "", 0, 100)
	must(t, err)
	due := 0
	for _, it := range updates.Items {
		if it.Kind == "task_due" {
			due++
			if it.TaskID != v.ID || it.Note != "Learning progress" || it.Progress == nil || it.Progress.Total != 1 {
				t.Fatal("incomplete due update")
			}
		}
	}
	if due != 1 {
		t.Fatal("due item missing or duplicated")
	}
	home := r.request(t, "GET", "/v1/home", r.api.ControlToken, nil, nil)
	status(t, home, 200)
	if !strings.Contains(home.Body.String(), `"tasks":[`) || !strings.Contains(home.Body.String(), v.ID) {
		t.Fatal("Home task summary missing")
	}
	feed := r.request(t, "GET", "/v1/tasks/feed?since="+epoch.Add(-time.Hour).Format(time.RFC3339), r.api.StatusToken, nil, nil)
	status(t, feed, 200)
	if strings.Contains(feed.Body.String(), "PRIVATE_DRAFT_CANARY") || strings.Contains(feed.Body.String(), "CANARY_MESSAGE") {
		t.Fatal("feed includes message prose")
	}
	status(t, r.request(t, "GET", "/v1/tasks/feed?since=bad", r.api.StatusToken, nil, nil), 400)
	for _, path := range []string{"/v1/tasks", "/v1/tasks/" + v.ID} {
		status(t, r.request(t, "GET", path, r.api.StatusToken, nil, nil), 403)
	}
	status(t, r.request(t, "POST", "/v1/tasks", r.api.StatusToken, map[string]any{"title": "No write", "kind": "short"}, nil), 403)
	// Control task creation and checklist/item routes use the same persisted store.
	w := r.request(t, "POST", "/v1/tasks", r.api.ControlToken, map[string]any{"title": "Book an appointment", "kind": "short"}, nil)
	status(t, w, 200)
	var owner tasks.Task
	must(t, json.Unmarshal(w.Body.Bytes(), &owner))
	w = r.request(t, "POST", "/v1/tasks/"+owner.ID+"/items", r.api.ControlToken, map[string]any{"kind": "note", "title": "Office hours", "text": "Call in the morning"}, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &owner))
	status(t, r.request(t, "DELETE", "/v1/tasks/"+owner.ID+"/items/"+owner.Items[0].ID, r.api.ControlToken, nil, nil), 200)
	w = r.request(t, "POST", "/v1/tasks/"+owner.ID+"/checklist", r.api.ControlToken, map[string]any{"text": "Confirm time"}, nil)
	status(t, w, 200)
	must(t, json.Unmarshal(w.Body.Bytes(), &owner))
	status(t, r.request(t, "PATCH", "/v1/tasks/"+owner.ID+"/checklist/"+owner.Checklist[0].ID, r.api.ControlToken, map[string]any{"done": true}, nil), 200)
	updates, err = ws.GetUpdates(context.Background(), r.a.ID, updates.Cursor, 0, 100)
	must(t, err)
	if len(updates.Items) != 0 {
		t.Fatal("AI got owner-created task events")
	}
	// Visibility is checked again against persisted events, after the tick.
	set := r.h.Settings()
	set.HiddenChats = []string{chatA}
	must(t, r.h.SetSettings(set))
	updates, err = ws.GetUpdates(context.Background(), r.a.ID, "", 0, 100)
	must(t, err)
	for _, it := range updates.Items {
		if it.TaskID == v.ID {
			t.Fatal("queued hidden task event leaked")
		}
	}
}
func TestTaskEffortControlDetailAndInvalidAtomicWrites(t *testing.T) {
	r := newRig(t)
	svc := r.mc.EffortService()
	xs, err := svc.List(&r.a, efforts.Filter{IncludeUntitled: true})
	must(t, err)
	if len(xs) == 0 {
		t.Fatal("missing derived effort")
	}
	out, err := r.call(t, "task", map[string]any{"action": "create", "title": "Track study work", "kind": "long", "effort_id": xs[0].ID})
	must(t, err)
	v := out.(tasks.Task)
	detail := r.request(t, "GET", "/v1/efforts/"+xs[0].ID, r.api.ControlToken, nil, nil)
	status(t, detail, 200)
	if !strings.Contains(detail.Body.String(), `"task_id":"`+v.ID+`"`) {
		t.Fatal("effort lacks task_id")
	}
	for _, bad := range []map[string]any{
		{"action": "update", "id": v.ID, "title": strings.Repeat("x", 61)},
		{"action": "update", "id": v.ID, "status": "bogus", "goal": "MUST_NOT_COMMIT"},
		{"action": "attach", "id": v.ID, "item": map[string]any{"kind": "link", "url": "file:///private"}},
		{"action": "checklist", "id": v.ID, "op": "add", "steps": []tasks.Step{{Text: strings.Repeat("x", 201)}}},
		{"action": "update", "id": v.ID, "waiting": map[string]any{"what": "notes", "nudge_after_days": 31}},
		{"action": "get", "id": v.ID, "unknown": "PRIVATE_INPUT_CANARY"},
	} {
		if _, err = r.call(t, "task", bad); err == nil {
			t.Fatal("invalid task write accepted")
		}
	}
	got, err := r.mc.TaskService().Get(&r.a, v.ID)
	must(t, err)
	if got.Title != v.Title || got.Goal == "MUST_NOT_COMMIT" || len(got.Items) != len(v.Items) {
		t.Fatal("invalid write changed stored task")
	}
}

func TestTaskOwnerSuggestionAndThisMacNudgeBridge(t *testing.T) {
	r := newRig(t)
	svc := r.mc.TaskService()
	v, err := svc.Create(nil, tasks.Create{Title: "Request the syllabus", Kind: "long", Chats: []tasks.Entity{{ID: chatA}}, People: []tasks.Entity{{ID: "maya", Name: "Maya Rao"}}})
	must(t, err)
	_, err = svc.Update(nil, v.ID, map[string]any{"waiting": tasks.Waiting{On: &v.People[0], What: "the syllabus", Since: epoch.AddDate(0, 0, -4)}})
	must(t, err)
	must(t, svc.Tick(context.Background()))
	status(t, r.request(t, "POST", "/v1/suggestions", r.api.ControlToken, map[string]any{"task_id": v.ID, "kind": "nudge", "draft_text": "A private local draft", "created_by": "this-mac"}, nil), 403)
	_, err = r.h.EnableThisMac()
	must(t, err)
	status(t, r.request(t, "POST", "/v1/suggestions", r.api.ControlToken, map[string]any{"task_id": v.ID, "kind": "nudge", "draft_text": "A private local draft", "created_by": "this-mac"}, nil), 200)
	xs, err := r.h.Suggestions()
	must(t, err)
	if len(xs) != 1 || xs[0].DraftText != "A private local draft" {
		t.Fatal("local worker did not enrich task nudge")
	}
	w := r.request(t, "POST", "/v1/suggestions/"+xs[0].ID+"/send", r.api.ControlToken, map[string]any{}, nil)
	status(t, w, 200)
	got, err := svc.Get(nil, v.ID)
	must(t, err)
	if got.Waiting.NudgeState != "sent" || len(got.UpdatesSent) != 1 || len(r.fake.Sent) != 1 {
		t.Fatal("owner-created task could not send through control review")
	}
}
