package tenant

import "github.com/skaphos/berth/internal/auth"

// Resolver maps an authenticated identity to a tenant identifier.
// Implementations control how callers are associated with tenants for
// namespace isolation and lease access control.
type Resolver interface {
	// ResolveTenant returns the tenant identifier for the given identity.
	ResolveTenant(identity *auth.Identity) (string, error)
}
