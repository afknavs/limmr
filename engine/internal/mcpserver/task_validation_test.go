package mcpserver

import (
	"context"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"strings"
	"testing"
)

func TestN1PerActionValidation(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	s := &Service{Hub: h, People: p}
	cases := []struct {
		action, field string
		args          Args
	}{
		{"create", "lifespan_days", Args{"action": "create", "title": "Exam preparation", "kind": "long", "lifespan_days": 4}},
		{"update", "checklist", Args{"action": "update", "id": "tsk_example", "checklist": []any{}}},
		{"list", "status", Args{"action": "list", "status": "invisible"}},
		{"get", "id", Args{"action": "get"}}, {"brief", "id", Args{"action": "brief"}},
		{"keep", "id", Args{"action": "keep"}}, {"dismiss", "reason", Args{"action": "dismiss", "id": "x", "reason": 5}},
		{"attach", "item.text", Args{"action": "attach", "id": "x", "item": map[string]any{"kind": "note"}}},
		{"checklist", "steps[0].id", Args{"action": "checklist", "id": "x", "op": "done", "steps": []any{map[string]any{"text": "Read"}}}},
		{"draft_check_in", "text", Args{"action": "draft_check_in", "id": "x"}},
		{"draft_nudge", "text", Args{"action": "draft_nudge", "id": "x", "text": " "}},
		{"needs_you", "reason", Args{"action": "needs_you", "id": "x", "reason": false}},
	}
	for _, c := range cases {
		t.Run(c.action+"_"+c.field, func(t *testing.T) {
			_, err := s.Call(context.Background(), a.ID, "task", c.args)
			expected := taskExamples[c.action]
			if c.field == "checklist" {
				expected = taskExamples["checklist"]
			}
			if err == nil || !strings.Contains(err.Error(), c.field) || !strings.Contains(err.Error(), expected) {
				t.Fatalf("want field and correct example: %v", err)
			}
		})
	}
	for _, alias := range []Args{{"action": "checklist", "id": "x", "op": "add", "items": []any{map[string]any{"text": "Read"}}}, {"action": "checklist", "id": "x", "op": "add", "text": "Read"}} {
		if err := validateTaskArgs(normalizeTaskArgs(alias)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestN1SemanticTaskErrorsNameReference(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Example AI", "custom", "local")
	testkit.Must(t, err)
	s := &Service{Hub: h, People: p}
	v, err := s.TaskService().Create(&a, tasks.Create{Title: "Exam guide", Kind: "long"})
	testkit.Must(t, err)
	for _, c := range []struct {
		field string
		args  Args
	}{
		{"lifespan_days", Args{"action": "update", "id": v.ID, "lifespan_days": 5}},
		{"check_in.to", Args{"action": "update", "id": v.ID, "check_in": map[string]any{"rule": "once", "at_time": "09:00", "to": "missing"}}},
		{"waiting.on.id", Args{"action": "update", "id": v.ID, "waiting": map[string]any{"what": "Syllabus", "on": map[string]any{"id": "missing"}}}},
		{"steps[0].id", Args{"action": "checklist", "id": v.ID, "op": "done", "steps": []any{map[string]any{"id": "missing"}}}},
		{"chats[0].id", Args{"action": "create", "title": "Exam guide", "kind": "long", "chats": []any{map[string]any{"id": "missing"}}}},
	} {
		_, err := s.Call(context.Background(), a.ID, "task", c.args)
		if err == nil || !strings.Contains(err.Error(), c.field) || !strings.Contains(err.Error(), "example:") {
			t.Fatalf("%s: %v", c.field, err)
		}
	}
}
