package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"limmr/internal/tasks"
	"limmr/internal/testkit"
)

type taskRPC func(string, any) map[string]any

func TestTaskControlsHandler(t *testing.T) {
	h, p, _ := testkit.Open(t)
	testkit.Seed(t, h, p)
	a, _, err := h.CreateAgent("Fixture AI", "custom", "local")
	testkit.Must(t, err)
	a.Preset = "own"
	testkit.Must(t, h.UpdateAgent(a))
	mc := &mcpserver.Service{Hub: h, People: p}
	handler := mc.Handler(false)
	rpc := func(method string, params any) map[string]any {
		t.Helper()
		b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		testkit.Must(t, err)
		r := httptest.NewRequest("POST", "http://localhost/mcp", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, mcpserver.WithAgent(r, a))
		if w.Code != 200 {
			t.Fatalf("MCP HTTP %d", w.Code)
		}
		var response struct {
			Result map[string]any `json:"result"`
			Error  any            `json:"error"`
		}
		testkit.Must(t, json.Unmarshal(w.Body.Bytes(), &response))
		if response.Error != nil {
			t.Fatal("MCP protocol error", response.Error)
		}
		return response.Result
	}
	init := rpc("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "fixture", "version": "1"}})
	if !strings.Contains(init["instructions"].(string), "owner_request") {
		t.Fatal("missing request guidance")
	}
	t.Logf("server instructions: %d constant / %d initialize characters", utf8.RuneCountInString(mcpserver.Instructions), utf8.RuneCountInString(init["instructions"].(string)))
	if utf8.RuneCountInString(init["instructions"].(string)) > 1200 {
		t.Fatal("instruction budget")
	}
	exerciseTaskControls(t, h, mc, a, rpc)
}

// Every capability uses JSON-RPC tools/call. Direct service/database operations
// below establish invented preconditions or verify persisted/silent behavior.
func exerciseTaskControls(t *testing.T, h *hub.Service, mc *mcpserver.Service, a model.Agent, rpc taskRPC) {
	t.Helper()
	ask := map[string]any{"text": "Please track this work"}
	call := func(args map[string]any, wantError bool) map[string]any {
		t.Helper()
		out := rpc("tools/call", map[string]any{"name": "task", "arguments": args})
		failed, _ := out["isError"].(bool)
		if failed != wantError {
			t.Fatalf("%s tool error=%v want=%v: %v", args["action"], failed, wantError, out["content"])
		}
		if failed {
			return nil
		}
		return out["structuredContent"].(map[string]any)["data"].(map[string]any)
	}
	taskCall := func(args map[string]any) tasks.Task {
		t.Helper()
		out := call(args, false)
		var task tasks.Task
		b, e := json.Marshal(out)
		testkit.Must(t, e)
		testkit.Must(t, json.Unmarshal(b, &task))
		return task
	}
	create := func(title string, extra map[string]any) tasks.Task {
		t.Helper()
		args := map[string]any{"action": "create", "title": title, "goal": "Complete " + title, "kind": "long", "owner_request": ask}
		for k, v := range extra {
			args[k] = v
		}
		return taskCall(args)
	}
	get := func(id string) tasks.Task { return taskCall(map[string]any{"action": "get", "id": id}) }
	entities := func(ids ...string) []any {
		out := []any{}
		for _, id := range ids {
			out = append(out, map[string]any{"id": id})
		}
		return out
	}
	person := model.PersonID(testkit.Phone)
	quote := "Please track the science exhibit"
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "fixture-owner-ask", ChatJID: testkit.Phone, IsFromMe: true, Content: quote, Timestamp: h.Now()}))
	_, err := h.Repo.DB.Exec("INSERT INTO send_authors VALUES(?,?,?,?)", testkit.Phone, "fixture-owner-ask", "you", "You")
	testkit.Must(t, err)
	testkit.Must(t, h.Messages.PutMessage(model.Message{ID: "fixture-contact-ask", ChatJID: testkit.Phone, Sender: testkit.Phone, Content: quote, Timestamp: h.Now()}))
	badAsk := map[string]any{"text": quote, "message_ref": "fixture-contact-ask"}
	var started, ended, ruleTask tasks.Task
	var changeID string
	t.Run("a_create", func(t *testing.T) {
		started = create("Science exhibit", nil)
		if started.Status != "active" || started.AskedByOwner == nil || started.AskedByOwner.Text != ask["text"] {
			t.Fatal("quote not stored")
		}
		referenced := create("Library planning", map[string]any{"owner_request": map[string]any{"text": quote, "message_ref": "fixture-owner-ask"}})
		if referenced.AskedByOwner == nil || referenced.AskedByOwner.MessageRef != "fixture-owner-ask" {
			t.Fatal("owner message lost")
		}
		call(map[string]any{"action": "create", "title": "Invalid owner request", "goal": "Must refuse", "kind": "long", "owner_request": badAsk}, true)
		suggested := call(map[string]any{"action": "create", "title": "Unasked work", "goal": "Only suggest", "kind": "long"}, false)
		if suggested["created"] != "suggestion" || get(suggested["id"].(string)).Status != "suggested" {
			t.Fatal("unasked creation activated")
		}
	})
	t.Run("b_statuses", func(t *testing.T) {
		for _, status := range []string{"waiting", "active", "quiet", "active", "done"} {
			task := taskCall(map[string]any{"action": "update", "id": started.ID, "status": status, "reason": "Owner confirmed this state"})
			if task.Status != status {
				t.Fatal("status not applied", status)
			}
			read := get(task.ID)
			found := false
			for _, e := range read.History {
				if strings.Contains(e.Text, "Owner confirmed this state") {
					found = true
				}
			}
			if !found {
				t.Fatal("status reason missing")
			}
		}
		ended = started
		dropped := create("Retired project", nil)
		dropped = taskCall(map[string]any{"action": "update", "id": dropped.ID, "status": "dropped", "reason": "Outcome no longer needed"})
		if dropped.Status != "dropped" || dropped.Summary.Text != "Outcome no longer needed" {
			t.Fatal("end reason missing")
		}
		call(map[string]any{"action": "update", "id": started.ID, "status": "active"}, true)
	})
	t.Run("c_checklist", func(t *testing.T) {
		task := create("Checklist project", nil)
		task = taskCall(map[string]any{"action": "checklist", "id": task.ID, "op": "add", "steps": []any{map[string]any{"text": "Review draft"}}})
		if len(task.Checklist) != 1 {
			t.Fatal("step missing")
		}
		step := task.Checklist[0].ID
		for _, op := range []string{"done", "undo", "remove"} {
			task = taskCall(map[string]any{"action": "checklist", "id": task.ID, "op": op, "steps": entities(step)})
			if op == "remove" {
				if len(task.Checklist) != 0 {
					t.Fatal("remove failed")
				}
			} else if task.Checklist[0].Done != (op == "done") {
				t.Fatal("toggle failed")
			}
		}
	})
	t.Run("d_membership", func(t *testing.T) {
		task := create("Membership project", nil)
		for _, action := range []string{"add_members", "remove_members"} {
			call(map[string]any{"action": action, "id": task.ID, "chats": entities(testkit.Group)}, true)
		}
		call(map[string]any{"action": "add_members", "id": task.ID, "people": entities(person), "owner_request": badAsk}, true)
		call(map[string]any{"action": "remove_members", "id": task.ID, "people": entities(person), "owner_request": badAsk}, true)
		before := len(get(task.ID).History)
		task = taskCall(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Group), "people": entities(person), "owner_request": ask})
		task = get(task.ID)
		if len(task.Chats) != 1 || len(task.People) != 1 || task.Membership.Chats[testkit.Group] != "agent:"+a.ID {
			t.Fatal("membership/provenance lost")
		}
		if len(task.History) != before+1 || !strings.Contains(task.History[0].Text, a.Name) || !strings.Contains(task.History[0].Text, ask["text"].(string)) {
			t.Fatal("add needs one attributed quoted history line", task.History)
		}
		before = len(task.History)
		taskCall(map[string]any{"action": "remove_members", "id": task.ID, "chats": entities(testkit.Group), "people": entities(person), "owner_request": ask})
		task = get(task.ID)
		if len(task.History) != before+1 || len(task.Chats) != 0 || len(task.People) != 0 || len(task.RemovedMembers.Chats) != 1 {
			t.Fatal("remove/history failed", task)
		}
		_, err := mc.TaskService().AttachEvidence(task.ID, []tasks.Entity{{ID: testkit.Group}}, []tasks.Entity{{ID: person}}, nil, "detector", "fixture evidence")
		testkit.Must(t, err)
		call(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Group), "people": entities(person)}, true)
		if read := get(task.ID); len(read.Chats)+len(read.People) != 0 {
			t.Fatal("removal not sticky")
		}
		task = taskCall(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Group), "people": entities(person), "owner_request": ask})
		if len(task.Chats) != 1 || len(task.People) != 1 || len(task.RemovedMembers.Chats)+len(task.RemovedMembers.People) != 0 {
			t.Fatal("explicit reversal failed")
		}
		// Undoing an explicit reversal restores both tombstones and prior provenance.
		reversalID := get(task.ID).History[0].ChangeID
		task = taskCall(map[string]any{"action": "undo", "id": task.ID, "change_id": reversalID, "owner_request": ask})
		if len(task.Chats)+len(task.People) != 0 || len(task.RemovedMembers.Chats) != 1 || len(task.RemovedMembers.People) != 1 || task.Membership.Chats[testkit.Group] != "" || task.Membership.People[person] != "" {
			t.Fatal("reversal undo lost before-state", task)
		}
		task = taskCall(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Group), "people": entities(person), "owner_request": ask})
		// A hidden chat is still refused with a quote, without partial attachment.
		settings := h.Settings()
		settings.HiddenChats = []string{testkit.Phone}
		testkit.Must(t, h.SetSettings(settings))
		call(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Phone), "owner_request": ask}, true)
		settings.HiddenChats = nil
		testkit.Must(t, h.SetSettings(settings))
		// Revoked access cannot reveal a removed chat's old labels or undo it.
		taskCall(map[string]any{"action": "remove_members", "id": task.ID, "chats": entities(testkit.Group), "owner_request": ask})
		read := get(task.ID)
		removalID := read.History[0].ChangeID
		limited, err := h.Agent(a.ID)
		testkit.Must(t, err)
		original := limited
		limited.Permissions.Chats = model.Scope{Mode: "only", JIDs: []string{testkit.Phone}}
		testkit.Must(t, h.UpdateAgent(limited))
		read = get(task.ID)
		for _, event := range read.History {
			if strings.Contains(event.Text, "Design Club") {
				t.Fatal("revoked history leaked removed chat")
			}
		}
		call(map[string]any{"action": "undo", "id": task.ID, "change_id": removalID, "owner_request": ask}, true)
		testkit.Must(t, h.UpdateAgent(original))
		limited = original
		limited.Permissions.SaveSummaries = false
		testkit.Must(t, h.UpdateAgent(limited))
		call(map[string]any{"action": "add_members", "id": task.ID, "chats": entities(testkit.Group), "owner_request": ask}, true)
		testkit.Must(t, h.UpdateAgent(original))
	})
	t.Run("e_followups_workspace", func(t *testing.T) {
		task := create("Exhibit followup", map[string]any{"follows": ended.ID, "chats": entities(testkit.Phone), "people": entities(person)})
		task = get(task.ID)
		if task.Follows != ended.ID || task.FollowsContext == nil {
			t.Fatal("followup context lost")
		}
		task = taskCall(map[string]any{"action": "update", "id": task.ID, "waiting": map[string]any{"on": map[string]any{"id": person}, "what": "Review notes"}, "summary": map[string]any{"text": "Draft ready"}})
		if task.Status != "waiting" || task.Waiting.What != "Review notes" || task.Summary.Text != "Draft ready" {
			t.Fatal("waiting/update failed")
		}
		task = taskCall(map[string]any{"action": "attach", "id": task.ID, "item": map[string]any{"kind": "note", "text": "Bring printed copy"}})
		if len(task.Items) != 1 {
			t.Fatal("note missing")
		}
		taskCall(map[string]any{"action": "draft_nudge", "id": task.ID, "text": "Review when ready"})
		task = taskCall(map[string]any{"action": "update", "id": task.ID, "waiting": nil})
		if task.Status != "active" || task.Waiting != nil {
			t.Fatal("waiting clear failed")
		}
	})
	t.Run("f_suggestion_evidence", func(t *testing.T) {
		suggested, err := mc.TaskService().Suggest(nil, tasks.Create{Title: "Evidence example", Goal: "Prepare evidence", Kind: "long", Chats: []tasks.Entity{{ID: testkit.Group}}, People: []tasks.Entity{{ID: person}}}, tasks.Suggestion{Signals: []string{"Fixture evidence"}, Confidence: .8, DetectedAt: h.Now(), EffortIDs: []string{"eff_fixture"}})
		testkit.Must(t, err)
		listed := call(map[string]any{"action": "list", "status": "suggested"}, false)
		found := false
		for _, v := range listed["tasks"].([]any) {
			x := v.(map[string]any)
			if x["id"] != suggested.ID {
				continue
			}
			found = true
			sig := x["suggestion"].(map[string]any)
			if x["chat_count"] != float64(1) || x["people_count"] != float64(1) || len(x["chats"].([]any)) != 1 || len(x["people"].([]any)) != 1 || sig["confidence"] != .8 || len(sig["signals"].([]any)) != 1 || sig["effort_ids"].([]any)[0] != "eff_fixture" || sig["detected_at"] == nil {
				t.Fatal("evidence incomplete", x)
			}
		}
		if !found {
			t.Fatal("suggestion not listed")
		}
		open := call(map[string]any{"action": "list"}, false)
		for _, v := range open["tasks"].([]any) {
			if v.(map[string]any)["status"] == "suggested" {
				t.Fatal("suggestions in default list")
			}
		}
	})
	t.Run("g_pick_dismiss", func(t *testing.T) {
		for _, action := range []string{"keep", "dismiss"} {
			suggestion := call(map[string]any{"action": "create", "title": "Pick example", "goal": "Review choice", "kind": "long"}, false)
			id := suggestion["id"].(string)
			if action == "keep" {
				denied := call(map[string]any{"action": "keep", "id": id}, false)
				if denied["kept"] != false || get(id).Status != "suggested" {
					t.Fatal("unquoted keep mutated")
				}
				call(map[string]any{"action": "keep", "id": id, "owner_request": badAsk}, true)
				if task := taskCall(map[string]any{"action": "keep", "id": id, "owner_request": ask}); task.Status != "active" || task.AskedByOwner == nil {
					t.Fatal("keep failed")
				}
			} else if task := taskCall(map[string]any{"action": "dismiss", "id": id, "reason": "Not needed"}); task.Status != "dropped" || task.CloseReason != "not_a_task" {
				t.Fatal("dismiss failed")
			}
		}
	})
	t.Run("g_keep_duplicate_membership_credit", func(t *testing.T) {
		destination := create("Merge approval example", map[string]any{"chats": entities(testkit.Group)})
		before := len(get(destination.ID).History)
		suggestion := call(map[string]any{"action": "create", "title": "Merge approval example", "goal": "Complete Merge approval example", "kind": "long", "chats": entities(testkit.Group, testkit.Phone), "people": entities(person), "different_because": "Fixture for duplicate approval"}, false)
		out := call(map[string]any{"action": "keep", "id": suggestion["id"], "owner_request": ask}, false)
		if out["attached_to_existing"] != true {
			t.Fatal("duplicate keep did not attach")
		}
		read := get(destination.ID)
		if len(read.Chats) != 2 || len(read.People) != 1 || len(read.History) != before+1 || !strings.Contains(read.History[0].Text, a.Name) || !strings.Contains(read.History[0].Text, ask["text"].(string)) {
			t.Fatal("duplicate membership attachment missing single quoted credit", read.History)
		}
	})
	t.Run("h_detail_rule_history", func(t *testing.T) {
		ruleTask = create("Scratch verification", map[string]any{"kind": "test", "chats": entities(testkit.Group)})
		hint := tasks.FinishHint{Why: "Weak fixture evidence", At: h.Now(), MessageRefs: []tasks.Ref{{ChatJID: testkit.Group, MessageID: "fixture-finish"}}}
		_, err := h.Repo.DB.Exec("UPDATE tasks SET data=json_set(data,'$.finish_hint',json(?)) WHERE id=?", dbutil.JSON(hint), ruleTask.ID)
		testkit.Must(t, err)
		read := get(ruleTask.ID)
		if read.AskedByOwner == nil || read.FinishHint == nil || len(read.History) == 0 || read.Membership.Chats[testkit.Group] != "agent:"+a.ID {
			t.Fatal("get missing detail")
		}
		now := h.Now().Add(25 * time.Hour)
		h.Now = func() time.Time { return now }
		var before, after int
		testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&before))
		testkit.Must(t, mc.TaskService().Tick(context.Background()))
		testkit.Must(t, h.Repo.DB.QueryRow("SELECT count(*) FROM agent_updates").Scan(&after))
		if before != after {
			t.Fatal("automatic change emitted updates")
		}
		read = get(ruleTask.ID)
		if read.Status != "dropped" || read.NeedsYou {
			t.Fatal("rule ending failed")
		}
		for _, e := range read.History {
			if e.Kind == "rule_obsolete" {
				changeID = e.ChangeID
				if !strings.Contains(e.Text, "day without activity") {
					t.Fatal("rule reason missing")
				}
			}
		}
		if changeID == "" {
			t.Fatal("rule history/undo id missing", read.History)
		}
	})
	t.Run("i_rule_undo", func(t *testing.T) {
		call(map[string]any{"action": "undo", "id": ruleTask.ID, "change_id": changeID}, true)
		call(map[string]any{"action": "undo", "id": ruleTask.ID, "change_id": changeID, "owner_request": badAsk}, true)
		task := taskCall(map[string]any{"action": "undo", "id": ruleTask.ID, "change_id": changeID, "owner_request": ask})
		if task.Status != "active" {
			t.Fatal("undo did not restore status")
		}
		read := get(task.ID)
		if !strings.Contains(read.History[0].Text, a.Name) || !strings.Contains(read.History[0].Text, ask["text"].(string)) {
			t.Fatal("undo missing credit/request")
		}
		call(map[string]any{"action": "undo", "id": ruleTask.ID, "change_id": changeID, "owner_request": ask}, true)
	})
}
