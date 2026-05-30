package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/skaphos/berth/internal/auth"
)

// ChainMiddleware wraps handler with the given middleware functions in
// left-to-right order. The first middleware in the list is the outermost
// wrapper.
func ChainMiddleware(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	wrapped := handler
	for i := len(middleware) - 1; i >= 0; i-- {
		wrapped = middleware[i](wrapped)
	}
	return wrapped
}

// AuthMiddleware returns an HTTP middleware that authenticates requests
// using authn. The token is extracted from the Authorization header
// (Bearer scheme). On success, the resulting [auth.Identity] is attached
// to the request context (retrievable via [IdentityFromContext]). On any
// failure the middleware short-circuits with a 401 JSON envelope.
//
// Pass a non-nil Authenticator to produce a working middleware. To
// disable authentication entirely (dev mode), pass `nil` for the
// `authn` parameter to [NewMux] instead of using this middleware.
func AuthMiddleware(authn auth.Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				recordOutcome(r.Context(), outcomeUnauthorized)
				writeError(w, http.StatusUnauthorized, "missing or malformed Authorization header")
				return
			}
			id, err := authn.Authenticate(r.Context(), token)
			if err != nil {
				// Deliberately generic so the response can't be used as an
				// oracle to enumerate valid key ids.
				recordOutcome(r.Context(), outcomeUnauthorized)
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			recordIdentity(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

// bearerToken extracts the token from an Authorization: Bearer header.
// Returns ("", false) if the header is missing, uses a different scheme,
// or has an empty token.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

type identityCtxKey struct{}

// WithIdentity returns a copy of ctx carrying the authenticated identity.
func WithIdentity(ctx context.Context, id *auth.Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// IdentityFromContext returns the authenticated identity attached to ctx,
// or nil if the request was unauthenticated (no-auth mode).
func IdentityFromContext(ctx context.Context) *auth.Identity {
	id, _ := ctx.Value(identityCtxKey{}).(*auth.Identity)
	return id
}
