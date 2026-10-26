package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

// MemoryStore is safe for concurrent use and never lends mutable slices to callers.
// Durable deployments must supply a hub.db-backed Store.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]Record
	hits    map[string][]Hit
	runs    map[string][]RunRecord
	replies map[string][]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]Record{}, hits: map[string][]Hit{}, runs: map[string][]RunRecord{}, replies: map[string][]time.Time{}}
}
func copyValue[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err = json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}
func (s *MemoryStore) Create(ctx context.Context, r Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.ID]; ok {
		return ErrExists
	}
	s.records[r.ID] = copyValue(r)
	return nil
}
func (s *MemoryStore) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return copyValue(r), nil
}
func (s *MemoryStore) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, copyValue(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *MemoryStore) Update(ctx context.Context, id string, fn func(*Record) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	original := copyValue(r)
	r = copyValue(r)
	if err := fn(&r); err != nil {
		return err
	}
	if r.ID != id {
		return ErrNotFound
	}
	if len(r.Instructions) < len(original.Instructions) || (len(original.Instructions) > 0 && jsonText(r.Instructions[:len(original.Instructions)]) != jsonText(original.Instructions)) || jsonText(r.CreatedVia) != jsonText(original.CreatedVia) {
		return errors.New("trigger instructions and provenance are append-only")
	}
	s.records[id] = copyValue(r)
	return nil
}
func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return ErrNotFound
	}
	delete(s.records, id)
	delete(s.hits, id)
	delete(s.runs, id)
	delete(s.replies, id)
	return nil
}
func (s *MemoryStore) PutHit(ctx context.Context, h Hit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[h.WatchID]; !ok {
		return ErrNotFound
	}
	for i, v := range s.hits[h.WatchID] {
		if v.ID == h.ID {
			s.hits[h.WatchID][i] = copyValue(h)
			return nil
		}
	}
	s.hits[h.WatchID] = append(s.hits[h.WatchID], copyValue(h))
	return nil
}
func (s *MemoryStore) Hits(ctx context.Context, id string, limit int) ([]Hit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return nil, ErrNotFound
	}
	v := s.hits[id]
	if limit > 0 && len(v) > limit {
		v = v[len(v)-limit:]
	}
	return copyValue(v), nil
}
func (s *MemoryStore) PutRun(ctx context.Context, r RunRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.HandoffID]; !ok {
		return ErrNotFound
	}
	for i, v := range s.runs[r.HandoffID] {
		if v.ID == r.ID {
			s.runs[r.HandoffID][i] = r
			return nil
		}
	}
	s.runs[r.HandoffID] = append(s.runs[r.HandoffID], r)
	return nil
}
func (s *MemoryStore) Runs(ctx context.Context, id string) ([]RunRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return nil, ErrNotFound
	}
	return append([]RunRecord{}, s.runs[id]...), nil
}
func (s *MemoryStore) ReplyCount(ctx context.Context, id string, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return 0, ErrNotFound
	}
	return s.replyCount(id, now), nil
}
func (s *MemoryStore) replyCount(id string, now time.Time) int {
	n := 0
	for _, at := range s.replies[id] {
		if at.After(now.Add(-time.Hour)) {
			n++
		}
	}
	return n
}
func (s *MemoryStore) ReserveReply(ctx context.Context, id string, now time.Time, max int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return false, ErrNotFound
	}
	if max <= 0 || s.replyCount(id, now) >= max {
		return false, nil
	}
	kept := s.replies[id][:0]
	for _, at := range s.replies[id] {
		if at.After(now.Add(-time.Hour)) {
			kept = append(kept, at)
		}
	}
	s.replies[id] = append(kept, now)
	return true, nil
}
