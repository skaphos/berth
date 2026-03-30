// Package tenant provides tenant resolution for multi-tenant Berth
// deployments.
//
// The [Resolver] interface maps an authenticated [auth.Identity] to a
// tenant identifier. Implementations determine how callers are associated
// with tenants, enabling namespace isolation and access control for lease
// operations.
package tenant
