package lease

import (
	"context"
	"errors"
	"time"
)

// Key uniquely identifies a lease within the system.
type Key struct {
	Namespace string
	Name      string
}

// Record is the persisted state of a single lease.
type Record struct {
	// Key uniquely identifies the lease.
	Key Key
	// Holder identifies the entity currently holding the lease.
	Holder string
	// TTL is the duration after RenewedAt before the lease expires.
	TTL time.Duration
	// AcquiredAt is when the current holder first acquired the lease. It is
	// preserved across renewals by the same holder.
	AcquiredAt time.Time
	// RenewedAt is when the holder last renewed (or first acquired) the lease.
	RenewedAt time.Time
	// FencingToken is a monotonically increasing identifier for the current
	// holder. It is bumped on each fresh acquisition (after release or
	// expiry) and stable across renewals by the same holder. A holder must
	// include its fencing token on every state-changing operation; an
	// operation tagged with a stale token is rejected by the [Store].
	//
	// The width is int32 to match coordination.k8s.io/v1.Lease's
	// spec.leaseTransitions field, which is the planned production backend.
	FencingToken int32
}

// ExpiresAt returns the time at which this record's TTL elapses.
func (r *Record) ExpiresAt() time.Time {
	return r.RenewedAt.Add(r.TTL)
}

// Expired reports whether the record's TTL has elapsed at the given time.
func (r *Record) Expired(now time.Time) bool {
	return now.After(r.ExpiresAt())
}

// Errors returned by [Store] implementations.
var (
	// ErrNotFound indicates that no record exists for the requested key.
	ErrNotFound = errors.New("lease: not found")
	// ErrConflict indicates that the expected fencing token did not match
	// the current record. The caller should re-read state and retry.
	ErrConflict = errors.New("lease: conflict")
)

// Store is the persistence interface for lease records. Implementations must
// guarantee linearizable execution of Get, Put, and Delete per [Key].
type Store interface {
	// Ping reports backend reachability with a constant-cost probe (no full
	// scan): a nil error means the backend answered. It exists so a readiness
	// check can verify the store without the unbounded cost of List, keeping
	// the unauthenticated /readyz route from amplifying into an expensive
	// backend query. Implementations must respect ctx cancellation.
	Ping(ctx context.Context) error

	// Get returns the record for key. Returns [ErrNotFound] if absent.
	Get(ctx context.Context, key Key) (*Record, error)

	// List returns a snapshot of all records. Order is not specified.
	List(ctx context.Context) ([]Record, error)

	// Put atomically writes record. If expected is 0 the operation succeeds
	// only when no record exists for record.Key; otherwise it succeeds only
	// when the current record's FencingToken equals expected. Returns
	// [ErrConflict] on a token mismatch.
	Put(ctx context.Context, expected int32, record *Record) error

	// Delete atomically removes the record for key. Succeeds only when the
	// current record's FencingToken equals expected. Returns [ErrConflict]
	// on a token mismatch and [ErrNotFound] when no record exists.
	Delete(ctx context.Context, key Key, expected int32) error
}
