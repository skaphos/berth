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

// TTLEnforcer garbage-collects expired lease records from a [Store]. TTL
// expiry is also enforced lazily on Acquire/Renew, but a key that is never
// reacquired would otherwise leave its row in a durable backend forever; this
// loop reclaims that space. It deletes only records whose TTL elapsed more
// than a grace window ago, using the same fencing-token CAS as [Manager] so a
// sweep can never delete a lease that was reacquired since the scan.
//
// The enforcer needs no cross-replica coordination: every API server replica
// may run its own loop, and the CAS-guarded Delete makes concurrent sweeps
// idempotent — the first wins, the rest observe ErrConflict/ErrNotFound and
// skip.
type TTLEnforcer struct {
	store    Store
	interval time.Duration
	grace    time.Duration
	now      func() time.Time
	log      *slog.Logger
}

// NewTTLEnforcer creates a TTLEnforcer that sweeps store every interval and
// deletes records expired longer than grace ago. A non-positive interval or
// grace falls back to the package default.
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
// context error. Each tick lists the store and deletes every record expired
// longer than the grace window ago.
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

// collect deletes records whose TTL elapsed more than the grace window ago.
// Delete uses each record's fencing token as the CAS precondition: if a holder
// reacquired the key since the scan, the token no longer matches and the
// delete is skipped (ErrConflict), so GC never reclaims a live lease. A failed
// list or delete is logged and the sweep continues; the next tick retries.
func (e *TTLEnforcer) collect(ctx context.Context) {
	records, err := e.store.List(ctx)
	if err != nil {
		e.log.WarnContext(ctx, "ttl gc: list failed", "error", err)
		return
	}

	now := e.now()
	for i := range records {
		r := records[i]
		if now.Before(r.ExpiresAt().Add(e.grace)) {
			continue
		}
		switch err := e.store.Delete(ctx, r.Key, r.FencingToken); {
		case err == nil:
			e.log.InfoContext(ctx, "ttl gc: deleted expired lease",
				"namespace", r.Key.Namespace, "name", r.Key.Name,
				"holder", r.Holder, "expired_at", r.ExpiresAt())
		case errors.Is(err, ErrConflict), errors.Is(err, ErrNotFound):
			// Raced a concurrent reacquire or delete; leave the live state alone.
		default:
			e.log.WarnContext(ctx, "ttl gc: delete failed",
				"namespace", r.Key.Namespace, "name", r.Key.Name, "error", err)
		}
	}
}
