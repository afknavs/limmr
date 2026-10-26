package mcpserver

import (
	"context"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"strconv"
)

func (s *Service) TaskService() *tasks.Service {
	s.taskOnce.Do(func() { s.taskService = tasks.New(s.Hub, s.EffortService()) })
	return s.taskService
}
func taskTool() Tool {
	t := def("task", "Track owner work: create/list/get/keep/dismiss/update/attach/checklist, draft_check_in/draft_nudge, needs_you. Never sends. Reads need read_messages; writes save_summaries. See task contract for nested fields.", "action", "id title goal kind effort_id chats:array people:array items:array checklist:array check_in:object lifespan_days:integer status limit:integer summary:object waiting:object item:object op steps:array text reason close_reason")
	p := t.InputSchema["properties"].(map[string]any)
	p["limit"] = map[string]any{"anyOf": []any{
		map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
		map[string]any{"type": "string", "minLength": 1, "maxLength": 3, "pattern": "^[0-9]+$"},
	}}
	p["action"] = map[string]any{"type": "string", "enum": []string{"create", "list", "get", "keep", "dismiss", "update", "attach", "checklist", "draft_check_in", "draft_nudge", "needs_you"}}
	for _, field := range []string{"chats", "people", "items", "checklist", "steps"} {
		p[field] = map[string]any{"type": "array", "maxItems": 500, "items": map[string]any{"type": "object", "additionalProperties": true}}
	}
	for _, field := range []string{"check_in", "summary", "waiting", "item"} {
		p[field] = map[string]any{"anyOf": []any{map[string]any{"type": "object", "additionalProperties": true}, map[string]any{"type": "null"}}}
	}
	t.Annotations = nil
	return t
}

// Some hosted connectors serialize the list limit as a decimal string. Keep
// the preferred integer form and normalize only this bounded read argument.
func normalizeTaskArgs(args Args) Args {
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

func (s *Service) callTask(ctx context.Context, a model.Agent, args Args, act *model.Activity) (any, error) {
	t := taskTool()
	if _, stringLimit := args["limit"].(string); stringLimit {
		return nil, fieldError("limit", "integer 1..500 or a decimal string in that range")
	}
	if !validate(t, args) {
		return nil, invalidArgs(t, args)
	}
	if !a.Permissions.ReadMessages {
		return nil, permissionError(a, "list_messages")
	}
	action := args.str("action")
	write := action != "get" && action != "list"
	if write && !a.Permissions.SaveSummaries {
		return nil, permissionError(a, "save_summary")
	}
	fields := map[string]any{}
	for k, v := range args {
		if k != "action" && k != "id" {
			fields[k] = v
		}
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
	case "get":
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
