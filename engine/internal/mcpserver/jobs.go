package mcpserver

import (
	"context"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"limmr/internal/circles"
	"limmr/internal/model"
)

// Each old contract stays available, including its validation and output shape.
var aliases = map[string]string{
	"get_started": "whoami", "check_connection": "whoami",
	"list_chats": "find", "get_chat": "read", "list_messages": "read", "get_message_context": "read", "catch_up": "read",
	"resolve_context": "find", "find_person": "find", "search_messages": "find",
	"get_person": "get", "get_circle": "get", "get_group": "get", "get_owner_profile": "get", "get_summaries": "get", "list_groups": "find",
	"people_near": "people", "people_to_understand": "people", "get_brief": "brief", "update_brief": "brief",
	"add_fact": "remember", "add_relation": "remember", "update_person": "remember", "set_person_role": "remember", "update_owner_profile": "remember", "save_summary": "remember", "add_topic": "remember", "update_topic": "remember",
	"list_circles": "circles", "propose_circle": "circles", "add_to_circle": "circles",
	"send_message": "send", "send_file": "send", "send_reaction": "send", "draft_message": "send", "send_draft": "send",
	"get_send_status": "send_status", "check_send": "send_status", "my_activity": "more",
	"create_watch": "watch", "list_watches": "watch", "update_watch": "watch", "stop_watch": "watch", "start_handoff": "watch", "stop_handoff": "watch",
}

func canonicalName(name string) string {
	if v := aliases[name]; v != "" {
		return v
	}
	return name
}

func calledAs(name string) string {
	if canonicalName(name) != name {
		return name
	}
	return ""
}
func isJob(name string) bool {
	return model.Contains([]string{"find", "read", "get", "people", "brief", "remember", "circles", "send", "send_status", "watch", "more", "task"}, name)
}

// Union schemas stay compact. Each action is validated against its full
// original schema before execution, including nested profile/watch/file fields.
func Catalog() []Tool {
	old := map[string]Tool{}
	for _, t := range LegacyCatalog() {
		old[t.Name] = t
	}
	union := func(name, description, required string, sources ...string) Tool {
		t := def(name, description, required, "")
		p := t.InputSchema["properties"].(map[string]any)
		for _, source := range sources {
			for k, v := range old[source].InputSchema["properties"].(map[string]any) {
				p[k] = v
			}
		}
		return t
	}
	choice := func(t *Tool, field string, values ...string) {
		t.InputSchema["properties"].(map[string]any)[field] = map[string]any{"type": "string", "enum": values}
	}
	find := def("find", "Find visible entities; clarify weak/close matches. types defaults to person|circle|chat|topic; message uses SQLite FTS5.", "query", "types:array limit:integer offset:integer chat_jid before after")
	find.InputSchema["properties"].(map[string]any)["types"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"person", "circle", "chat", "topic", "message"}}, "maxItems": 5}
	read := union("read", "Read messages + chat metadata. chat: chat_jid, newest first, limit/offset; around: chat_jid,message_id,radius 0..50; since: id (circle/topic; omit=all), since/cursor RFC3339, page_token. Finish next_page_token; inspect author.type.", "mode", "list_messages", "get_message_context", "catch_up")
	choice(&read, "mode", "chat", "around", "since")
	read.InputSchema["properties"].(map[string]any)["cursor"] = map[string]any{"type": "string"}
	get := def("get", "Read one entity. kind=person|circle|group uses id; owner_profile needs no id; summaries uses id=chat JID and optional limit.", "kind", "id limit:integer")
	choice(&get, "kind", "person", "circle", "group", "owner_profile", "summaries")
	people := union("people", "near: focus,depth,limit for graph neighbours; to_understand: frequent contacts missing context. Owner reviews proposals.", "view", "people_near")
	choice(&people, "view", "near", "to_understand")
	brief := union("brief", "get: id. update: id,summary (120 words),what_changed; cite evidence_message_ids. Optional fields: more{}. Skip empty catch-ups.", "action id", "get_brief", "update_brief")
	choice(&brief, "action", "get", "update")
	remember := union("remember", "Attributed writes; profiles need owner review.\nfact: subject,subject_id,text\nrelation: a,b,relation_kind\nperson_update: person_id,fields\nrole: circle_id,person_id,role\nowner_profile: fields\nsummary: chat_jid,period,text OR subject=person,subject_id,text. Pronouns/gender: person_update fields.details.pronouns (he/him, she/her, they/them), never notes\ntopic_add: circle_id,title\ntopic_update: circle_id,topic_id\nOptional fields: more{}.", "kind", "add_fact", "add_relation", "update_person", "set_person_role", "update_owner_profile", "save_summary", "add_topic", "update_topic")
	rp := remember.InputSchema["properties"].(map[string]any)
	rp["relation_kind"] = map[string]any{"type": "string"}
	rp["fields"] = map[string]any{"anyOf": []any{old["update_person"].InputSchema["properties"].(map[string]any)["fields"], old["update_owner_profile"].InputSchema["properties"].(map[string]any)["fields"]}}
	choice(&remember, "kind", "fact", "relation", "person_update", "role", "owner_profile", "summary", "topic_add", "topic_update")
	cs := union("circles", "list; propose: name,kind,chats,people,why,goal?; add/update: circle_id,chats,people,why (additive). Current chats; merge half-overlap. Own AIs: Undo; given AIs: review.", "action", "list_circles", "propose_circle", "add_to_circle")
	choice(&cs, "action", "list", "propose", "add", "update")
	send := union("send", "Send text, file, or react={emoji,message_id}. Own AIs: quote owner_instruction; otherwise confirm returns final_text for consent. queue_for_approval only for requested review. draft_only=true never sends; draft_id sends a draft. One file source: attachment_id|content_text|content_base64|url|file; filename?, as_document? (needs: sending).", "", "send_message", "send_file", "send_draft")
	sp := send.InputSchema["properties"].(map[string]any)
	delete(sp, "voice")
	delete(sp, "signed")
	sp["draft_only"] = map[string]any{"type": "boolean"}
	sp["react"] = map[string]any{"type": "object", "properties": map[string]any{"emoji": map[string]any{"type": "string"}, "message_id": map[string]any{"type": "string"}}, "required": []string{"emoji", "message_id"}, "additionalProperties": false}
	fileSchema(&send)
	status := union("send_status", "Check your send without sending; supply exactly one of message_id,approval_id,key.", "", "get_send_status")
	watch := union("watch", "Explicit grant_text; ask Allow. create: goal,grant_text,scope,match,watch_action; list; update: id,instruction?|digest?|status?|escalate?; stop: id; handoff_start: goal,grant_text,scope,runner,chat_jid; handoff_stop: id. Quiet hours/budgets apply.", "action", "create_watch", "list_watches", "update_watch", "stop_watch", "start_handoff", "stop_handoff")
	watch.InputSchema["properties"].(map[string]any)["watch_action"] = map[string]any{"type": "string"}
	choice(&watch, "action", "create", "list", "update", "stop", "handoff_start", "handoff_stop")
	more := def("more", "{} lists rare action schemas, including efforts.*. {action,args} executes one; permissions and cached aliases remain supported.", "", "action args:object")
	more.InputSchema["properties"].(map[string]any)["args"] = map[string]any{"type": "object", "additionalProperties": true}
	who := old["whoami"]
	who.Description = "Call first: identity, access, permissions, connection health and setup steps. Then scan relevant chats."
	xs := []Tool{who, old["get_updates"], old["get_digest"], find, read, old["scan_chats"], get, people, brief, remember, cs, send, status, old["upload_attachment"], old["notify_me"], old["download_media"], watch, old["suggest"], more, taskTool()}
	for i := range xs {
		if isJob(xs[i].Name) {
			write := model.Contains([]string{"brief", "remember", "circles", "send", "watch", "more", "task"}, xs[i].Name)
			world := xs[i].Name == "send" || xs[i].Name == "more" || xs[i].Name == "watch"
			xs[i].Annotations = &mcp.ToolAnnotations{ReadOnlyHint: !write, DestructiveHint: boolPtr(write), OpenWorldHint: boolPtr(world)}
		}
		// The protocol is stated once in server Instructions, not per definition.
		xs[i].Description = strings.TrimSuffix(xs[i].Description, protocol)
		switch xs[i].Name {
		case "get_updates":
			xs[i].Description = "Conversation catch-up: watch hits, feeds and send status. Private cursor; wait_seconds 0..25, limit 1..100."
		case "notify_me":
			xs[i].Description = "Notify owner on WhatsApp as this AI (needs: sending). Stable key required. Optional file source; as_document forces document. Uses You circle rules."
		case "download_media":
			xs[i].Description = "Download visible media (needs: media downloads). Local: path; remote: single-use download_url, valid 15 minutes."
		case "upload_attachment":
			xs[i].Description = "Upload one file source (needs: sending). Returns attachment_id, name, size, sha256."
		}
		if xs[i].Name == "send" || xs[i].Name == "notify_me" {
			xs[i].Description += " During updates, retry the tool error after a minute."
		}
	}
	return xs
}

func (s *Service) setup(a model.Agent) (circles.SetupStatus, error) {
	linked := false
	if s.Link != nil {
		state := s.Link.State().State
		linked = state == "ready" || state == "syncing"
	}
	v, e := s.CircleService().Setup(&a, linked)
	if e != nil {
		return v, e
	}
	v.NextSteps = s.permittedSteps(a, v.NextSteps)
	rewrite := strings.NewReplacer(
		"update_owner_profile", "remember(kind=owner_profile)", "get_owner_profile", "get(kind=owner_profile)",
		"list_circles", "circles(action=list)", "list_chats", "more list_chats", "get_chat", "read(mode=chat)",
		"propose_circle", "circles(action=propose)", "add_relation", "remember(kind=relation)",
		"update_person", "remember(kind=person_update)", "set_person_role", "remember(kind=role)",
		"find_person", "find(types=[person])", "list_messages", "read(mode=chat)",
		"get_brief", "brief(action=get)", "catch_up", "read(mode=since)", "update_brief", "brief(action=update)",
		"create_watch", "watch(action=create)")
	for i := range v.NextSteps {
		v.NextSteps[i].How = rewrite.Replace(v.NextSteps[i].How)
		for j, name := range v.NextSteps[i].Tools {
			listed := canonicalName(name)
			if name == "list_chats" || name == "list_groups" {
				listed = "more"
			}
			v.NextSteps[i].Tools[j] = listed
		}
	}
	return v, nil
}

func (s *Service) callJob(ctx context.Context, a model.Agent, name string, args Args, act *model.Activity) (any, error) {
	if name == "task" {
		args = normalizeTaskArgs(args)
	}
	for _, t := range Catalog() {
		if t.Name == name && !validate(t, args) {
			return nil, invalidArgs(t, args)
		}
	}
	call := func(target string, fields Args) (any, error) { return s.callLegacy(ctx, a, target, fields, act) }
	// Strip dispatch selectors before legacy validation; all other fields are still
	// checked, so a field for another mode cannot accidentally authorize a write.
	fields := Args{}
	for k, v := range args {
		fields[k] = v
	}
	pick := func(selector string, targets map[string]string) (any, error) {
		target := targets[args.str(selector)]
		if target == "" {
			return nil, model.ErrInvalid
		}
		delete(fields, selector)
		return call(target, fields)
	}
	switch name {
	case "task":
		return s.callTask(ctx, a, args, act)
	case "more":
		if len(args) == 0 {
			return rareIndex(), nil
		}
		target := args.str("action")
		if target == "" {
			return nil, fieldError("action", strings.Join(rareNames(), " | "))
		}
		if strings.HasPrefix(target, "efforts.") {
			if !model.Contains(rareNames(), target) {
				return nil, fieldError("action", strings.Join(rareNames(), " | "))
			}
			raw, _ := args["args"].(map[string]any)
			return s.callEffort(a, target, Args(raw), act)
		}
		known := false
		for _, t := range LegacyCatalog() {
			known = known || t.Name == target
		}
		if !known {
			return nil, fieldError("action", strings.Join(rareNames(), " | "))
		}
		raw, _ := args["args"].(map[string]any)
		if raw == nil {
			raw = map[string]any{}
		}
		act.Tool = canonicalName(target)
		act.CalledAs = "more:" + target
		// whoami is an unchanged public name, not a recursive dispatcher.
		return s.callLegacy(ctx, a, target, Args(raw), act)
	case "get":
		delete(fields, "kind")
		delete(fields, "id")
		target, key := "", ""
		switch args.str("kind") {
		case "person":
			target, key = "get_person", "person_id"
		case "circle":
			target, key = "get_circle", "id"
		case "group":
			target, key = "get_group", "group_jid"
		case "owner_profile":
			target = "get_owner_profile"
		case "summaries":
			target, key = "get_summaries", "chat_jid"
		}
		if key != "" {
			if strings.TrimSpace(args.str("id")) == "" {
				return nil, fieldError("id", "non-empty visible entity ID; summaries uses a chat JID")
			}
			fields[key] = args.str("id")
		}
		return call(target, fields)
	case "people":
		return pick("view", map[string]string{"near": "people_near", "to_understand": "people_to_understand"})
	case "brief":
		return pick("action", map[string]string{"get": "get_brief", "update": "update_brief"})
	case "remember":
		if args.str("kind") == "relation" {
			if strings.TrimSpace(args.str("relation_kind")) == "" {
				return nil, fieldError("relation_kind", "non-empty relationship label")
			}
			delete(fields, "relation_kind")
			fields["kind"] = args.str("relation_kind")
			return call("add_relation", fields)
		}
		return pick("kind", map[string]string{"fact": "add_fact", "person_update": "update_person", "role": "set_person_role", "owner_profile": "update_owner_profile", "summary": "save_summary", "topic_add": "add_topic", "topic_update": "update_topic"})
	case "circles":
		return pick("action", map[string]string{"list": "list_circles", "propose": "propose_circle", "add": "add_to_circle", "update": "add_to_circle"})
	case "watch":
		action := args.str("action")
		delete(fields, "action")
		if action == "create" {
			fields["action"] = args.str("watch_action")
			delete(fields, "watch_action")
		}
		target := map[string]string{"create": "create_watch", "list": "list_watches", "update": "update_watch", "stop": "stop_watch", "handoff_start": "start_handoff", "handoff_stop": "stop_handoff"}[action]
		return call(target, fields)
	case "send_status":
		return call("get_send_status", fields)
	case "send":
		return s.sendJob(ctx, a, args, act)
	case "read":
		delete(fields, "mode")
		mode := args.str("mode")
		if mode == "since" {
			if args.str("cursor") != "" {
				if args.str("since") != "" {
					return nil, fieldError("since/cursor", "exactly one RFC3339 timestamp with timezone")
				}
				fields["since"] = args.str("cursor")
			}
			delete(fields, "cursor")
			if args.str("id") == "" {
				fields["id"] = "_visible"
			}
			out, e := call("catch_up", fields)
			if e != nil {
				return nil, e
			}
			page := out.(circles.CatchUp)
			chats := []model.Chat{}
			for _, group := range page.Chats {
				v, e := s.chatMetadata(a, group.ChatJID)
				if e != nil {
					return nil, e
				}
				chats = append(chats, v)
			}
			return map[string]any{"chats": chats, "updates": page}, nil
		}
		target := "list_messages"
		if mode == "around" {
			target = "get_message_context"
		}
		out, e := call(target, fields)
		if e != nil {
			return nil, e
		}
		chat, e := s.chatMetadata(a, args.str("chat_jid"))
		if e != nil {
			return nil, e
		}
		return map[string]any{"chat": chat, "messages": out}, nil
	case "find":
		return s.findJob(ctx, a, args, act)
	}
	return nil, model.ErrNotFound
}
func (s *Service) chatMetadata(a model.Agent, jid string) (model.Chat, error) {
	jid, e := s.Hub.ResolveChat(jid)
	if e != nil {
		return model.Chat{}, e
	}
	chats, _, e := s.Hub.Visible(&a)
	if e != nil {
		return model.Chat{}, e
	}
	for _, c := range chats {
		if c.JID == jid {
			return c, nil
		}
	}
	return model.Chat{}, model.ErrNotFound
}
func rareIndex() any {
	out := []map[string]any{}
	for _, t := range append(LegacyCatalog(), effortTools()...) {
		required, _ := t.InputSchema["required"].([]string)
		optional := []string{}
		for field := range t.InputSchema["properties"].(map[string]any) {
			if !model.Contains(required, field) {
				optional = append(optional, field)
			}
		}
		sort.Strings(optional)
		tool := canonicalName(t.Name)
		if strings.HasPrefix(t.Name, "efforts.") {
			tool = "more"
		}
		out = append(out, map[string]any{"action": t.Name, "tool": tool, "required": required, "optional": optional, "types": fieldTypes(t), "schema": t.InputSchema, "description": strings.TrimSuffix(t.Description, protocol)})
	}
	return map[string]any{"actions": out}
}
func rareNames() []string {
	xs := []string{}
	for _, t := range append(LegacyCatalog(), effortTools()...) {
		xs = append(xs, t.Name)
	}
	sort.Strings(xs)
	return xs
}
func (s *Service) findJob(ctx context.Context, a model.Agent, args Args, act *model.Activity) (any, error) {
	types := stringsArg(args, "types")
	if len(types) == 0 {
		types = []string{"person", "circle", "chat", "topic"}
	}
	candidates := []circles.Candidate{}
	messages := []model.Message{}
	entities := false
	for _, typ := range types {
		entities = entities || typ != "message"
	}
	if entities {
		// Narrow filters need only the capabilities of their selected sources.
		for _, typ := range types {
			if typ == "person" && !a.Permissions.ReadPeople {
				return nil, permissionError(a, "find_person")
			}
			if typ == "chat" && !a.Permissions.ReadMessages {
				return nil, permissionError(a, "list_chats")
			}
			if (typ == "circle" || typ == "topic") && (!a.Permissions.ReadMessages || !a.Permissions.ReadPeople) {
				return nil, permissionError(a, "resolve_context")
			}
		}
		if a.Permissions.ReadMessages && a.Permissions.ReadPeople {
			out, e := s.callLegacy(ctx, a, "resolve_context", Args{"query": args.str("query")}, act)
			if e != nil {
				return nil, e
			}
			for _, v := range out.(circles.Resolution).Candidates {
				if model.Contains(types, v.Kind) {
					candidates = append(candidates, v)
				}
			}
		} else if model.Contains(types, "chat") {
			out, e := s.callLegacy(ctx, a, "list_chats", Args{"query": args.str("query"), "limit": float64(50)}, act)
			if e != nil {
				return nil, e
			}
			for _, c := range out.([]model.Chat) {
				candidates = append(candidates, circles.Candidate{Kind: "chat", ID: c.JID, Title: c.Name, Score: 1, Why: "chat name"})
			}
		}
		// Exact person aliases and substring matches retain find_person behaviour.
		if model.Contains(types, "person") {
			out, e := s.callLegacy(ctx, a, "find_person", Args{"query": args.str("query"), "limit": float64(50)}, act)
			if e != nil {
				return nil, e
			}
			for _, p := range out.([]model.Person) {
				found := false
				for _, c := range candidates {
					found = found || c.Kind == "person" && c.ID == p.ID
				}
				if !found {
					candidates = append(candidates, circles.Candidate{Kind: "person", ID: p.ID, Title: p.Name, Score: 1, Why: "person name or alias"})
				}
			}
		}
	}
	if model.Contains(types, "message") {
		fields := Args{}
		for _, k := range []string{"query", "chat_jid", "limit", "offset", "before", "after"} {
			if v, ok := args[k]; ok {
				fields[k] = v
			}
		}
		out, e := s.callLegacy(ctx, a, "search_messages", fields, act)
		if e != nil {
			return nil, e
		}
		messages = out.([]model.Message)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	limit := min(args.num("limit", 20), 50)
	candidates = candidates[:min(limit, len(candidates))]
	clarify := len(candidates) == 0 || candidates[0].Score < .6 || len(candidates) > 1 && candidates[0].Score-candidates[1].Score <= .1000001
	act.Kind = "search"
	act.Query = args.str("query")
	act.Count = len(candidates)
	if len(messages) > 0 {
		act.Count = len(messages)
	}
	return map[string]any{"candidates": candidates, "messages": messages, "needs_clarification": clarify}, nil
}

func (s *Service) sendJob(ctx context.Context, a model.Agent, args Args, act *model.Activity) (any, error) {
	if !a.Permissions.RequestSend {
		return nil, permissionError(a, "send_message")
	}
	fields := Args{}
	for k, v := range args {
		fields[k] = v
	}
	draftOnly, _ := fields["draft_only"].(bool)
	delete(fields, "draft_only")
	if args.str("draft_id") != "" {
		if draftOnly {
			return nil, model.ErrInvalid
		}
		return s.callLegacy(ctx, a, "send_draft", fields, act)
	}
	target, kind := "send_message", "message"
	if react, ok := fields["react"].(map[string]any); ok {
		if hasSource(fields) || args.str("text") != "" || args.str("reply_to") != "" {
			return nil, model.ErrInvalid
		}
		delete(fields, "react")
		fields["text"] = react["emoji"]
		fields["message_id"] = react["message_id"]
		target, kind = "send_reaction", "reaction"
	} else if hasSource(fields) {
		target, kind = "send_file", "file"
	}
	// Validate before file IO, including fields specific to the chosen send kind.
	for _, t := range LegacyCatalog() {
		if t.Name == target && !validate(t, fields) {
			return nil, invalidArgs(t, fields)
		}
	}
	if !draftOnly {
		return s.callLegacy(ctx, a, target, fields, act)
	}
	// Draft-only also supports files and reactions while suppressing instructed/free delivery.
	if args.str("recipient") == "" {
		return nil, model.ErrInvalid
	}
	if key := args.str("key"); key != "" {
		if previous, err := s.Hub.SendStatus(a, "", "", key); err == nil {
			return previous, nil
		}
	}
	attachment := ""
	if kind == "file" {
		at, e := s.attachment(ctx, a, fields)
		if e != nil {
			return nil, e
		}
		attachment = at.ID
	}
	reply := fields.str("reply_to")
	if kind == "reaction" {
		reply = fields.str("message_id")
	}
	options := sendOptions(args)
	options.DraftOnly = true
	d, e := s.Hub.DraftSendKey(ctx, a, kind, args.str("recipient"), fields.str("text"), "", reply, attachment, args.str("key"), options)
	act.Kind = "send_requested"
	act.ChatJIDs = []string{d.ChatJID}
	act.Count = 1
	act.OwnerInstruction = d.OwnerInstruction
	act.QueueForApproval = d.QueueForApproval
	return d, e
}

// Permitted for a mixed tool means at least one action is available; execution
// always checks the selected legacy action again.
func jobPermitted(p model.Permissions, name string) bool {
	switch name {
	case "find":
		return p.Search || p.ReadMessages || p.ReadPeople
	case "read":
		return p.ReadMessages
	case "get":
		return p.ReadPeople || p.ReadMessages
	case "people":
		return p.ReadPeople
	case "brief":
		return p.ReadMessages && p.ReadPeople
	case "remember":
		return p.WritePeople || p.SaveSummaries
	case "circles":
		return p.ReadMessages && p.ReadPeople
	case "send":
		return p.RequestSend
	case "send_status":
		return true
	case "watch":
		return p.ReadMessages && p.ReadPeople
	case "more":
		return true
	}
	return false
}

func fieldTypes(t Tool) map[string]any {
	out := map[string]any{}
	for k, v := range t.InputSchema["properties"].(map[string]any) {
		schema := v.(map[string]any)
		typ := schema["type"]
		if typ == nil {
			typ = "object|string"
		}
		if choices, ok := schema["enum"]; ok {
			typ = choices
		}
		out[k] = typ
	}
	return out
}
