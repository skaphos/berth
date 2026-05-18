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

The `berth-operator` chart installs the BerthLease CRD by default
(`installCRDs: true`). For out-of-band CRD management — recommended when
you need control over CRD upgrades — apply it directly and disable the
chart-managed install:

```bash
kubectl apply -f config/crd/berthlease.yaml
# then: helm install ... --set installCRDs=false
```

### Deploy with Helm

The charts target the cross-cluster topology: one `berth-apiserver`
release in a control plane (with state in a coordination cluster) and a
`berth-operator` release in each tenant cluster.

#### Step 1 — install `berth-apiserver`

Pick a TLS source and a coordination backend. Minimal in-cluster
deployment (API server runs inside the coordination cluster, cert-manager
issues the serving cert, static API keys for auth):

```bash
helm install berth-apiserver deploy/helm/berth-apiserver \
  --namespace berth-system --create-namespace \
  --set coordination.namespace=berth-coordination \
  --set coordination.inCluster=true \
  --set tls.certManager.enabled=true \
  --set tls.certManager.issuerRef.name=berth-ca \
  --set tls.certManager.issuerRef.kind=ClusterIssuer \
  --set auth.mode=static-keys \
  --set auth.staticKeys.secretName=berth-api-keys
```

External coordination cluster (API server runs elsewhere, kubeconfig in
a Secret), OIDC auth, BYO TLS Secret:

```bash
helm install berth-apiserver deploy/helm/berth-apiserver \
  --namespace berth-system --create-namespace \
  --set coordination.namespace=berth-coordination \
  --set coordination.kubeconfig.secretName=berth-coordination-kubeconfig \
  --set tls.existingSecret=berth-apiserver-tls \
  --set auth.mode=oidc \
  --set auth.oidc.issuerURL=https://your-org.okta.com/oauth2/default \
  --set auth.oidc.audience=berth-api
```

The chart enforces invariants at render time: TLS source required, at
most one coordination backend, required Secret/ConfigMap names. Failures
surface as clear messages from `helm install`.

#### Step 2 — install `berth-operator` in each tenant cluster

Each cluster must pass a **distinct** `clusterID`:

```bash
# East cluster
helm install berth-operator deploy/helm/berth-operator \
  --namespace berth-system --create-namespace \
  --set clusterID=cluster-east \
  --set berth.apiServer=https://berth.example.com:8443 \
  --set berth.apiKey.secretName=berth-api-key \
  --set berth.tls.caBundleConfigMap=berth-ca-bundle

# West cluster — same values modulo clusterID
helm install berth-operator deploy/helm/berth-operator \
  --namespace berth-system --create-namespace \
  --set clusterID=cluster-west \
  --set berth.apiServer=https://berth.example.com:8443 \
  --set berth.apiKey.secretName=berth-api-key \
  --set berth.tls.caBundleConfigMap=berth-ca-bundle
```

To run with OIDC instead of a static key, enable the bundled token-broker
sidecar:

```bash
helm install berth-operator deploy/helm/berth-operator \
  --namespace berth-system --create-namespace \
  --set clusterID=cluster-east \
  --set berth.apiServer=https://berth.example.com:8443 \
  --set berth.tls.caBundleConfigMap=berth-ca-bundle \
  --set sidecarBroker.enabled=true \
  --set sidecarBroker.oidc.issuerURL=https://your-org.okta.com/oauth2/default \
  --set sidecarBroker.oidc.audience=berth-api \
  --set sidecarBroker.oidc.clientID=berth-operator \
  --set sidecarBroker.oidc.clientSecret.secretName=berth-oidc-client
```

See `deploy/helm/berth-apiserver/values.yaml` and
`deploy/helm/berth-operator/values.yaml` for the full value reference and
inline documentation.

### API Server Flags

```
--listen-addr               Listen address (default ":8443")
--tls-cert-file             Path to TLS certificate (required)
--tls-key-file              Path to TLS private key (required)
--store-backend             Lease store backend: 'mem' (in-memory; dev only),
                            'k8s' (coordination.k8s.io/v1.Lease in a separate
                            cluster), or 'sql' (Postgres / MariaDB / SQLite).
                            When unset, a legacy heuristic applies: empty
                            --coordination-namespace → 'mem', set → 'k8s';
                            a deprecation warning is logged. The implicit
                            fallback will be removed one release after the
                            SQL backend ships. Set explicitly.
--coordination-kubeconfig   Path to a kubeconfig pointing at the coordination
                            cluster (empty = in-cluster config). Only valid
                            with --store-backend=k8s.
--coordination-namespace    Namespace in the coordination cluster where Berth
                            Lease objects are stored. Required when
                            --store-backend=k8s.
--sql-driver                SQL driver: 'postgres', 'mysql', or 'sqlite'.
                            Required when --store-backend=sql.
--sql-dsn                   SQL DSN (e.g. 'postgres://user:pass@host/berth').
                            Mutually exclusive with --sql-dsn-file. Only
                            valid with --store-backend=sql.
--sql-dsn-file              Path to a file containing the SQL DSN. Re-read
                            so an external rotator can refresh credentials
                            without restarting the API server. Mutually
                            exclusive with --sql-dsn. Only valid with
                            --store-backend=sql.
--sql-migrate               'auto' (apply pending migrations at startup) or
                            'off' (fail fast on schema drift). Defaults to
                            'auto' when --store-backend=sql. Only valid with
                            --store-backend=sql.
--auth-mode                 'none', 'static-keys', or 'oidc'. Defaults to
                            'static-keys' when the resolved store backend
                            is 'k8s' or 'sql'; defaults to 'none' for 'mem'.
                            Use 'none' only for dev — the server logs a
                            loud warning at startup.
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
--berth-ca-bundle-file       Path to a PEM file with extra CAs trusted when
                             verifying the API server TLS chain. The bundle
                             is appended to the system trust store.
--berth-server-name          Override the SNI / TLS certificate name used
                             when connecting to the API server. Defaults to
                             the host in --berth-api-server.
--berth-insecure-skip-tls-verify
                             Development only. Disables API server TLS
                             verification entirely.
```

### Lease storage backend

The API server's lease state is authoritative for at-most-once semantics
across clusters. The backend is selected explicitly via `--store-backend`:

| Backend | When | Durability | HA |
|---|---|---|---|
| `--store-backend=k8s` | Cross-cluster topology with a dedicated coordination cluster | `coordination.k8s.io/v1.Lease` objects in `--coordination-namespace` | API server scales to multiple replicas; state is shared through the coordination kube-apiserver |
| `--store-backend=sql` | Runner-local topology (no separate Kubernetes coordination cluster). Drivers: `postgres`, `mysql`, `sqlite` | Rows in a SQL database supplied via `--sql-dsn` / `--sql-dsn-file` | Postgres / MariaDB: multi-replica safe; SQLite: single replica |
| `--store-backend=mem` | Dev / demo only | None — state is lost on restart | Single replica only |

For the `k8s` backend, point `--coordination-kubeconfig` at a small
dedicated cluster — **not** at one of the tenant clusters that Berth
coordinates Deployments on, since losing that cluster would also lose the
lease store. A managed control plane (EKS/GKE/AKS) is fine. Berth pools all
leases for all tenants under `--coordination-namespace`; the coordination
cluster does not need per-tenant namespaces.

The `sql` backend lands with SKA-316 — the flags accept their inputs
today but `--store-backend=sql` returns a `not implemented` startup error
until the SQL `lease.Store` ships.

> Implicit backend selection (no `--store-backend` flag, picking `mem` or
> `k8s` from `--coordination-namespace`) is deprecated and will be
> removed one release after the SQL backend ships. Always set
> `--store-backend` explicitly in new deployments.

## Build

Requires Go 1.26+. All tasks are orchestrated by [Task](https://taskfile.dev),
declared as a Go tool in `tools/go.mod` so contributors only need the
system Go toolchain.

```bash
go -C tools tool task --list   # see all targets

go -C tools tool task build         # build all four binaries to bin/
go -C tools tool task test          # unit tests with coverage profile
go -C tools tool task lint          # golangci-lint v2 (govet, staticcheck, errcheck, ...)
go -C tools tool task staticcheck   # staticcheck directly
go -C tools tool task vuln          # govulncheck
go -C tools tool task generate      # regenerate DeepCopy
go -C tools tool task manifests     # regenerate CRD manifests (+ chart copy)
go -C tools tool task docker-build  # build all three container images locally
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
