package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/tenant"
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
// [ReadinessChecker] it probes the backend on each request and answers 503
// with the error logged server-side on failure; otherwise (no backend, e.g.
// the dev/no-store configuration) it answers 200, there being nothing to gate.
func readyzHandler(mgr LeaseManager) http.HandlerFunc {
	checker, _ := mgr.(ReadinessChecker)
	return func(w http.ResponseWriter, r *http.Request) {
		if checker != nil {
			if err := checker.Ready(r.Context()); err != nil {
				slog.Default().LogAttrs(r.Context(), slog.LevelWarn,
					"readiness check failed", slog.String("error", err.Error()))
				writePlain(w, http.StatusServiceUnavailable, "not ready\n")
				return
			}
		}
		writePlain(w, http.StatusOK, "ok\n")
	}
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
