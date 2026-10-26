package hub

import (
	"encoding/json"
	"limmr/internal/model"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Icons are owner presentation metadata, kept independently so inference/older clients
// cannot overwrite them when replacing an entity. No people-lane code is involved.
type EntityIcon struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var symbolName = regexp.MustCompile(`^[a-z0-9][a-z0-9.]{0,99}$`)

func (v EntityIcon) Valid() bool {
	if v.Kind == "symbol" {
		return symbolName.MatchString(v.Value)
	}
	if v.Kind != "emoji" || strings.TrimSpace(v.Value) == "" || utf8.RuneCountInString(v.Value) > 16 {
		return false
	}
	for _, r := range v.Value {
		if unicode.IsControl(r) || r < 128 {
			return false
		}
	}
	return true
}
func (s *Service) IconEntity(bucket, id string) (map[string]any, error) {
	var v map[string]any
	e := s.Repo.Get(bucket, id, &v)
	return v, e
}
func (s *Service) SetEntityIcon(bucket, id string, icon *EntityIcon) error {
	if _, e := s.IconEntity(bucket, id); e != nil {
		return e
	}
	if icon == nil {
		return s.Repo.Delete("entity_icons", bucket+"/"+id)
	}
	if !icon.Valid() {
		return model.ErrInvalid
	}
	return s.Repo.Put("entity_icons", bucket+"/"+id, icon)
}

// Decorate all nested control representations, including Home cards, map nodes,
// person profiles and integration summaries, without changing their existing shapes.
func (s *Service) WithEntityIcons(value any) (any, error) {
	rows, e := s.Repo.DB.Query(`SELECT id,data FROM objects WHERE bucket='entity_icons'`)
	if e != nil {
		return nil, e
	}
	icons := map[string]json.RawMessage{}
	for rows.Next() {
		var id, data string
		if e = rows.Scan(&id, &data); e != nil {
			rows.Close()
			return nil, e
		}
		parts := strings.SplitN(id, "/", 2)
		if len(parts) == 2 {
			icons[parts[1]] = json.RawMessage(data)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(icons) == 0 {
		return value, nil
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	var v any
	if e = json.Unmarshal(raw, &v); e != nil {
		return nil, e
	}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case map[string]any:
			id, ok := x["id"].(string)
			if !ok {
				id, ok = x["jid"].(string)
			}
			if ok {
				if icon, ok := icons[id]; ok {
					x["icon"] = icon
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
	return v, nil
}
