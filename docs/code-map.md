# Code Map

This is a contributor map of the repository. It describes ownership by path
and points to the files most likely to matter when changing behavior.

## Entrypoints

| Path | Owns |
| --- | --- |
| `cmd/apiserver/main.go` | API server flags, TLS startup, auth construction, store construction, signal handling. |
| `cmd/apiserver/store.go` | Store backend resolution and validation. |
| `cmd/operator/main.go` | Controller-runtime manager setup, operator flags, TLS and token source wiring. |
| `cmd/berth/main.go` | Cobra CLI scaffold. |
| `cmd/berth-oidc-broker/main.go` | OIDC client-credentials token refresh loop and atomic token-file writes. |

## API and Client

| Path | Owns |
| --- | --- |
| `internal/api/routes.go` | HTTP mux, unauthenticated health route, auth wrapping for lease routes. |
| `internal/api/leases.go` | Lease HTTP request and response schema plus acquire, renew, release handlers. |
| `internal/api/middleware.go` | Bearer token extraction and request identity context. |
| `pkg/client` | Public Go client for direct lease integration. |

## Lease Core

| Path | Owns |
| --- | --- |
| `internal/lease/store.go` | `Store`, `Record`, and `Key` contracts. |
| `internal/lease/manager.go` | Acquire, renew, release, expiry, and fencing-token behavior. |
| `internal/lease/memstore.go` | In-memory store for tests and development. |
| `internal/lease/k8sstore.go` | Kubernetes `coordination.k8s.io/v1.Lease` store implementation. |
| `internal/lease/ttl.go` | Background expiry scan loop. |

## Authentication

| Path | Owns |
| --- | --- |
| `internal/auth/auth.go` | Authenticator and identity interfaces. |
| `internal/auth/noauth.go` | Development-only no-auth implementation. |
| `internal/auth/static.go` | Static API-key file loading, hashing, and reload behavior. |
| `internal/auth/oidc.go` | OIDC discovery, JWT validation, and required-claim checks. |
| `internal/operator/tokensource.go` | Operator token-file reads with a short cache for sidecar rotation. |
| `internal/operator/tlsconfig.go` | Operator API-server TLS client configuration. |

## Operator

| Path | Owns |
| --- | --- |
| `api/v1alpha1/types.go` | `BerthLease` API schema and generated CRD source comments. |
| `internal/operator/reconciler.go` | `BerthLease` reconcile lifecycle, finalizer, status, and requeue policy. |
| `internal/operator/actions.go` | Suspend and scale action application. |
| `internal/operator/leaseclient.go` | Interface between reconciler and central lease client. |

## Deployment

| Path | Owns |
| --- | --- |
| `config/crd/berthlease.yaml` | Generated CRD manifest. |
| `config/rbac` | Static RBAC manifests. |
| `deploy/helm/berth-apiserver` | API server Helm chart. |
| `deploy/helm/berth-operator` | Operator Helm chart and bundled CRD copy. |
| `.github/workflows/ci.yaml` | DCO, REUSE, generated drift, lint, test, static analysis, and build gates. |
| `.github/workflows/charts.yaml` | Helm chart validation. |
| `.github/workflows/e2e.yaml` | Cross-cluster end-to-end workflow. |
| `.github/workflows/release-*.yml`, `.github/workflows/release.yml` | Release Please, tag push, and artifact publishing. |

## Tests

Unit tests live next to the package under test. The end-to-end harness lives in
`test/e2e` and uses three kind clusters: coordination, east, and west.

Use:

```bash
go -C tools tool task test
go -C tools tool task e2e-all
```
