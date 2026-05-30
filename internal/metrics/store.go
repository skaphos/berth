package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

// WrapStore decorates inner so every call records its latency and outcome
// under the given backend label (e.g. "mem", "k8s", "sql"). The wrapper is
// transparent: it forwards arguments and results unchanged and only observes.
func (m *Metrics) WrapStore(backend string, inner lease.Store) lease.Store {
	return &meteredStore{metrics: m, backend: backend, inner: inner}
}

type meteredStore struct {
	metrics *Metrics
	backend string
	inner   lease.Store
}

func (s *meteredStore) Ping(ctx context.Context) error {
	start := time.Now()
	err := s.inner.Ping(ctx)
	s.observe("ping", start, err)
	return err
}

func (s *meteredStore) Get(ctx context.Context, key lease.Key) (*lease.Record, error) {
	start := time.Now()
	rec, err := s.inner.Get(ctx, key)
	s.observe("get", start, err)
	return rec, err
}

func (s *meteredStore) List(ctx context.Context) ([]lease.Record, error) {
	start := time.Now()
	recs, err := s.inner.List(ctx)
	s.observe("list", start, err)
	return recs, err
}

func (s *meteredStore) Put(ctx context.Context, expected int32, record *lease.Record) error {
	start := time.Now()
	err := s.inner.Put(ctx, expected, record)
	s.observe("put", start, err)
	return err
}

func (s *meteredStore) Delete(ctx context.Context, key lease.Key, expected int32) error {
	start := time.Now()
	err := s.inner.Delete(ctx, key, expected)
	s.observe("delete", start, err)
	return err
}

func (s *meteredStore) observe(op string, start time.Time, err error) {
	s.metrics.ObserveStoreCall(op, s.backend, outcomeFor(err), time.Since(start))
}

// outcomeFor classifies a store error into a low-cardinality label. The
// sentinel errors are expected control-flow signals (a denied CAS, an absent
// key), distinct from an unexpected backend "error".
func outcomeFor(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, lease.ErrConflict):
		return "conflict"
	case errors.Is(err, lease.ErrNotFound):
		return "notfound"
	default:
		return "error"
	}
}
