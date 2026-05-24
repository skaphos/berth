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
| API server | `cmd/apiserver`, `internal/api`, `internal/lease` | Serves `/healthz` and `/v1alpha1` lease endpoints over TLS. |
| Operator | `cmd/operator`, `internal/operator` | Watches `BerthLease` resources and applies workload actions in tenant clusters. |
| OIDC broker | `cmd/berth-oidc-broker` | Fetches OAuth2 client-credentials tokens and writes them atomically to a shared file. |
| Go client | `pkg/client` | Direct integration surface for acquire, renew, and release calls. |
| Helm charts | `deploy/helm` | Deploy the API server and operator. |

## Lease API

The API server exposes three lease lifecycle endpoints:

| Method | Path | Meaning |
| --- | --- | --- |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/acquire` | Acquire a lease or report the current holder. |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/renew` | Extend a held lease after validating holder and fencing token. |
| `POST` | `/v1alpha1/namespaces/{namespace}/leases/{name}/release` | Release a held lease after validating holder and fencing token. |

`/healthz` is always unauthenticated. Lease endpoints are authenticated when
the API server is configured with `--auth-mode=static-keys` or
`--auth-mode=oidc`.

## Lease Model

A lease is identified by namespace and name. Its record tracks:

- holder identity
- TTL
- acquisition time
- last renewal time
- monotonic fencing token

The fencing token increments on fresh acquisition after release or expiry and
stays stable across renewals by the same holder. Clients must send it on
renew and release so stale holders cannot modify a lease they lost.

TTL expiry is enforced lazily during acquire and renew. A background
`TTLEnforcer` can scan and clean expired records, but correctness does not
depend on the scan loop.

## Storage Backends

| Backend | Status | Use |
| --- | --- | --- |
| `mem` | Implemented | Development and tests only. State is per-process and lost on restart. |
| `k8s` | Implemented | Production cross-cluster topology. Stores records as Kubernetes `Lease` objects in a coordination cluster namespace. |
| `sql` | Flagged, not implemented | Reserved for SKA-316 runner-local topology. Startup currently fails for `--store-backend=sql`. |

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

## Known Limitations

- The operator currently runs one replica and does not use leader election.
- `at-least-once` is part of the CRD surface, but the current central manager
  implements exclusive holder behavior.
- The SQL backend is configured in flags and Helm values but is not
  implemented.
- The management console package exists as a placeholder.
- CLI lease commands are scaffolds and print `not implemented`.
