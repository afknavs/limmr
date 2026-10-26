package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
)

func t1Fixture(t *testing.T) (*rig, map[string]map[string]any) {
	t.Helper()
	r := newRig(t)
	setupWatches(t, r)
	n1Owner(t, r)
	c := circleFixture(t, r, "project", "confirm", false, chatA)
	_, e := r.mc.CircleService().Mutate(c.ID, func(c *model.Circle) error {
		c.Topics = []model.Topic{{ID: "sketch", Title: "Sketch review", Status: "active", Chats: []string{chatA}}}
		return nil
	})
	must(t, e)
	message := firstMessage(t, r, chatA)
	path := filepath.Join(r.home, "invented-media")
	must(t, os.WriteFile(path, []byte("invented media"), 0600))
	r.mc.Downloader = fixtureDownloader{path: path}
	at, e := r.h.Attachment([]byte("invented attachment"), "sketch.txt")
	must(t, e)
	d, e := r.h.DraftMessage(context.Background(), r.a, chatA, "Prepared draft", "", false)
	must(t, e)
	w := createWatch(t, r, nil)
	h := createHandoff(t, r, nil)
	// Handoff fixture adds a free circle; the confirm circle remains strictest.
	r.h.Now = func() time.Time { return epoch.Add(3 * time.Second) }
	_, e = r.call(t, "catch_up", map[string]any{"id": c.ID})
	must(t, e)
	args := map[string]map[string]any{
		"whoami": {}, "get_started": {}, "check_connection": {}, "get_updates": {}, "get_digest": {},
		"list_chats": {}, "get_chat": {"chat_jid": chatA}, "scan_chats": {}, "list_messages": {"chat_jid": chatA}, "search_messages": {"query": "Saturday"}, "get_message_context": {"chat_jid": chatA, "message_id": message.ID, "radius": 0},
		"people_to_understand": {}, "find_person": {"query": "Maya"}, "get_owner_profile": {}, "update_owner_profile": {"fields": map[string]any{"work": "Library assistant"}}, "get_person": {"person_id": "maya"}, "list_groups": {}, "get_group": {"group_jid": groupA}, "people_near": {"focus": "maya"},
		"add_fact": {"subject": "person", "subject_id": "maya", "text": "Organises reviews", "evidence_message_ids": []string{message.ID}}, "update_person": {"person_id": "maya", "fields": map[string]any{"notes": "Morning reviews"}}, "add_relation": {"a": "me", "b": "maya", "kind": "friend"}, "save_summary": {"chat_jid": chatA, "period": "today", "text": "Sketch discussion"}, "get_summaries": {"chat_jid": chatA},
		"download_media": {"chat_jid": chatA, "message_id": message.ID}, "upload_attachment": {"content_text": "Sketch", "filename": "sketch.txt"}, "notify_me": {"text": "Sketch summary ready", "key": "fixture-notify"},
		"send_message": {"recipient": chatA, "text": "Hello"}, "send_file": {"recipient": chatA, "attachment_id": at.ID}, "send_reaction": {"recipient": chatA, "message_id": message.ID, "text": "👍"}, "get_send_status": {"approval_id": d.ID}, "check_send": {"approval_id": d.ID}, "my_activity": {},
		"resolve_context": {"query": "Sketch"}, "list_circles": {}, "get_circle": {"id": c.ID}, "get_brief": {"id": c.ID}, "catch_up": {"id": c.ID}, "update_brief": {"id": c.ID, "summary": "Sketches were discussed.", "what_changed": "Review started.", "evidence_message_ids": []string{message.ID}}, "suggest": {"circle_id": c.ID, "kind": "reply", "why": "Review requested", "chat_jid": chatA, "draft_text": "Ready"},
		"propose_circle": {"name": "Garden work", "kind": "community", "chats": []string{groupB}, "people": []string{}, "why": "Shared work"}, "add_to_circle": {"circle_id": c.ID, "chats": []string{groupA}, "people": []string{}, "why": "Design Club shares the sketch review"}, "set_person_role": {"circle_id": c.ID, "person_id": "maya", "role": "reviewer"}, "add_topic": {"circle_id": c.ID, "title": "Morning review"}, "update_topic": {"circle_id": c.ID, "topic_id": "sketch", "status": "watching"}, "draft_message": {"chat": chatA, "text": "Sketches ready"}, "send_draft": {"draft_id": d.ID},
		"create_watch": {"goal": "Watch exam news", "grant_text": "Watch exam news", "scope": map[string]any{"chats": []string{chatA}}, "match": map[string]any{"kind": "any"}, "action": "notify"}, "list_watches": {}, "update_watch": {"id": w.ID, "instruction": "Include dates"}, "stop_watch": {"id": w.ID}, "start_handoff": {"goal": "Help review sketches", "grant_text": "Help review sketches", "scope": map[string]any{"chats": []string{chatA}}, "runner": map[string]any{"kind": "codex", "session_id": "other-session", "workdir": r.home}, "chat_jid": chatA}, "stop_handoff": {"id": h.ID},
	}
	return r, args
}
func t1RPC(t *testing.T, r *rig, name string, args any) map[string]any {
	t.Helper()
	w := r.request(t, "POST", "/mcp", r.token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, map[string]string{"Accept": "application/json, text/event-stream"})
	status(t, w, 200)
	var envelope map[string]any
	must(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	if envelope["error"] != nil {
		t.Fatal("JSON-RPC lookup rejected tool", name)
	}
	return envelope["result"].(map[string]any)
}
func t1Data(t *testing.T, result map[string]any) any {
	t.Helper()
	if result["isError"] == true {
		t.Fatal(result["content"])
	}
	if structured, ok := result["structuredContent"].(map[string]any); ok {
		return structured["data"]
	}
	var out any
	must(t, json.Unmarshal([]byte(result["content"].([]any)[0].(map[string]any)["text"].(string)), &out))
	return out
}
func shape(v any) any {
	switch x := v.(type) {
	case map[string]any:
		r := map[string]any{}
		for k, v := range x {
			r[k] = shape(v)
		}
		return r
	case []any:
		if len(x) == 0 {
			return "array"
		}
		unique := map[string]bool{}
		for _, v := range x {
			unique[encode(shape(v))] = true
		}
		keys := []string{}
		for k := range unique {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	default:
		return fmt.Sprintf("%T", v)
	}
}
func TestT1EveryLegacyNameThroughTransportAndMore(t *testing.T) {
	if len(mcpserver.LegacyCatalog()) != 53 {
		t.Fatal("legacy coverage changed")
	}
	for _, tool := range mcpserver.LegacyCatalog() {
		t.Run(tool.Name, func(t *testing.T) {
			direct, args := t1Fixture(t)
			_, e := direct.h.Authenticate(direct.token)
			must(t, e)
			baseline, e := direct.call(t, tool.Name, args[tool.Name])
			must(t, e)
			var normalized any
			must(t, json.Unmarshal([]byte(encode(baseline)), &normalized))
			for _, via := range []string{"alias", "more"} {
				t.Run(via, func(t *testing.T) {
					r, args := t1Fixture(t)
					name, input := tool.Name, any(args[tool.Name])
					if via == "more" {
						name = "more"
						input = map[string]any{"action": tool.Name, "args": input}
					}
					result := t1Data(t, t1RPC(t, r, name, input))
					if !reflect.DeepEqual(shape(normalized), shape(result)) {
						t.Fatalf("response shape changed for %s via %s", tool.Name, via)
					}
					rows, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 1})
					must(t, e)
					if len(rows) != 1 {
						t.Fatal("missing activity")
					}
					if via == "more" && rows[0].CalledAs != "more:"+tool.Name {
						t.Fatal("missing dispatcher attribution")
					}
					if via == "alias" {
						listed := false
						for _, entry := range mcpserver.Catalog() {
							listed = listed || entry.Name == tool.Name
						}
						if !listed && rows[0].CalledAs != tool.Name {
							t.Fatal("missing hidden alias attribution")
						}
					}
					recognized := false
					for _, entry := range mcpserver.Catalog() {
						recognized = recognized || rows[0].Tool == entry.Name
					}
					if !recognized {
						t.Fatal("activity did not use a canonical name")
					}
				})
			}
		})
	}
}

func TestT1JobModes(t *testing.T) {
	r, args := t1Fixture(t)
	run := func(name string, input map[string]any) any { t.Helper(); return t1Data(t, t1RPC(t, r, name, input)) }
	run("whoami", map[string]any{})
	for _, kind := range []string{"person", "circle", "group", "owner_profile", "summaries"} {
		id := map[string]any{"person": "maya", "circle": args["get_circle"]["id"], "group": groupA, "summaries": chatA}[kind]
		input := map[string]any{"kind": kind}
		if id != nil {
			input["id"] = id
		}
		run("get", input)
	}
	run("people", map[string]any{"view": "near", "focus": "maya"})
	run("people", map[string]any{"view": "to_understand"})
	for _, mode := range []string{"chat", "around", "since"} {
		input := map[string]any{"mode": mode}
		if mode != "since" {
			input["chat_jid"] = chatA
		}
		if mode == "around" {
			input["message_id"] = firstMessage(t, r, chatA).ID
			input["radius"] = 0
		}
		run("read", input)
	}
	run("find", map[string]any{"query": "Maya"})
	run("find", map[string]any{"query": "Saturday", "types": []string{"message"}})
	for _, action := range []string{"get", "update"} {
		input := map[string]any{"action": action}
		for k, v := range args[action+"_brief"] {
			input[k] = v
		}
		run("brief", input)
	}
	for kind, old := range map[string]string{"fact": "add_fact", "relation": "add_relation", "person_update": "update_person", "role": "set_person_role", "owner_profile": "update_owner_profile", "summary": "save_summary", "topic_add": "add_topic", "topic_update": "update_topic"} {
		input := map[string]any{}
		for k, v := range args[old] {
			input[k] = v
		}
		if kind == "relation" {
			input["relation_kind"] = input["kind"]
		}
		input["kind"] = kind
		run("remember", input)
	}
	for action, old := range map[string]string{"list": "list_circles", "propose": "propose_circle", "add": "add_to_circle", "update": "add_to_circle"} {
		input := map[string]any{"action": action}
		for k, v := range args[old] {
			input[k] = v
		}
		run("circles", input)
	}
	for _, pair := range [][2]string{{"create", "create_watch"}, {"list", "list_watches"}, {"update", "update_watch"}, {"stop", "stop_watch"}, {"handoff_start", "start_handoff"}, {"handoff_stop", "stop_handoff"}} {
		action, old := pair[0], pair[1]
		input := map[string]any{}
		for k, v := range args[old] {
			input[k] = v
		}
		if action == "create" {
			input["watch_action"] = input["action"]
		}
		input["action"] = action
		run("watch", input)
	}
	run("send", map[string]any{"recipient": chatA, "text": "Draft", "draft_only": true})
	run("send", map[string]any{"recipient": chatA, "content_text": "Sketch", "filename": "sketch.txt", "draft_only": true})
	run("send", map[string]any{"recipient": chatA, "react": map[string]any{"emoji": "👍", "message_id": firstMessage(t, r, chatA).ID}, "draft_only": true})
	run("send", map[string]any{"draft_id": args["send_draft"]["draft_id"]})
	run("send_status", map[string]any{"approval_id": args["check_send"]["approval_id"]})
	index := run("more", map[string]any{}).(map[string]any)["actions"].([]any)
	if len(index) != 60 {
		t.Fatal("incomplete compatibility index")
	}
}

func TestT1PermissionsDraftOnlyAndValidation(t *testing.T) {
	r, args := t1Fixture(t)
	p := model.Defaults(true)
	r.agent(t, p)
	for name, input := range map[string]any{
		"remember": map[string]any{"kind": "fact", "subject": "person", "subject_id": "maya", "text": "No write"},
		"send":     map[string]any{"recipient": chatA, "text": "No send"},
		"brief":    map[string]any{"action": "update", "id": args["get_circle"]["id"], "summary": "No write", "what_changed": "Nothing"},
		"circles":  map[string]any{"action": "add", "circle_id": args["get_circle"]["id"], "chats": []string{}, "people": []string{}, "why": "No write"},
		"watch":    map[string]any{"action": "handoff_start", "goal": "No send", "grant_text": "No send", "scope": map[string]any{"chats": []string{chatA}}, "runner": map[string]any{"kind": "codex", "session_id": "test", "workdir": r.home}, "chat_jid": chatA},
		"more":     map[string]any{"action": "send_message", "args": map[string]any{"recipient": chatA, "text": "No send"}},
	} {
		if t1RPC(t, r, name, input)["isError"] != true {
			t.Fatal("capability bypass", name)
		}
	}
	t1Data(t, t1RPC(t, r, "brief", map[string]any{"action": "get", "id": args["get_circle"]["id"]}))
	p = model.Preset("own")
	r.agent(t, p)
	for _, extra := range []map[string]any{{"text": "Draft"}, {"content_text": "Draft", "filename": "draft.txt"}, {"react": map[string]any{"emoji": "👍", "message_id": firstMessage(t, r, chatA).ID}}} {
		input := map[string]any{"recipient": chatA, "draft_only": true, "owner_instruction": "Send this now."}
		for k, v := range extra {
			input[k] = v
		}
		out := t1Data(t, t1RPC(t, r, "send", input)).(map[string]any)
		if out["state"] != "draft" {
			t.Fatal("draft-only did not return a draft")
		}
	}
	if len(r.fake.Sent) != 0 {
		t.Fatal("draft-only delivered")
	}
	for name, input := range map[string]any{"read": map[string]any{"mode": "around", "chat_jid": chatA, "message_id": firstMessage(t, r, chatA).ID, "radius": 51}, "remember": map[string]any{"kind": "owner_profile", "fields": map[string]any{"unknown": "x"}}, "send": map[string]any{"draft_id": "x", "text": "ambiguous"}, "find": map[string]any{"query": "x", "types": []string{"secret"}}, "more": map[string]any{"action": "more", "args": map[string]any{}}} {
		if t1RPC(t, r, name, input)["isError"] != true {
			t.Fatal("invalid mode accepted", name)
		}
	}
}

func TestT1ScopedSincePagingAndFind(t *testing.T) {
	r := newRig(t)
	p := r.a.Permissions
	p.Chats = model.Scope{Mode: "only", JIDs: []string{chatA}}
	r.agent(t, p)
	input := map[string]any{"mode": "since", "since": epoch.Add(-24 * time.Hour).Format(time.RFC3339Nano), "limit": 1}
	ids := map[string]bool{}
	for i := 0; i < 5; i++ {
		out, e := r.call(t, "read", input)
		must(t, e)
		page := out.(map[string]any)["updates"].(circles.CatchUp)
		for _, c := range page.Chats {
			if c.ChatJID != chatA {
				t.Fatal("scope leak")
			}
			for _, m := range c.Messages {
				if ids[m.Message.ID] {
					t.Fatal("duplicate page")
				}
				ids[m.Message.ID] = true
			}
		}
		if page.Next == "" {
			break
		}
		input["page_token"] = page.Next
	}
	if len(ids) != 3 {
		t.Fatal("lost messages", len(ids))
	}
	out, e := r.call(t, "find", map[string]any{"query": "Saturday", "types": []string{"message"}})
	must(t, e)
	if strings.Contains(encode(out), chatB) {
		t.Fatal("find leaked hidden chat")
	}
}

func TestT1HandoffCannotNestThroughAnyEntry(t *testing.T) {
	r := newRig(t)
	_, args := t1Fixture(t)
	s, clock, requests := setupWatches(t, r)
	createHandoff(t, r, nil)
	req := wake(t, s, clock, requests)
	ctx := runContext(t, r, req)
	for _, entry := range []string{"start_handoff", "more", "watch"} {
		raw := args["start_handoff"]
		if entry == "more" {
			raw = map[string]any{"action": "start_handoff", "args": raw}
		}
		if entry == "watch" {
			copy := map[string]any{"action": "handoff_start"}
			for k, v := range raw {
				copy[k] = v
			}
			raw = copy
		}
		b, _ := json.Marshal(raw)
		decoded, e := mcpserver.DecodeArgs(b)
		must(t, e)
		if _, e = r.mc.Call(ctx, r.a.ID, entry, decoded); e == nil {
			t.Fatal("nested handoff accepted", entry)
		}
	}
}

func TestT1NarrowFindPermissionsAndUsage(t *testing.T) {
	r := newRig(t)
	for typ, permissions := range map[string]model.Permissions{
		"person":  {ReadPeople: true, Chats: model.Scope{Mode: "all"}},
		"chat":    {ReadMessages: true, Chats: model.Scope{Mode: "all"}},
		"message": {Search: true, Chats: model.Scope{Mode: "all"}},
	} {
		r.agent(t, permissions)
		query := "Maya"
		if typ == "message" {
			query = "Saturday"
		}
		_, e := r.call(t, "find", map[string]any{"query": query, "types": []string{typ}})
		must(t, e)
	}
	// Use a fresh agent's rows so the aggregation checks cover just these calls.
	r = newRig(t)
	for name, input := range map[string]any{
		"get_chat":   map[string]any{"chat_jid": chatA},
		"list_chats": map[string]any{},
		"read":       map[string]any{"mode": "chat", "chat_jid": chatA, "limit": 1},
		"find":       map[string]any{"query": "Saturday", "types": []string{"chat", "message"}},
	} {
		_, e := r.call(t, name, input)
		must(t, e)
	}
	rows, e := r.h.Activity(nil, hub.ActivityFilter{Limit: 100})
	must(t, e)
	sessions := hub.GroupReadSessions(rows)
	if len(sessions) != 1 || sessions[0].Messages != 13 {
		t.Fatal("canonical names broke read totals", sessions)
	}
	history, e := r.h.StatsHistory(1)
	must(t, e)
	if history.Days[0].AgentReads != 2 {
		t.Fatal("directory metadata counted as message reads", history.Days[0].AgentReads)
	}
}

func TestT1ExpiredMoreWhoamiOnly(t *testing.T) {
	r := newRig(t)
	p := r.a.Permissions
	until := epoch.Add(-time.Hour)
	p.Access.Until = &until
	r.agent(t, p)
	_, e := r.call(t, "more", map[string]any{"action": "whoami", "args": map[string]any{}})
	must(t, e)
	if _, e = r.call(t, "more", map[string]any{"action": "get_started", "args": map[string]any{}}); e == nil {
		t.Fatal("dispatcher bypassed expiry")
	}
}

func TestT1SendModesPreserveInstructionAndQueue(t *testing.T) {
	for _, kind := range []string{"message", "file", "reaction"} {
		t.Run(kind, func(t *testing.T) {
			for _, queue := range []bool{false, true} {
				t.Run(fmt.Sprint(queue), func(t *testing.T) {
					r := newRig(t)
					circleFixture(t, r, "project", "confirm", false, chatA)
					r.a.Preset = "own"
					r.agent(t, model.Preset("own"))
					input := map[string]any{"recipient": chatA, "owner_instruction": "Send the reviewed sketch now.", "queue_for_approval": queue}
					switch kind {
					case "message":
						input["text"] = "Reviewed sketch"
					case "file":
						input["content_text"] = "Reviewed sketch"
						input["filename"] = "sketch.txt"
					case "reaction":
						input["react"] = map[string]any{"emoji": "👍", "message_id": firstMessage(t, r, chatA).ID}
					}
					out, e := r.call(t, "send", input)
					must(t, e)
					draft := out.(model.Draft)
					if queue {
						if len(r.fake.Sent) != 0 || draft.State != "suggested" {
							t.Fatal("review queue delivered")
						}
					} else if draft.State != "sent" || len(r.fake.Sent) != 1 || r.fake.Sent[0].Kind != kind {
						t.Fatal("send mode did not deliver through fake client", kind)
					}
				})
			}
		})
	}
}
