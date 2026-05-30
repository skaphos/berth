package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/skaphos/berth/internal/auth"
)

// Lease outcome labels. They name a request's *semantic* result, which HTTP
// status alone cannot express — notably held-by-other, a denied contender that
// still returns 200. Recorded on the per-request context by the handlers (and
// auth middleware) and consumed by the logging and metrics middleware.
const (
	outcomeAcquired     = "acquired"
	outcomeHeldByOther  = "held-by-other"
	outcomeRenewed      = "renewed"
	outcomeReleased     = "released"
	outcomeConflict     = "conflict"
	outcomeUnauthorized = "unauthorized"
	outcomeError        = "error"
)

// requestContext is per-request observability state threaded through the
// middleware chain via the request context. The outermost observability
// middleware creates it; inner layers (auth, the handlers) annotate it; the
// outer layers read it back after the handler returns. It is a pointer shared
// by reference, so annotations made deep in the chain are visible to the outer
// middleware even though context values only flow downward.
type requestContext struct {
	id       string
	identity *auth.Identity
	outcome  string
	// errDetail holds the unredacted server-side error for a 5xx. The logging
	// middleware emits it (at error level) so the client can be handed a generic
	// body — the backend kind and topology never cross the wire.
	errDetail string
}

type requestContextKey struct{}

// ensureRequestContext returns the request carrying a *requestContext, creating
// and attaching one if absent. It is idempotent so either observability
// middleware can be the outermost without double-allocating.
func ensureRequestContext(r *http.Request) (*http.Request, *requestContext) {
	if rc := requestContextFrom(r.Context()); rc != nil {
		return r, rc
	}
	rc := &requestContext{id: requestID(r)}
	return r.WithContext(context.WithValue(r.Context(), requestContextKey{}, rc)), rc
}

func requestContextFrom(ctx context.Context) *requestContext {
	rc, _ := ctx.Value(requestContextKey{}).(*requestContext)
	return rc
}

// RequestIDFromContext returns the correlation id assigned to the request, or
// "" if no observability middleware is installed. Handlers use it to correlate
// a client-facing response with the server-side log line.
func RequestIDFromContext(ctx context.Context) string {
	if rc := requestContextFrom(ctx); rc != nil {
		return rc.id
	}
	return ""
}

// recordOutcome tags the request's semantic outcome for the observability
// middleware. The last writer wins; handlers call it once per request.
func recordOutcome(ctx context.Context, outcome string) {
	if rc := requestContextFrom(ctx); rc != nil {
		rc.outcome = outcome
	}
}

// recordError stashes the detailed error for a failed request so the logging
// middleware can record it server-side, keyed by the same correlation id the
// client receives. The detail is never written to the response body.
func recordError(ctx context.Context, err error) {
	if rc := requestContextFrom(ctx); rc != nil {
		rc.errDetail = err.Error()
	}
}

// recordIdentity attaches the authenticated identity so the logging middleware
// can emit the holder and tenant (never the token).
func recordIdentity(ctx context.Context, id *auth.Identity) {
	if rc := requestContextFrom(ctx); rc != nil {
		rc.identity = id
	}
}

// requestID derives a correlation id: an inbound, well-formed X-Request-Id is
// honored; otherwise the trace-id from a W3C traceparent header; otherwise a
// fresh random id. Inbound values are constrained to a safe charset and length
// so they cannot inject into log output.
func requestID(r *http.Request) string {
	if v := r.Header.Get("X-Request-Id"); isSafeRequestID(v) {
		return v
	}
	if tp := r.Header.Get("traceparent"); tp != "" {
		// version "-" trace-id "-" parent-id "-" flags; trace-id is 32 hex chars.
		if id := traceID(tp); id != "" {
			return id
		}
	}
	return randomID()
}

func traceID(traceparent string) string {
	const versionLen, traceIDLen = 2, 32
	// Expect at least "vv-<32 hex>-": skip the version and dash, take 32 hex.
	if len(traceparent) < versionLen+1+traceIDLen+1 || traceparent[versionLen] != '-' {
		return ""
	}
	id := traceparent[versionLen+1 : versionLen+1+traceIDLen]
	if traceparent[versionLen+1+traceIDLen] != '-' || !isHex(id) {
		return ""
	}
	return id
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// isSafeRequestID accepts a bounded id of [A-Za-z0-9_-] so an attacker cannot
// smuggle newlines or control characters into the logs through the header.
func isSafeRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return len(s) > 0
}

// LoggingMiddleware emits one structured access-log line per request: method,
// path, matched route, status, latency, correlation id, and — when the caller
// authenticated — the holder and tenant. It never logs the bearer token. It is
// meant to be the outermost wrapper so it observes the final status (including
// auth rejections) and owns the correlation id, which it also echoes in the
// X-Request-Id response header. /healthz is logged at debug to keep liveness
// probes out of the steady-state log.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, rc := ensureRequestContext(r)
			w.Header().Set("X-Request-Id", rc.id)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)

			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("route", route),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", rc.id),
			}
			if rc.outcome != "" {
				attrs = append(attrs, slog.String("outcome", rc.outcome))
			}
			if rc.identity != nil {
				attrs = append(attrs, slog.String("holder", rc.identity.Holder), slog.String("tenant", rc.identity.Tenant))
			}

			// A 5xx carries its unredacted detail here only — the client got a
			// generic envelope. The error level and shared request_id let an
			// operator find this line from the id echoed in that envelope.
			level := slog.LevelInfo
			switch {
			case rc.errDetail != "":
				attrs = append(attrs, slog.String("error", rc.errDetail))
				level = slog.LevelError
			case r.URL.Path == "/healthz":
				level = slog.LevelDebug
			}
			logger.LogAttrs(r.Context(), level, "api request", attrsToLogAttrs(attrs)...)
		})
	}
}

// attrsToLogAttrs narrows the []any built above to []slog.Attr for LogAttrs,
// which avoids the alternating key/value allocation of the variadic form.
func attrsToLogAttrs(attrs []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if at, ok := a.(slog.Attr); ok {
			out = append(out, at)
		}
	}
	return out
}
