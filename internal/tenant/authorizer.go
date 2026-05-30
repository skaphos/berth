package tenant

import (
	"errors"
	"fmt"
	"strings"

	"github.com/skaphos/berth/internal/auth"
)

// Authorizer decides whether an authenticated identity may operate on a given
// lease namespace and act as a given holder. It is consulted by the API server
// only for authenticated requests; in auth-mode=none there is no identity and
// authorization is bypassed entirely.
//
// Both methods return nil to allow and a non-nil error to deny. The error is
// for server-side logging only — handlers translate any denial into a generic
// 403 so the response cannot be used to probe the policy.
type Authorizer interface {
	// AuthorizeNamespace reports whether id may operate on leases in namespace.
	AuthorizeNamespace(id *auth.Identity, namespace string) error
	// AuthorizeHolder reports whether id may act as the given lease holder.
	AuthorizeHolder(id *auth.Identity, holder string) error
}

// IdentityResolver is the default [Resolver]: it returns the tenant carried on
// the authenticated identity verbatim. Authenticators populate Identity.Tenant
// (static keys use the key id; OIDC uses the configured tenant claim), so for
// the common deployment the identity already names its own tenant.
type IdentityResolver struct{}

// ResolveTenant returns id.Tenant, or an error if the identity is missing or
// carries no tenant (fail closed — an unidentifiable caller authorizes nothing).
func (IdentityResolver) ResolveTenant(id *auth.Identity) (string, error) {
	if id == nil {
		return "", errors.New("nil identity")
	}
	if id.Tenant == "" {
		return "", errors.New("identity carries no tenant")
	}
	return id.Tenant, nil
}

// DefaultAuthorizer is the out-of-the-box policy: permissive on namespace,
// tenant-scoped on holder.
//
//   - Namespace: any authenticated identity may operate on any namespace. The
//     cross-cluster failover model has distinct clusters (distinct tenants)
//     contend for the same namespace/name, so a namespace gate would break it.
//   - Holder: the holder must be owned by the caller's tenant, meaning it equals
//     the tenant or is prefixed "<tenant>/". This is the cross-tenant guard: a
//     caller cannot acquire, renew, or release a lease as a holder that belongs
//     to another tenant (e.g. impersonating it after expiry).
//
// Stricter policies (e.g. a tenant→namespace allow-list) can be introduced as
// alternative Authorizer implementations without touching the HTTP layer.
type DefaultAuthorizer struct {
	resolver Resolver
}

// NewDefaultAuthorizer returns a [DefaultAuthorizer] backed by the
// identity-passthrough [IdentityResolver].
func NewDefaultAuthorizer() *DefaultAuthorizer {
	return &DefaultAuthorizer{resolver: IdentityResolver{}}
}

// AuthorizeNamespace allows any identity that resolves to a tenant. It does not
// restrict which namespace that tenant may use; holder ownership is the guard.
func (a *DefaultAuthorizer) AuthorizeNamespace(id *auth.Identity, _ string) error {
	_, err := a.resolver.ResolveTenant(id)
	return err
}

// AuthorizeHolder allows holders owned by the caller's tenant: the bare tenant
// name, or any "<tenant>/..." sub-holder. Everything else is denied.
func (a *DefaultAuthorizer) AuthorizeHolder(id *auth.Identity, holder string) error {
	tenant, err := a.resolver.ResolveTenant(id)
	if err != nil {
		return err
	}
	if holder == tenant || strings.HasPrefix(holder, tenant+"/") {
		return nil
	}
	return fmt.Errorf("holder %q is not within tenant %q", holder, tenant)
}
