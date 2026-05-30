package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/tenant"
)

const (
	// readinessCacheTTL collapses a burst of /readyz requests into at most one
	// backend probe per window. The route is unauthenticated, so without this
	// an external caller could drive a backend query per request; the kubelet
	// probes far slower than this, so caching does not stale-gate real drains.
	readinessCacheTTL = time.Second
	// readinessProbeTimeout bounds each backend probe server-side so a hung
	// store cannot pin the readiness goroutine indefinitely (the operator-side
	// bound does not protect the API server itself).
	readinessProbeTimeout = 2 * time.Second
)

// ReadinessChecker reports whether the server's backend is reachable. The
// lease [lease.Manager] satisfies it by probing its store; NewMux serves
// /readyz from it so a store outage drains the pod (503) instead of leaving
// it routable and surfacing failures as 500s.
type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

// NewMux returns the HTTP routes for the API server.
//
//   - /healthz is an always-200 liveness route, served unauthenticated.
//   - /readyz is an unauthenticated readiness route: when mgr implements
//     [ReadinessChecker] it probes the backend and answers 503 on error so
//     the pod is drained during a store outage; otherwise it answers 200.
//   - The /v1alpha1/* lease endpoints are served only when mgr is non-nil.
//   - When authn is non-nil, every lease endpoint is wrapped in
//     [AuthMiddleware] and authorized by authz against the request namespace
//     and holder; authz defaults to [tenant.NewDefaultAuthorizer] when nil.
//     When authn is nil, the lease endpoints are unauthenticated and
//     unauthorized — intended only for `--auth-mode=none` development setups;
//     cmd/apiserver logs a loud warning in that case.
func NewMux(mgr LeaseManager, authn auth.Authenticator, authz tenant.Authorizer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", readyzHandler(mgr))
	if mgr == nil {
		return mux
	}
	var wrap func(http.Handler) http.Handler
	if authn != nil {
		wrap = AuthMiddleware(authn)
		if authz == nil {
			authz = tenant.NewDefaultAuthorizer()
		}
	}
	registerLeaseRoutes(mux, mgr, wrap, authz)
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok\n")
}

// readyzHandler serves the readiness route. When mgr implements
// [ReadinessChecker] it probes the backend (via a cached, serialized,
// timeout-bounded gate) and answers 503 on failure; otherwise (no backend,
// e.g. the dev/no-store configuration) it answers 200, there being nothing
// to gate. The route is unauthenticated by design (kubelet probes carry no
// credentials); the gate ensures it cannot be weaponized against the store.
func readyzHandler(mgr LeaseManager) http.HandlerFunc {
	checker, _ := mgr.(ReadinessChecker)
	gate := &readinessGate{
		checker: checker,
		ttl:     readinessCacheTTL,
		timeout: readinessProbeTimeout,
		now:     time.Now,
		log:     slog.Default(),
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := gate.ready(); err != nil {
			writePlain(w, http.StatusServiceUnavailable, "not ready\n")
			return
		}
		writePlain(w, http.StatusOK, "ok\n")
	}
}

// readinessGate fronts a [ReadinessChecker] with a small TTL cache so a storm
// of /readyz requests collapses into at most one backend probe per window.
// The check runs under the mutex, so concurrent requests share a single
// in-flight probe rather than each hitting the backend, and the probe uses a
// fresh timeout-bounded context decoupled from any one request (a client
// disconnect must not cancel — and poison the cache of — a shared check).
type readinessGate struct {
	checker ReadinessChecker
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time
	log     *slog.Logger

	mu        sync.Mutex
	checkedAt time.Time
	lastErr   error
	primed    bool
}

// ready returns the cached probe result when it is within ttl, otherwise it
// runs a fresh backend probe. A nil checker (no backend) is always ready.
func (g *readinessGate) ready() error {
	if g.checker == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.primed && g.now().Sub(g.checkedAt) < g.ttl {
		return g.lastErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()
	err := g.checker.Ready(ctx)
	g.lastErr = err
	g.checkedAt = g.now()
	g.primed = true
	if err != nil {
		g.log.LogAttrs(ctx, slog.LevelWarn, "readiness check failed",
			slog.String("error", err.Error()))
	}
	return err
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
