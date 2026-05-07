// Package auth provides authentication primitives for the Berth API server.
//
// The core abstraction is the [Authenticator] interface, which validates a
// bearer token and returns an [Identity] describing the authenticated
// caller.
//
// # Static API keys
//
// [StaticAuthenticator] is the production implementation. Keys are stored
// as SHA-256 hashes — both the on-disk file format and the in-memory
// representation are hashed — so raw token material lives only on the
// caller's side (e.g., a Kubernetes Secret mounted by the operator). Use
// [NewStaticAuthenticatorFromKeysFile] for the production deployment and
// [NewStaticAuthenticator] for tests.
//
// The keys file is one entry per line:
//
//	# comments and blank lines are ignored
//	team-a:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
//	team-b:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
//
// [StaticAuthenticator.Reload] re-reads the file on demand; the cmd/apiserver
// binary wires this to SIGHUP so operators can rotate keys without
// restarting the server.
//
// # Dev / no-auth
//
// [NoOpAuthenticator] accepts every request and returns a synthetic
// anonymous Identity. It is selected by `--auth-mode=none` and exists so
// the API server can run end-to-end against the in-memory lease store
// without secret material. cmd/apiserver logs a loud warning at startup
// when this authenticator is selected.
package auth
