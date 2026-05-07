package api

import "net/http"

// NewMux returns the HTTP routes for the API server. If mgr is non-nil, the
// lease endpoints under /v1alpha1/namespaces/{namespace}/leases/{name}/...
// are registered. /healthz is always served.
func NewMux(mgr LeaseManager) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	if mgr != nil {
		registerLeaseRoutes(mux, mgr)
	}
	return mux
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
