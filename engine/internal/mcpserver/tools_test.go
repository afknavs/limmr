package mcpserver

import (
	"encoding/json"
	"limmr/internal/model"
	"os"
	"strings"
	"testing"
)

func TestPublishedSchemasMatchCatalog(t *testing.T) {
	b, e := os.ReadFile("../../../docs/API.md")
	if e != nil {
		t.Fatal(e)
	}
	doc := string(b)
	if len(Catalog()) != 20 {
		t.Fatal("tool catalog size changed")
	}
	for _, tool := range Catalog() {
		t.Run(tool.Name, func(t *testing.T) {
			marker := "### `" + tool.Name + "`"
			_, section, ok := strings.Cut(doc, marker)
			if !ok {
				t.Fatal("undocumented tool")
			}
			_, section, ok = strings.Cut(section, "```json\n")
			if !ok {
				t.Fatal("missing schema")
			}
			schema, _, ok := strings.Cut(section, "\n```")
			if !ok {
				t.Fatal("unterminated schema")
			}
			var got any
			if json.Unmarshal([]byte(schema), &got) != nil {
				t.Fatal("invalid documented schema")
			}
			want, _ := json.Marshal(tool.InputSchema)
			actual, _ := json.Marshal(got)
			if string(want) != string(actual) {
				t.Fatal("documented schema drift")
			}
		})
	}
}
func TestStableCatalogAcrossCapabilities(t *testing.T) {
	s := &Service{}
	for _, p := range []model.Permissions{model.Defaults(true), model.Preset("own"), {}} {
		if len(s.Tools(model.Agent{Permissions: p})) != len(Catalog()) {
			t.Fatal("catalog depends on permissions")
		}
	}
	if len(s.Tools(model.Agent{ID: "this-mac"})) != 0 {
		t.Fatal("admin-only AI exposed")
	}
}
func TestArgumentsRejectTypeConfusion(t *testing.T) {
	tool := def("test", "fixture", "n:integer xs:array fields:object", "")
	for _, raw := range []string{`{}`, `{"n":1.5,"xs":[],"fields":{}}`, `{"n":-1,"xs":[],"fields":{}}`, `{"n":1,"xs":[2],"fields":{}}`, `{"n":1,"xs":[],"fields":{"label":false}}`, `{"n":1,"xs":[],"fields":{},"extra":true}`} {
		args, e := DecodeArgs(json.RawMessage(raw))
		if e != nil {
			t.Fatal(e)
		}
		if validate(tool, args) {
			t.Fatal("invalid arguments accepted")
		}
	}
}

func TestCatalogBudgetAndAnnotations(t *testing.T) {
	catalog := Catalog()
	b, err := json.Marshal(map[string]any{"tools": catalog})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) > 20 || len(b) >= 21000 {
		t.Fatalf("catalog exceeds budget: %d tools, %d bytes", len(catalog), len(b))
	}
	legacy, _ := json.Marshal(map[string]any{"tools": LegacyCatalog()})
	t.Logf("catalog: %d -> %d tools, %d -> %d bytes (rough tokens /4: %d -> %d)", len(LegacyCatalog()), len(catalog), len(legacy), len(b), (len(legacy)+3)/4, (len(b)+3)/4)
	read := map[string]bool{"whoami": true, "get_updates": true, "get_digest": true, "find": true, "read": true, "scan_chats": true, "get": true, "people": true, "send_status": true}
	for _, tool := range catalog {
		hints := tool.Annotations
		if hints == nil || hints.DestructiveHint == nil || hints.OpenWorldHint == nil {
			t.Fatal("missing annotations", tool.Name)
		}
		if hints.ReadOnlyHint != read[tool.Name] || *hints.DestructiveHint == read[tool.Name] {
			t.Fatal("wrong action annotation", tool.Name)
		}
		if tool.Name == "more" || tool.Name == "watch" || tool.Name == "send" {
			if !*hints.OpenWorldHint {
				t.Fatal("open-world dispatcher marked closed", tool.Name)
			}
		}
	}
	for _, tool := range LegacyCatalog() {
		if canonicalName(tool.Name) == tool.Name {
			continue
		}
		for _, listed := range catalog {
			if listed.Name == tool.Name {
				t.Fatal("legacy alias listed", tool.Name)
			}
		}
	}
}
