package acquire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

// Renewer is the runtime-singleton sidecar. It renews the lease the init
// container acquired and, on loss, drives the configured Enforcer to stop
// the main container until it can reacquire.
type Renewer struct {
	cfg      *Config
	lc       LeaseClient
	state    *State
	enforcer Enforcer
	log      *slog.Logger

	// now is injectable so tests can drive expiry without real time.
	now func() time.Time

	// runtime state
	holder string
	token  int32
	held   bool
	// expiresAt is a local monotonic deadline, anchored before the last
	// successful RPC. A zero value means the handoff is not yet confirmed.
	expiresAt time.Time
}

// NewRenewer builds a Renewer, selecting the enforcer from cfg.Enforce.
func NewRenewer(cfg *Config, lc LeaseClient, state *State, log *slog.Logger) *Renewer {
	return &Renewer{
		cfg:      cfg,
		lc:       lc,
		state:    state,
		enforcer: newEnforcer(cfg, state, log),
		log:      log.With("lease", cfg.LeaseName, "namespace", cfg.LeaseNamespace, "mode", string(cfg.Mode)),
		now:      time.Now,
	}
}

// loadHandoff reads the holder/token the init container persisted. When
// the state is missing or belongs to another identity (including an older
// holder format), it uses the configured holder and an unheld state. Run
// enforces the gate before attempting acquisition under the current identity.
func (r *Renewer) loadHandoff() {
	r.holder, r.token, r.held = r.cfg.Holder(), 0, false
	r.expiresAt = time.Time{}
	holder, herr := r.state.ReadHolder()
	token, terr := r.state.ReadToken()
	if herr == nil && terr == nil && holder == r.holder && token > 0 {
		r.holder, r.token, r.held = holder, token, true
		// The persisted token identifies a lease, but proves no remaining
		// lifetime. Run keeps enforcement active until a fresh Renew succeeds.
		return
	}
	r.log.Warn("no matching lease handoff; gating before acquiring as the configured holder")
}

// Run renews until the context is canceled, then performs a best-effort
// release when configured. It returns nil on graceful shutdown.
func (r *Renewer) Run(ctx context.Context) error {
	r.loadHandoff()
	if err := r.enforcer.Hold(ctx); err != nil {
		return fmt.Errorf("enforce before confirming lease handoff: %w", err)
	}
	r.log = r.log.With("holder", r.holder)
	r.log.Info("sidecar starting", "held", r.held, "heartbeat", r.cfg.HeartbeatInterval, "ttl", r.cfg.TTL)
	if r.held && ctx.Err() == nil {
		r.tickHeld(ctx)
	}

	ticker := time.NewTicker(r.cfg.HeartbeatInterval)
	defer ticker.Stop()
	expiry := time.NewTimer(r.cfg.TTL)
	defer expiry.Stop()

	for {
		// Wake at expiry even when it falls between heartbeat ticks. RPCs
		// are separately bounded by the same deadline while this loop is busy.
		var expiryC <-chan time.Time
		if r.held && !r.expiresAt.IsZero() {
			expiry.Reset(r.expiresAt.Sub(r.now()))
			expiryC = expiry.C
		} else {
			expiry.Stop()
		}
		select {
		case <-ctx.Done():
			r.shutdown()
			return nil
		case <-expiryC:
			r.log.Warn("local lease deadline reached; enforcing")
			r.lose(ctx)
		case <-ticker.C:
			if r.held {
				r.tickHeld(ctx)
			} else {
				r.tickReacquire(ctx)
			}
		}
	}
}

// tickCtx bounds a lease RPC to one heartbeat or the remaining confirmed
// lifetime, whichever is shorter. A stalled request cannot consume the time
// reserved for enforcement at the local deadline.
func (r *Renewer) tickCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline := r.now().Add(r.cfg.HeartbeatInterval)
	if r.held && !r.expiresAt.IsZero() && r.expiresAt.Before(deadline) {
		deadline = r.expiresAt
	}
	return context.WithDeadline(ctx, deadline)
}

// tickHeld renews the lease we believe we hold. On a definitive loss
// (server says held by another, or a conflict) it enforces immediately.
// On a transient error it keeps the main container running only until the
// last-known expiry; past that point it can no longer prove at-most-once,
// so it enforces. This is the failover-after-expiry guarantee (SKA-436).
func (r *Renewer) tickHeld(ctx context.Context) {
	started := r.now()
	if !r.expiresAt.IsZero() && !started.Before(r.expiresAt) {
		r.lose(ctx)
		return
	}
	rpcCtx, cancel := r.tickCtx(ctx)
	defer cancel()

	res, err := r.lc.Renew(rpcCtx, r.cfg.LeaseNamespace, r.cfg.LeaseName, r.holder, r.token, r.cfg.TTL)
	switch {
	case errors.Is(err, client.ErrConflict):
		r.log.Warn("renew conflict: lease lost")
		r.lose(ctx)
	case err != nil:
		if !r.now().Before(r.expiresAt) {
			r.log.Warn("renew failing past lease expiry; enforcing", "error", err, "expired_at", r.expiresAt)
			r.lose(ctx)
		} else {
			r.log.Warn("renew failed; still within lease TTL, will retry", "error", err, "expires_at", r.expiresAt)
		}
	case !res.Acquired:
		r.log.Warn("renew reports lease held by another: lease lost", "current_holder", res.Holder)
		r.lose(ctx)
	case rpcCtx.Err() != nil || !r.now().Before(started.Add(r.cfg.TTL)) ||
		(!r.expiresAt.IsZero() && !r.now().Before(r.expiresAt)):
		// Even a successful response may arrive too late to prove ownership.
		r.log.Warn("renew response arrived after its local deadline; enforcing")
		r.lose(ctx)
	default:
		r.token = res.FencingToken
		r.expiresAt = started.Add(r.cfg.TTL)
		if err := r.enforcer.Release(ctx); err != nil {
			r.log.Warn("enforcer release failed", "error", err)
		}
		r.log.Debug("lease renewed", "fencing_token", r.token, "expires_at", r.expiresAt)
	}
}

// tickReacquire runs while the main container is gated. It re-enforces
// (a kubelet-restarted container must stay gated) and attempts to
// reacquire; on success it restores the main container and resumes
// renewing.
func (r *Renewer) tickReacquire(ctx context.Context) {
	if err := r.enforcer.Hold(ctx); err != nil {
		r.log.Warn("re-enforce while gated failed", "error", err)
	}

	// Bounded for the same reason as tickHeld: a hung Acquire would stop
	// the loop re-enforcing the gate on a kubelet-restarted container.
	rpcCtx, cancel := r.tickCtx(ctx)
	defer cancel()

	started := r.now()
	res, err := r.lc.Acquire(rpcCtx, r.cfg.LeaseNamespace, r.cfg.LeaseName, r.holder, r.cfg.TTL)
	switch {
	case err != nil:
		r.log.Warn("reacquire failed", "error", err)
	case res.Acquired && (rpcCtx.Err() != nil || !r.now().Before(started.Add(r.cfg.TTL))):
		r.log.Warn("acquire response arrived after its local deadline; remaining gated")
	case res.Acquired:
		r.token = res.FencingToken
		r.expiresAt = started.Add(r.cfg.TTL)
		r.held = true
		if err := r.state.WriteAcquired(r.holder, r.token); err != nil {
			r.log.Warn("persist reacquired state failed", "error", err)
		}
		if err := r.enforcer.Release(ctx); err != nil {
			r.log.Warn("enforcer release failed", "error", err)
		}
		r.log.Info("lease reacquired; main container released", "fencing_token", r.token, "expires_at", r.expiresAt)
	default:
		r.log.Info("waiting to reacquire", "current_holder", res.Holder)
	}
}

// lose transitions to the not-held state and gates the main container.
func (r *Renewer) lose(ctx context.Context) {
	r.held = false
	if err := r.enforcer.Hold(ctx); err != nil {
		r.log.Warn("enforce on lease loss failed", "error", err)
	}
}

// shutdown performs a best-effort release on graceful termination when
// configured. The lease otherwise simply expires on its TTL.
func (r *Renewer) shutdown() {
	if r.cfg.ReleaseOnShutdown == nil || !*r.cfg.ReleaseOnShutdown || !r.held {
		r.log.Info("sidecar shutting down without release", "held", r.held)
		return
	}
	// Use a short, independent context: the parent is already canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch err := r.lc.Release(ctx, r.cfg.LeaseNamespace, r.cfg.LeaseName, r.holder, r.token); {
	case errors.Is(err, client.ErrConflict):
		// Another holder already owns it; nothing to release. Not an error,
		// but we did not release, so don't claim we did.
		r.log.Info("release on shutdown skipped: lease already held by another (conflict)")
	case err != nil:
		r.log.Warn("best-effort release on shutdown failed", "error", err)
	default:
		r.log.Info("released lease on shutdown")
	}
}
