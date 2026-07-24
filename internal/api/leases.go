package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/internal/tenant"
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

// errorResponse is the standard error envelope. RequestID is set on 5xx
// responses so an operator can correlate the generic client-facing error with
// the detailed server-side log line; it is omitted when empty.
type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"requestId,omitempty"`
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
// is non-nil, each handler is composed through it (used for auth) and authz
// gates the authenticated identity against the request namespace and holder.
func registerLeaseRoutes(mux *http.ServeMux, mgr LeaseManager, wrap func(http.Handler) http.Handler, authz tenant.Authorizer) {
	register := func(pattern string, h http.HandlerFunc) {
		var handler http.Handler = h
		if wrap != nil {
			handler = wrap(handler)
		}
		mux.Handle(pattern, handler)
	}
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/acquire", handleAcquire(mgr, authz))
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/renew", handleRenew(mgr, authz))
	register("POST /v1alpha1/namespaces/{namespace}/leases/{name}/release", handleRelease(mgr, authz))
}

// authorize gates an authenticated request against the namespace and holder it
// targets. It returns true (allow) when no identity is present — that is only
// possible in auth-mode=none, where AuthMiddleware is not installed and no
// identity is ever attached. When an identity is present, authz must be non-nil
// (NewMux guarantees this whenever authentication is enabled); a nil authz fails
// closed. On any denial it writes a generic 403 and returns false.
func authorize(w http.ResponseWriter, r *http.Request, authz tenant.Authorizer, namespace, holder string) bool {
	id := IdentityFromContext(r.Context())
	if id == nil {
		return true
	}
	if authz == nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if err := authz.AuthorizeNamespace(id, namespace); err != nil {
		writeError(w, http.StatusForbidden, "not authorized for this namespace")
		return false
	}
	if err := authz.AuthorizeHolder(id, holder); err != nil {
		writeError(w, http.StatusForbidden, "holder not authorized for this identity")
		return false
	}
	return true
}

func handleAcquire(mgr LeaseManager, authz tenant.Authorizer) http.HandlerFunc {
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
		if !authorize(w, r, authz, key.Namespace, req.Holder) {
			return
		}
		if !validateKey(w, r, key) {
			return
		}
		res, err := mgr.Acquire(r.Context(), key, req.Holder, time.Duration(req.TTLSeconds)*time.Second)
		if err != nil {
			writeInternalError(w, r, err)
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

func handleRenew(mgr LeaseManager, authz tenant.Authorizer) http.HandlerFunc {
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
		if !authorize(w, r, authz, key.Namespace, req.Holder) {
			return
		}
		if !validateKey(w, r, key) {
			return
		}
		res, err := mgr.Renew(r.Context(), key, req.Holder, req.FencingToken, time.Duration(req.TTLSeconds)*time.Second)
		if err != nil {
			writeInternalError(w, r, err)
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

func handleRelease(mgr LeaseManager, authz tenant.Authorizer) http.HandlerFunc {
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
		if !authorize(w, r, authz, key.Namespace, req.Holder) {
			return
		}
		if !validateKey(w, r, key) {
			return
		}
		err := mgr.Release(r.Context(), key, req.Holder, req.FencingToken)
		if err != nil {
			if errors.Is(err, lease.ErrConflict) {
				recordOutcome(r.Context(), outcomeConflict)
				writeError(w, http.StatusConflict, "lease held by another identity or token")
				return
			}
			writeInternalError(w, r, err)
			return
		}
		recordOutcome(r.Context(), outcomeReleased)
		w.WriteHeader(http.StatusNoContent)
	}
}

// validateKey rejects malformed lease keys with a 400 naming the offending
// field and allowed format. It runs after authorize so authorization
// failures keep returning 403 first (no pre-auth probing of key rules), and
// before any manager call so an invalid key never reaches a store — in the
// k8s backend an unvalidated dotted namespace would make two distinct keys
// collide into one backing object across tenants.
func validateKey(w http.ResponseWriter, r *http.Request, key lease.Key) bool {
	if err := lease.ValidateKey(key); err != nil {
		recordOutcome(r.Context(), outcomeInvalidKey)
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
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

// writeInternalError handles an unexpected backend failure: it records the
// detailed err for the server-side log (keyed by the request's correlation id)
// and returns a deliberately generic 500 envelope carrying only that id. The
// backend kind and topology embedded in err never reach the client.
//
// The 5xx contract — a generic body that always carries a correlation id, with
// the detail kept server-side — must hold even when no observability middleware
// is installed (NewMux used directly by a test or a package client). In that
// case there is no request context to carry the id or the detail, so this
// helper synthesizes an id and logs the detail itself rather than dropping both.
func writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	recordOutcome(ctx, outcomeError)
	recordError(ctx, err)

	id := RequestIDFromContext(ctx)
	if id == "" {
		// No observability middleware in this chain: recordError had nowhere to
		// stash the detail and no id was assigned. Mint one and log here so the
		// operator can still correlate the generic response to the cause.
		id = requestID(r)
		slog.Default().LogAttrs(ctx, slog.LevelError, "api request error",
			slog.String("request_id", id),
			slog.String("error", err.Error()),
		)
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{
		Error:     "internal error",
		RequestID: id,
	})
}
