package lease

import (
	"context"
	"time"
)

type TTLEnforcer struct {
	store        Store
	scanInterval time.Duration
}

func NewTTLEnforcer(store Store, scanInterval time.Duration) *TTLEnforcer {
	return &TTLEnforcer{store: store, scanInterval: scanInterval}
}

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
