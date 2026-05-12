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
target in response to lease transitions. Two action shapes are supported, and
at most one may be set per action:

- `suspend` — toggles `spec.suspend` on the target. Use for CronJob.
- `scale` — patches the target's scale subresource. Use for Deployment,
  StatefulSet, or ReplicaSet. A typical singleton wires
  `acquireAction.scale.replicas` to the desired running count and
  `releaseAction.scale.replicas: 0`.

## Usage

### Defining a BerthLease

**CronJob singleton (suspend action):**

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

**Cross-cluster Deployment singleton (scale action):**

```yaml
apiVersion: berth.skaphos.io/v1alpha1
kind: BerthLease
metadata:
  name: ingest-worker
  namespace: pipeline
spec:
  leaseName: "ingest-worker"
  holderIdentity: "ignored-when-operator-runs-with-cluster-id"
  ttlSeconds: 30
  heartbeatIntervalSeconds: 10
  semantics: "at-most-once"
  target:
    apiVersion: apps/v1
    kind: Deployment
    name: ingest-worker
  acquireAction:
    scale:
      replicas: 3
  releaseAction:
    scale:
      replicas: 0
```

Apply the **same** manifest unchanged to every cluster. Each cluster's operator
must run with a distinct `--cluster-id`:

```bash
# cluster-east
operator --berth-api-server https://berth.example.com:8443 --cluster-id cluster-east

# cluster-west
operator --berth-api-server https://berth.example.com:8443 --cluster-id cluster-west
```

`--cluster-id`, when set, overrides `spec.holderIdentity` and is used as the
holder identity for every Acquire call. Only one cluster's operator will hold
the lease at a time and scale its Deployment to 3 replicas; the others scale
to 0. When `--cluster-id` is not set, the operator falls back to
`spec.holderIdentity` — useful when an external client manages identity
itself.

#### Failure modes and recovery time

Failover RTO is bounded by `ttlSeconds + reacquire interval`. With the
example above (`ttlSeconds: 30`, `heartbeatIntervalSeconds: 10`):

- The holder cluster heartbeats every 10 seconds.
- If the holder dies or is partitioned, the lease becomes reclaimable
  30 seconds after its last successful heartbeat.
- Standby operators retry Acquire every `min(heartbeatIntervalSeconds,
  ttlSeconds/3)` — 10 seconds in this case — so within ~40 seconds total a
  standby cluster acquires and scales its Deployment up.

Tune `ttlSeconds` to trade off failover speed against tolerance for
transient API-server unreachability. A 30-second TTL is a reasonable
default; shorter TTLs (≤10s) make the system jittery under network
hiccups, longer ones (≥60s) extend failover time.

**Split-brain window.** When the holder loses connectivity to the API
server, its Deployment continues running until that operator next
reconciles and observes its Acquire return `Acquired=false`. During the
window between (a) server-side TTL expiry, (b) the standby successfully
reacquiring and scaling up, and (c) the original holder noticing it lost
the lease and scaling down, **both clusters can be running their
Deployment**. Two mitigations:

1. **Short TTL + short heartbeat** narrow the window. With the defaults
   above the worst case is ~10 seconds (one reconcile cycle).
2. **Fencing tokens** are returned by Acquire/Renew. The Berth API
   server itself rejects writes from a stale holder (the operator can't
   accidentally Release/Renew a lease it has lost). True end-to-end
   fencing — where the Deployment's downstream calls are also rejected
   when stale — requires the workload to validate the token, which is
   out of scope for the operator-as-holder pattern.

For workloads where momentary overlap is unacceptable, run with a very
short TTL or use the application-level pattern where the workload
itself acquires the lease (and exits when it loses it).

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
--listen-addr               Listen address (default ":8443")
--tls-cert-file             Path to TLS certificate (required)
--tls-key-file              Path to TLS private key (required)
--coordination-kubeconfig   Path to a kubeconfig pointing at the coordination
                            cluster (empty = in-cluster config)
--coordination-namespace    Namespace in the coordination cluster where Berth
                            Lease objects are stored. When empty, the API
                            server falls back to an in-memory store (dev only
                            — state is lost on restart and HA is not possible).
--auth-mode                 'none', 'static-keys', or 'oidc'. Defaults to
                            'static-keys' when --coordination-namespace is set;
                            defaults to 'none' otherwise. Use 'none' only for
                            dev — the server logs a loud warning at startup.
--api-keys-file             Path to a file of '<key-id>:<sha256-hex>' entries.
                            Required when --auth-mode=static-keys. SIGHUP
                            reloads the file in place (no restart needed).
--oidc-issuer-url           OIDC issuer URL (e.g. https://your-org.okta.com/oauth2/default,
                            https://pingfed.example.com). Required when --auth-mode=oidc.
--oidc-audience             Expected JWT 'aud' claim. Required when --auth-mode=oidc.
--oidc-required-claim       Repeatable key=value claim that must be present
                            (string or string-array). Example: groups=berth-clients.
--oidc-username-claim       JWT claim copied into the identity holder field
                            (default 'sub').
--oidc-tenant-claim         JWT claim copied into the identity tenant field
                            (default 'sub'); array-valued claims use the first element.
--oidc-jwks-url             Override the JWKS URL discovered from the issuer
                            (rarely needed).
```

### Authentication

The API server accepts bearer-token auth on the `/v1alpha1/*` endpoints when
`--auth-mode=static-keys` is set (the default in production). `/healthz`
remains unauthenticated.

The `--api-keys-file` is a plain-text file with one entry per line:

```
# Berth API keys — comments and blank lines ignored.
team-a:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
team-b:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
```

The hash is the SHA-256 of the raw token. The API server only stores hashes;
the raw token lives only on the client side (operator-mounted Secret).
Generate a key like this:

```bash
RAW=$(openssl rand -hex 32)
HASH=$(printf '%s' "$RAW" | sha256sum | awk '{print $1}')
echo "team-a:$HASH"   # add to the keys file
echo "$RAW"           # distribute via the operator's --berth-api-key Secret
```

Rotate keys by editing the file and sending `SIGHUP` to the API server pod.
The current key set is replaced atomically; if the new file is malformed,
the previous key set is preserved.

#### OIDC (Okta, PingFederate, Entra, etc.)

For production deployments where you want short-lived, IdP-issued tokens
instead of long-lived static keys, run the API server with OIDC:

```
berth-apiserver \
  --auth-mode=oidc \
  --oidc-issuer-url=https://your-org.okta.com/oauth2/default \
  --oidc-audience=berth-api \
  --oidc-required-claim=groups=berth-clients
```

For PingFederate, swap the issuer URL: `--oidc-issuer-url=https://pingfed.example.com`.
For Entra (Azure AD): `https://login.microsoftonline.com/<tenant-id>/v2.0`.
Berth fetches `<issuer>/.well-known/openid-configuration` at startup,
validates JWT signature against the JWKS, and rejects tokens with the
wrong `iss`/`aud`/`exp` or missing required claims.

The operator side then uses a sidecar token broker. Berth ships a
reference broker as `berth-oidc-broker`:

```yaml
# operator pod sketch
spec:
  containers:
    - name: token-broker
      image: ghcr.io/skaphos/berth-oidc-broker:latest
      args:
        - --oidc-issuer-url=https://your-org.okta.com/oauth2/default
        - --oidc-client-id=$(OIDC_CLIENT_ID)
        - --oidc-client-secret-file=/etc/berth-oidc/secret
        - --oidc-audience=berth-api
        - --output=/var/run/berth/token
      env:
        - { name: OIDC_CLIENT_ID, valueFrom: { secretKeyRef: { name: berth-oidc, key: client-id } } }
      volumeMounts:
        - { name: token, mountPath: /var/run/berth }
        - { name: oidc-secret, mountPath: /etc/berth-oidc, readOnly: true }
    - name: operator
      image: ghcr.io/skaphos/berth-operator:latest
      args:
        - --berth-api-server=https://berth.example.com:8443
        - --berth-api-key-file=/var/run/berth/token
        - --cluster-id=cluster-east
      volumeMounts:
        - { name: token, mountPath: /var/run/berth, readOnly: true }
  volumes:
    - { name: token, emptyDir: { medium: Memory } }
    - { name: oidc-secret, secret: { secretName: berth-oidc } }
```

The broker performs OAuth2 client credentials against the IdP, writes
the access token atomically to the shared `Memory`-backed volume, and
refreshes well before expiry. The operator picks up rotations via its
`--berth-api-key-file` watcher (1-second cache TTL).

For Entra/Azure AD, AWS Cognito, Google Cloud, and other IdPs that need
extra parameters on the token request, the broker accepts
`--oidc-audience` (passed as the `audience` form parameter, which is what
Auth0 and some Okta authorization servers require) and `--oidc-scopes`.
For more exotic flows (token exchange, certificate-bound tokens) you can
substitute your own broker — the operator only cares that the file at
`--berth-api-key-file` contains a valid bearer token.

### Operator Flags

```
--metrics-bind-address       Metrics endpoint (default ":8080")
--health-probe-bind-address  Health probe endpoint (default ":8081")
--berth-api-server           Berth API server base URL (required)
--berth-api-key              Static bearer token. Mutually exclusive with
                             --berth-api-key-file.
--berth-api-key-file         Path to a file containing the bearer token.
                             Re-read on each request (cached briefly), so an
                             external sidecar (typically the OIDC broker)
                             can rotate it without restarting the operator.
                             Mutually exclusive with --berth-api-key.
--cluster-id                 Cluster-distinct holder identity. When set,
                             overrides spec.holderIdentity on every Acquire
                             call. Required for the cross-cluster singleton
                             pattern; leave empty to fall back to
                             spec.holderIdentity.
```

### Lease storage backend

The API server's lease state is authoritative for at-most-once semantics
across clusters. Two backends are available:

| Backend | When | Durability | HA |
|---|---|---|---|
| **K8s coordination cluster** (default in production) | `--coordination-namespace` is set | State persists in `coordination.k8s.io/v1.Lease` objects in the named namespace | API server can be scaled to multiple replicas; they share state via the kube-apiserver |
| **In-memory** (dev/demo only) | `--coordination-namespace` is empty | None — state is lost on restart | Single replica only |

For the production backend, point `--coordination-kubeconfig` at a small
dedicated cluster — **not** at one of the tenant clusters that Berth
coordinates Deployments on, since losing that cluster would also lose the
lease store. A managed control plane (EKS/GKE/AKS) is fine. Berth pools all
leases for all tenants under `--coordination-namespace`; the coordination
cluster does not need per-tenant namespaces.

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
