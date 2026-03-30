# Berth

Distributed lease coordination service for Kubernetes multi-cluster workloads.

Berth provides TTL-based distributed leases that coordinate exclusive or shared
access to resources across Kubernetes clusters. Leases are expressed as
Kubernetes custom resources (`BerthLease`), managed via an API server, and
reconciled by an operator that can suspend or resume workloads in response to
lease state transitions.

## Components

Berth ships three binaries:

| Binary | Purpose |
|--------|---------|
| `apiserver` | HTTPS API server for lease operations |
| `operator` | Kubernetes controller that reconciles `BerthLease` resources |
| `berth` | CLI client for interacting with the API server |

## Concepts

### Lease Lifecycle

A lease moves through these states:

```
acquire ──► held ──► released
                 └──► expired (TTL without heartbeat)
```

1. A holder **acquires** a lease by creating a `BerthLease` resource with its
   identity and desired TTL.
2. While held, the holder sends periodic **heartbeats** to reset the TTL clock.
3. The holder **releases** the lease explicitly, or it **expires** when the TTL
   elapses without a heartbeat.

### Lease Semantics

Each lease declares an acquisition mode:

- **`at-most-once`** — guarantees that at most one holder can hold the lease at
  any time. Use this for leader election and exclusive resource access.
- **`at-least-once`** — permits concurrent holders. Use this for
  availability-oriented coordination where brief overlap is acceptable.

### Workload Targeting

A lease can optionally reference a Kubernetes workload via `target`. When
configured, the operator applies `acquireAction` and `releaseAction` to the
target in response to lease transitions. For example, suspending a CronJob
while a lease is held and resuming it on release.

## Usage

### Defining a BerthLease

```yaml
apiVersion: berth.skaphos.io/v1alpha1
kind: BerthLease
metadata:
  name: ingest-coordinator
  namespace: pipeline
spec:
  leaseName: "ingest-coordinator"
  holderIdentity: "worker-east-1"
  ttlSeconds: 30
  heartbeatIntervalSeconds: 10
  semantics: "at-most-once"
  target:
    apiVersion: batch/v1
    kind: CronJob
    name: ingest-pipeline
  acquireAction:
    suspend: false
  releaseAction:
    suspend: true
```

This lease:
- Is held exclusively (`at-most-once`) by `worker-east-1`.
- Expires after 30 seconds without a heartbeat.
- Expects heartbeats every 10 seconds.
- Unsuspends the `ingest-pipeline` CronJob when acquired, and suspends it when
  released or expired.

### Using the Go Client

The `pkg/client` package provides a Go client for the API server:

```go
import "github.com/skaphos/berth/pkg/client"

c := client.New("https://berth.example.com:8443",
    client.WithAPIKey("my-api-key"),
    client.WithTLSConfig(tlsCfg),
)

if err := c.Ping(ctx); err != nil {
    log.Fatal(err)
}
```

### Using the CLI

```bash
# List all leases
berth --api-server https://berth.example.com:8443 --api-key $BERTH_KEY lease list

# Get a specific lease
berth lease get ingest-coordinator

# Release a lease
berth lease release ingest-coordinator
```

## Deployment

### Prerequisites

- Kubernetes 1.28+
- Helm 3

### Install the CRD

```bash
kubectl apply -f config/crd/berthlease.yaml
```

### Deploy with Helm

```bash
# API server
helm install berth-apiserver deploy/helm/berth-apiserver \
  --set image.repository=your-registry/berth-apiserver \
  --set image.tag=latest

# Operator
helm install berth-operator deploy/helm/berth-operator \
  --set image.repository=your-registry/berth-operator \
  --set image.tag=latest
```

### API Server Flags

```
--listen-addr    Listen address (default ":8443")
--tls-cert-file  Path to TLS certificate (required)
--tls-key-file   Path to TLS private key (required)
--kubeconfig     Path to kubeconfig (omit for in-cluster)
```

### Operator Flags

```
--metrics-bind-address       Metrics endpoint (default ":8080")
--health-probe-bind-address  Health probe endpoint (default ":8081")
```

## Build

Requires Go 1.26+.

```bash
make build        # Build all binaries to bin/
make test         # Run tests
make lint         # Run golangci-lint and go vet
make generate     # Regenerate deepcopy code
make manifests    # Regenerate CRD manifests
make docker-build # Build Docker images
```

## Project Layout

```
api/v1alpha1/       Kubernetes CRD types (BerthLease, BerthLeaseList)
cmd/apiserver/      API server entrypoint
cmd/operator/       Operator entrypoint
cmd/berth/          CLI entrypoint
internal/api/       HTTP server, routes, middleware
internal/auth/      Authentication (Authenticator interface, static keys)
internal/lease/     Lease state, Store interface, Manager, TTL enforcement
internal/operator/  Kubernetes reconciler (BerthLeaseReconciler)
internal/tenant/    Tenant resolution (Resolver interface)
internal/console/   Web console server (placeholder)
internal/k8s/       Kubernetes client initialization
pkg/client/         Public Go client library
config/crd/         Generated CRD manifests
config/rbac/        RBAC manifests
deploy/helm/        Helm charts for API server and operator
```

## License

See [LICENSE](LICENSE).
