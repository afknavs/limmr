package mcpserver

import (
	"fmt"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"net/url"
	"sort"
	"strings"
	"time"
)

var taskExamples = map[string]string{
	"create":         `{"action":"create","title":"Exam preparation","kind":"long"}`,
	"list":           `{"action":"list","status":"open","limit":20}`,
	"get":            `{"action":"get","id":"tsk_example"}`,
	"brief":          `{"action":"brief","id":"tsk_example"}`,
	"keep":           `{"action":"keep","id":"tsk_example"}`,
	"dismiss":        `{"action":"dismiss","id":"tsk_example","reason":"Not needed"}`,
	"update":         `{"action":"update","id":"tsk_example","status":"active"}`,
	"attach":         `{"action":"attach","id":"tsk_example","item":{"kind":"note","text":"Review memory next"}}`,
	"checklist":      `{"action":"checklist","id":"tsk_example","op":"add","steps":[{"text":"Read"}]}`,
	"draft_check_in": `{"action":"draft_check_in","id":"tsk_example","text":"This week’s review is ready"}`,
	"draft_nudge":    `{"action":"draft_nudge","id":"tsk_example","text":"Please share the syllabus"}`,
	"needs_you":      `{"action":"needs_you","id":"tsk_example","reason":"Choose a time"}`,
}

func taskError(action, field, why string) error {
	example := taskExamples[action]
	if example == "" {
		example = taskExamples["create"]
	}
	return fmt.Errorf("task %s: %s %s; accepted: correct example: %s: %w", action, field, why, example, model.ErrInvalid)
}
func taskString(min, max int) map[string]any {
	return map[string]any{"type": "string", "minLength": min, "maxLength": max}
}
func taskEnum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
func taskInt(min, max int) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}
func taskObject(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
func taskArray(item map[string]any) map[string]any {
	return map[string]any{"type": "array", "maxItems": 500, "items": item}
}
func taskNullable(value map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{value, map[string]any{"type": "null"}}}
}
func taskDate() map[string]any {
	return map[string]any{"type": "string", "format": "date-time", "minLength": 1}
}

// Validate the selected action, including nested fields, before any mutation.
// Diagnostics contain only field paths and server-owned contracts, never values.
func validateTaskArgs(args Args) error {
	action := args.str("action")
	if action == "update" {
		if _, ok := args["checklist"]; ok {
			return taskError("checklist", "checklist", "cannot be edited with update; use the checklist action")
		}
	}
	if taskExamples[action] == "" {
		return taskError("create", "action", "must be create, list, get, brief, keep, dismiss, update, attach, checklist, draft_check_in, draft_nudge or needs_you")
	}
	entity := taskObject(map[string]any{"id": taskString(1, 200), "name": taskString(0, 200)}, "id")
	ref := taskObject(map[string]any{"chat_jid": taskString(1, 200), "message_id": taskString(1, 200)}, "chat_jid", "message_id")
	item := taskObject(map[string]any{"kind": taskEnum("note", "link", "file"), "title": taskString(0, 120), "text": taskString(0, 2000), "url": taskString(1, 4000), "attachment_id": taskString(1, 200), "message_ref": ref, "source": taskEnum("owner", "ai", "chat")}, "kind")
	step := taskObject(map[string]any{"id": taskString(0, 200), "text": taskString(1, 200), "done": map[string]any{"type": "boolean"}, "order": taskInt(0, 499)})
	check := taskObject(map[string]any{"rule": taskEnum("weekly", "every_n_days", "once", "reply"), "weekday": taskInt(0, 6), "every_days": taskInt(1, 60), "at_time": taskString(5, 5), "to": taskString(1, 200), "note": taskString(0, 300), "next_at": taskDate()}, "rule", "at_time", "to")
	waiting := taskObject(map[string]any{"on": taskNullable(entity), "what": taskString(1, 200), "since": taskDate(), "nudge_after_days": taskInt(1, 30)}, "what")
	props := map[string]any{"action": taskEnum(action)}
	required := []string{"action"}
	if action != "create" && action != "list" {
		props["id"] = taskString(1, 200)
		required = append(required, "id")
	}
	editable := map[string]any{"title": taskString(1, 60), "goal": taskString(0, 1000), "kind": taskEnum("long", "short"), "lifespan_days": taskInt(1, 3650), "end_at": taskDate(), "end_condition": taskEnum("agent_done", "checklist_done"), "check_in": taskNullable(check)}
	switch action {
	case "create":
		for k, v := range editable {
			props[k] = v
		}
		required = append(required, "title", "kind")
		props["effort_id"] = taskString(1, 200)
		props["chats"] = taskArray(entity)
		props["people"] = taskArray(entity)
		props["items"] = taskArray(item)
		props["checklist"] = taskArray(step)
	case "update":
		for k, v := range editable {
			props[k] = v
		}
		props["status"] = taskEnum("active", "waiting", "quiet", "done", "dropped")
		props["summary"] = taskObject(map[string]any{"text": taskString(0, 1500)}, "text")
		props["waiting"] = taskNullable(waiting)
		props["reason"] = taskString(0, 1500)
		props["close_reason"] = taskEnum("done", "lifespan", "owner")
	case "list":
		props["status"] = taskEnum("open", "closed", "all", "suggested", "quiet")
		props["limit"] = taskInt(1, 500)
	case "dismiss":
		props["reason"] = taskString(0, 300)
	case "attach":
		props["item"] = item
		required = append(required, "item")
	case "checklist":
		props["op"] = taskEnum("add", "done", "undo", "remove", "reorder")
		arr := taskArray(step)
		arr["minItems"] = 1
		props["steps"] = arr
		required = append(required, "op", "steps")
	case "draft_check_in", "draft_nudge":
		props["text"] = taskString(1, 2000)
		required = append(required, "text")
	case "needs_you":
		props["reason"] = taskString(0, 300)
		required = append(required, "reason")
	}
	schema := taskObject(props, required...)
	if !validValue(schema, map[string]any(args)) {
		problems := valueProblems("", schema, map[string]any(args))
		sort.Strings(problems)
		if len(problems) == 0 {
			return taskError(action, "action", "has invalid fields")
		}
		return taskError(action, strings.TrimPrefix(problems[0], "."), "(use the fields for this action)")
	}
	for _, key := range []string{"title", "text"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) == "" {
			return taskError(action, key, "must contain text")
		}
	}
	if action == "create" && args.str("kind") == "long" {
		if _, ok := args["lifespan_days"]; ok {
			return taskError(action, "lifespan_days", "applies only to short tasks")
		}
	}
	if action == "update" && len(args) == 2 {
		return taskError(action, "status", "or another editable field is required")
	}
	if args["reason"] != nil && action == "update" && args.str("status") != "done" && args.str("status") != "dropped" {
		return taskError(action, "reason", "requires status done or dropped")
	}
	if v, ok := args["check_in"].(map[string]any); ok {
		if _, err := time.Parse("15:04", v["at_time"].(string)); err != nil {
			return taskError(action, "check_in.at_time", "must be HH:MM")
		}
		if v["rule"] == "weekly" && v["weekday"] == nil {
			return taskError(action, "check_in.weekday", "is required for weekly")
		}
		if v["rule"] == "every_n_days" && v["every_days"] == nil {
			return taskError(action, "check_in.every_days", "is required for every_n_days")
		}
	}
	for _, key := range []string{"end_at"} {
		if v, ok := args[key].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				return taskError(action, key, "must be RFC3339 with timezone")
			}
		}
	}
	validateItem := func(v map[string]any, path string) error {
		switch v["kind"] {
		case "note":
			if text, _ := v["text"].(string); strings.TrimSpace(text) == "" {
				return taskError(action, path+".text", "is required for a note")
			}
		case "link":
			u, _ := url.Parse(fmt.Sprint(v["url"]))
			if u == nil || u.User != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" {
				return taskError(action, path+".url", "must be an HTTP(S) URL")
			}
		case "file":
			if v["attachment_id"] == nil {
				return taskError(action, path+".attachment_id", "is required for a file")
			}
		}
		return nil
	}
	if v, ok := args["item"].(map[string]any); ok {
		if err := validateItem(v, "item"); err != nil {
			return err
		}
	}
	if values, ok := args["items"].([]any); ok {
		for i, v := range values {
			if err := validateItem(v.(map[string]any), fmt.Sprintf("items[%d]", i)); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"steps", "checklist"} {
		if values, ok := args[key].([]any); ok {
			for i, v := range values {
				step := v.(map[string]any)
				field := "text"
				if action == "checklist" && args.str("op") != "add" {
					field = "id"
				}
				if value, _ := step[field].(string); strings.TrimSpace(value) == "" {
					return taskError(action, fmt.Sprintf("%s[%d].%s", key, i, field), "is required")
				}
			}
		}
	}
	return nil
}

// Resolve semantic constraints against the current workspace so failures name
// the precise reference rather than turning storage errors into generic JSON errors.
func (s *Service) validateTaskState(a model.Agent, args Args) error {
	action := args.str("action")
	if action == "list" {
		return nil
	}
	var task tasks.Task
	if action == "create" {
		task.Kind = args.str("kind")
		if id := args.str("effort_id"); id != "" {
			effort, err := s.EffortService().Get(&a, id)
			if err != nil {
				return taskError(action, "effort_id", "must name an accessible effort")
			}
			task.Chats = effort.Chats
			task.People = effort.People
		}
		for _, field := range []string{"chats", "people"} {
			if values, ok := args[field].([]any); ok {
				for i, v := range values {
					entity := v.(map[string]any)
					id := entity["id"].(string)
					if field == "chats" {
						jid, err := s.Hub.ResolveChat(id)
						if err != nil || !s.Hub.CanChat(&a, jid) {
							return taskError(action, fmt.Sprintf("chats[%d].id", i), "must name an accessible chat")
						}
						var n int
						if s.Hub.Messages.DB.QueryRow("SELECT count(*) FROM chats WHERE jid=?", jid).Scan(&n) != nil || n == 0 {
							return taskError(action, fmt.Sprintf("chats[%d].id", i), "must name an existing chat")
						}
						task.Chats = append(task.Chats, tasks.Entity{ID: jid})
					} else {
						var p model.Person
						if s.Hub.Repo.Get("people", id, &p) != nil {
							return taskError(action, fmt.Sprintf("people[%d].id", i), "must name an existing person")
						}
						task.People = append(task.People, tasks.Entity{ID: id})
					}
				}
			}
		}
	} else {
		var err error
		task, err = s.TaskService().Get(&a, args.str("id"))
		if err != nil {
			return err
		}
	}
	if kind := args.str("kind"); kind != "" {
		task.Kind = kind
	}
	if _, ok := args["lifespan_days"]; ok && task.Kind != "short" {
		return taskError(action, "lifespan_days", "applies only to short tasks")
	}
	if check, ok := args["check_in"].(map[string]any); ok {
		found := false
		for _, ch := range task.Chats {
			found = found || ch.ID == check["to"]
		}
		if !found {
			return taskError(action, "check_in.to", "must be one of this task's linked chats")
		}
		if check["rule"] == "reply" && check["next_at"] == nil {
			return taskError(action, "check_in.next_at", "is required for a reply check-in")
		}
	}
	if waiting, ok := args["waiting"].(map[string]any); ok {
		if person, ok := waiting["on"].(map[string]any); ok {
			found := false
			for _, p := range task.People {
				found = found || p.ID == person["id"]
			}
			if !found {
				return taskError(action, "waiting.on.id", "must be one of this task's linked people")
			}
		}
	}
	if action == "checklist" && args.str("op") != "add" {
		values := args["steps"].([]any)
		seen := map[string]bool{}
		for i, value := range values {
			id := value.(map[string]any)["id"].(string)
			found := false
			for _, step := range task.Checklist {
				found = found || step.ID == id
			}
			if !found || seen[id] {
				return taskError(action, fmt.Sprintf("steps[%d].id", i), "must name a unique existing checklist step")
			}
			seen[id] = true
		}
		if args.str("op") == "reorder" && len(values) != len(task.Checklist) {
			return taskError(action, "steps", "must include every current step exactly once for reorder")
		}
	}
	if action == "attach" {
		item := args["item"].(map[string]any)
		if ref, ok := item["message_ref"].(map[string]any); ok {
			linked := false
			for _, ch := range task.Chats {
				linked = linked || ch.ID == ref["chat_jid"]
			}
			if !linked {
				return taskError(action, "item.message_ref.chat_jid", "must be one of this task's linked chats")
			}
		}
	}
	if action == "draft_check_in" && task.CheckIn == nil {
		return taskError(action, "check_in", "must be scheduled with update before drafting")
	}
	if action == "draft_nudge" && task.Waiting == nil {
		return taskError(action, "waiting", "must be set with update before drafting")
	}
	return nil
}
