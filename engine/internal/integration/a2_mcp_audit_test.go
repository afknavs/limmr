package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"limmr/internal/efforts"
	"limmr/internal/mcpserver"
)

func auditCall(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args any, reject bool) any {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	must(t, err)
	if result.IsError != reject {
		t.Fatalf("%s: rejected=%t, want %t; %v", name, result.IsError, reject, result.Content)
	}
	if reject {
		text := ""
		for _, c := range result.Content {
			if c, ok := c.(*mcp.TextContent); ok {
				text += c.Text
			}
		}
		if !strings.Contains(text, "accepted:") || strings.Contains(text, "A2_PRIVATE_VALUE") {
			t.Fatalf("unhelpful or unsafe argument error: %s", text)
		}
		return text
	}
	if data, ok := result.StructuredContent.(map[string]any); ok {
		return data["data"]
	}
	var data any
	must(t, json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data))
	return data
}

// A real SDK client serializes HTTP over net.Pipe: no network bind, installed
// daemon or credentials. All sends use Fake; aliases are exercised as well.
func TestA2MCPClientAudit(t *testing.T) {
	r, _ := t1Fixture(t)
	ctx, session, _ := sdkSession(t, r)
	tools, err := session.ListTools(ctx, nil)
	must(t, err)
	if len(tools.Tools) != 20 {
		t.Fatal("catalog changed")
	}
	data, err := json.Marshal(tools)
	must(t, err)
	t.Logf("client tools/list: tools=%d bytes=%d estimated_tokens_ceil_bytes_div_4=%d", len(tools.Tools), len(data), (len(data)+3)/4)
	index := auditCall(t, ctx, session, "more", map[string]any{}, false).(map[string]any)
	schemas := []mcpserver.Tool{}
	for _, raw := range index["actions"].([]any) {
		x := raw.(map[string]any)
		if strings.HasPrefix(x["action"].(string), "efforts.") && x["tool"] != "more" {
			t.Fatal("efforts discovery points at an unlisted tool")
		}
		schemas = append(schemas, mcpserver.Tool{Name: x["action"].(string), InputSchema: x["schema"].(map[string]any)})
	}
	if len(schemas) != len(mcpserver.LegacyCatalog())+7 {
		t.Fatal("rare index incomplete")
	}
	for i, tool := range append(mcpserver.Catalog(), schemas...) {
		listed := i < len(tools.Tools)
		prefix := "more/"
		if listed {
			prefix = "listed/"
		}
		t.Run(prefix+tool.Name, func(t *testing.T) {
			r, inputs := t1Fixture(t)
			ctx, session, _ := sdkSession(t, r)
			input := auditInput(t, r, inputs, tool.Name)
			// Normalize fixture values to the same types the wire decodes.
			var args map[string]any
			must(t, json.Unmarshal([]byte(encode(input)), &args))
			call := func(a map[string]any, reject bool) any {
				if listed {
					return auditCall(t, ctx, session, tool.Name, a, reject)
				}
				return auditCall(t, ctx, session, "more", map[string]any{"action": tool.Name, "args": a}, reject)
			}
			clone := func() map[string]any {
				out := map[string]any{}
				for k, v := range args {
					out[k] = v
				}
				return out
			}
			// Every supplied field, including optional fields, is rejected with a wrong type.
			props := tool.InputSchema["properties"].(map[string]any)
			for k, raw := range props {
				wrong := clone()
				schema := raw.(map[string]any)
				wrong[k] = "A2_PRIVATE_VALUE"
				if schema["type"] == "string" || schema["anyOf"] != nil {
					wrong[k] = []any{"A2_PRIVATE_VALUE"}
				}
				msg := call(wrong, true).(string)
				if !strings.Contains(msg, k) {
					t.Fatalf("error does not name field %s", k)
				}
			}
			// Required lists are []string in Go catalogs and []any in the discovery response.
			required := []string{}
			switch xs := tool.InputSchema["required"].(type) {
			case []string:
				required = xs
			case []any:
				for _, x := range xs {
					required = append(required, x.(string))
				}
			}
			for _, k := range required {
				missing := clone()
				delete(missing, k)
				msg := call(missing, true).(string)
				if !strings.Contains(msg, k) {
					t.Fatal("missing field unnamed")
				}
			}
			unknown := clone()
			unknown["a2_unknown_argument"] = "A2_PRIVATE_VALUE"
			call(unknown, true)
			call(args, false)
			if !listed && !strings.HasPrefix(tool.Name, "efforts.") {
				// Existing cached names retain their schemas at the HTTP dispatch boundary.
				// A fresh rig avoids repeat-write conflicts after the successful more call.
				r2, in2 := t1Fixture(t)
				c2, s2, _ := sdkSession(t, r2)
				auditCall(t, c2, s2, tool.Name, in2[tool.Name], false)
			}
		})
	}
	ctx, session, _ = sdkSession(t, r)
	auditCall(t, ctx, session, "whoami", []string{"A2_PRIVATE_VALUE"}, true)
	for _, args := range []map[string]any{{"args": map[string]any{}}, {"action": "A2_PRIVATE_VALUE"}, {"action": "efforts.A2_PRIVATE_VALUE"}} {
		auditCall(t, ctx, session, "more", args, true)
	}
}

func auditInput(t *testing.T, r *rig, old map[string]map[string]any, name string) map[string]any {
	t.Helper()
	if strings.HasPrefix(name, "efforts.") {
		svc := r.mc.EffortService()
		xs, err := svc.List(&r.a, efforts.Filter{IncludeUntitled: true})
		must(t, err)
		if len(xs) == 0 || len(xs[0].MessageRefs) < 2 {
			t.Fatal("missing effort fixture")
		}
		id := xs[0].ID
		switch name {
		case "efforts.list", "efforts.unnamed":
			return map[string]any{}
		case "efforts.get":
			return map[string]any{"id": id}
		case "efforts.update":
			return map[string]any{"id": id, "title": "Weekend sketches", "status": "moving"}
		case "efforts.attach", "efforts.split":
			return map[string]any{"id": id, "message_refs": xs[0].MessageRefs[:1]}
		case "efforts.merge":
			x, err := svc.Regroup(&r.a, "split", id, "", xs[0].MessageRefs[:1])
			must(t, err)
			return map[string]any{"id": id, "other_id": x.ID}
		}
	}
	if args, ok := old[name]; ok {
		return args
	}
	switch name {
	case "find":
		return map[string]any{"query": "Maya"}
	case "read":
		return map[string]any{"mode": "chat", "chat_jid": chatA}
	case "get":
		return map[string]any{"kind": "person", "id": "maya"}
	case "people":
		return map[string]any{"view": "near", "focus": "maya"}
	case "brief":
		return map[string]any{"action": "get", "id": old["get_brief"]["id"]}
	case "remember":
		return map[string]any{"kind": "person_update", "person_id": "maya", "fields": map[string]any{"notes": "Morning reviews"}}
	case "circles":
		return map[string]any{"action": "list"}
	case "send":
		return map[string]any{"recipient": chatA, "text": "Synthetic draft", "draft_only": true}
	case "send_status":
		return old["get_send_status"]
	case "task":
		return map[string]any{"action": "list"}
	case "watch":
		return map[string]any{"action": "list"}
	case "more":
		return map[string]any{}
	}
	t.Fatalf("missing valid input for %s", name)
	return nil
}

func TestA2ConcurrentMCPClients(t *testing.T) {
	r, _ := t1Fixture(t)
	ctx, session, _ := sdkSession(t, r)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name, args := "find", map[string]any{"query": "Maya"}
			if i%2 == 0 {
				name, args = "more", map[string]any{"action": "efforts.list", "args": map[string]any{}}
			}
			out, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				errs <- err
			} else if out.IsError {
				errs <- fmt.Errorf("concurrent %s rejected", name)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
