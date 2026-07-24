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
	// Holder identifies the entity currently holding the lease. An empty
	// Holder marks a tombstone: a released or garbage-collected lease whose
	// record is retained so FencingToken and Version stay a per-key
	// high-water mark. No live lease can have an empty Holder ([Manager]
	// requires a non-empty holder on every operation), so the marker is
	// unambiguous.
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
	// expiry) and stable across renewals by the same holder. Because release
	// and garbage collection tombstone the record instead of deleting it,
	// the token is monotonic across the entire life of the key: a newly
	// issued token is strictly greater than every token ever issued for the
	// key, so downstream systems can safely reject any token at or below
	// the highest they have observed.
	//
	// The width is deliberately int32, a domain decision rather than a
	// backend artifact. A 31-bit positive range allows ~2.1e9 holder
	// transitions per key — decades of churn at any realistic failover rate,
	// so the ceiling is unreachable in practice — while matching
	// coordination.k8s.io/v1.Lease's spec.leaseTransitions field so the k8s
	// [Store] backend persists the token without narrowing. [Manager] treats
	// math.MaxInt32 as a hard ceiling and refuses to wrap (wrapping would
	// reuse a token and break fencing safety) rather than overflow into a
	// negative or duplicate value; the sql and mem backends inherit the same
	// width and invariant.
	FencingToken int32
	// Version is the record's write counter and the [Store]'s sole
	// compare-and-swap predicate. The store owns it: a create stores 1 and
	// every subsequent successful Put stores the expected version plus one,
	// ignoring the caller-supplied value. Because it changes on every write
	// — renewals included — and no code path hard-deletes a record, a
	// version value observed for a key can gate at most one successful
	// write, ever. That property is what makes a stale renew lose to a
	// concurrent reclaim and a garbage-collection sweep unable to touch a
	// record written after its scan.
	Version int64
}

// ExpiresAt returns the time at which this record's TTL elapses.
func (r *Record) ExpiresAt() time.Time {
	return r.RenewedAt.Add(r.TTL)
}

// Expired reports whether the record's TTL has elapsed at the given time.
func (r *Record) Expired(now time.Time) bool {
	return now.After(r.ExpiresAt())
}

// Tombstone reports whether the record marks a released or garbage-collected
// lease rather than a live holder. See [Record.Holder].
func (r *Record) Tombstone() bool {
	return r.Holder == ""
}

// Errors returned by [Store] implementations.
var (
	// ErrNotFound indicates that no record exists for the requested key.
	ErrNotFound = errors.New("lease: not found")
	// ErrConflict indicates that the expected version did not match the
	// current record. The caller should re-read state and retry.
	ErrConflict = errors.New("lease: conflict")
)

// Store is the persistence interface for lease records. Implementations must
// guarantee linearizable execution of Get, Put, and List per [Key].
//
// Store deliberately has no delete operation. Berth retains every record for
// the life of the backing store's data: release and TTL garbage collection
// write tombstones ([Record.Holder] empty) so [Record.FencingToken] and
// [Record.Version] are never reset or reused for a key. Removing a record
// out of band (SQL DELETE, kubectl delete lease) forfeits that guarantee for
// the key.
type Store interface {
	// Ping reports backend reachability with a constant-cost probe (no full
	// scan): a nil error means the backend answered. It exists so a readiness
	// check can verify the store without the unbounded cost of List, keeping
	// the unauthenticated /readyz route from amplifying into an expensive
	// backend query. Implementations must respect ctx cancellation.
	Ping(ctx context.Context) error

	// Get returns the record for key. Returns [ErrNotFound] if absent.
	Get(ctx context.Context, key Key) (*Record, error)

	// List returns a snapshot of all records, tombstones included. Order is
	// not specified.
	List(ctx context.Context) ([]Record, error)

	// Put atomically writes record. If expectedVersion is 0 the operation
	// succeeds only when no record exists for record.Key and stores the
	// record with Version 1; otherwise it succeeds only when the current
	// record's Version equals expectedVersion, and stores the record with
	// Version expectedVersion+1. The caller-supplied record.Version is
	// ignored. Returns [ErrConflict] when the predicate does not hold.
	Put(ctx context.Context, expectedVersion int64, record *Record) error
}
