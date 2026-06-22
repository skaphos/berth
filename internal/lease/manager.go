package lease

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

// maxAcquireAttempts caps Acquire's CAS retry loop. A bounded loop keeps a
// pathological hot lease from spinning forever inside a single call; the
// caller can retry at the request layer if needed.
const maxAcquireAttempts = 8

// Manager orchestrates lease acquisition, renewal, and release using a
// pluggable [Store] backend. Manager enforces server-side TTL semantics: a
// lease is reclaimable as soon as its RenewedAt+TTL has elapsed.
type Manager struct {
	store Store
	now   func() time.Time
}

// NewManager creates a Manager backed by the given [Store]. The returned
// Manager uses [time.Now] for TTL calculations.
func NewManager(store Store) *Manager {
	return &Manager{store: store, now: time.Now}
}

// WithClock returns a copy of m that uses fn for time. Intended for tests.
func (m *Manager) WithClock(fn func() time.Time) *Manager {
	return &Manager{store: m.store, now: fn}
}

// Ready reports whether the backing store is reachable via the store's
// constant-cost [Store.Ping]. It returns the store's error unmodified so a
// readiness probe can fail (503) and drain the pod during a store outage,
// distinct from an always-200 liveness check. Ping (not List) keeps the
// unauthenticated /readyz route from amplifying into a full backend scan.
func (m *Manager) Ready(ctx context.Context) error {
	return m.store.Ping(ctx)
}

// AcquireResult reports the outcome of an [Manager.Acquire] or
// [Manager.Renew] call.
type AcquireResult struct {
	// Acquired is true when the caller now holds (or continues to hold) the
	// lease. When false, Holder/FencingToken/ExpiresAt describe the entity
	// that does hold it.
	Acquired bool
	// Holder is the identity of the current holder.
	Holder string
	// FencingToken is the current fencing token. The caller must include
	// this token on every subsequent Renew or Release call.
	FencingToken int32
	// ExpiresAt is the time at which the current TTL elapses.
	ExpiresAt time.Time
	// AcquiredAt is when the current holder first acquired the lease.
	AcquiredAt time.Time
}

// Acquire attempts to acquire or renew the lease at key for holder. ttl
// must be positive.
//
// Cases:
//   - No record exists: a new lease is created; FencingToken is 1.
//   - Same holder, not expired: lease is renewed in place, FencingToken
//     preserved.
//   - Existing record is expired: lease is reclaimed for holder, FencingToken
//     is bumped.
//   - Held by another, not expired: returns Acquired=false and the existing
//     holder's identity, fencing token, and expiry.
func (m *Manager) Acquire(ctx context.Context, key Key, holder string, ttl time.Duration) (AcquireResult, error) {
	if holder == "" {
		return AcquireResult{}, errors.New("acquire: holder is required")
	}
	if ttl <= 0 {
		return AcquireResult{}, errors.New("acquire: ttl must be positive")
	}

	for range maxAcquireAttempts {
		cur, err := m.store.Get(ctx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return AcquireResult{}, fmt.Errorf("acquire: get: %w", err)
		}

		now := m.now()
		var (
			expected   int32
			token      int32
			acquiredAt time.Time
		)

		switch {
		case cur == nil:
			expected = 0
			token = 1
			acquiredAt = now
		case cur.Holder == holder && !cur.Expired(now):
			expected = cur.FencingToken
			token = cur.FencingToken
			acquiredAt = cur.AcquiredAt
		case cur.Expired(now):
			// Reclaiming an expired lease bumps the fencing token. int32 is a
			// deliberate domain bound (see [Record.FencingToken]); refuse to
			// wrap at the ceiling rather than reuse a token, which would let a
			// stale holder's writes pass the fencing check.
			if cur.FencingToken == math.MaxInt32 {
				return AcquireResult{}, fmt.Errorf(
					"acquire: fencing token for %s/%s exhausted at int32 ceiling (%d); lease cannot be safely reacquired",
					key.Namespace, key.Name, math.MaxInt32)
			}
			expected = cur.FencingToken
			token = cur.FencingToken + 1
			acquiredAt = now
		default:
			return AcquireResult{
				Acquired:     false,
				Holder:       cur.Holder,
				FencingToken: cur.FencingToken,
				ExpiresAt:    cur.ExpiresAt(),
				AcquiredAt:   cur.AcquiredAt,
			}, nil
		}

		next := &Record{
			Key:          key,
			Holder:       holder,
			TTL:          ttl,
			AcquiredAt:   acquiredAt,
			RenewedAt:    now,
			FencingToken: token,
		}
		if err := m.store.Put(ctx, expected, next); err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			return AcquireResult{}, fmt.Errorf("acquire: put: %w", err)
		}
		return AcquireResult{
			Acquired:     true,
			Holder:       holder,
			FencingToken: token,
			ExpiresAt:    next.ExpiresAt(),
			AcquiredAt:   acquiredAt,
		}, nil
	}
	return AcquireResult{}, errors.New("acquire: exhausted retries")
}

// Renew extends the TTL of a lease held by holder under fencing token. ttl
// must be positive. Returns Acquired=false (with no error) if the lease has
// been lost — different holder, mismatched token, expired, or absent.
func (m *Manager) Renew(ctx context.Context, key Key, holder string, token int32, ttl time.Duration) (AcquireResult, error) {
	if holder == "" {
		return AcquireResult{}, errors.New("renew: holder is required")
	}
	if token <= 0 {
		return AcquireResult{}, errors.New("renew: token must be positive")
	}
	if ttl <= 0 {
		return AcquireResult{}, errors.New("renew: ttl must be positive")
	}

	cur, err := m.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return AcquireResult{Acquired: false}, nil
		}
		return AcquireResult{}, fmt.Errorf("renew: get: %w", err)
	}

	now := m.now()
	if cur.Holder != holder || cur.FencingToken != token || cur.Expired(now) {
		return AcquireResult{
			Acquired:     false,
			Holder:       cur.Holder,
			FencingToken: cur.FencingToken,
			ExpiresAt:    cur.ExpiresAt(),
			AcquiredAt:   cur.AcquiredAt,
		}, nil
	}

	next := *cur
	next.RenewedAt = now
	next.TTL = ttl
	if err := m.store.Put(ctx, cur.FencingToken, &next); err != nil {
		if errors.Is(err, ErrConflict) {
			return AcquireResult{
				Acquired:     false,
				Holder:       cur.Holder,
				FencingToken: cur.FencingToken,
			}, nil
		}
		return AcquireResult{}, fmt.Errorf("renew: put: %w", err)
	}
	return AcquireResult{
		Acquired:     true,
		Holder:       holder,
		FencingToken: next.FencingToken,
		ExpiresAt:    next.ExpiresAt(),
		AcquiredAt:   next.AcquiredAt,
	}, nil
}

// Release voluntarily releases a lease held by holder under fencing token.
// The operation is idempotent: releasing an already-absent lease, or losing
// a race with another reclaimer, returns nil. Returns [ErrConflict] when
// the lease is currently held by a different identity or token.
func (m *Manager) Release(ctx context.Context, key Key, holder string, token int32) error {
	if holder == "" {
		return errors.New("release: holder is required")
	}
	cur, err := m.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("release: get: %w", err)
	}
	if cur.Holder != holder || cur.FencingToken != token {
		return ErrConflict
	}
	if err := m.store.Delete(ctx, key, cur.FencingToken); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			return nil
		}
		return fmt.Errorf("release: delete: %w", err)
	}
	return nil
}
