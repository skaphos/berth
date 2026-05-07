package auth

import "context"

// NoOpAuthenticator is an [Authenticator] that accepts every request,
// including those with no Authorization header (the API server's
// middleware can bypass header parsing when the configured authenticator
// is this type — see internal/api).
//
// It exists for `--auth-mode=none` development setups alongside the
// in-memory lease store. cmd/apiserver logs a loud startup warning when
// this authenticator is selected so production deployments cannot
// silently end up unauthenticated.
type NoOpAuthenticator struct{}

// Authenticate returns a synthetic anonymous identity regardless of the
// supplied token (which may be empty).
func (NoOpAuthenticator) Authenticate(_ context.Context, _ string) (*Identity, error) {
	return &Identity{Holder: "anonymous", Tenant: "anonymous"}, nil
}
