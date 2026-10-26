package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"limmr/internal/efforts"
	"limmr/internal/model"
)

func TestA6NamingQueueClientAndOwnerReadback(t *testing.T) {
	for _, local := range []bool{true, false} {
		t.Run(fmt.Sprintf("on_device=%t", local), func(t *testing.T) {
			r := newRig(t)
			_, err := r.m.DB.Exec("DELETE FROM messages")
			must(t, err)
			for i, text := range []string{"budget is for proposal", "budget for the proposal", "budget and a proposal"} {
				must(t, r.m.PutMessage(model.Message{ID: fmt.Sprintf("a6-budget-%d", i), ChatJID: chatA,
					Sender: chatA, Content: text, IsFromMe: i == 1, Timestamp: epoch.Add(time.Duration(i-3) * time.Hour)}))
			}
			ctx, session, _ := sdkSession(t, r)
			queue := auditCall(t, ctx, session, "more", map[string]any{"action": "efforts.unnamed", "args": map[string]any{}}, false).(map[string]any)
			if queue["total"] != float64(1) {
				t.Fatal("client naming queue incomplete")
			}
			wire, _ := json.Marshal(queue)
			if strings.Contains(string(wire), "snippets") || strings.Contains(string(wire), "participant_first_names") {
				t.Fatal("owner naming inputs leaked to MCP")
			}
			w := r.request(t, "GET", "/v1/efforts/unnamed", r.api.ControlToken, nil, nil)
			status(t, w, 200)
			var owner struct {
				Efforts []efforts.NamingCandidate `json:"efforts"`
				Total   int                       `json:"total"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &owner))
			if owner.Total != 1 || len(owner.Efforts[0].Snippets) == 0 {
				t.Fatal("owner evidence missing")
			}
			candidate := owner.Efforts[0]
			if local {
				status(t, r.request(t, "PATCH", "/v1/efforts/"+candidate.ID, r.api.ControlToken,
					map[string]any{"title": "Festival budget", "summary": "Review the festival proposal.",
						"only_unnamed": true, "expected_revision": candidate.Revision}, nil), 200)
			} else {
				auditCall(t, ctx, session, "more", map[string]any{"action": "efforts.update", "args": map[string]any{"id": candidate.ID, "title": "Festival budget", "summary": "Review the festival proposal."}}, false)
			}
			w = r.request(t, "GET", "/v1/efforts?q=Festival", r.api.ControlToken, nil, nil)
			status(t, w, 200)
			var list struct {
				Efforts []efforts.Effort `json:"efforts"`
			}
			must(t, json.Unmarshal(w.Body.Bytes(), &list))
			credit := r.a.Name
			if local {
				credit = "This Mac"
			}
			if len(list.Efforts) != 1 || list.Efforts[0].CreatedBy != credit || len(list.Efforts[0].MessageRefs) != 3 {
				t.Fatal("owner list lost naming credit or evidence")
			}
			remaining := auditCall(t, ctx, session, "more", map[string]any{"action": "efforts.unnamed", "args": map[string]any{}}, false).(map[string]any)
			if remaining["total"] != float64(0) {
				t.Fatal("named effort stayed queued")
			}
		})
	}
}

func TestA6MCPUpdatePauseIsPromptRetryableToolError(t *testing.T) {
	r := newRig(t)
	ctx, session, _ := sdkSession(t, r)
	status(t, r.request(t, "POST", "/v1/updates/prepare", r.api.ControlToken, nil, nil), 200)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "send", Arguments: map[string]any{"recipient": chatA, "text": "Synthetic draft"}})
	must(t, err)
	if !result.IsError || len(r.fake.Sent) != 0 {
		t.Fatal("paused send did not fail closed")
	}
	encoded, _ := json.Marshal(result.Content)
	if !strings.Contains(string(encoded), model.ErrUpdating.Error()) {
		t.Fatal("MCP omitted update retry guidance")
	}
	auditCall(t, ctx, session, "whoami", map[string]any{}, false)
	status(t, r.request(t, "POST", "/v1/updates/resume", r.api.ControlToken, nil, nil), 200)
}
