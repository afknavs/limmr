package mcpserver

import (
	"errors"
	"strings"
	"testing"

	"limmr/internal/model"
)

func TestInvalidArgsNamesTheProblemWithoutValues(t *testing.T) {
	tool := def("read_fixture", "fixture", "mode", "chat_jid limit:integer")
	e := invalidArgs(tool, Args{"chat": "secret-value", "limit": "two"})
	if !errors.Is(e, model.ErrInvalid) {
		t.Fatal("must stay ErrInvalid")
	}
	msg := e.Error()
	for _, want := range []string{`unknown argument "chat"`, `missing "mode"`, `bad value for "limit"`, "accepted: chat_jid, limit, mode"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %q", want, msg)
		}
	}
	if strings.Contains(msg, "secret-value") || strings.Contains(msg, "two") {
		t.Fatal("argument values leaked into the error")
	}
}

func TestA2NestedAndSemanticArgumentDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, raw, field, accepted string }{
		{"efforts.attach", `{"id":"fixture","message_refs":[]}`, "message_refs", "minItems=1"},
		{"efforts.attach", `{"id":"fixture","message_refs":[{"chat_jid":"fixture","message_id":123}]}`, "message_refs[0].message_id", "string"},
		{"efforts.update", `{"id":"fixture","status":"A2_PRIVATE_VALUE"}`, "status", "waiting_on_them"},
		{"efforts.update", `{"id":"fixture","due":"A2_PRIVATE_VALUE"}`, "due", "RFC3339"},
		{"create_watch", `{"goal":"fixture","grant_text":"fixture","action":"notify","scope":{"chats":["fixture"]},"match":{"kind":"any","regex":123}}`, "match.regex", "string"},
	} {
		t.Run(tc.name+tc.field, func(t *testing.T) {
			args, err := DecodeArgs([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			for _, tool := range append(LegacyCatalog(), effortTools()...) {
				if tool.Name != tc.name {
					continue
				}
				if validate(tool, args) {
					t.Fatal("invalid input accepted")
				}
				message := invalidArgs(tool, args).Error()
				if !strings.Contains(message, tc.field) || !strings.Contains(message, tc.accepted) || strings.Contains(message, "A2_PRIVATE_VALUE") {
					t.Fatal("missing or unsafe field guidance", message)
				}
			}
		})
	}
}
