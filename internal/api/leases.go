package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

// LeaseManager is the subset of [lease.Manager] consumed by the HTTP layer.
// Defined as an interface for testability and to keep the api package free
// of a hard dependency on the lease package's internals.
type LeaseManager interface {
	Acquire(ctx context.Context, key lease.Key, holder string, ttl time.Duration) (lease.AcquireResult, error)
	Renew(ctx context.Context, key lease.Key, holder string, token int32, ttl time.Duration) (lease.AcquireResult, error)
	Release(ctx context.Context, key lease.Key, holder string, token int32) error
}

// AcquireRequest is the JSON body of POST .../acquire.
type AcquireRequest struct {
	Holder     string `json:"holder"`
	TTLSeconds int32  `json:"ttlSeconds"`
}

// RenewRequest is the JSON body of POST .../renew.
type RenewRequest struct {
	Holder       string `json:"holder"`
	FencingToken int32  `json:"fencingToken"`
	TTLSeconds   int32  `json:"ttlSeconds"`
}

// ReleaseRequest is the JSON body of POST .../release.
type ReleaseRequest struct {
	Holder       string `json:"holder"`
	FencingToken int32  `json:"fencingToken"`
}

// LeaseResponse is the JSON body returned by acquire and renew. It mirrors
// [lease.AcquireResult]. When Acquired is false, the Holder/FencingToken/
// ExpiresAt fields describe the entity currently holding the lease.
type LeaseResponse struct {
	Acquired     bool      `json:"acquired"`
	Holder       string    `json:"holder,omitempty"`
	FencingToken int32     `json:"fencingToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	AcquiredAt   time.Time `json:"acquiredAt,omitempty"`
}

// errorResponse is the standard error envelope.
type errorResponse struct {
	Error string `json:"error"`
}

func leaseResponseFrom(r lease.AcquireResult) LeaseResponse {
	return LeaseResponse{
		Acquired:     r.Acquired,
		Holder:       r.Holder,
		FencingToken: r.FencingToken,
		ExpiresAt:    r.ExpiresAt,
		AcquiredAt:   r.AcquiredAt,
	}
}

// registerLeaseRoutes wires the lease HTTP endpoints onto mux. When wrap
// is non-nil, each handler is composed through it (used for auth).
func registerLeaseRoutes(mux *http.ServeMux, mgr LeaseManager, wrap func(http.Handler) http.Handler) {
	register := func(pattern string, h http.HandlerFunc) {
		var handler http.Handler = h
		if wrap != nil {
			handler = wrap(handler)
		}
		mux.Handle(pattern, handler)
	}
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/acquire", handleAcquire(mgr))
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/renew", handleRenew(mgr))
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/release", handleRelease(mgr))
}

func handleAcquire(mgr LeaseManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req AcquireRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Holder == "" {
			writeError(w, http.StatusBadRequest, "holder is required")
			return
		}
		if req.TTLSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "ttlSeconds must be positive")
			return
		}
		key := lease.Key{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
		res, err := mgr.Acquire(r.Context(), key, req.Holder, time.Duration(req.TTLSeconds)*time.Second)
		if err != nil {
			recordOutcome(r.Context(), outcomeError)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if res.Acquired {
			recordOutcome(r.Context(), outcomeAcquired)
		} else {
			recordOutcome(r.Context(), outcomeHeldByOther)
		}
		writeJSON(w, http.StatusOK, leaseResponseFrom(res))
	}
}

func handleRenew(mgr LeaseManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RenewRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Holder == "" {
			writeError(w, http.StatusBadRequest, "holder is required")
			return
		}
		if req.FencingToken == 0 {
			writeError(w, http.StatusBadRequest, "fencingToken must be non-zero")
			return
		}
		if req.TTLSeconds <= 0 {
			writeError(w, http.StatusBadRequest, "ttlSeconds must be positive")
			return
		}
		key := lease.Key{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
		res, err := mgr.Renew(r.Context(), key, req.Holder, req.FencingToken, time.Duration(req.TTLSeconds)*time.Second)
		if err != nil {
			recordOutcome(r.Context(), outcomeError)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if res.Acquired {
			recordOutcome(r.Context(), outcomeRenewed)
		} else {
			recordOutcome(r.Context(), outcomeHeldByOther)
		}
		writeJSON(w, http.StatusOK, leaseResponseFrom(res))
	}
}

func handleRelease(mgr LeaseManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ReleaseRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Holder == "" {
			writeError(w, http.StatusBadRequest, "holder is required")
			return
		}
		key := lease.Key{Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}
		err := mgr.Release(r.Context(), key, req.Holder, req.FencingToken)
		if err != nil {
			if errors.Is(err, lease.ErrConflict) {
				recordOutcome(r.Context(), outcomeConflict)
				writeError(w, http.StatusConflict, "lease held by another identity or token")
				return
			}
			recordOutcome(r.Context(), outcomeError)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		recordOutcome(r.Context(), outcomeReleased)
		w.WriteHeader(http.StatusNoContent)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
