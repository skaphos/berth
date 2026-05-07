package lease

import (
	"context"
	"time"
)

// TTLEnforcer periodically scans the [Store]. Expiry semantics are enforced
// lazily on Acquire/Renew, so this loop is a hygiene mechanism: it surfaces
// the records useful for metrics or for a future cleanup of long-expired
// rows in durable backends.
type TTLEnforcer struct {
	store        Store
	scanInterval time.Duration
}

// NewTTLEnforcer creates a TTLEnforcer that scans store at scanInterval.
func NewTTLEnforcer(store Store, scanInterval time.Duration) *TTLEnforcer {
	return &TTLEnforcer{store: store, scanInterval: scanInterval}
}

// Run scans the store on a ticker until ctx is canceled. If scanInterval is
// zero or negative, it defaults to 30 seconds.
func (e *TTLEnforcer) Run(ctx context.Context) error {
	if e.scanInterval <= 0 {
		e.scanInterval = 30 * time.Second
	}
	ticker := time.NewTicker(e.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _ = e.store.List(ctx)
		}
	}
}
