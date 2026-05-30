package lease

import (
	"context"
	"sync"
)

// MemStore is an in-memory [Store] implementation. It is safe for concurrent
// use and intended for tests, single-process deployments, and bring-up of
// the lease layer ahead of a durable backend.
type MemStore struct {
	mu      sync.Mutex
	records map[Key]Record
}

// NewMemStore returns an empty [MemStore].
func NewMemStore() *MemStore {
	return &MemStore{records: make(map[Key]Record)}
}

// Ping implements [Store]. The in-memory store is always reachable; it only
// honors ctx cancellation.
func (s *MemStore) Ping(ctx context.Context) error {
	return ctx.Err()
}

// Get implements [Store].
func (s *MemStore) Get(ctx context.Context, key Key) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, ErrNotFound
	}
	out := rec
	return &out, nil
}

// List implements [Store].
func (s *MemStore) List(ctx context.Context) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	return out, nil
}

// Put implements [Store] with compare-and-swap semantics on FencingToken.
func (s *MemStore) Put(ctx context.Context, expected int32, rec *Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rec == nil {
		return ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.records[rec.Key]
	if expected == 0 {
		if exists {
			return ErrConflict
		}
	} else {
		if !exists || cur.FencingToken != expected {
			return ErrConflict
		}
	}
	s.records[rec.Key] = *rec
	return nil
}

// Delete implements [Store].
func (s *MemStore) Delete(ctx context.Context, key Key, expected int32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.records[key]
	if !exists {
		return ErrNotFound
	}
	if cur.FencingToken != expected {
		return ErrConflict
	}
	delete(s.records, key)
	return nil
}
