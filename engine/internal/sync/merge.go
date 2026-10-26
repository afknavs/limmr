package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"limmr/internal/dbutil"
	"sort"
	"strings"
	"time"
)

type Alias struct {
	FromID string    `json:"from_id"`
	ToID   string    `json:"to_id"`
	At     time.Time `json:"at"`
}

func (s *Service) alias(tx *sql.Tx, c Change) error {
	var a Alias
	if json.Unmarshal(c.Data, &a) != nil || a.FromID != c.ID || a.ToID == "" || a.ToID == a.FromID {
		return ErrInvalid
	}
	target, err := canonical(tx, a.ToID)
	if err != nil || target == a.FromID {
		return ErrInvalid
	}
	a.ToID = target
	// Re-key references by exact JSON string values, never by substring replacement.
	rows, err := tx.Query(`SELECT bucket,id,data FROM objects WHERE bucket IN ('people','facts','relations','memberships','circles') ORDER BY bucket,id`)
	if err != nil {
		return err
	}
	type row struct{ bucket, id, raw string }
	all := []row{}
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.bucket, &r.id, &r.raw); err != nil {
			break
		}
		all = append(all, r)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	var source, targetObj map[string]any
	for _, r := range all {
		if r.bucket == "people" {
			if r.id == a.FromID {
				_ = json.Unmarshal([]byte(r.raw), &source)
			}
			if r.id == target {
				_ = json.Unmarshal([]byte(r.raw), &targetObj)
			}
		}
	}
	for _, r := range all {
		if r.bucket == "people" && r.id == a.FromID {
			continue
		}
		var v any
		_ = json.Unmarshal([]byte(r.raw), &v)
		next := rewrite(v, a.FromID, target)
		raw, _ := json.Marshal(next)
		if string(raw) != r.raw {
			if _, err = tx.Exec(`UPDATE objects SET data=? WHERE bucket=? AND id=?`, string(raw), r.bucket, r.id); err != nil {
				return err
			}
		}
	}
	if source != nil {
		if targetObj == nil {
			targetObj = source
		} else {
			fill(targetObj, source)
		}
		targetObj["id"] = target
		if _, err = tx.Exec(`INSERT INTO objects VALUES('people',?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data`, target, dbutil.JSON(targetObj)); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM objects WHERE bucket='people' AND id=?`, a.FromID); err != nil {
			return err
		}
	}
	rows, err = tx.Query(`SELECT id,data FROM tasks`)
	if err != nil {
		return err
	}
	all = nil
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.raw); err != nil {
			break
		}
		all = append(all, r)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range all {
		var v any
		_ = json.Unmarshal([]byte(r.raw), &v)
		raw := dbutil.JSON(rewrite(v, a.FromID, target))
		if raw != r.raw {
			if _, err = tx.Exec(`UPDATE tasks SET data=? WHERE id=?`, raw, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}
func rewrite(v any, from, to string) any {
	switch x := v.(type) {
	case string:
		if x == from {
			return to
		}
	case []any:
		for i := range x {
			x[i] = rewrite(x[i], from, to)
		}
	case map[string]any:
		for k := range x {
			x[k] = rewrite(x[k], from, to)
		}
	}
	return v
}
func fill(dst, src map[string]any) {
	for k, v := range src {
		if dst[k] == nil || dst[k] == "" {
			dst[k] = v
		} else if a, ok := dst[k].(map[string]any); ok {
			if b, ok := v.(map[string]any); ok {
				fill(a, b)
			}
		} else if a, ok := dst[k].([]any); ok {
			if b, ok := v.([]any); ok {
				for _, x := range b {
					found := false
					for _, y := range a {
						found = found || dbutil.JSON(x) == dbutil.JSON(y)
					}
					if !found {
						a = append(a, x)
					}
				}
				dst[k] = a
			}
		}
	}
}

// Each author's immutable source version gets a stable key. Both peers rebuild
// presentation numbering in the same (requested version,HLC,origin) order.
func (s *Service) brief(tx *sql.Tx, c Change) (bool, error) {
	var v struct {
		Circle  string `json:"circle_id"`
		Version int    `json:"version"`
	}
	if json.Unmarshal(c.Data, &v) != nil || v.Circle == "" || v.Version < 1 {
		return false, ErrInvalid
	}
	key := fmt.Sprintf("%s:%d", c.Origin, c.HLC)
	res, err := tx.Exec(`INSERT OR IGNORE INTO sync_briefs VALUES(?,?,?,?,?)`, v.Circle, key, string(c.Data), c.HLC, c.Origin)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	rows, err := tx.Query(`SELECT data,hlc,origin FROM sync_briefs WHERE circle=? ORDER BY CAST(json_extract(data,'$.version') AS INTEGER),hlc,origin`, v.Circle)
	if err != nil {
		return false, err
	}
	type brief struct {
		raw string
		h   int64
		o   string
	}
	all := []brief{}
	for rows.Next() {
		var b brief
		if err = rows.Scan(&b.raw, &b.h, &b.o); err != nil {
			break
		}
		all = append(all, b)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(`UPDATE sync_state SET suppress=1`); err != nil {
		return false, err
	}
	// Production briefs use zero-padded keys. Move existing presentation rows
	// aside before renumbering: their unique (circle,version) index otherwise
	// collides with a second row for version 1. Immutable sources stay intact.
	existing, err := tx.Query(`SELECT id FROM objects WHERE bucket='briefs' AND json_extract(data,'$.circle_id')=? ORDER BY json_extract(data,'$.version')`, v.Circle)
	if err != nil {
		return false, err
	}
	var oldIDs []string
	for existing.Next() {
		var id string
		if err = existing.Scan(&id); err != nil {
			break
		}
		oldIDs = append(oldIDs, id)
	}
	if err == nil {
		err = existing.Err()
	}
	existing.Close()
	if err != nil {
		return false, err
	}
	if len(oldIDs) > len(all) {
		return false, ErrInvalid
	}
	temporary := dbutil.ID()
	for i, id := range oldIDs {
		if _, err = tx.Exec(`UPDATE objects SET id=?,data=json_set(data,'$.version',?) WHERE bucket='briefs' AND id=?`, temporary+fmt.Sprint(i), -i-1, id); err != nil {
			return false, err
		}
	}
	version := 0
	for index, b := range all {
		var obj map[string]any
		_ = json.Unmarshal([]byte(b.raw), &obj)
		requested := int(obj["version"].(float64))
		version = max(version+1, requested)
		obj["version"] = version
		id := fmt.Sprintf("%s:%09d", v.Circle, version)
		if index < len(oldIDs) {
			_, err = tx.Exec(`UPDATE objects SET id=?,data=? WHERE bucket='briefs' AND id=?`, id, dbutil.JSON(obj), temporary+fmt.Sprint(index))
		} else {
			_, err = tx.Exec(`INSERT INTO objects VALUES('briefs',?,?)`, id, dbutil.JSON(obj))
		}
		if err != nil {
			return false, err
		}
	}
	_, err = tx.Exec(`UPDATE sync_state SET suppress=0; INSERT INTO sync_log(bucket,id,hlc,origin,op,data) VALUES(?,?,?,?,'upsert',?)`, c.Bucket, c.ID, c.HLC, c.Origin, string(c.Data))
	return true, err
}

type Review struct {
	ID     string   `json:"id"`
	Bucket string   `json:"bucket"`
	IDs    []string `json:"ids"`
	Name   string   `json:"name"`
	State  string   `json:"state"`
}

func (s *Service) review(tx *sql.Tx, c Change, out *PushResponse) error {
	var incoming map[string]any
	_ = json.Unmarshal(c.Data, &incoming)
	field := "title"
	if c.Bucket == "circles" {
		field = "name"
	}
	name, _ := incoming[field].(string)
	if strings.TrimSpace(name) == "" {
		return nil
	}
	query := `SELECT id,data FROM tasks WHERE id<>?`
	args := []any{c.ID}
	if c.Bucket == "circles" {
		query = `SELECT id,data FROM objects WHERE bucket='circles' AND id<>?`
	}
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	reviews := []Review{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		var obj map[string]any
		_ = json.Unmarshal([]byte(raw), &obj)
		other, _ := obj[field].(string)
		if incoming["kind"] != obj["kind"] || !similar(name, other) || !overlaps(incoming, obj) {
			continue
		}
		ids := []string{id, c.ID}
		sort.Strings(ids)
		r := Review{ID: dbutil.Digest(c.Bucket + ":" + strings.Join(ids, ":")), Bucket: c.Bucket, IDs: ids, Name: min(name, other), State: "pending"}
		reviews = append(reviews, r)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range reviews {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO sync_reviews VALUES(?,?)`, r.ID, dbutil.JSON(r)); err != nil {
			return err
		}
		out.Conflicts = append(out.Conflicts, r.ID)
	}
	return nil
}
func similar(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	words := map[string]bool{}
	for _, w := range strings.Fields(a) {
		words[w] = true
	}
	common := 0
	other := map[string]bool{}
	for _, w := range strings.Fields(b) {
		other[w] = true
	}
	for w := range other {
		if words[w] {
			common++
		}
	}
	union := len(words) + len(other) - common
	return union > 0 && float64(common)/float64(union) >= 0.6
}
func overlaps(a, b map[string]any) bool {
	for _, key := range []string{"chats", "people", "chat_jids", "person_ids"} {
		xs, _ := a[key].([]any)
		ys, _ := b[key].([]any)
		for _, x := range xs {
			for _, y := range ys {
				xm, ok := x.(map[string]any)
				ym, ok2 := y.(map[string]any)
				if ok && ok2 {
					for _, field := range []string{"id", "chat_jid", "person_id"} {
						xid, _ := xm[field].(string)
						yid, _ := ym[field].(string)
						if xid != "" && xid == yid {
							return true
						}
					}
				}
				if !ok && !ok2 && dbutil.JSON(x) == dbutil.JSON(y) {
					return true
				}
			}
		}
	}
	return false
}
func (s *Service) Reviews() ([]Review, error) {
	rows, err := s.Repo.DB.Query(`SELECT data FROM sync_reviews ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var raw string
		var r Review
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
