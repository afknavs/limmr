package mcpserver

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
)

type agentKey struct{}

func WithAgent(r *http.Request, a model.Agent) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), agentKey{}, a))
}
func Agent(r *http.Request) (model.Agent, bool) {
	a, ok := r.Context().Value(agentKey{}).(model.Agent)
	return a, ok
}

// Stateless JSON transport advertises a stable compact catalog. The receiving
// middleware dispatches tools/call before SDK lookup, allowing cached old names
// without registering (and thus listing) those compatibility aliases.
func (s *Service) Handler(remote bool) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		a, ok := Agent(r)
		if !ok {
			return nil
		}
		instructions := Instructions
		if _, ok := hub.HandoffIdentity(r.Context()); ok {
			instructions = "Call whoami first. Follow the explicit handoff grant and appended instructions. Replies are attributed to you and limited to the granted chat. Current permissions, circle rules, quiet hours and budgets still apply. Unattended replies without owner_instruction are parked. Quote the owner's request in owner_instruction for own AIs; queue_for_approval only for requested review. Escalate uncertainty with watch(action=update,escalate=true). Never create a watch or handoff inside a handoff session."
		}
		instructions += "\n" + AuthorGuidance
		server := mcp.NewServer(&mcp.Implementation{Name: "limmr", Version: "0.1.0"}, &mcp.ServerOptions{Instructions: instructions, PageSize: 100})
		execute := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, e := DecodeArgs(req.Params.Arguments)
			if e != nil {
				_ = s.Hub.WithContext(ctx).Record(model.Activity{AgentID: a.ID, Tool: canonicalName(req.Params.Name), CalledAs: calledAs(req.Params.Name), Kind: "denied"})
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "arguments: accepted: JSON object with fields from tools/list or more {}."}}}, nil
			}
			if identity, ok := hub.HandoffIdentity(r.Context()); ok {
				ctx = hub.WithHandoff(ctx, identity)
			}
			out, e := s.Call(hub.WithBackground(ctx, r.Header.Get("X-Limmr-Background") == "1" || hub.Background(r.Context(), a)), a.ID, req.Params.Name, args)
			if e != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: e.Error()}}}, nil
			}
			result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: dbutil.JSON(out)}}}
			if req.Params.Name != "scan_chats" {
				result.StructuredContent = map[string]any{"data": out}
			}
			return result, nil
		}
		for _, t := range s.Tools(a) {
			server.AddTool(&mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Meta: t.Meta, Annotations: t.Annotations}, execute)
		}
		server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
			return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
				if call, ok := req.(*mcp.CallToolRequest); ok && method == "tools/call" {
					return execute(ctx, call)
				}
				result, e := next(ctx, method, req)
				kind := "read"
				if e != nil {
					kind = "denied"
				}
				_ = s.Hub.WithContext(ctx).Record(model.Activity{AgentID: a.ID, Tool: method, Kind: kind})
				return result, e
			}
		})
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: remote, MaxRequestBodyBytes: 28 << 20})
}

// Keep compact client instructions independent of per-tool descriptions.
const Instructions = "Call whoami; get_updates. me=owner; memory first; confirm profile. Read relevant chats. Circles last; finite work=tasks; warned circle needs task_override_reason. Never create a task the owner did not ask for. Without his quote, create suggests; keep with owner_request after yes. Membership/undo/merge/convert_circle need owner_request. Task: goal; deadlines=end_at; done=met, dropped=failed. Follow-ups: follows=old id; owner-only reopen. Duplicates: update or different_because. Titles <=5 owner words. subject needs evidence; unsure=unset; corrections stick. Cover: own file/default; set_cover replaces. Needs you ends on answer/end/7 days. Find/clarify/brief/read/answer/update with evidence; skip empty. Never invent/delete. Respect access/circles. Send: quote owner_instruction; else show final_text, get consent. queue_for_approval only on request. Watches/handoffs only on request. notify_me stable key."
