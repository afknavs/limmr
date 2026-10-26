package mcpserver

// Keep profile details typed in the MCP schema; legacy fields remain strings.
func profileSchema(t *Tool) {
	str := map[string]any{"type": "string"}
	props := map[string]any{}
	for _, k := range []string{"label", "circle", "role", "how_known", "notes"} {
		props[k] = str
	}
	detail := map[string]any{}
	for _, k := range []string{"birthday", "location", "pronouns", "org", "title"} {
		detail[k] = str
	}
	labeled := map[string]any{"type": "object", "properties": map[string]any{"label": str, "value": str, "source": str}, "required": []string{"label", "value"}, "additionalProperties": false}
	for _, k := range []string{"phones", "emails", "handles"} {
		detail[k] = map[string]any{"type": "array", "items": labeled, "maxItems": 50}
	}
	props["details"] = map[string]any{"type": "object", "properties": detail, "additionalProperties": false}
	t.InputSchema["properties"].(map[string]any)["fields"] = map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}

func ownerProfileSchema(t *Tool) {
	str := map[string]any{"type": "string"}
	props := map[string]any{}
	for _, k := range []string{"name", "about", "studies", "work"} {
		props[k] = map[string]any{"type": "string", "maxLength": 300}
	}
	props["name"] = map[string]any{"type": "string", "maxLength": 100}
	props["about"] = map[string]any{"type": "string", "maxLength": 600}
	props["people"] = map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object", "properties": map[string]any{"name": str, "relation": str, "person_id": map[string]any{"anyOf": []any{str, map[string]any{"type": "null"}}}}, "required": []string{"name", "relation"}, "additionalProperties": false}}
	props["projects"] = map[string]any{"type": "array", "maxItems": 30, "items": map[string]any{"type": "object", "properties": map[string]any{"name": str, "with": map[string]any{"type": "array", "maxItems": 50, "items": str}}, "required": []string{"name", "with"}, "additionalProperties": false}}
	t.InputSchema["properties"].(map[string]any)["fields"] = map[string]any{"type": "object", "properties": props, "additionalProperties": false}
}
