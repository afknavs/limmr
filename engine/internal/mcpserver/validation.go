package mcpserver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"limmr/internal/model"
)

// Describe only server-owned schemas, never supplied values (even parse errors
// can contain message text, dates, URLs or credentials).
func schemaValues(s map[string]any) string {
	if xs, ok := s["enum"].([]string); ok {
		return strings.Join(xs, " | ")
	}
	if xs, ok := s["anyOf"].([]any); ok {
		out := []string{}
		for _, x := range xs {
			out = append(out, schemaValues(x.(map[string]any)))
		}
		return strings.Join(out, " or ")
	}
	out, _ := s["type"].(string)
	if s["format"] == "date-time" {
		out += " (RFC3339 with timezone, or empty to use default/clear)"
	}
	for _, bound := range []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"} {
		if v, ok := s[bound]; ok {
			out += fmt.Sprintf(" %s=%v", bound, v)
		}
	}
	if out == "array" || s["type"] == "array" {
		out += " of " + schemaValues(s["items"].(map[string]any))
	}
	if s["type"] == "object" {
		if props, ok := s["properties"].(map[string]any); ok {
			keys := []string{}
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			out += " fields: " + strings.Join(keys, ", ")
		}
	}
	return out
}

func fieldError(field, accepted string) error {
	return fmt.Errorf("bad value for %s; accepted: %s: %w", strconv.Quote(field), accepted, model.ErrInvalid)
}

func valueProblems(path string, schema map[string]any, v any) []string {
	// Descend only through known schema keys. Unknown keys are field names;
	// object values and array contents are never formatted into diagnostics.
	if xs, ok := v.(map[string]any); ok && schema["type"] == "object" {
		props, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]string)
		out := []string{}
		for _, k := range required {
			if _, ok := xs[k]; !ok {
				out = append(out, "missing "+strconv.Quote(path+"."+k)+"; accepted: "+schemaValues(props[k].(map[string]any)))
			}
		}
		for k, x := range xs {
			if p, ok := props[k].(map[string]any); ok {
				if !validValue(p, x) {
					out = append(out, valueProblems(path+"."+k, p, x)...)
				}
			} else if schema["additionalProperties"] != true {
				if p, ok := schema["additionalProperties"].(map[string]any); ok {
					if !validValue(p, x) {
						out = append(out, valueProblems(path+"."+k, p, x)...)
					}
				} else {
					out = append(out, "unknown argument "+strconv.Quote(path+"."+k)+"; accepted: "+schemaValues(schema))
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if xs, ok := v.([]any); ok && schema["type"] == "array" {
		item := schema["items"].(map[string]any)
		for i, x := range xs {
			if !validValue(item, x) {
				return valueProblems(fmt.Sprintf("%s[%d]", path, i), item, x)
			}
		}
	}
	return []string{"bad value for " + strconv.Quote(path) + "; accepted: " + schemaValues(schema)}
}
