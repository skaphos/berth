package auth

import "context"

// Identity represents an authenticated caller. It carries the holder name
// used for lease operations, the tenant for namespace isolation, and the
// raw credential string.
type Identity struct {
	// Holder is the identity name used as the lease holder.
	Holder string
	// Tenant is the resolved tenant identifier for access control.
	Tenant string
	// Raw is the original credential string presented by the caller.
	Raw string
}

// Authenticator validates a bearer token and returns the corresponding
// [Identity]. Implementations return a non-nil error when authentication
// fails.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*Identity, error)
}
