package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"sort"
	"strconv"
)

func (s *Service) TaskService() *tasks.Service {
	s.taskOnce.Do(func() { s.taskService = tasks.New(s.Hub, s.EffortService()) })
	return s.taskService
}

const ownerRequestInstruction = "Ask the owner; keep it with owner_request once he says yes"

func taskTool() Tool {
	t := def("task", "Tasks; never sends. create without owner_request suggests. keep/add_members/remove_members/undo/merge/convert_circle need owner_request. title <=5 words/60 chars; goal required. See docs/MCP.md.", "action", "owner_request:object id title goal kind effort_id follows different_because subject:object subject_evidence:array chats:array people:array items:array checklist:array check_in:object lifespan_days:integer end_at end_condition status limit:integer summary:object waiting:object item:object op steps:array text reason close_reason from_id item_id change_id")
	p := t.InputSchema["properties"].(map[string]any)
	p["owner_request"] = ownerRequestSchema()
	p["kind"] = taskEnum("long", "short", "test")
	p["limit"] = map[string]any{"anyOf": []any{
		map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		map[string]any{"type": "string", "minLength": 1, "maxLength": 3, "pattern": "^[0-9]+$"},
	}}
	p["action"] = map[string]any{"type": "string", "enum": []string{"create", "list", "get", "brief", "keep", "dismiss", "update", "attach", "checklist", "draft_check_in", "draft_nudge", "needs_you", "merge", "set_cover", "convert_circle", "add_members", "remove_members", "undo"}}
	for _, field := range []string{"chats", "people", "items", "checklist", "steps", "subject_evidence"} {
		p[field] = map[string]any{"type": "array", "maxItems": 500, "items": map[string]any{"type": "object", "additionalProperties": true}}
	}
	for _, field := range []string{"check_in", "summary", "waiting", "item", "subject"} {
		p[field] = map[string]any{"anyOf": []any{map[string]any{"type": "object", "additionalProperties": true}, map[string]any{"type": "null"}}}
	}

	t.Annotations = nil
	return t
}

// Some hosted connectors serialize the list limit as a decimal string. Keep
// the preferred integer form and normalize only this bounded read argument.
func normalizeTaskArgs(args Args) Args {
	if args.str("action") == "checklist" {
		out := Args{}
		for k, v := range args {
			out[k] = v
		}
		if _, hasSteps := out["steps"]; !hasSteps {
			if items, ok := out["items"]; ok {
				out["steps"] = items
				delete(out, "items")
			} else if text, ok := out["text"].(string); ok {
				out["steps"] = []any{map[string]any{"text": text}}
				delete(out, "text")
			}
		}
		return out
	}
	text, ok := args["limit"].(string)
	if args.str("action") != "list" || !ok || len(text) == 0 || len(text) > 3 {
		return args
	}
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return args
		}
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > 500 {
		return args
	}
	out := Args{}
	for k, v := range args {
		out[k] = v
	}
	out["limit"] = float64(n)
	return out
}

func (s *Service) callTask(ctx context.Context, a model.Agent, args Args, act *model.Activity) (out any, err error) {
	if err := validateTaskArgs(args); err != nil {
		return nil, err
	}
	action := args.str("action")
	defer func() {
		if err != nil || out == nil {
			return
		}
		if act.TaskID == "" {
			act.TaskID = args.str("id")
		}
		verbs := map[string]string{"get": "Opened a task", "brief": "Read a task brief", "list": "Listed tasks", "create": "Created a task", "keep": "Kept a suggested task", "dismiss": "Dismissed a suggested task", "attach": "Added a file to a task", "checklist": "Updated task steps", "draft_check_in": "Prepared a task check-in", "draft_nudge": "Prepared a task reminder"}
		if act.Description == "" {
			act.Description = verbs[action]
		}
		if act.Description == "" {
			act.Description = "Updated a task"
		}
		if task, ok := out.(tasks.Task); ok {
			act.TaskID = task.ID
			if len(act.ChatJIDs) == 0 {
				for _, chat := range task.Chats {
					act.ChatJIDs = append(act.ChatJIDs, chat.ID)
				}
			}
		}
	}()
	defer func() {
		if err == model.ErrInvalid {
			field := "id"
			keys := []string{}
			for k := range args {
				if k != "action" && k != "id" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				field = keys[0]
			}
			err = taskError(action, field, "conflicts with the stored task; get the task and use its current chat/step ids")
		}
		var duplicate *tasks.CreationConflict
		if errors.As(err, &duplicate) {
			out = map[string]any{"created": false, "review_required": true, "candidates": duplicate.Candidates, "instruction": duplicate.Error()}
			err = nil
			return
		}
		if errors.Is(err, model.ErrConflict) {
			err = taskError(action, "status", "does not allow this action; drafts need a scheduled check_in or waiting state, and keep/dismiss need a suggestion")
		}
		if errors.Is(err, model.ErrNotFound) {
			err = taskError(action, "id", "or a referenced chat, person, item or step is unavailable; use ids returned by list/get")
		}
	}()
	if !a.Permissions.ReadMessages {
		return nil, permissionError(a, "list_messages")
	}
	write := action != "get" && action != "brief" && action != "list"
	if write && !a.Permissions.SaveSummaries {
		return nil, permissionError(a, "save_summary")
	}
	fields := map[string]any{}
	for k, v := range args {
		if k != "action" && k != "id" {
			fields[k] = v
		}
	}
	if err := s.validateTaskState(a, args); err != nil {
		return nil, err
	}
	svc := s.TaskService()
	id := args.str("id")
	if write {
		act.Kind = "summary"
	}
	switch action {
	case "merge":
		var request tasks.OwnerRequest
		if err := tasks.Decode(fields["owner_request"], &request); err != nil {
			return nil, err
		}
		return svc.MergeWithOwnerRequest(&a, id, args.str("from_id"), request)
	case "set_cover":
		return svc.SetCover(&a, id, args.str("item_id"))
	case "convert_circle":
		var in tasks.Create
		if err := tasks.Decode(fields, &in); err != nil {
			return nil, err
		}
		return svc.ConvertCircle(&a, id, in)
	case "add_members", "remove_members", "undo":
		var in struct {
			OwnerRequest tasks.OwnerRequest `json:"owner_request"`
			Chats        []tasks.Entity     `json:"chats"`
			People       []tasks.Entity     `json:"people"`
			ChangeID     string             `json:"change_id"`
		}
		if err := tasks.Decode(fields, &in); err != nil {
			return nil, err
		}
		if action == "undo" {
			return svc.UndoMembershipChangeWithOwnerRequest(&a, id, in.ChangeID, in.OwnerRequest)
		}
		if action == "add_members" {
			return svc.AddMembersWithOwnerRequest(&a, id, in.Chats, in.People, in.OwnerRequest)
		}
		chats, people := []string{}, []string{}
		for _, ch := range in.Chats {
			chats = append(chats, ch.ID)
		}
		for _, p := range in.People {
			people = append(people, p.ID)
		}
		return svc.RemoveMembersWithOwnerRequest(&a, id, chats, people, in.OwnerRequest)
	case "create":
		var in tasks.Create
		if err := tasks.Decode(fields, &in); err != nil {
			return nil, err
		}
		if in.OwnerRequest == nil {
			effortIDs := []string{}
			if in.EffortID != "" {
				effortIDs = append(effortIDs, in.EffortID)
			}
			task, err := svc.Suggest(&a, in, tasks.Suggestion{Signals: []string{"agent_proposed"}, DetectedAt: s.Hub.Now(), EffortIDs: effortIDs})
			if err != nil {
				return nil, err
			}
			act.TaskID = task.ID
			act.Description = "Suggested work"
			for _, chat := range task.Chats {
				act.ChatJIDs = append(act.ChatJIDs, chat.ID)
			}
			return map[string]any{"created": "suggestion", "id": task.ID, "instruction": ownerRequestInstruction}, nil
		}
		return svc.Create(&a, in)
	case "list":
		for k := range fields {
			if k != "status" && k != "limit" {
				return nil, model.ErrInvalid
			}
		}
		xs, err := svc.List(&a, args.str("status"), args.num("limit", 100))
		if err == nil {
			for i := range xs {
				if xs[i].Status != "suggested" {
					continue
				}
				t, e := svc.Get(&a, xs[i].ID)
				if e != nil {
					return nil, e
				}
				xs[i].Chats, xs[i].People = t.Chats, t.People
				nc, np := len(t.Chats), len(t.People)
				xs[i].ChatCount, xs[i].PeopleCount = &nc, &np
			}
		}
		act.Count = len(xs)
		return map[string]any{"tasks": xs}, err
	case "get", "brief":
		if len(fields) > 0 || id == "" {
			return nil, model.ErrInvalid
		}
		task, err := svc.Get(&a, id)
		if err == nil {
			act.Count = 1
			for _, chat := range task.Chats {
				act.ChatJIDs = append(act.ChatJIDs, chat.ID)
			}
		}
		return task, err
	case "keep":
		if fields["owner_request"] == nil {
			act.Description = "Owner request needed to keep suggestion"
			return map[string]any{"kept": false, "instruction": ownerRequestInstruction}, nil
		}
		var request tasks.OwnerRequest
		if err := tasks.Decode(fields["owner_request"], &request); err != nil {
			return nil, err
		}
		task, err := svc.KeepWithOwnerRequest(&a, id, request)
		if err == nil && task.AttachedToExisting {
			act.TaskID = task.ID
			act.Description = "Attached suggestion to existing task"
			return map[string]any{"task": task, "attached_to_existing": true, "message": fmt.Sprintf("Attached to %q", task.Title)}, nil
		}
		return task, err
	case "dismiss":
		if id == "" || len(fields) > 1 {
			return nil, model.ErrInvalid
		}
		for k := range fields {
			if k != "reason" {
				return nil, model.ErrInvalid
			}
		}
		return svc.Dismiss(&a, id, args.str("reason"))
	case "update":
		return svc.Update(&a, id, fields)
	case "attach":
		if len(fields) != 1 {
			return nil, model.ErrInvalid
		}
		var it tasks.Item
		if err := tasks.Decode(fields["item"], &it); err != nil {
			return nil, err
		}
		return svc.Attach(&a, id, it)
	case "checklist":
		if len(fields) != 2 {
			return nil, model.ErrInvalid
		}
		var steps []tasks.Step
		if err := tasks.Decode(fields["steps"], &steps); err != nil {
			return nil, err
		}
		return svc.Checklist(&a, id, args.str("op"), steps)
	case "draft_check_in", "draft_nudge":
		if len(fields) != 1 {
			return nil, model.ErrInvalid
		}
		kind := "check_in"
		if action == "draft_nudge" {
			kind = "nudge"
		}
		return svc.Draft(&a, id, kind, args.str("text"))
	case "needs_you":
		if len(fields) != 1 {
			return nil, model.ErrInvalid
		}
		return svc.NeedsYou(&a, id, args.str("reason"))
	}
	return nil, model.ErrInvalid
}
