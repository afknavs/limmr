package mcpserver

import (
	"encoding/json"
	"limmr/internal/efforts"
	"limmr/internal/model"
)

func (s *Service) EffortService() *efforts.Service {
	s.effortOnce.Do(func() { s.effortService = &efforts.Service{Hub: s.Hub} })
	return s.effortService
}
func effortTools() []Tool {
	xs := []Tool{
		def("efforts.unnamed", "Efforts waiting for names: ids, revisions, top terms, artefact names and counts. Requires access to every involved chat; never includes message text. Name with efforts.update, credited to you.", "", "limit:integer"),
		def("efforts.list", "Purpose-level work visible to you, including unnamed internal purposes. Name these with efforts.update before they appear in the app; search title, people, chats and snippets.", "", "status q circle limit:integer"),
		def("efforts.get", "Effort with status, title/summary, created_by naming credit, evidence refs and newest 50 messages/actions. Confidence scores evidence cohesion, not probability or message volume.", "id", ""),
		def("efforts.update", "Attributed title/summary/status edits win over heuristics. Due must match an explicit message date (RFC3339), or be empty to clear.", "id", "title summary status due"),
		def("efforts.merge", "Combine two visible efforts; keeps the first ID.", "id other_id", ""),
		def("efforts.split", "Move selected message_refs into a new effort.", "id message_refs:array", ""),
		def("efforts.attach", "Attach visible message_refs to an effort.", "id message_refs:array", ""),
	}
	for i := range xs {
		p := xs[i].InputSchema["properties"].(map[string]any)
		if _, ok := p["title"]; ok {
			p["title"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 40}
		}
		if _, ok := p["summary"]; ok {
			p["summary"] = map[string]any{"type": "string", "maxLength": 500}
		}
		if _, ok := p["status"]; ok {
			p["status"] = map[string]any{"type": "string", "enum": []string{"on_you", "waiting_on_them", "moving", "done", "dormant"}}
		}
		if _, ok := p["limit"]; ok {
			p["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 500}
		}
		if xs[i].Name == "efforts.attach" || xs[i].Name == "efforts.split" {
			xs[i].InputSchema["properties"].(map[string]any)["message_refs"] = map[string]any{"type": "array", "minItems": 1, "maxItems": 500, "items": map[string]any{"type": "object", "properties": map[string]any{"chat_jid": map[string]any{"type": "string"}, "message_id": map[string]any{"type": "string"}}, "required": []string{"chat_jid", "message_id"}, "additionalProperties": false}}
		}
	}
	return xs
}
func (s *Service) callEffort(a model.Agent, name string, args Args, act *model.Activity) (out any, err error) {
	if !a.Permissions.ReadMessages || !a.Permissions.ReadPeople || (args.str("q") != "" && !a.Permissions.Search) {
		return nil, model.ErrForbidden
	}
	found := false
	for _, t := range effortTools() {
		if t.Name == name {
			found = true
			defer func() {
				if err == model.ErrInvalid {
					err = invalidArgs(t, args)
				}
			}()
			if !validate(t, args) {
				return nil, invalidArgs(t, args)
			}
		}
	}
	if !found {
		return nil, model.ErrNotFound
	}
	if name != "efforts.list" && name != "efforts.get" && name != "efforts.unnamed" && !a.Permissions.SaveSummaries {
		return nil, model.ErrForbidden
	}
	svc := s.EffortService()
	switch name {
	case "efforts.unnamed":
		xs, total, e := svc.Unnamed(&a, args.num("limit", 100))
		out, err = map[string]any{"efforts": xs, "total": total}, e
		act.Count = len(xs)
	case "efforts.list":
		out, err = svc.List(&a, efforts.Filter{Status: args.str("status"), Query: args.str("q"), Circle: args.str("circle"), Limit: args.num("limit", 100), IncludeUntitled: true})
	case "efforts.get":
		out, err = svc.Get(&a, args.str("id"))
	case "efforts.update":
		u := efforts.Update{}
		b, _ := json.Marshal(args)
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(b, &raw)
		delete(raw, "id")
		b, _ = json.Marshal(raw)
		_ = json.Unmarshal(b, &u)
		out, err = svc.Update(&a, args.str("id"), u)
	default:
		var refs []efforts.Ref
		b, _ := json.Marshal(args["message_refs"])
		_ = json.Unmarshal(b, &refs)
		out, err = svc.Regroup(&a, name[len("efforts."):], args.str("id"), args.str("other_id"), refs)
	}
	act.Tool = name
	act.CalledAs = "more:" + name
	var v efforts.Effort
	switch x := out.(type) {
	case efforts.Effort:
		v = x
		act.Kind = "summary"
		act.Note = "Updated effort: " + x.Title
	case []efforts.Effort:
		act.Count = len(x)
		for _, item := range x {
			for _, chat := range item.Chats {
				if !model.Contains(act.ChatJIDs, chat.ID) {
					act.ChatJIDs = append(act.ChatJIDs, chat.ID)
				}
			}
		}
	case efforts.Detail:
		v = x.Effort
	}
	for _, r := range v.MessageRefs {
		if !model.Contains(act.ChatJIDs, r.ChatJID) {
			act.ChatJIDs = append(act.ChatJIDs, r.ChatJID)
		}
		act.MessageIDs = append(act.MessageIDs, r.MessageID)
	}
	if v.ID != "" {
		act.Count = len(v.MessageRefs)
	}
	return out, err
}

func (s *Service) effortNamingHint(a model.Agent) (string, bool) {
	return s.EffortService().ReadyNamingHint(a)
}
