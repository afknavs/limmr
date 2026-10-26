package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"limmr/internal/circles"
	"limmr/internal/hub"
	"limmr/internal/model"
	"limmr/internal/triggers"
	"strings"
	"time"
)

func watchCatalog() []Tool {
	xs := []Tool{
		def("create_watch", "Create an explicitly authorised watch. Quote the user's exact instruction in grant_text; the client must ask Allow. No sends. For a standing feed use action=feed, scope={all:true}, match={kind:any}; one active feed per AI.", "goal grant_text scope:object match:object action", "title session_id expires_at"),
		def("update_watch", "Append one instruction, write digest text, pause/resume, or escalate your watch. Old instructions and original grant cannot be replaced.", "id", "instruction digest status escalate:boolean"),
		def("stop_watch", "Permanently stop your watch and cancel its active run.", "id", ""),
		def("start_handoff", "Start an explicitly authorised, single-chat session handoff. Quote the user's exact instruction in grant_text; client must ask Allow. Default expiry 3 days. Quiet hours and circle confirmation park replies.", "goal grant_text scope:object runner:object chat_jid", "title match:object expires_at limits:object escalate:array stop_words:array"),
		def("stop_handoff", "Permanently stop your handoff and revoke pending run sends.", "id", ""),
		def("list_watches", "List your watches and handoffs within current permissions.", "", ""),
	}
	object := func(props map[string]any, required ...string) map[string]any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	str := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	array := map[string]any{"type": "array", "items": str}
	scope := object(map[string]any{"all": map[string]any{"type": "boolean"}, "chats": array, "circles": array, "people": array})
	match := object(map[string]any{"kind": str, "keywords": array, "regex": str, "people": array}, "kind")
	runner := object(map[string]any{"kind": str, "session_id": str, "workdir": str}, "kind", "session_id", "workdir")
	quiet := object(map[string]any{"start": str, "end": str, "timezone": str}, "start", "end", "timezone")
	limits := object(map[string]any{"max_replies_per_hour": integer, "max_runs": integer, "quiet_hours": quiet})
	for i := range xs {
		p := xs[i].InputSchema["properties"].(map[string]any)
		for k, v := range map[string]any{"scope": scope, "match": match, "runner": runner, "limits": limits} {
			if _, ok := p[k]; ok {
				p[k] = v
			}
		}
	}
	return xs
}
func isWatchTool(name string) bool {
	for _, t := range watchCatalog() {
		if t.Name == name {
			return true
		}
	}
	return false
}
func decodeWatch[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil {
		return out, model.ErrInvalid
	}
	return out, nil
}
func (s *Service) watchScope(a model.Agent, scope triggers.Scope, match triggers.Match) error {
	_, visible, err := s.Hub.Visible(&a)
	if err != nil {
		return err
	}
	for _, chat := range scope.Chats {
		if !model.Contains(visible, chat) {
			return model.ErrNotFound
		}
	}
	for _, id := range scope.Circles {
		raw, err := s.CircleService().Circle(nil, id)
		if err != nil || !raw.Confirmed || !s.Hub.CircleAccess(&a, raw) {
			return model.ErrNotFound
		}
		for _, j := range circles.JIDs(raw) {
			if !model.Contains(visible, j) {
				return model.ErrNotFound
			}
		}
	}
	ps, err := s.People.People(&a, "")
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, p := range ps {
		ids[p.ID] = true
	}
	for _, list := range [][]string{scope.People, match.People} {
		for _, id := range list {
			if !ids[id] {
				return model.ErrNotFound
			}
		}
	}
	return nil
}
func (s *Service) ownWatch(ctx context.Context, a model.Agent, id string) (triggers.Record, error) {
	r, err := s.Watches.Record(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Owner != a.ID || r.CreatedVia.Agent != a.ID {
		return r, model.ErrNotFound
	}
	if err = s.watchScope(a, r.Scope, r.Match); err != nil {
		return r, err
	}
	hits, err := s.Watches.Store.Hits(ctx, id, 0)
	if err != nil {
		return r, err
	}
	for _, h := range hits {
		for _, m := range h.Messages {
			if !s.Hub.CanChat(&a, m.ChatJID) {
				return r, model.ErrNotFound
			}
		}
	}
	return r, nil
}
func (s *Service) callWatch(ctx context.Context, a model.Agent, name string, args Args, act *model.Activity) (out any, err error) {
	defer func() {
		if err != nil && !errors.Is(err, model.ErrNotFound) && !errors.Is(err, model.ErrInvalid) && !errors.Is(err, model.ErrConflict) && !errors.Is(err, model.ErrForbidden) {
			err = errors.New("Watch operation failed.")
		}
	}()
	if s.Watches == nil {
		return nil, model.ErrConflict
	}
	act.Kind = "watch." + strings.TrimSuffix(name, "_watch")
	if identity, ok := hub.HandoffIdentity(ctx); ok {
		if name == "create_watch" || name == "start_handoff" || name != "list_watches" && args.str("id") != identity.HandoffID {
			return nil, model.ErrForbidden
		}
	}
	if name == "list_watches" {
		xs, err := s.Watches.List(ctx, false)
		if err != nil {
			return nil, err
		}
		out := xs[:0]
		for _, x := range xs {
			if _, err := s.ownWatch(ctx, a, x.ID); err == nil {
				out = append(out, x)
			}
		}
		return map[string]any{"watches": out}, nil
	}
	if name == "create_watch" || name == "start_handoff" {
		if strings.TrimSpace(args.str("grant_text")) == "" {
			return nil, model.ErrInvalid
		}
		scope, err := decodeWatch[triggers.Scope](args["scope"])
		if err != nil {
			return nil, err
		}
		match := triggers.Match{Kind: triggers.Any}
		if v, ok := args["match"]; ok {
			match, err = decodeWatch[triggers.Match](v)
			if err != nil {
				return nil, err
			}
		}
		if err = s.watchScope(a, scope, match); err != nil {
			return nil, err
		}
		w := triggers.Watch{Title: args.str("title"), Owner: a.ID, Goal: args.str("goal"), Scope: scope, Match: match, Action: triggers.Action(args.str("action")), CreatedVia: triggers.Provenance{Agent: a.ID, SessionID: args.str("session_id"), GrantText: args.str("grant_text")}}
		if v := args.str("expires_at"); v != "" {
			at, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, model.ErrInvalid
			}
			w.ExpiresAt = &at
		}
		if w.Action == triggers.Feed {
			w.QuietWindow = time.Second
			w.MaxWindow = 5 * time.Second
		}
		var r triggers.Record
		if name == "start_handoff" {
			cfg, err := decodeWatch[triggers.RunnerConfig](args["runner"])
			if err != nil {
				return nil, err
			}
			if a.Transport != "local" || cfg.Kind != a.Kind || !a.Permissions.RequestSend {
				return nil, model.ErrForbidden
			}
			chat := args.str("chat_jid")
			if !s.Hub.CanChat(&a, chat) {
				return nil, model.ErrNotFound
			}
			_, visible, err := s.Hub.Visible(&a)
			if err != nil {
				return nil, err
			}
			if !model.Contains(visible, chat) {
				return nil, model.ErrNotFound
			}
			w.CreatedVia.SessionID = cfg.SessionID
			// The granted chat must itself be covered by the explicit watch scope.
			inScope := model.Contains(scope.Chats, chat)
			for _, id := range scope.Circles {
				c, _ := s.CircleService().Circle(&a, id)
				inScope = inScope || model.Contains(circles.JIDs(c), chat)
			}
			for _, id := range scope.People {
				var p model.Person
				if s.Hub.Repo.Get("people", id, &p) == nil {
					inScope = inScope || model.Contains(p.JIDs, chat)
				}
			}
			if !inScope {
				return nil, model.ErrInvalid
			}
			limits := triggers.Limits{}
			if raw, ok := args["limits"]; ok {
				limits, err = decodeWatch[triggers.Limits](raw)
				if err != nil {
					return nil, err
				}
			}
			r, err = s.Watches.Engine.StartHandoff(ctx, triggers.Handoff{Watch: w, HandoffConfig: triggers.HandoffConfig{Runner: cfg, SendGrant: triggers.SendGrant{ChatJID: chat}, Limits: limits, Escalate: stringsArg(args, "escalate"), StopWords: stringsArg(args, "stop_words")}})
		} else {
			r, err = s.Watches.Engine.CreateWatch(ctx, w)
		}
		if err != nil {
			return nil, model.ErrInvalid
		}
		act.ChatJIDs = scope.Chats
		return s.Watches.Detail(ctx, r.ID)
	}
	r, err := s.ownWatch(ctx, a, args.str("id"))
	if err != nil {
		return nil, err
	}
	if name == "stop_handoff" && r.Handoff == nil {
		return nil, model.ErrInvalid
	}
	act.ChatJIDs = r.Scope.Chats
	switch name {
	case "stop_watch", "stop_handoff":
		err = s.Watches.Status(ctx, r.ID, triggers.Stopped)
	case "update_watch":
		// Validate every field before applying any mutation.
		if text, ok := args["instruction"].(string); ok && (strings.TrimSpace(text) == "" || len([]rune(text)) > 500) {
			return nil, model.ErrInvalid
		}
		if digest, ok := args["digest"].(string); ok && len(digest) > 65536 {
			return nil, model.ErrInvalid
		}
		if escalate, _ := args["escalate"].(bool); escalate && r.Handoff == nil {
			return nil, model.ErrInvalid
		}
		status := args.str("status")
		if status != "" && status != "active" && status != "paused" {
			return nil, model.ErrInvalid
		}
		if len(args) == 1 {
			return nil, model.ErrInvalid
		}
		if text, ok := args["instruction"].(string); ok {
			err = s.Watches.Instruction(ctx, r.ID, text)
		}
		if err == nil {
			if digest, ok := args["digest"].(string); ok {
				err = s.Watches.Store.SetDigest(ctx, r.ID, digest)
			}
		}
		if err == nil && status != "" {
			err = s.Watches.Status(ctx, r.ID, triggers.Status(status))
		}
		if escalate, _ := args["escalate"].(bool); err == nil && escalate {
			if r.Handoff == nil {
				return nil, model.ErrInvalid
			}
			err = s.Watches.Engine.Escalate(ctx, r.ID)
			act.Kind = "watch.escalated"
		}
	}
	if err != nil {
		return nil, err
	}
	return s.Watches.Detail(ctx, r.ID)
}
