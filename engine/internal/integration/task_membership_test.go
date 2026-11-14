package integration_test

import (
	"encoding/json"
	"limmr/internal/tasks"
	"testing"
)

func TestTaskOwnerMembershipControlAndAgentRefusal(t *testing.T) {
	r := newRig(t)
	v := newTask(t, r)
	body := map[string]any{"chats": []string{chatA}, "people": []string{"maya"}}
	path := "/v1/tasks/" + v.ID + "/remove"
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "POST", path, token, body, nil), 403)
	}
	// No new MCP membership operation: the owner control route is the only entry.
	if _, err := r.call(t, "task", map[string]any{"action": "remove", "id": v.ID, "people": []string{"maya"}}); err == nil {
		t.Fatal("MCP removed a participant")
	}
	response := r.request(t, "POST", path, r.api.ControlToken, body, nil)
	status(t, response, 200)
	var removed tasks.Task
	must(t, json.Unmarshal(response.Body.Bytes(), &removed))
	if len(removed.Chats) != 0 || len(removed.People) != 0 {
		t.Fatal("control response retained member", removed)
	}
	response = r.request(t, "GET", "/v1/tasks/"+v.ID, r.api.ControlToken, nil, nil)
	status(t, response, 200)
	must(t, json.Unmarshal(response.Body.Bytes(), &removed))
	changeID := ""
	for _, event := range removed.History {
		if event.Kind == "owner_remove" {
			changeID = event.ChangeID
		}
	}
	if changeID == "" {
		t.Fatal("history lacks change id")
	}
	undo := "/v1/tasks/" + v.ID + "/changes/" + changeID + "/undo"
	for _, token := range []string{r.token, r.api.StatusToken} {
		status(t, r.request(t, "POST", undo, token, map[string]any{}, nil), 403)
	}
	status(t, r.request(t, "POST", undo, r.api.ControlToken, map[string]any{}, nil), 200)
	restored, err := r.mc.TaskService().Get(nil, v.ID)
	must(t, err)
	if len(restored.Chats) != 1 || len(restored.People) != 1 {
		t.Fatal("control undo did not restore membership")
	}
	status(t, r.request(t, "POST", undo, r.api.ControlToken, map[string]any{}, nil), 409)
	status(t, r.request(t, "POST", path, r.api.ControlToken, map[string]any{"people": []string{"me"}}, nil), 400)
}
