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

// Put implements [Store] with compare-and-swap semantics on Version.
func (s *MemStore) Put(ctx context.Context, expectedVersion int64, rec *Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rec == nil {
		return ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists := s.records[rec.Key]
	if expectedVersion == 0 {
		if exists {
			return ErrConflict
		}
	} else {
		if !exists || cur.Version != expectedVersion {
			return ErrConflict
		}
	}
	stored := *rec
	stored.Version = expectedVersion + 1
	s.records[rec.Key] = stored
	return nil
}
