package acquire

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

// LeaseClient is the subset of the Berth API client the helper uses. It
// is an interface so tests can substitute a fake without a live server.
// *client.Client satisfies it.
type LeaseClient interface {
	Acquire(ctx context.Context, namespace, name, holder string, ttl time.Duration) (client.AcquireResult, error)
	Renew(ctx context.Context, namespace, name, holder string, fencingToken int32, ttl time.Duration) (client.AcquireResult, error)
	Release(ctx context.Context, namespace, name, holder string, fencingToken int32) error
}

// retryInterval is the cadence the init container retries Acquire at
// while it holds the pod. It mirrors the heartbeat (ttl/3 by default) so
// a waiting pod reacts within roughly the same window as a renewing one.
func (c *Config) retryInterval() time.Duration {
	if c.HeartbeatInterval > 0 {
		return c.HeartbeatInterval
	}
	return c.TTL / 3
}

// Hold blocks until it acquires the lease, then persists the holder,
// fencing token, and health marker and installs the check binary so the
// sidecar and probe can take over. It returns the winning AcquireResult.
//
// It retries on both transient errors and "held by another" until the
// context is canceled (the init container is expected to block the pod).
// Callers that want a bounded gate should pass a context with a deadline.
func Hold(ctx context.Context, cfg *Config, lc LeaseClient, state *State, log *slog.Logger) (client.AcquireResult, error) {
	holder := cfg.Holder()
	retry := cfg.retryInterval()
	log = log.With("lease", cfg.LeaseName, "namespace", cfg.LeaseNamespace, "holder", holder, "mode", string(cfg.Mode))

	for {
		res, err := lc.Acquire(ctx, cfg.LeaseNamespace, cfg.LeaseName, holder, cfg.TTL)
		switch {
		case err != nil:
			log.Warn("acquire failed, retrying", "error", err, "retry_in", retry)
		case res.Acquired:
			if err := state.WriteAcquired(holder, res.FencingToken); err != nil {
				return res, fmt.Errorf("persist lease state: %w", err)
			}
			if err := state.InstallCheckBinary(); err != nil {
				return res, fmt.Errorf("install check binary: %w", err)
			}
			log.Info("lease acquired", "fencing_token", res.FencingToken, "expires_at", res.ExpiresAt)
			return res, nil
		default:
			log.Info("lease held by another, waiting", "current_holder", res.Holder, "retry_in", retry)
		}

		if !sleep(ctx, retry) {
			return client.AcquireResult{}, ctx.Err()
		}
	}
}

// sleep blocks for d or until ctx is canceled. Returns true if it
// completed normally, false if canceled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
