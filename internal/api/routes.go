package api

import (
	"net/http"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/tenant"
)

// NewMux returns the HTTP routes for the API server.
//
//   - /healthz is always served unauthenticated.
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

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
