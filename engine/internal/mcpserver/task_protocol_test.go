package mcpserver_test

import (
	"context"
	"errors"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
	"strings"
	"testing"
)

const askInstruction = "Ask the owner; keep it with owner_request once he says yes"

func TestTaskOwnerRequestGate(t *testing.T) {
	for _, preset := range []string{"own", "given"} {
		t.Run(preset, func(t *testing.T) {
			h, p, _ := testkit.Open(t)
			testkit.Seed(t, h, p)
			a, _, err := h.CreateAgent("Example AI", "custom", "local")
			testkit.Must(t, err)
			a.Preset = preset
			testkit.Must(t, h.UpdateAgent(a))
			mc := &mcpserver.Service{Hub: h, People: p}
			call := func(args map[string]any) any {
				t.Helper()
				out, err := mc.Call(context.Background(), a.ID, "task", args)
				testkit.Must(t, err)
				return out
			}
			args := map[string]any{"action": "create", "title": "Science fair plan", "goal": "Prepare the science fair", "kind": "long", "chats": []any{map[string]any{"id": testkit.Group}}}
			response := call(args).(map[string]any)
			if response["created"] != "suggestion" || response["instruction"] != askInstruction {
				t.Fatal(response)
			}
			id := response["id"].(string)
			suggestion, err := mc.TaskService().Get(&a, id)
			testkit.Must(t, err)
			if suggestion.Status != "suggested" || suggestion.Origin != "agent" || suggestion.CreatedBy.AgentID != a.ID || suggestion.AskedByOwner != nil || suggestion.Suggestion == nil || len(suggestion.Suggestion.Signals) != 1 || suggestion.Suggestion.Signals[0] != "agent_proposed" || suggestion.Suggestion.DetectedAt != h.Now() {
				t.Fatalf("not an agent suggestion: %+v", suggestion)
			}
			duplicate := call(args).(map[string]any)
			if duplicate["created"] != false || len(duplicate["candidates"].([]tasks.TaskSummary)) != 1 {
				t.Fatal("duplicate check bypassed", duplicate)
			}
			var before, after string
			testkit.Must(t, h.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", id).Scan(&before))
			refusal := call(map[string]any{"action": "keep", "id": id}).(map[string]any)
			testkit.Must(t, h.Repo.DB.QueryRow("SELECT data FROM tasks WHERE id=?", id).Scan(&after))
			if refusal["kept"] != false || refusal["instruction"] != askInstruction || before != after {
				t.Fatal("keep without ask changed task", refusal)
			}
			quote := "Please track the science fair"
			kept := call(map[string]any{"action": "keep", "id": id, "owner_request": map[string]any{"text": quote}}).(tasks.Task)
			if kept.Status != "active" || kept.AskedByOwner == nil || kept.AskedByOwner.Text != quote || kept.AskedByOwner.AgentID != a.ID || kept.AskedByOwner.At != h.Now() {
				t.Fatalf("keep lost ask: %+v", kept)
			}
			history, err := mc.TaskService().History(id)
			testkit.Must(t, err)
			found := false
			for _, event := range history {
				if event.Kind == "kept" && strings.Contains(event.Text, quote) {
					found = true
				}
			}
			if !found {
				t.Fatal("kept history missing quote", history)
			}
			quote = "Please track my revision"
			created := call(map[string]any{"action": "create", "title": "Revision plan", "goal": "Finish revision", "kind": "long", "owner_request": map[string]any{"text": quote}}).(tasks.Task)
			if created.Status != "active" || created.AskedByOwner == nil || created.AskedByOwner.Text != quote || created.AskedByOwner.AgentID != a.ID || created.AskedByOwner.At != h.Now() {
				t.Fatalf("create lost ask: %+v", created)
			}
			read := call(map[string]any{"action": "get", "id": created.ID}).(tasks.Task)
			if read.AskedByOwner == nil || *read.AskedByOwner != *created.AskedByOwner {
				t.Fatal("get lost stored ask")
			}
			history, err = mc.TaskService().History(created.ID)
			testkit.Must(t, err)
			if len(history) == 0 || !strings.Contains(history[0].Text, quote) {
				t.Fatal("creation history missing quote", history)
			}
		})
	}
}

func TestTaskOwnerRequestMessageAuthorAndAccess(t *testing.T) {
	for _, action := range []string{"create", "keep"} {
		for _, source := range []string{"owner", "contact", "agent", "unknown", "missing", "hidden", "out_of_scope", "deleted", "ambiguous"} {
			t.Run(action+"/"+source, func(t *testing.T) {
				h, p, _ := testkit.Open(t)
				testkit.Seed(t, h, p)
				a, _, err := h.CreateAgent("Example AI", "custom", "local")
				testkit.Must(t, err)
				mc := &mcpserver.Service{Hub: h, People: p}
				quote := "Please track the science fair"
				m := model.Message{ID: "fair-ask", ChatJID: testkit.Phone, IsFromMe: source != "contact", Timestamp: h.Now(), Content: quote}
				if source != "missing" {
					testkit.Must(t, h.Messages.PutMessage(m))
				}
				if source != "contact" && source != "agent" && source != "unknown" && source != "missing" {
					_, err = h.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", m.ChatJID, m.ID, "you", "You")
					testkit.Must(t, err)
				}
				if source == "agent" {
					_, err = h.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", m.ChatJID, m.ID, a.ID, a.Name)
					testkit.Must(t, err)
				}
				if source == "deleted" {
					_, err = h.Messages.DB.Exec("UPDATE messages SET deleted_at=? WHERE id=?", h.Now(), m.ID)
					testkit.Must(t, err)
				}
				if source == "hidden" {
					var settings model.Settings
					testkit.Must(t, h.Repo.Get("settings", "main", &settings))
					settings.HiddenChats = []string{testkit.Phone}
					testkit.Must(t, h.Repo.Put("settings", "main", settings))
				}
				if source == "out_of_scope" {
					a.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Group}}
					testkit.Must(t, h.UpdateAgent(a))
				}
				if source == "ambiguous" {
					m.ChatJID = testkit.Group
					testkit.Must(t, h.Messages.PutMessage(m))
				}
				args := map[string]any{"action": action, "owner_request": map[string]any{"text": quote, "message_ref": m.ID}}
				var id string
				if action == "create" {
					args["title"] = "Science fair plan"
					args["goal"] = "Prepare the fair"
					args["kind"] = "long"
				} else {
					suggestion, err := mc.TaskService().Suggest(&a, tasks.Create{Title: "Science fair plan", Kind: "long"}, tasks.Suggestion{})
					testkit.Must(t, err)
					id = suggestion.ID
					args["id"] = id
				}
				out, err := mc.Call(context.Background(), a.ID, "task", args)
				if source == "owner" {
					testkit.Must(t, err)
					task := out.(tasks.Task)
					if task.Status != "active" || task.AskedByOwner == nil || task.AskedByOwner.MessageRef != m.ID {
						t.Fatal("valid reference lost", task)
					}
				} else {
					if !errors.Is(err, model.ErrInvalid) {
						t.Fatal("non-owner/inaccessible ask accepted", err)
					}
					if id != "" {
						task, e := mc.TaskService().Get(&a, id)
						testkit.Must(t, e)
						if task.Status != "suggested" || task.AskedByOwner != nil {
							t.Fatal("invalid keep mutated task")
						}
					}
					open, e := mc.TaskService().List(&a, "open", 100)
					testkit.Must(t, e)
					if len(open) != 0 {
						t.Fatal("invalid ask created active work")
					}
				}
			})
		}
	}
}

func TestTaskOwnerRequestShapeValidation(t *testing.T) {
	for _, request := range []any{nil, "quoted", map[string]any{}, map[string]any{"text": ""}, map[string]any{"text": "   "}, map[string]any{"text": strings.Repeat("x", 501)}, map[string]any{"text": "Please track this", "message_ref": false}, map[string]any{"text": "Please track this", "agent_id": "forged"}} {
		h, p, _ := testkit.Open(t)
		a, _, err := h.CreateAgent("Example AI", "custom", "local")
		testkit.Must(t, err)
		mc := &mcpserver.Service{Hub: h, People: p}
		_, err = mc.Call(context.Background(), a.ID, "task", map[string]any{"action": "create", "title": "Science fair", "goal": "Prepare the fair", "kind": "long", "owner_request": request})
		if !errors.Is(err, model.ErrInvalid) {
			t.Fatalf("invalid request accepted: %T, %v", request, err)
		}
	}
}
