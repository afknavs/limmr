package integration_test

import (
	"encoding/json"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"testing"
)

func TestTaskRulesMCPCreateRefusalMergeCoverAndConversion(t *testing.T) {
	r := newRig(t)
	args := map[string]any{"action": "create", "owner_request": map[string]any{"text": "Please track this work"}, "title": "Review studio samples", "goal": "Choose the studio samples", "kind": "long", "chats": []any{map[string]any{"id": chatA}}}
	out, err := r.call(t, "task", args)
	must(t, err)
	a := out.(tasks.Task)
	out, err = r.call(t, "task", args)
	must(t, err)
	refusal := out.(map[string]any)
	if refusal["created"] != false || len(refusal["candidates"].([]tasks.TaskSummary)) != 1 {
		t.Fatal("missing duplicate refusal")
	}
	args["different_because"] = "Choose samples for another room"
	out, err = r.call(t, "task", args)
	must(t, err)
	b := out.(tasks.Task)
	out, err = r.call(t, "task", map[string]any{"action": "merge", "owner_request": map[string]any{"text": "Merge these tasks"}, "id": a.ID, "from_id": b.ID})
	must(t, err)
	src, err := r.mc.TaskService().Get(&r.a, b.ID)
	must(t, err)
	if src.MergedInto != a.ID || src.CloseReason != "merged" {
		t.Fatal("merge transport lost archive")
	}
	must(t, r.m.PutMessage(model.Message{ID: "sample-image", ChatJID: chatA, Sender: chatA, MediaType: "image", Timestamp: epoch}))
	_, err = r.h.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", `[{"id":"sample-image-item","kind":"file","source":"owner","attachment_id":"fixture-image","media_type":"image","message_ref":{"chat_jid":"`+chatA+`","message_id":"sample-image"}}]`, a.ID)
	must(t, err)
	out, err = r.call(t, "task", map[string]any{"action": "set_cover", "id": a.ID, "item_id": "sample-image-item"})
	must(t, err)
	if out.(tasks.Task).CoverItemID != "sample-image-item" {
		t.Fatal("MCP cover not pinned")
	}
	c := circleFixture(t, r, "project", "confirm", false, groupA)
	out, err = r.call(t, "task", map[string]any{"action": "convert_circle", "owner_request": map[string]any{"text": "Track the studio visit"}, "id": c.ID, "title": "Hold studio visit", "goal": "Complete the studio visit"})
	must(t, err)
	converted := out.(tasks.Task)
	var saved model.Circle
	must(t, r.h.Repo.Get("circles", c.ID, &saved))
	if !saved.Archived || saved.ConvertedTaskID != converted.ID || len(converted.Chats) != 1 || len(converted.People) != 1 {
		t.Fatal("MCP conversion lost context")
	}
	// Explicit goal is required at the public MCP boundary.
	delete(args, "goal")
	if _, err = r.call(t, "task", args); err == nil {
		t.Fatal("MCP accepted missing goal")
	}
}

func TestTaskRulesControlOperationsAndConflictEnvelope(t *testing.T) {
	r := newRig(t)
	create := map[string]any{"title": "Review room samples", "goal": "Pick room materials", "kind": "long", "chats": []any{map[string]any{"id": chatA}}}
	response := r.request(t, "POST", "/v1/tasks", r.api.ControlToken, create, nil)
	status(t, response, 200)
	var a tasks.Task
	must(t, json.Unmarshal(response.Body.Bytes(), &a))
	response = r.request(t, "POST", "/v1/tasks", r.api.ControlToken, create, nil)
	status(t, response, 409)
	var conflict struct{ Candidates []tasks.TaskSummary }
	must(t, json.Unmarshal(response.Body.Bytes(), &conflict))
	if len(conflict.Candidates) != 1 || conflict.Candidates[0].ID != a.ID {
		t.Fatal("control conflict missing candidates")
	}
	create["different_because"] = "Different room with separate materials"
	response = r.request(t, "POST", "/v1/tasks", r.api.ControlToken, create, nil)
	status(t, response, 200)
	var b tasks.Task
	must(t, json.Unmarshal(response.Body.Bytes(), &b))
	status(t, r.request(t, "POST", "/v1/tasks/"+a.ID+"/merge", r.api.StatusToken, map[string]any{"from_id": b.ID}, nil), 403)
	status(t, r.request(t, "POST", "/v1/tasks/"+a.ID+"/merge", r.api.ControlToken, map[string]any{"from_id": b.ID}, nil), 200)
	_, err := r.h.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.items',json(?)) WHERE id=?", `[{"id":"selected","kind":"file","source":"owner","attachment_id":"fixture-image","media_type":"image"}]`, a.ID)
	must(t, err)
	response = r.request(t, "POST", "/v1/tasks/"+a.ID+"/cover", r.api.ControlToken, map[string]any{"item_id": "selected"}, nil)
	status(t, response, 200)
	must(t, json.Unmarshal(response.Body.Bytes(), &a))
	if a.CoverItemID != "selected" {
		t.Fatal("control cover not pinned")
	}
	c := circleFixture(t, r, "project", "confirm", false, groupA)
	response = r.request(t, "POST", "/v1/circles/"+c.ID+"/convert-to-task", r.api.ControlToken, map[string]any{"title": "Visit the studio", "goal": "Complete the visit"}, nil)
	status(t, response, 200)
	var converted tasks.Task
	must(t, json.Unmarshal(response.Body.Bytes(), &converted))
	var saved model.Circle
	must(t, r.h.Repo.Get("circles", c.ID, &saved))
	if !saved.Archived || saved.ConvertedTaskID != converted.ID {
		t.Fatal("control conversion failed")
	}
}
