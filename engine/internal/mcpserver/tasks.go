package mcpserver

import (
	"context"
	"errors"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"sort"
	"strconv"
)

func (s *Service) TaskService() *tasks.Service {
	s.taskOnce.Do(func() { s.taskService = tasks.New(s.Hub, s.EffortService()) })
	return s.taskService
}
func taskTool() Tool {
	t := def("task", "Track owner work; never sends. Reads need read_messages, writes save_summaries; owner notes are read-only. List excludes quiet unless status=quiet/all; update status=quiet/active to quiet/wake. Checklist uses steps (items/text aliases), not update.checklist.", "action", "id title goal kind effort_id chats:array people:array items:array checklist:array check_in:object lifespan_days:integer end_at end_condition status limit:integer summary:object waiting:object item:object op steps:array text reason close_reason")
	p := t.InputSchema["properties"].(map[string]any)
	p["limit"] = map[string]any{"anyOf": []any{
		map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		map[string]any{"type": "string", "minLength": 1, "maxLength": 3, "pattern": "^[0-9]+$"},
	}}
	p["action"] = map[string]any{"type": "string", "enum": []string{"create", "list", "get", "brief", "keep", "dismiss", "update", "attach", "checklist", "draft_check_in", "draft_nudge", "needs_you"}}
	for _, field := range []string{"chats", "people", "items", "checklist", "steps"} {
		p[field] = map[string]any{"type": "array", "maxItems": 500, "items": map[string]any{"type": "object", "additionalProperties": true}}
	}
	for _, field := range []string{"check_in", "summary", "waiting", "item"} {
		p[field] = map[string]any{"anyOf": []any{map[string]any{"type": "object", "additionalProperties": true}, map[string]any{"type": "null"}}}
	}
	t.Description += " create: title,kind; list: status,limit; get/brief/keep: id; dismiss: id,reason; update: id,fields; attach: id,item; checklist: id,op,steps; drafts: id,text; needs_you: id,reason. See docs/MCP.md."
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
		act.TaskID = args.str("id")
		verbs := map[string]string{"get": "Opened a task", "brief": "Read a task brief", "list": "Listed tasks", "create": "Created a task", "keep": "Kept a suggested task", "dismiss": "Dismissed a suggested task", "attach": "Added a file to a task", "checklist": "Updated task steps", "draft_check_in": "Prepared a task check-in", "draft_nudge": "Prepared a task reminder"}
		act.Description = verbs[action]
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
	case "create":
		var in tasks.Create
		if err := tasks.Decode(fields, &in); err != nil {
			return nil, err
		}
		return svc.Create(&a, in)
	case "list":
		for k := range fields {
			if k != "status" && k != "limit" {
				return nil, model.ErrInvalid
			}
		}
		xs, err := svc.List(&a, args.str("status"), args.num("limit", 100))
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
		if id == "" || len(fields) != 0 {
			return nil, model.ErrInvalid
		}
		return svc.Keep(&a, id)
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
