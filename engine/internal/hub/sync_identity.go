package hub

import (
	"database/sql"
	"encoding/json"
	"limmr/internal/model"
)

type rowReader interface{ QueryRow(string, ...any) *sql.Row }

func personAlias(db rowReader, id string) (string, error) {
	seen := map[string]bool{}
	for n := 0; n < 100; n++ {
		if seen[id] {
			return "", model.ErrInvalid
		}
		seen[id] = true
		var raw string
		err := db.QueryRow(`SELECT data FROM objects WHERE bucket='person_aliases' AND id=?`, id).Scan(&raw)
		if err == sql.ErrNoRows {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		var a struct {
			To string `json:"to_id"`
		}
		if json.Unmarshal([]byte(raw), &a) != nil || a.To == "" {
			return "", model.ErrInvalid
		}
		id = a.To
	}
	return "", model.ErrInvalid
}
func aliasValue(db rowReader, bucket, id string, b []byte) (string, []byte, error) {
	if bucket != "people" {
		return id, b, nil
	}
	target, err := personAlias(db, id)
	if err != nil {
		return "", nil, err
	}
	if target != id {
		var obj map[string]any
		if err = json.Unmarshal(b, &obj); err != nil {
			return "", nil, err
		}
		obj["id"] = target
		b, err = json.Marshal(obj)
	}
	return target, b, err
}
