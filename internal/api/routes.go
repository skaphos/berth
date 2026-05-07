package api

import (
	"net/http"

	"github.com/skaphos/berth/internal/auth"
)

// NewMux returns the HTTP routes for the API server.
//
//   - /healthz is always served unauthenticated.
//   - The /v1alpha1/* lease endpoints are served only when mgr is non-nil.
//   - When authn is non-nil, every lease endpoint is wrapped in
//     [AuthMiddleware]. When authn is nil, the lease endpoints are
//     unauthenticated — intended only for `--auth-mode=none` development
//     setups; cmd/apiserver logs a loud warning in that case.
func NewMux(mgr LeaseManager, authn auth.Authenticator) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	if mgr == nil {
		return mux
	}
	var wrap func(http.Handler) http.Handler
	if authn != nil {
		wrap = AuthMiddleware(authn)
	}
	registerLeaseRoutes(mux, mgr, wrap)
	return mux
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
