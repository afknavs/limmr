package hub

import "encoding/json"

func decode(s string, v any) error { return json.Unmarshal([]byte(s), v) }
