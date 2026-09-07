package auth

import (
	"context"
	"errors"
	"strings"
)

// Identity represents an authenticated caller. It carries the holder name
// used for lease operations, the tenant for namespace isolation, and the
// raw credential string.
type Identity struct {
	// Holder is the identity name used as the lease holder.
	Holder string
	// Tenant is the resolved tenant identifier for access control. It must not contain "/".
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

// ValidateTenant checks that a tenant cannot overlap another tenant's holder
// prefix. Tenant IDs are exact strings: "/" is reserved for holder scoping and
// is never decoded or normalized. Missing tenants are rejected by authorization.
func ValidateTenant(tenant string) error {
	if strings.Contains(tenant, "/") {
		return errors.New("tenant id must not contain '/' (reserved for holder scoping)")
	}
	return nil
}
