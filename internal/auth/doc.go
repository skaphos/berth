// Package auth provides authentication primitives for the Berth API server.
//
// The core abstraction is the [Authenticator] interface, which validates a
// bearer token and returns an [Identity] describing the authenticated caller.
// Identity carries the holder name, tenant, and the raw token string.
//
// # Static Authentication
//
// [StaticAuthenticator] implements [Authenticator] using a fixed map of
// API keys to identities. It is intended for development, testing, and
// single-tenant deployments where external identity providers are not
// required.
//
//	keys := map[string]auth.Identity{
//	    "secret-key": {Holder: "worker-1", Tenant: "acme"},
//	}
//	authn := auth.NewStaticAuthenticator(keys)
//	id, err := authn.Authenticate(ctx, "secret-key")
package auth
