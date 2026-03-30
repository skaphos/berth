package lease

import (
	"context"
	"time"
)

// TTLEnforcer periodically scans for expired leases and enforces their
// time-to-live constraints.
type TTLEnforcer struct {
	store        Store
	scanInterval time.Duration
}

// NewTTLEnforcer creates a TTLEnforcer that scans the given [Store] at the
// specified interval.
func NewTTLEnforcer(store Store, scanInterval time.Duration) *TTLEnforcer {
	return &TTLEnforcer{store: store, scanInterval: scanInterval}
}

// Run starts the TTL enforcement loop, scanning leases in the given namespace
// until the context is canceled. If scanInterval is zero or negative, it
// defaults to 30 seconds.
func (e *TTLEnforcer) Run(ctx context.Context, namespace string) error {
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
			_, _ = e.store.List(ctx, namespace)
		}
	}
}
