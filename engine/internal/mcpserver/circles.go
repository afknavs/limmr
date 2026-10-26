package mcpserver

import (
	"context"
	"encoding/json"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
)

const protocol = " Resolve context, ask the user when ambiguous, get_brief, catch_up, answer, update_brief whenever new messages arrived, then optionally suggest."

func boolPtr(v bool) *bool { return &v }
func isWrite(n string) bool {
	return model.Contains([]string{"update_owner_profile", "add_to_circle", "add_fact", "update_person", "add_relation", "save_summary", "notify_me", "upload_attachment", "download_media", "send_message", "send_file", "send_reaction", "draft_message", "send_draft", "update_brief", "suggest", "propose_circle", "set_person_role", "add_topic", "update_topic"}, n)
}
func circleCatalog() []Tool {
	xs := []Tool{
		def("resolve_context", "Rank visible topics, circles, people and chats using query, recency and your cursors only. If needs_clarification is true, ask the user which candidate.", "query", ""),
		def("list_circles", "List confirmed circles visible to you.", "", ""),
		def("get_circle", "Get a circle's visible chats, people with per-circle roles and topics.", "id", ""),
		def("get_brief", "Read the cheap stored understanding and your catch-up cursor before reading messages.", "id", ""),
		def("catch_up", "Read new messages across visible chats with sender roles. Continue next_page_token until empty; only then cursor advances. Update brief after answering.", "id", "since limit:integer page_token"),
		def("update_brief", "Write the next version (summary at most 120 words), visible evidence and coverage no later than your caught-up cursor. First brief requires evidence_message_ids. Skip if catch_up returned no messages.", "id summary what_changed", "open_asks:array upcoming:array people_notes:array evidence_message_ids:array covers_until"),
		def("suggest", "Park a reply/action/reminder on Home for the owner; this does not send.", "circle_id kind why", "chat_jid draft_text voice variations:array"),
		def("propose_circle", "Propose a few life-now circles (usually 3–6), never one per group/Community. Up to 50 chats and 50 people; at least one chat must be current. At most 8 pending per AI. Returns proposal id; the owner keeps it in Limmr. Then call get_started.", "name kind chats:array people:array why", "goal"),
		def("add_to_circle", "Add current chats or named people to an existing circle, citing their chat names or recent message IDs in why. Own AIs apply directly with Undo; given AIs go to review. Existing owner edits are preserved.", "circle_id chats:array people:array why", ""),
		def("set_person_role", "Set a person's role in one fully visible circle, attributed to you.", "circle_id person_id role", ""),
		def("add_topic", "Add a topic spanning visible chats and people in a fully visible circle.", "circle_id title", "status chats:array people:array keywords:array"),
		def("update_topic", "Update a topic in a fully visible circle.", "circle_id topic_id", "title status chats:array people:array keywords:array"),
		def("draft_message", "Draft exact text. Own AIs: owner_instruction sends now, even in background. Otherwise show final_text and confirm in conversation; send_now sends free drafts. queue_for_approval requests review in Limmr.", "chat text", "voice signed:boolean send_now:boolean variations:array"),
		def("send_draft", "Send your draft after consent in this conversation; quote it in owner_instruction for immediate delivery by own AIs. Otherwise confirm waits 2 seconds. Rechecks access and circle rules; queued drafts stay on Home.", "draft_id", "voice signed:boolean"),
	}
	for i := range xs {
		if xs[i].Name == "propose_circle" {
			p := xs[i].InputSchema["properties"].(map[string]any)
			p["chats"].(map[string]any)["maxItems"] = 50
			p["people"].(map[string]any)["maxItems"] = 50
		}
		xs[i].Description += protocol
		if xs[i].Name == "suggest" || xs[i].Name == "draft_message" {
			p := xs[i].InputSchema["properties"].(map[string]any)
			p["variations"] = map[string]any{"type": "array", "maxItems": 3, "items": map[string]any{"type": "string", "maxLength": 1000, "minLength": 1}}
		}
		if xs[i].Name == "update_brief" {
			p := xs[i].InputSchema["properties"].(map[string]any)
			p["open_asks"] = map[string]any{"type": "array", "items": map[string]any{"anyOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}, "chat_jid": map[string]any{"type": "string"}, "from": map[string]any{"type": "string"}, "at": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false},
			}}}
			p["upcoming"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"date": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"}}, "required": []string{"date", "text"}, "additionalProperties": false}}
			p["people_notes"] = map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"person_id": map[string]any{"type": "string"}, "contribution": map[string]any{"type": "string"}}, "required": []string{"person_id", "contribution"}, "additionalProperties": false}}
		}
	}
	return xs
}
func isCircleTool(n string) bool {
	for _, t := range circleCatalog() {
		if t.Name == n {
			return true
		}
	}
	return false
}
func (s *Service) CircleService() *circles.Service {
	s.circleOnce.Do(func() { s.circleService = &circles.Service{Hub: s.Hub, People: s.People} })
	return s.circleService
}
func (s *Service) callCircle(ctx context.Context, a model.Agent, name string, args Args, act *model.Activity) (any, error) {
	c := s.CircleService()
	id := args.str("id")
	switch name {
	case "resolve_context":
		act.Query = args.str("query")
		return c.Resolve(a, args.str("query"))
	case "list_circles":
		xs, e := c.List(&a)
		for _, v := range xs {
			act.ChatJIDs = append(act.ChatJIDs, circles.JIDs(v)...)
		}
		act.Count = len(xs)
		return map[string]any{"circles": xs}, e
	case "get_circle":
		v, e := c.Circle(&a, id)
		if e == nil {
			act.ChatJIDs = circles.JIDs(v)
			act.Count = 1
		}
		return v, e
	case "get_brief":
		v, e := c.Context(&a, id)
		if e != nil {
			return nil, e
		}
		act.ChatJIDs = circles.JIDs(v)
		out, err := c.GetBrief(a, id)
		if err == nil {
			hint, ready := s.effortNamingHint(a)
			out.(map[string]any)["efforts_ready"] = ready
			if hint != "" {
				out.(map[string]any)["effort_naming_hint"] = hint
			}
		}
		return out, err
	case "catch_up":
		v, e := c.CatchUp(a, id, args.str("since"), args.str("page_token"), args.num("limit", 100))
		for i := range v.Chats {
			for j := range v.Chats[i].Messages {
				m, err := s.Hub.Attribute(v.Chats[i].Messages[j].Message)
				if err != nil {
					return nil, err
				}
				v.Chats[i].Messages[j].Message = m
			}
		}
		for _, g := range v.Chats {
			act.ChatJIDs = append(act.ChatJIDs, g.ChatJID)
			for _, m := range g.Messages {
				act.MessageIDs = append(act.MessageIDs, m.Message.ID)
			}
		}
		act.Count = v.NewMessages
		return v, e
	case "update_brief":
		b := model.Brief{Summary: args.str("summary"), WhatChanged: args.str("what_changed"), Evidence: stringsArg(args, "evidence_message_ids")}
		var e error
		if args["open_asks"] != nil {
			b.OpenAsks, e = circles.Decode[[]model.OpenAsk](args["open_asks"])
			if e != nil {
				return nil, e
			}
		}
		b.Upcoming, e = circles.Decode[[]model.Upcoming](args["upcoming"])
		if e != nil {
			return nil, e
		}
		b.PeopleNotes, e = circles.Decode[[]model.PeopleNote](args["people_notes"])
		if e != nil {
			return nil, e
		}
		if args.str("covers_until") != "" {
			if e = json.Unmarshal([]byte(`"`+args.str("covers_until")+`"`), &b.CoversUntil); e != nil {
				return nil, model.ErrInvalid
			}
		}
		b, e = c.WriteBrief(&a, id, b)
		act.Kind = "summary"
		act.ChatJIDs = b.ChatJIDs
		act.MessageIDs = b.Evidence
		act.Note = b.Summary + "\n" + b.WhatChanged
		act.Count = 1
		return b, e
	case "suggest":
		x, e := c.Suggest(a, model.Suggestion{Variations: stringsArg(args, "variations"), CircleID: args.str("circle_id"), Kind: args.str("kind"), ChatJID: args.str("chat_jid"), DraftText: args.str("draft_text"), Why: args.str("why")})
		act.Kind = "people_write"
		if x.ChatJID != "" {
			act.ChatJIDs = []string{x.ChatJID}
		}
		act.Count = 1
		return x, e
	case "propose_circle":
		v := model.Circle{Name: args.str("name"), Kind: args.str("kind"), Goal: args.str("goal")}
		for _, j := range stringsArg(args, "chats") {
			v.Chats = append(v.Chats, model.CircleChat{ChatJID: j})
		}
		for _, p := range stringsArg(args, "people") {
			v.People = append(v.People, model.CirclePerson{PersonID: p, Source: a.ID})
		}
		if id, err := c.ProjectTask(a, v, args.str("why")); err != nil {
			return nil, err
		} else if id != "" {
			act.Kind = "summary"
			act.ChatJIDs = circles.JIDs(v)
			act.Count = 1
			return map[string]string{"converted_to_task": id}, nil
		}
		v, e := c.Create(&a, v, args.str("why"))
		act.Kind = "people_write"
		act.ChatJIDs = circles.JIDs(v)
		act.Count = 1
		if e != nil {
			return nil, e
		}
		if v.Confirmed {
			return map[string]any{"id": v.ID, "circle": v, "confirmed_by": "auto"}, nil
		}
		var proposal model.Proposal
		e = s.Hub.Repo.Get("proposals", v.ID, &proposal)
		return proposal, e
	case "add_to_circle":
		act.Kind = "people_write"
		return c.AddTo(a, args.str("circle_id"), stringsArg(args, "chats"), stringsArg(args, "people"), args.str("why"))
	case "set_person_role", "add_topic", "update_topic":
		// Reject writes through partial projections: unscoped metadata must never be
		// overwritten with an agent's incomplete view.
		circleID := args.str("circle_id")
		raw, e := c.Circle(nil, circleID)
		if e != nil {
			return nil, e
		}
		visible, e := c.Circle(&a, circleID)
		if e != nil {
			return nil, e
		}
		if len(raw.Chats) != len(visible.Chats) || len(raw.People) != len(visible.People) || len(raw.Topics) != len(visible.Topics) {
			return nil, model.ErrNotFound
		}
		act.Kind = "people_write"
		act.ChatJIDs = circles.JIDs(visible)
		return c.MutateAs(&a, circleID, func(v *model.Circle) error {
			if name == "set_person_role" {
				for i, p := range v.People {
					if p.PersonID == args.str("person_id") {
						v.People[i].Role = args.str("role")
						v.People[i].Source = a.ID
						return nil
					}
				}
				return model.ErrNotFound
			}
			raw := map[string]json.RawMessage{}
			for _, k := range []string{"title", "status", "chats", "people", "keywords"} {
				if val, ok := args[k]; ok {
					b, _ := json.Marshal(val)
					raw[k] = b
				}
			}
			method := "POST"
			if name == "update_topic" {
				method = "PATCH"
			}
			return c.EditMember(v, "topics", method, args.str("topic_id"), raw)
		})
	case "draft_message":
		phase(ctx, "bridge")
		sendNow, _ := args["send_now"].(bool)
		d, e := s.Hub.DraftMessageVariations(ctx, a, args.str("chat"), args.str("text"), args.str("voice"), sendNow, stringsArg(args, "variations"), sendOptions(args))
		act.Kind = "send_requested"
		act.OwnerInstruction = d.OwnerInstruction
		act.QueueForApproval = d.QueueForApproval
		act.ChatJIDs = []string{d.ChatJID}
		act.Count = 1
		return d, e
	case "send_draft":
		phase(ctx, "bridge")
		d, e := s.Hub.SendDraft(ctx, a, args.str("draft_id"), sendOptions(args))
		act.Kind = "send_requested"
		act.OwnerInstruction = d.OwnerInstruction
		act.QueueForApproval = d.QueueForApproval
		act.ChatJIDs = []string{d.ChatJID}
		act.Count = 1
		return d, e
	}
	return nil, model.ErrNotFound
}
func stringsArg(args Args, key string) []string {
	out := []string{}
	xs, _ := args[key].([]any)
	for _, x := range xs {
		if v, ok := x.(string); ok {
			out = append(out, v)
		}
	}
	return out
}

func sendOptions(args Args) hub.SendOptions {
	queue, _ := args["queue_for_approval"].(bool)
	asDocument, _ := args["as_document"].(bool)
	draftOnly, _ := args["draft_only"].(bool)
	return hub.SendOptions{TaskID: args.str("task_id"), DraftOnly: draftOnly, OwnerInstruction: args.str("owner_instruction"), QueueForApproval: queue, AsDocument: asDocument}
}
