// Package sync implements the knowledge replication protocol. Authentication and
// listener registration belong to the caller (Y7); Handler must not be public unauthenticated.
package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"limmr/internal/hub"
	"limmr/internal/model"
	"sort"
	"time"
)

const Version = 1
const Schema = 10
const MaxChanges = 500
const MaxBytes = 4 << 20

var ErrUpdateNeeded = errors.New("update_needed")
var ErrInvalid = errors.New("invalid_sync_request")

type Change struct {
	Seq    int64           `json:"seq"`
	Bucket string          `json:"bucket"`
	ID     string          `json:"id"`
	HLC    int64           `json:"hlc"`
	Origin string          `json:"origin"`
	Op     string          `json:"op"`
	Data   json.RawMessage `json:"data"`
	Items  []Item          `json:"items,omitempty"`
}
type Item struct {
	List    string          `json:"list"`
	ID      string          `json:"id"`
	HLC     int64           `json:"hlc"`
	Origin  string          `json:"origin"`
	Removed bool            `json:"removed"`
	Data    json.RawMessage `json:"data"`
}
type PullRequest struct {
	SinceSeq     int64  `json:"since_seq"`
	PeerEngineID string `json:"peer_engine_id"`
	SyncVersion  int    `json:"sync_version"`
	Schema       int    `json:"schema"`
}
type PullResponse struct {
	Changes  []Change `json:"changes"`
	NextSeq  int64    `json:"next_seq"`
	More     bool     `json:"more"`
	Lease    Lease    `json:"lease"`
	EngineID string   `json:"engine_id"`
}
type PushRequest struct {
	Blob         *BlobChunk `json:"blob,omitempty"`
	Changes      []Change   `json:"changes"`
	FromEngineID string     `json:"from_engine_id"`
	SyncVersion  int        `json:"sync_version"`
	Schema       int        `json:"schema"`
}
type PushResponse struct {
	Applied   int      `json:"applied"`
	Stale     int      `json:"stale"`
	Conflicts []string `json:"conflicts"`
	AckSeq    int64    `json:"ack_seq"`
}
type Peer struct {
	EngineID      string    `json:"peer_engine_id"`
	LastPulledSeq int64     `json:"last_pulled_seq"`
	LastPushedSeq int64     `json:"last_pushed_seq"`
	LastSyncAt    time.Time `json:"last_sync_at"`
	LastError     string    `json:"last_error"`
	Applied       int       `json:"applied"`
	Stale         int       `json:"stale"`
	Sent          int       `json:"sent"`
	Paused        bool      `json:"paused"`
	AgentID       string    `json:"agent_id,omitempty"`
}
type Service struct {
	Repo     *hub.Repo
	Home     string
	EngineID string
	Now      func() time.Time
}

func New(r *hub.Repo, home string, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	s := &Service{Repo: r, Home: home, Now: now}
	err := r.DB.QueryRow(`SELECT engine_id FROM sync_state WHERE id=1`).Scan(&s.EngineID)
	return s, err
}
func (s *Service) acceptPeer(id string) bool {
	var p Peer
	err := s.Repo.Get("settings", "sync_peer", &p)
	return (err == nil && (p.EngineID == "" || p.EngineID == id)) || errors.Is(err, model.ErrNotFound)
}
func versions(v, schema int) error {
	if v != Version || schema > Schema {
		return ErrUpdateNeeded
	}
	if schema < 9 {
		return ErrInvalid
	}
	return nil
}
func validBucket(c Change) bool {
	switch c.Bucket {
	case "people", "person_aliases", "circles", "briefs", "facts", "relations", "entity_icons", "task_dismissals", "tasks", "task_events":
		return true
	case "memberships":
		var v map[string]any
		_ = json.Unmarshal(c.Data, &v)
		_, group := v["group"]
		return !group
	case "settings":
		return c.ID == "owner_profile" || c.ID == "automation_lease"
	}
	return false
}
func (s *Service) Pull(ctx context.Context, q PullRequest) (PullResponse, error) {
	out := PullResponse{EngineID: s.EngineID, NextSeq: q.SinceSeq, Changes: []Change{}}
	if err := versions(q.SyncVersion, q.Schema); err != nil {
		return out, err
	}
	if q.SinceSeq < 0 || q.PeerEngineID == "" || !s.acceptPeer(q.PeerEngineID) {
		return out, ErrInvalid
	}
	tx, err := s.Repo.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT seq,bucket,id,hlc,origin,op,data FROM sync_log WHERE seq>? ORDER BY seq LIMIT 501`, q.SinceSeq)
	if err != nil {
		return out, err
	}
	all := []Change{}
	for rows.Next() {
		var c Change
		var raw string
		if err = rows.Scan(&c.Seq, &c.Bucket, &c.ID, &c.HLC, &c.Origin, &c.Op, &raw); err != nil {
			break
		}
		c.Data = json.RawMessage(raw)
		all = append(all, c)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return out, err
	}
	size := 1024
	for _, c := range all {
		if c.Bucket == "tasks" {
			c.Items, err = readItems(tx, c.ID)
			if err != nil {
				return out, err
			}
		}
		b, _ := json.Marshal(c)
		if len(out.Changes) == MaxChanges || size+len(b)+1 > MaxBytes {
			out.More = true
			break
		}
		out.Changes = append(out.Changes, c)
		out.NextSeq = c.Seq
		size += len(b) + 1
	}
	if len(all) > 0 && len(out.Changes) == 0 {
		return out, fmt.Errorf("change exceeds page limit")
	}
	var raw string
	if tx.QueryRow(`SELECT data FROM objects WHERE bucket='settings' AND id='automation_lease'`).Scan(&raw) == nil {
		_ = json.Unmarshal([]byte(raw), &out.Lease)
	}
	return out, tx.Commit()
}
func readItems(tx *sql.Tx, id string) ([]Item, error) {
	rows, err := tx.Query(`SELECT list,id,hlc,origin,removed,data FROM sync_items WHERE task_id=? ORDER BY list,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var i Item
		var raw string
		if err = rows.Scan(&i.List, &i.ID, &i.HLC, &i.Origin, &i.Removed, &raw); err != nil {
			return nil, err
		}
		i.Data = json.RawMessage(raw)
		out = append(out, i)
	}
	return out, rows.Err()
}
func newer(h int64, o string, oldH int64, oldO string) bool { return h > oldH || h == oldH && o > oldO }
func (s *Service) Push(ctx context.Context, q PushRequest) (PushResponse, error) {
	out := PushResponse{Conflicts: []string{}}
	if err := versions(q.SyncVersion, q.Schema); err != nil {
		return out, err
	}
	b, err := json.Marshal(q)
	if err != nil || len(b) > MaxBytes || len(q.Changes) > MaxChanges || q.FromEngineID == "" || !s.acceptPeer(q.FromEngineID) {
		return out, ErrInvalid
	}
	if q.Blob != nil {
		if len(q.Changes) != 0 {
			return out, ErrInvalid
		}
		return out, s.receiveBlob(q.Blob)
	}
	// Validate the entire batch before opening a write transaction.
	for _, c := range q.Changes {
		if !validBucket(c) {
			return out, ErrUpdateNeeded
		}
		if c.ID == "" || c.HLC <= 0 || c.HLC > 1<<62 || c.Origin == "" || (c.Op != "upsert" && c.Op != "tombstone") || !json.Valid(c.Data) {
			return out, ErrInvalid
		}
		var obj map[string]any
		if json.Unmarshal(c.Data, &obj) != nil || obj == nil {
			return out, ErrInvalid
		}
		for _, i := range c.Items {
			if c.Bucket != "tasks" || !contains([]string{"checklist", "items", "updates_sent"}, i.List) || i.ID == "" || i.HLC <= 0 || i.HLC > 1<<62 || i.Origin == "" || !json.Valid(i.Data) {
				return out, ErrInvalid
			}
		}
	}
	tx, err := s.Repo.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// Aliases must be installed before older person edits in the same page.
	changes := append([]Change(nil), q.Changes...)
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].Bucket == "person_aliases" && changes[j].Bucket != "person_aliases"
	})
	touched := map[string]bool{}
	for _, c := range changes {
		if c.Bucket == "tasks" {
			touched[c.ID] = true
		}
		if c.Bucket == "task_events" {
			var e struct {
				TaskID string `json:"task_id"`
			}
			if json.Unmarshal(c.Data, &e) == nil {
				touched[e.TaskID] = true
			}
		}
		if c.Seq > out.AckSeq {
			out.AckSeq = c.Seq
		}
		if _, err = tx.Exec(`UPDATE sync_state SET clock=max(clock,?),remote_hlc=?,remote_origin=? WHERE id=1`, c.HLC, c.HLC, c.Origin); err != nil {
			return out, err
		}
		applied, err := s.apply(tx, c, &out)
		if err != nil {
			return PushResponse{}, err
		}
		var privacy struct {
			Forgotten bool `json:"forgotten"`
		}
		_ = json.Unmarshal(c.Data, &privacy)
		if applied && privacy.Forgotten {
			if err = hub.ScrubForgotten(tx, c.Bucket, c.ID); err != nil {
				return PushResponse{}, err
			}
		}
		if applied {
			out.Applied++
		} else {
			out.Stale++
		}
	}
	if _, err = tx.Exec(`UPDATE sync_state SET suppress=1 WHERE id=1`); err != nil {
		return out, err
	}
	if err = reconcileSlots(tx, touched); err != nil {
		return out, err
	}
	if err = recomputeTasks(tx, touched); err != nil {
		return out, err
	}
	if _, err = tx.Exec(`UPDATE sync_state SET remote_hlc=NULL,remote_origin=NULL,suppress=0 WHERE id=1`); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func canonical(tx *sql.Tx, id string) (string, error) {
	seen := map[string]bool{}
	for n := 0; n < 100; n++ {
		if seen[id] {
			return "", ErrInvalid
		}
		seen[id] = true
		var raw string
		err := tx.QueryRow(`SELECT data FROM objects WHERE bucket='person_aliases' AND id=?`, id).Scan(&raw)
		if err == sql.ErrNoRows {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		var a Alias
		if json.Unmarshal([]byte(raw), &a) != nil || a.ToID == "" {
			return "", ErrInvalid
		}
		id = a.ToID
	}
	return "", ErrInvalid
}
func (s *Service) apply(tx *sql.Tx, c Change, out *PushResponse) (bool, error) {
	if c.Bucket == "people" {
		id, err := canonical(tx, c.ID)
		if err != nil {
			return false, err
		}
		if id != c.ID {
			if c.Op == "tombstone" {
				return false, nil
			}
			c.ID = id
			var v map[string]any
			_ = json.Unmarshal(c.Data, &v)
			v["id"] = id
			c.Data, _ = json.Marshal(v)
		}
	}
	if c.Bucket == "briefs" && c.Op == "upsert" {
		return s.brief(tx, c)
	}
	var oldH int64
	var oldO, oldOp string
	err := tx.QueryRow(`SELECT hlc,origin,op FROM sync_meta WHERE bucket=? AND id=?`, c.Bucket, c.ID).Scan(&oldH, &oldO, &oldOp)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	win := newer(c.HLC, c.Origin, oldH, oldO)
	if c.Bucket == "task_events" && c.Op == "upsert" && oldOp == "upsert" {
		// Independently observed slots can share an id. Their immutable content
		// belongs to the earliest observation, not the last replication write.
		var incoming struct {
			At time.Time `json:"at"`
		}
		var at string
		if err = json.Unmarshal(c.Data, &incoming); err != nil || incoming.At.IsZero() {
			return false, ErrInvalid
		}
		if err = tx.QueryRow(`SELECT at FROM task_events WHERE id=?`, c.ID).Scan(&at); err != nil {
			return false, err
		}
		oldAt, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return false, err
		}
		win = incoming.At.Before(oldAt) || incoming.At.Equal(oldAt) && c.Origin < oldO
	}
	itemChanged := false
	if c.Bucket == "tasks" && c.Op == "upsert" && (oldOp != "tombstone" || win) {
		for _, i := range c.Items {
			if _, e := tx.Exec(`UPDATE sync_state SET clock=max(clock,?)`, i.HLC); e != nil {
				return false, e
			}
			res, e := tx.Exec(`INSERT INTO sync_items VALUES(?,?,?,?,?,?,?) ON CONFLICT(task_id,list,id) DO UPDATE SET hlc=excluded.hlc,origin=excluded.origin,removed=excluded.removed,data=excluded.data WHERE excluded.hlc>sync_items.hlc OR (excluded.hlc=sync_items.hlc AND excluded.origin>sync_items.origin)`, c.ID, i.List, i.ID, i.HLC, i.Origin, i.Removed, string(i.Data))
			if e != nil {
				return false, e
			}
			n, _ := res.RowsAffected()
			itemChanged = itemChanged || n > 0
		}
	}
	if !win && !itemChanged {
		return false, nil
	}
	if !win {
		var raw string
		if err = tx.QueryRow(`SELECT data FROM tasks WHERE id=?`, c.ID).Scan(&raw); err != nil {
			return false, err
		}
		c.Data = json.RawMessage(raw)
		c.HLC = oldH
		c.Origin = oldO
		if _, err = tx.Exec(`UPDATE sync_state SET remote_hlc=?,remote_origin=?`, oldH, oldO); err != nil {
			return false, err
		}
	}
	if c.Op == "tombstone" {
		// Retain the complete previous row in sync_deleted, even though normal store readers hide it.
		if c.Bucket == "tasks" || c.Bucket == "task_events" {
			_, err = tx.Exec(`DELETE FROM `+c.Bucket+` WHERE id=?`, c.ID)
		} else {
			_, err = tx.Exec(`DELETE FROM objects WHERE bucket=? AND id=?`, c.Bucket, c.ID)
		}
		if err != nil {
			return false, err
		}
		_, err = tx.Exec(`INSERT OR REPLACE INTO sync_deleted VALUES(?,?,?,?); INSERT INTO sync_meta VALUES(?,?,?,?,'tombstone') ON CONFLICT(bucket,id) DO UPDATE SET hlc=excluded.hlc,origin=excluded.origin,op='tombstone'; INSERT INTO sync_log(bucket,id,hlc,origin,op,data) VALUES(?,?,?,?,'tombstone',?)`, c.Bucket, c.ID, string(c.Data), s.Now().UnixMilli(), c.Bucket, c.ID, c.HLC, c.Origin, c.Bucket, c.ID, c.HLC, c.Origin, string(c.Data))
		return true, err
	}
	if c.Bucket == "person_aliases" {
		if err = s.alias(tx, c); err != nil {
			return false, err
		}
	}
	if c.Bucket == "tasks" {
		c.Data, err = localTaskDerived(tx, c.ID, c.Data)
		if err != nil {
			return false, err
		}
		c.Data, err = materializeItems(tx, c.ID, c.Data)
		if err != nil {
			return false, err
		}
	}
	if win && (c.Bucket == "tasks" || c.Bucket == "circles") {
		if err = s.review(tx, c, out); err != nil {
			return false, err
		}
	}
	switch c.Bucket {
	case "tasks":
		_, err = tx.Exec(`INSERT INTO tasks VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data`, c.ID, string(c.Data))
	case "task_events":
		var e struct {
			ID, TaskID, At, Kind, Text string
			Payload                    json.RawMessage
		}
		var v map[string]json.RawMessage
		_ = json.Unmarshal(c.Data, &v)
		_ = json.Unmarshal(v["task_id"], &e.TaskID)
		_ = json.Unmarshal(v["at"], &e.At)
		_ = json.Unmarshal(v["kind"], &e.Kind)
		_ = json.Unmarshal(v["text"], &e.Text)
		e.Payload = v["payload"]
		if len(e.Payload) == 0 {
			e.Payload = json.RawMessage(`null`)
		}
		_, err = tx.Exec(`INSERT INTO task_events VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET task_id=excluded.task_id,at=excluded.at,kind=excluded.kind,text=excluded.text,payload=excluded.payload`, c.ID, e.TaskID, e.At, e.Kind, e.Text, string(e.Payload))
	default:
		_, err = tx.Exec(`INSERT INTO objects VALUES(?,?,?) ON CONFLICT(bucket,id) DO UPDATE SET data=excluded.data`, c.Bucket, c.ID, string(c.Data))
	}
	return true, err
}
func materializeItems(tx *sql.Tx, id string, raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	items, err := readItems(tx, id)
	if err != nil {
		return nil, err
	}
	for _, list := range []string{"checklist", "items", "updates_sent"} {
		xs := []json.RawMessage{}
		for _, i := range items {
			if i.List == list && !i.Removed {
				xs = append(xs, i.Data)
			}
		}
		if len(xs) > 0 || obj[list] != nil {
			obj[list] = xs
		}
	}
	done, total := 0, 0
	for _, i := range items {
		if i.List == "checklist" && !i.Removed {
			total++
			var v map[string]any
			_ = json.Unmarshal(i.Data, &v)
			if v["done"] == true {
				done++
			}
		}
	}
	obj["progress"] = map[string]int{"done": done, "total": total}
	return json.Marshal(obj)
}
