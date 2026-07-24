package lease

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Default GC cadence and grace window for [NewTTLEnforcer].
const (
	defaultGCInterval = 30 * time.Second
	defaultGCGrace    = 5 * time.Minute
)

// TTLEnforcer garbage-collects expired lease records in a [Store]. TTL
// expiry is also enforced lazily on Acquire/Renew, but a key that is never
// reacquired would otherwise keep its holder, TTL, and timing state in a
// durable backend forever; this loop tombstones that state. It rewrites only
// records whose TTL elapsed more than a grace window ago, using the same
// version CAS as [Manager]: the write is conditioned on the [Record.Version]
// captured during the scan, and versions are never reused for a key, so a
// sweep can never touch a lease that was written since the scan.
//
// Records are tombstoned ([Record.Holder] cleared, [Record.FencingToken]
// preserved), never deleted — deletion would reset the key's token
// high-water mark and break downstream fencing (see [Record.FencingToken]).
//
// The enforcer needs no cross-replica coordination: every API server replica
// may run its own loop, and the CAS-guarded write makes concurrent sweeps
// idempotent — the first wins, the rest observe ErrConflict and skip.
type TTLEnforcer struct {
	store    Store
	interval time.Duration
	grace    time.Duration
	now      func() time.Time
	log      *slog.Logger
}

// NewTTLEnforcer creates a TTLEnforcer that sweeps store every interval and
// tombstones records expired longer than grace ago. A non-positive interval
// or grace falls back to the package default.
func NewTTLEnforcer(store Store, interval, grace time.Duration) *TTLEnforcer {
	if interval <= 0 {
		interval = defaultGCInterval
	}
	if grace <= 0 {
		grace = defaultGCGrace
	}
	return &TTLEnforcer{
		store:    store,
		interval: interval,
		grace:    grace,
		now:      time.Now,
		log:      slog.Default(),
	}
}

// Run sweeps the store on a ticker until ctx is canceled, returning the
// context error. Each tick lists the store and tombstones every record
// expired longer than the grace window ago.
func (e *TTLEnforcer) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			e.collect(ctx)
		}
	}
}

// collect tombstones records whose TTL elapsed more than the grace window
// ago. Tombstones themselves are skipped — they are the terminal state. The
// write uses each record's scanned version as the CAS precondition: if any
// party wrote the key since the scan, the version no longer matches and the
// record is skipped (ErrConflict), so GC never disturbs a live lease. A
// failed list or write is logged and the sweep continues; the next tick
// retries.
func (e *TTLEnforcer) collect(ctx context.Context) {
	records, err := e.store.List(ctx)
	if err != nil {
		// A failure because the context is done means we're shutting down (or
		// hit the sweep deadline); that is expected, not worth a warning.
		if ctx.Err() == nil {
			e.log.WarnContext(ctx, "ttl gc: list failed", "error", err)
		}
		return
	}

	now := e.now()
	swept := 0
	for i := range records {
		r := records[i]
		if r.Tombstone() || now.Before(r.ExpiresAt().Add(e.grace)) {
			continue
		}
		switch err := e.store.Put(ctx, r.Version, tombstoneFrom(&r, now)); {
		case err == nil:
			swept++
			// Per-record detail at Debug so a large first sweep doesn't burst
			// Info logs; the summary below reports the count at Info.
			e.log.DebugContext(ctx, "ttl gc: tombstoned expired lease",
				"namespace", r.Key.Namespace, "name", r.Key.Name,
				"holder", r.Holder, "expired_at", r.ExpiresAt())
		case errors.Is(err, ErrConflict):
			// Raced a concurrent reacquire, release, or sweep; leave the
			// current state alone.
		default:
			// Once the context is done, remaining writes will fail the same
			// way; stop quietly rather than logging a warning per record.
			if ctx.Err() != nil {
				return
			}
			e.log.WarnContext(ctx, "ttl gc: tombstone failed",
				"namespace", r.Key.Namespace, "name", r.Key.Name, "error", err)
		}
	}
	if swept > 0 {
		e.log.InfoContext(ctx, "ttl gc: tombstoned expired leases", "swept", swept, "scanned", len(records))
	}
}
