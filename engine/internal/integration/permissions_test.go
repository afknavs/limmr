package integration_test

import (
	"errors"
	"limmr/internal/hub"
	"limmr/internal/mcpserver"
	"limmr/internal/model"
	"strings"
	"testing"
)

func TestPermissions(t *testing.T) {
	for _, mode := range []string{"only", "all_except", "all"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			p := r.a.Permissions
			p.Chats = model.Scope{Mode: mode, JIDs: []string{chatA, groupA}}
			if mode == "all_except" {
				p.Chats.JIDs = []string{chatB, groupB}
			}
			r.agent(t, p)
			if mode == "all" {
				settings := r.h.Settings()
				settings.HiddenChats = []string{chatB, groupB}
				must(t, r.h.SetSettings(settings))
			}
			for _, test := range []struct {
				name string
				args any
			}{{"list_chats", map[string]any{}}, {"search_messages", map[string]any{"query": "Saturday"}}, {"find_person", map[string]any{"query": ""}}, {"list_groups", map[string]any{}}, {"people_near", map[string]any{"focus": "me", "depth": 4}}} {
				t.Run(test.name, func(t *testing.T) {
					out, e := r.call(t, test.name, test.args)
					must(t, e)
					raw := encode(out)
					for _, canary := range []string{chatB, groupB, "Alex Chen", "Garden Club"} {
						if strings.Contains(raw, canary) {
							t.Fatalf("excluded data in %s", test.name)
						}
					}
				})
			}
			for _, test := range []struct {
				name string
				args any
			}{{"get_chat", map[string]any{"chat_jid": chatB}}, {"list_messages", map[string]any{"chat_jid": chatB}}, {"get_message_context", map[string]any{"chat_jid": chatB, "message_id": firstMessage(t, r, chatB).ID}}, {"get_person", map[string]any{"person_id": "alex"}}, {"get_group", map[string]any{"group_jid": groupB}}, {"people_near", map[string]any{"focus": "alex"}}} {
				t.Run(test.name+"_excluded", func(t *testing.T) {
					_, e := r.call(t, test.name, test.args)
					if e != model.ErrNotFound {
						t.Fatalf("want invisible not_found, got %v", e)
					}
				})
			}
			must(t, r.h.Record(model.Activity{AgentID: r.a.ID, Tool: "list_messages", Kind: "read", ChatJIDs: []string{chatB}, MessageIDs: []string{"excluded-id"}, Count: 1}))
			xs, e := r.h.Activity(&r.a, hub.ActivityFilter{})
			must(t, e)
			if strings.Contains(encode(xs), "excluded-id") {
				t.Fatal("activity leaked excluded chat")
			}
			for _, route := range []string{"/v1/chats", "/v1/people", "/v1/groups", "/v1/graph?depth=4", "/v1/graph/export", "/v1/activity"} {
				t.Run(route, func(t *testing.T) {
					w := r.request(t, "GET", route, r.token, nil, nil)
					status(t, w, 200)
					if strings.Contains(w.Body.String(), chatB) || strings.Contains(w.Body.String(), "Alex Chen") {
						t.Fatal("API projection leaked")
					}
				})
			}
		})
	}
}
func TestCapabilityToolFiltering(t *testing.T) {
	r := newRig(t)
	p := model.Defaults(true)
	r.agent(t, p)
	tools := r.mc.Tools(r.a)
	if len(tools) != len(mcpserver.Catalog()) {
		t.Fatal("stable catalog missing tools")
	}
	for _, name := range []string{"send_message", "send_file", "send_reaction", "add_fact", "save_summary", "download_media"} {
		t.Run(name, func(t *testing.T) {
			_, e := r.call(t, name, map[string]any{})
			if e == nil || !strings.Contains(e.Error(), "is off for Example AI") {
				t.Fatalf("got %v", e)
			}
		})
	}
	p.ReadMessages = false
	p.ReadPeople = false
	p.Search = false
	r.agent(t, p)
	tools = r.mc.Tools(r.a)
	if len(tools) != len(mcpserver.Catalog()) {
		t.Fatal("catalog changed after revocation")
	}
	for _, tool := range mcpserver.Catalog() {
		if tool.Name != "get_updates" && tool.Name != "whoami" && tool.Name != "get_started" && tool.Name != "my_activity" && tool.Name != "check_connection" && tool.Name != "check_send" && tool.Name != "get_send_status" && tool.Name != "send_status" && tool.Name != "more" && mcpserver.Permitted(p, tool.Name) {
			t.Fatal("capability bypass")
		}
	}
}
func TestCredentials(t *testing.T) {
	r := newRig(t)
	a, e := r.h.Authenticate(r.token)
	must(t, e)
	if a.ID != r.a.ID {
		t.Fatal("wrong identity")
	}
	var persisted string
	must(t, r.h.Repo.DB.QueryRow("SELECT digest FROM credentials WHERE agent_id=?", r.a.ID).Scan(&persisted))
	if persisted == r.token || len(persisted) != 64 {
		t.Fatal("token not hashed")
	}
	newToken, e := r.h.Rotate(a.ID)
	must(t, e)
	if _, e = r.h.Authenticate(r.token); e != model.ErrForbidden {
		t.Fatal("old credential remained valid")
	}
	_, e = r.h.Authenticate(newToken)
	must(t, e)
	must(t, r.h.Revoke(a.ID))
	if _, e = r.h.Authenticate(newToken); e != model.ErrForbidden {
		t.Fatal("revoked credential valid")
	}
}
func TestDisabledAgent(t *testing.T) {
	r := newRig(t)
	r.a.Enabled = false
	must(t, r.h.UpdateAgent(r.a))
	if _, e := r.h.Authenticate(r.token); e != model.ErrForbidden {
		t.Fatal("disabled credential")
	}
	if _, e := r.call(t, "whoami", map[string]any{}); e != model.ErrForbidden {
		t.Fatal("disabled call")
	}
}
func TestScopeValidation(t *testing.T) {
	for _, mode := range []string{"", "bogus", "ONLY"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			r.a.Permissions.Chats.Mode = mode
			if e := r.h.UpdateAgent(r.a); !errors.Is(e, model.ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
}
