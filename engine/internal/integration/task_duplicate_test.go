package integration_test

import (
	"encoding/json"
	"fmt"
	"limmr/internal/tasks"
	"testing"
)

func duplicateControlFixture(t *testing.T, r *rig) (tasks.Task, tasks.Task) {
	t.Helper()
	dst := newTask(t, r)
	src, err := r.mc.TaskService().Suggest(nil, tasks.Create{Title: dst.Title, Goal: dst.Goal, Kind: "long", Chats: []tasks.Entity{{ID: chatA}, {ID: groupA}}, People: []tasks.Entity{{ID: "maya"}}, DifferentBecause: "Review this fixture duplicate"}, tasks.Suggestion{DetectedAt: epoch})
	must(t, err)
	return dst, src
}

func TestR1ControlKeepAndRuleUndoEndpoint(t *testing.T) {
	r := newRig(t)
	dst, src := duplicateControlFixture(t, r)
	response := r.request(t, "POST", "/v1/tasks/"+src.ID+"/keep", r.api.ControlToken, nil, nil)
	status(t, response, 200)
	var got tasks.Task
	must(t, json.Unmarshal(response.Body.Bytes(), &got))
	if got.ID != dst.ID || !got.AttachedToExisting || len(got.Chats) != 2 {
		t.Fatal("control did not return existing task", got)
	}
	source, err := r.mc.TaskService().Get(nil, src.ID)
	must(t, err)
	if source.Status != "dropped" || source.MergedInto != dst.ID || source.CloseReason != "merged" {
		t.Fatal("suggestion not merged")
	}
	response = r.request(t, "GET", "/v1/tasks/"+dst.ID, r.api.ControlToken, nil, nil)
	status(t, response, 200)
	must(t, json.Unmarshal(response.Body.Bytes(), &got))
	var change string
	for _, e := range got.History {
		if e.Kind == "rule_duplicate" {
			change = e.ChangeID
		}
	}
	if change == "" {
		t.Fatal("missing history undo link")
	}
	path := "/v1/tasks/" + dst.ID + "/changes/" + change + "/undo"
	status(t, r.request(t, "POST", path, r.token, nil, nil), 403)
	status(t, r.request(t, "POST", path, r.api.ControlToken, nil, nil), 200)
	source, err = r.mc.TaskService().Get(nil, src.ID)
	must(t, err)
	if source.Status != "suggested" || source.MergedInto != "" {
		t.Fatal("control undo did not restore source")
	}
	destination, err := r.mc.TaskService().Get(nil, dst.ID)
	must(t, err)
	if len(destination.Chats) != 1 {
		t.Fatal("control undo retained attached members")
	}
}

func TestR1AgentKeepAttachesAndSaysSo(t *testing.T) {
	for _, preset := range []string{"own", "given"} {
		t.Run(preset, func(t *testing.T) {
			r := newRig(t)
			r.a.Preset = preset
			must(t, r.h.UpdateAgent(r.a))
			dst, src := duplicateControlFixture(t, r)
			out, err := r.call(t, "task", map[string]any{"action": "keep", "id": src.ID, "owner_request": map[string]any{"text": "Please track the science fair"}})
			must(t, err)
			result, ok := out.(map[string]any)
			if !ok || result["attached_to_existing"] != true || result["message"] != fmt.Sprintf("Attached to %q", dst.Title) {
				t.Fatal("MCP keep did not explain attachment", out)
			}
			if result["task"].(tasks.Task).ID != dst.ID {
				t.Fatal("MCP returned wrong task")
			}
			var payload struct {
				Evidence struct {
					OwnerRequest *tasks.AskedByOwner `json:"owner_request"`
				}
			}
			var raw string
			must(t, r.h.Repo.DB.QueryRow("SELECT payload FROM task_events WHERE task_id=? AND kind='rule_duplicate'", dst.ID).Scan(&raw))
			must(t, json.Unmarshal([]byte(raw), &payload))
			if payload.Evidence.OwnerRequest == nil || payload.Evidence.OwnerRequest.AgentID != r.a.ID || payload.Evidence.OwnerRequest.Text != "Please track the science fair" {
				t.Fatal("attachment lost owner request")
			}
		})
	}
}
