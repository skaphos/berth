# Architecture

Berth coordinates Kubernetes workloads that run in more than one cluster and
need a shared decision about which cluster may act. Kubernetes
`coordination.k8s.io/Lease` objects are cluster-local, so identical workloads
in separate clusters cannot coordinate with native Kubernetes leases alone.

Berth adds a central lease API and an optional per-cluster operator. The API
server owns authoritative lease state. Each tenant cluster runs an operator
that competes for a lease and applies workload actions locally based on whether
it holds that lease.

## Goals

- Provide cross-cluster mutual exclusion for named workloads or operations.
- Keep the authoritative state Kubernetes-native when deployed with the `k8s`
  backend.
- Let users choose direct API integration or declarative `BerthLease`
  resources.
- Support static bearer tokens and OIDC-issued bearer tokens.

## Non-Goals

- Berth is not a scheduler. It decides who may act, not when work is created.
- Berth is not a queue or message broker.
- Berth does not provide stronger fault tolerance than the selected backing
  store.
- The current CLI is not the primary operational interface; its lease commands
  are still stubs.

## Runtime Components

| Component | Path | Runtime role |
| --- | --- | --- |
| API server | `cmd/apiserver`, `internal/api`, `internal/lease` | Serves `/healthz`, `/readyz`, and `/v1alpha1` lease endpoints over TLS. |
| Operator | `cmd/operator`, `internal/operator` | Watches `BerthLease` resources and applies workload actions in tenant clusters. |
| OIDC broker | `cmd/berth-oidc-broker` | Fetches OAuth2 client-credentials tokens and writes them atomically to a shared file. |
| Go client | `pkg/client` | Direct integration surface for acquire, renew, and release calls. |
| Injection webhook | `internal/webhook` (in the operator process) | Optional mutating webhook that injects the `berth-acquire` helper into opted-in Pods. Off unless `--enable-injection-webhook`. |
| `berth-acquire` helper | `cmd/berth-acquire`, `internal/acquire` | Injected init container (and sidecar) that gates a Pod on a lease. |
| Helm charts | `deploy/helm` | Deploy the API server and operator. |

## Lease API

The API server exposes three lease lifecycle endpoints:

| Method | Path | Meaning |
| --- | --- | --- |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/acquire` | Acquire a lease or report the current holder. |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/renew` | Extend a held lease after validating holder and fencing token. |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/release` | Release a held lease after validating holder and fencing token. |

Lease request bodies are limited to 4,096 bytes, including whitespace, and must
contain exactly one JSON object. Larger bodies return HTTP 413; malformed or
multiple JSON values return HTTP 400. Acquire and renew accept nonempty holders
of at most 253 decoded UTF-8 bytes across every backend; longer holders return
HTTP 400. Holder identities are never truncated or rewritten.

Before upgrading, shorten configurations that generate longer holder identities.
Existing oversized holders cannot renew but can still release if the request
fits the body limit. Larger legacy records must expire and be reclaimed or
cleared by TTL collection, which preserves their fencing-token high-water mark.

All three validate the key after authorization: `{namespace}` must be an RFC
1123 DNS label (no dots), `{name}` an RFC 1123 DNS subdomain, joined length
at most 253 characters. Malformed keys get a `400` naming the field — they
never reach a store, which is what keeps the key→stored-object mapping
injective across tenants (see [Concepts → Leases](concepts.md#leases)).

Both health routes are unauthenticated. `/healthz` is an always-200 liveness
check. `/readyz` is readiness: it probes the lease store via a constant-cost
`Store.Ping` (a DB ping / single-item k8s List, never a full scan) and returns
`503` when the store is unreachable, so a store outage drains the pod from
Service endpoints instead of surfacing as `500`s on lease calls. Because the
route is unauthenticated, the probe is fronted by a short-TTL, single-flight,
timeout-bounded gate so a request storm collapses into at most one backend
probe per second and cannot be amplified into a backend-query DoS. The chart's
readiness probe targets `/readyz`; liveness stays on `/healthz`. The operator's
own `/readyz` is gated on reachability of the central API server (`pkg/client`
`Ping` against `/healthz`), so an operator that cannot reach the API reports
not-ready.

Both health routes are unauthenticated by design (kubelet probes carry no
credentials); restrict them to the kubelet/ingress path with a NetworkPolicy
in production.

Lease endpoints are authenticated when the API server is configured with
`--auth-mode=static-keys` or `--auth-mode=oidc`.


### Observability

Every request is access-logged once and instrumented for Prometheus. The
logging middleware (the outermost wrapper) emits method, path, route, status,
latency, a correlation id, and — for authenticated callers — the holder and
tenant; it never logs the bearer token, and assigns/propagates the correlation
id via the `X-Request-Id` response header (honoring an inbound W3C
`traceparent` trace id when present). Alongside the RED metrics, a
`berth_apiserver_lease_outcomes_total{outcome}` counter records each request's
semantic result (`acquired`, `held-by-other`, `renewed`, `released`,
`conflict`, `unauthorized`, `error`) — the contention and auth-failure signal
that HTTP status alone hides.

An unexpected backend failure returns a deliberately generic
`{"error":"internal error","requestId":"<id>"}` envelope: the wrapped store
error — which names the backend kind (SQL vs Kubernetes) and adjacent topology —
is recorded only in the server-side access-log line (at error level, under the
same correlation id), never on the wire. Operators cross-reference the two via
the `requestId`. Client-actionable `4xx` validation messages are returned
verbatim.

`/metrics` is served on a separate unauthenticated
port (`--metrics-addr`); see
[Scalability: Phase 2](operations/scalability.md#phase-2-api-server-prometheus-instrumentation).

### Authorization

Authentication establishes *who* the caller is; authorization decides *what*
they may touch. Every authenticated lease request is additionally checked
against the caller's resolved tenant (the static key id, or the OIDC tenant
claim):

- **Namespace** — the default policy is permissive: any authenticated caller may
  operate on any namespace. The cross-cluster failover model deliberately has
  distinct clusters (distinct tenants) contend for the same `namespace/name`, so
  a namespace gate would break legitimate contention. Stricter, opt-in policies
  (e.g. a tenant→namespace allow-list) can be layered on without changing the
  wire contract.
- **Holder** — the request `holder` must be *owned* by the caller's tenant: it
  must equal the tenant or be scoped under `"<tenant>/"`. This is the
  cross-tenant guard — a caller cannot acquire, renew, or release a lease as a
  holder that belongs to another tenant (notably, it cannot impersonate another
  holder after that holder's lease expires). A holder outside the caller's tenant
  returns `403`.

Under `--auth-mode=none` no identity is established and both checks are skipped;
this is a development-only mode and the API server warns loudly at startup.


## Lease Model

A lease is identified by namespace and name. Its record tracks:

- holder identity (empty marks a **tombstone** — a released or
  garbage-collected lease whose record is retained)
- TTL
- acquisition time
- last renewal time
- monotonic fencing token
- a store-owned **version**, advanced on every write

The fencing token increments on fresh acquisition after release or expiry and
stays stable across renewals by the same holder. Clients must send it on
renew and release so stale holders cannot modify a lease they lost. Records
are never deleted: release and garbage collection clear the holder and keep
the token as the key's high-water mark, so token values never repeat for a
key ([Concepts → Fencing tokens](concepts.md#fencing-tokens)).

Every conditional store write is predicated on the record **version**, which
changes on every successful write — renewals included. Two writes prepared
from the same observed state can therefore never both land: a stale renew
racing a standby's reclaim loses deterministically, whichever order the
writes arrive in. The fencing token is a domain value, not the concurrency
predicate.

TTL expiry is enforced lazily during acquire and renew. A background
`TTLEnforcer` sweeps expired records into tombstones after a grace window,
reclaiming the holder/TTL state a never-reacquired key would otherwise keep
forever. Each sweep write is predicated on the version captured during the
scan; because versions are never reused for a key, a sweep can never touch a
lease written after its scan — correctness does not depend on the scan loop,
its timing, or how many API-server replicas run one concurrently.

## Storage Backends

| Backend | Status | Use |
| --- | --- | --- |
| `mem` | Implemented | Development and tests only. State is per-process and lost on restart. |
| `k8s` | Implemented | Production cross-cluster topology. Stores records as Kubernetes `Lease` objects in a coordination cluster namespace. |
| `sql` | Implemented | Runner-local topology. Stores records in Postgres, MariaDB/MySQL, or SQLite using ACID database transactions. |

Postgres and MariaDB/MySQL support multi-replica API server deployments through
shared SQL compare-and-swap updates. SQLite is durable and ACID, but remains a
single-writer backend; run it with one API server replica only.

Use explicit `--store-backend`. The legacy heuristic that selects `mem` or
`k8s` from `--coordination-namespace` is deprecated.

## Operator Reconcile Flow

The operator watches namespaced `BerthLease` resources.

1. It adds `berth.skaphos.io/lease-release` as a finalizer.
2. It validates the small set of invariants the reconciler depends on.
3. It chooses the holder identity from `--cluster-id` when set, otherwise
   `spec.holderIdentity`.
4. It calls the central API server to acquire or renew the lease.
5. If acquired, it applies `spec.acquireAction` to the target and records
   `status.leaseState=held`.
6. If not acquired, it applies `spec.releaseAction`, records
   `status.leaseState=waiting`, and retries at `min(heartbeat, ttl/3)`.
7. On deletion, it performs a best-effort release if it still has a current
   fencing token, then removes the finalizer.

## Workload Actions

`BerthLease.spec.target` points at a workload in the same namespace as the
`BerthLease`.

| Action | Behavior | Typical target |
| --- | --- | --- |
| `suspend` | Patches a target's `spec.suspend`. | `batch/v1` `CronJob` |
| `scale` | Patches the target scale subresource. | `Deployment`, `StatefulSet`, `ReplicaSet` |

Each action may set at most one of `suspend` or `scale`.

## Target Convergence and Watch Strategy

The operator reconciles a `BerthLease` on a level-triggered cadence: a held
lease re-reconciles every `heartbeatIntervalSeconds`, and a standby
re-reconciles every `min(heartbeatIntervalSeconds, ttlSeconds/3)`. On each
reconcile it re-reads the `spec.target` workload and re-applies the
state-appropriate action (`acquireAction` while held, `releaseAction` while
waiting). A manual edit to a managed target — for example, someone scaling a
gated Deployment back up by hand — is therefore re-converged on the next
reconcile, within the heartbeat window (about 10 s at the default
`ttlSeconds: 30` / `heartbeatIntervalSeconds: 10`). The re-apply is idempotent:
when the target already matches the desired action the reconciler skips the
write, so steady-state convergence costs one live read (a single GET to the API
server — the operator runs no informer over targets) and no write.

The operator deliberately does **not** watch target workloads. It registers
only `For(&BerthLease{})` and relies on the periodic re-assert above rather than
mapping Deployment / StatefulSet / ReplicaSet / CronJob events back to their
owning `BerthLease`. This is an intentional trade-off, not an omission:

- **A cluster-wide workload watch defeats the scale target.** The sizing target
  is up to 2,000 leases per tenant cluster (see
  [Scalability](operations/scalability.md)). A broad informer would cache
  *every* Deployment, StatefulSet, ReplicaSet, and CronJob in the cluster — not
  just the ones a `BerthLease` references — and ReplicaSets in particular
  accumulate with every rollout, so the cache grows with total cluster workload
  rather than with Berth's own footprint. The initial LIST and ongoing WATCH
  also add load to the API path the `k8s` backend is already throttle-bound on
  (client-go `QPS=5/Burst=10`).
- **The watch cannot be cheaply scoped.** Targets are arbitrary user-owned
  workloads with no Berth-applied label or field, so a `cache.ByObject`
  selector cannot narrow the informer without the operator first stamping a
  marker on every user workload — a write beyond the declared action.
- **The benefit is small.** A watch would cut drift-correction latency from
  one heartbeat (about 10 s) to sub-second. For a coordination primitive whose
  failover RTO is already bounded by the lease TTL (tens of seconds), that
  margin does not justify the cache and API-server cost.

If a future deployment genuinely needs sub-second correction in a small or
single-tenant cluster, the supported extension would be an **opt-in** flag (for
example `--watch-targets`) that registers a scoped `Watches` source plus a field
index on `BerthLease` by target reference — never an always-on cluster-wide
watch. It is a deliberate non-default.

## Injected Workload Gating

For workloads whose image cannot be changed, the operator can run a mutating
admission webhook (`--enable-injection-webhook`) that injects the
`berth-acquire` helper into Pods carrying `berth.skaphos.io/inject: acquire`.
Unlike operator-as-holder — which gates a workload by **scaling** it from
outside — injection gates **at the Pod level** from inside:

- An init container blocks Pod startup until it acquires the lease (the "hold").
- In `runtime-singleton` mode a native sidecar renews the lease and, on loss,
  stops the main container (kubelet kills it via an injected liveness probe with
  `enforce=probe`, or the sidecar signals it with `enforce=signal`) and keeps it
  gated until it re-acquires.

This path talks to the lease API directly and does **not** require a
`BerthLease` object. It creates more potential holders than operator-as-holder,
so its split-brain surface is larger; it is a deliberate fallback. Full
contract, modes, and trade-offs: [Workload gating via injection](workload-gating-injection.md).

## Failure Behavior

Failover time is bounded by the holder TTL plus the standby reacquire interval.
For `ttlSeconds: 30` and `heartbeatIntervalSeconds: 10`, a standby should
usually acquire within about 40 seconds after the previous holder's last
successful heartbeat.

There can be a short split-brain window after the old holder loses API-server
connectivity and before it observes lease loss and applies its release action.
The API server rejects stale renew/release calls by fencing token, but a target
workload needs its own token-aware integration for end-to-end fencing against
downstream systems.

These bounds assume lease calls fail fast. An API server that accepts a
connection and then stalls is not a connectivity error the caller sees, so every
lease call is bounded: the Go client applies `client.DefaultTimeout` (override
with `client.WithTimeout`), and the injected sidecar additionally bounds each
renew and reacquire at one heartbeat interval. A holder therefore re-evaluates
enforcement within a heartbeat of its last attempt, whatever the server does,
and the past-expiry self-fence still fires against an unresponsive API.

## Known Limitations

- The operator runs a single active replica by default. Leader election is
  opt-in (`leaderElection.enabled` / `--leader-elect`) for in-cluster HA; with
  more than one replica it is required so exactly one replica reconciles at a
  time. Even under a shared `--cluster-id` holder it prevents duplicate central
  API load and racing `BerthLease` status writes.
- Manual edits to a managed target workload are re-converged on the next
  heartbeat reconcile (bounded by the heartbeat interval, ~10 s at defaults),
  not via a workload watch — an intentional trade-off for the per-cluster scale
  target; see [Target Convergence and Watch Strategy](#target-convergence-and-watch-strategy).
- `at-least-once` is part of the CRD surface, but the current central manager
  implements exclusive holder behavior.
- The management console package exists as a placeholder.
- CLI lease commands are scaffolds and print `not implemented`.
