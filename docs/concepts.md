# Concepts

This page builds the mental model behind Berth: what a lease is, who can hold
one, how holders keep (or lose) it, and the two ways you wire a lease to a
workload. It is the "why it behaves that way" companion to the
[Getting Started](getting-started.md) tutorial and the
[Architecture](architecture.md) reference.

## The problem Berth solves

Kubernetes `coordination.k8s.io/Lease` objects are **cluster-local**. Two
identical workloads in two different clusters cannot see each other's native
leases, so neither can decide "only one of us should be active right now." That
is exactly the situation when you run the same job in multiple clusters for
availability but need *at most one* of them doing the work — a singleton
consumer, a leader-only batch job, a cron that must not double-fire.

Berth adds a **central lease surface** that every participating cluster talks to,
so they share one authoritative answer to "who may act?"

## Leases

A **lease** is a named, time-bounded, single-holder claim. It is identified by a
**namespace** and a **name** (for example `pipeline/ingest-worker`), and at any
moment it has at most one holder.

The two segments have a required shape, checked on every lease request: the
namespace must be an RFC 1123 **DNS label** (lowercase alphanumerics and `-`,
at most 63 characters — no dots), the name an RFC 1123 **DNS subdomain**
(dots allowed), and the dot-joined pair at most 253 characters. This mirrors
how Kubernetes names namespaces, and it is a safety rule, not a style rule:
it guarantees two distinct keys can never collide into one stored object in
any backend, so no tenant can address another tenant's lease by crafting an
ambiguous key. Requests with malformed segments are rejected with `400` and a
message naming the offending field.

A lease's record tracks:

- the **holder identity** — who currently owns it,
- a **TTL** — how long the claim survives without renewal,
- acquisition and last-renewal timestamps,
- a monotonic **fencing token**.

The authoritative copy lives in the central API server's store, not in any one
cluster. Clusters don't coordinate with each other directly; they each talk to
the API server, which arbitrates.

!!! note "What a lease is *not*"
    A lease is not a scheduler, a queue, or a lock you `Lock()`/`Unlock()`
    synchronously. It is a *claim with an expiry*. Holders must keep renewing it,
    and if they stop, it lapses. Berth decides **who may act**, never **when work
    is created**.

## Holder identity

The **holder identity** is the string that owns the lease. Whoever presents the
matching identity (and a valid fencing token) may renew or release it.

How the identity is chosen depends on how you use Berth:

- **Operator as holder (declarative).** Each `berth-operator` competes on behalf
  of its cluster. Its identity comes from the `--cluster-id` flag when set,
  otherwise from the `BerthLease`'s `spec.holderIdentity`. This is why you give
  east and west *distinct* `--cluster-id`s but apply the *same* `BerthLease` to
  both — same lease name, different contenders.
- **Direct integration.** Code using the [Go client](https://github.com/skaphos/berth/tree/main/pkg/client) or the
  injected `berth-acquire` helper supplies its own holder string (for injected
  pods, typically derived from the pod/workload identity).

A caller may only act as a holder its tenant owns — authorization rejects
acquiring or renewing under a holder string that belongs to another tenant, so a
holder cannot impersonate another after that holder's lease expires. See
[Architecture → Authorization](architecture.md#authorization).

## TTL, heartbeat, and failover

Two durations govern a lease's lifetime:

- **TTL** (`ttlSeconds`) — how long the claim lives without a renewal. If the
  holder goes silent for longer than the TTL, the lease is considered expired and
  becomes available to others.
- **Heartbeat** (`heartbeatIntervalSeconds`) — how often the holder renews. The
  heartbeat must be comfortably shorter than the TTL so a renewal or two can be
  lost without losing the lease.

A standby that does *not* hold the lease keeps retrying acquisition on an
interval of **`min(heartbeat, ttl/3)`**. So worst-case failover time is bounded
by roughly **TTL + the standby's reacquire interval**:

```
holder stops renewing ──▶ TTL elapses ──▶ standby's next acquire wins
        t=0                  t≈TTL              t≈TTL + reacquire
```

With `ttlSeconds: 15` and `heartbeatIntervalSeconds: 5`, takeover lands in about
25 seconds. Lower the TTL for faster failover at the cost of more renewal traffic
and less tolerance for transient API-server blips; raise it for the opposite
trade-off.

TTL expiry is enforced **lazily** — it is checked during the next acquire or
renew, so correctness never depends on a background sweep running on time.

## Fencing tokens

Every lease carries a **fencing token**: a monotonically increasing integer that
**bumps on each fresh acquisition** (after a release or an expiry) and **stays
constant across renewals by the same holder**. Clients must send the token on
renew and release.

This is the guard against a *stale holder* — one that lost the lease (say, after
a network partition) but doesn't know it yet. Its token is now behind the
current one, so the API server rejects its renew/release calls.

Monotonicity holds across the **entire life of the key**, not just one
holder's tenure: releasing a lease (or having it garbage-collected after
expiry) leaves a small **tombstone** record behind that preserves the
highest token ever issued, and the next acquisition always goes strictly
past it. A token value, once issued for a key, is never issued again. That
is what makes the downstream guidance below sound — a downstream can keep
only the highest token it has seen and reject anything at or below it.

The token is a 32-bit counter with a hard ceiling that refuses to wrap, so a
reused token can never silently authorize a stale write. Because the counter
now survives release and garbage collection, the ceiling is cumulative over
the key's lifetime — still ~2.1 billion holder transitions, unreachable at
any realistic failover rate.

!!! warning "Fencing protects the lease, not your downstream"
    Berth rejects stale lease operations by token, but it cannot fence *your*
    side effects. If a brief [split-brain](architecture.md#failure-behavior)
    window matters for, say, writes to a shared database, your workload needs to
    carry the fencing token through to that downstream system and have the
    downstream reject stale tokens too.

!!! note "Tombstones and out-of-band deletion"
    Tombstones are how the token high-water mark survives: Berth never
    deletes a lease record, it only clears the holder. One O(1) record per
    distinct key ever used stays in the backing store. Deleting a record out
    of band (a SQL `DELETE`, `kubectl delete lease` in the coordination
    namespace) resets that key's token history and forfeits the
    never-reused guarantee for it — don't do it while anything downstream
    still compares tokens for that key.

## Lease semantics

`spec.semantics` selects the lease-window behavior:

- **`at-most-once`** — the implemented exclusive-holder mode. One holder at a
  time; this is what you want for "never run two copies."
- **`at-least-once`** — accepted by the schema but **currently behaves the same
  as `at-most-once`** (exclusive holder) until the central manager implements
  distinct at-least-once windows. Don't rely on at-least-once semantics yet.

## Connecting a lease to a workload

Holding a lease is only useful if something *happens* when you win or lose it.
There are two ways to wire that up.

### Declarative: `BerthLease` + operator

A `BerthLease` custom resource ties a lease to a target workload and an action
for each transition:

| Field | Role |
| --- | --- |
| `spec.target` | The workload to act on, in the same namespace as the `BerthLease`. |
| `spec.acquireAction` | Applied when this cluster **wins** the lease. |
| `spec.releaseAction` | Applied when this cluster **loses** or never had it. |

Each action is one of:

| Action | Effect | Typical target |
| --- | --- | --- |
| `scale` | Patches the target's scale subresource (replica count). | `Deployment`, `StatefulSet`, `ReplicaSet` |
| `suspend` | Patches the target's `spec.suspend`. | `CronJob` |

The operator reconciles the loop for you: acquire/renew, apply `acquireAction`
when held and `releaseAction` when not, and on deletion do a best-effort release.
This is the path the [Getting Started](getting-started.md) tutorial uses.

### Direct integration: Go client or injection

When you'd rather drive the lease yourself, or you can't model the workload as a
single scalable target:

- **[Go client](https://github.com/skaphos/berth/tree/main/pkg/client)** (`pkg/client`) — call acquire/renew/release
  from your own code and gate behavior on the result. You own the heartbeat loop
  and the fencing token.
- **Injected `berth-acquire` helper** — for workloads whose image you can't
  change. An admission webhook injects an init container that blocks pod startup
  until it holds the lease, and (optionally) a sidecar that enforces loss at
  runtime. See [Workload gating via injection](workload-gating-injection.md).

Injection gates **at the pod level from inside**, whereas operator-as-holder
gates **by scaling from outside**. Injection creates more potential holders, so
its split-brain surface is larger — it's a deliberate fallback for unmodifiable
images, not the default.

## Topologies and storage backends

Where the authoritative lease state lives determines which deployment shape you
pick. Choose the backend by topology — not the other way around:

| Topology | Backend | Lease state lives in |
| --- | --- | --- |
| **Cross-cluster coordination plane** — one central API server arbitrates across tenant clusters. | `k8s` | `coordination.k8s.io/Lease` objects in a coordination cluster namespace. |
| **Runner-local HA** — Berth runs inside the runner cluster, multiple API-server replicas. | `sql` (Postgres or MariaDB/MySQL) | Shared SQL database, via ACID compare-and-swap. |
| **Runner-local edge / dev** — single API-server replica. | `sql` (SQLite) or `mem` | Local SQLite file, or in-memory (ephemeral, lost on restart). |

The `k8s` backend keeps state Kubernetes-native; the `sql` backend scales API
server replicas horizontally on Postgres/MariaDB. SQLite is durable but
single-writer (one replica only); `mem` is for development and tests. Details and
flags: [Architecture → Storage Backends](architecture.md#storage-backends) and
the [Configuration reference](reference/configuration.md).

## Authentication and authorization

The central lease endpoints are authenticated. Berth supports:

- **`static-keys`** — bearer tokens checked against a file of `key-id:sha256`
  hashes (the default for the `k8s` and `sql` backends).
- **`oidc`** — JWT bearer tokens validated against an issuer; an optional
  OIDC broker sidecar fetches and rotates the operator's token.
- **`none`** — no auth; development only, and the server warns loudly.

Authentication establishes *who* you are; **authorization** then checks *what*
you may touch — chiefly that the `holder` you act as belongs to your tenant. Full
contract: [Architecture → Authorization](architecture.md#authorization) and the
auth flags in the [Configuration reference](reference/configuration.md).

## Where to go next

- **Do it** — [Getting Started](getting-started.md) walks the whole loop on a
  local three-cluster topology.
- **Reason about runtime and failure** — [Architecture](architecture.md).
- **Look up flags and values** — [Configuration reference](reference/configuration.md).
- **Gate an unmodifiable image** — [Workload gating via injection](workload-gating-injection.md).
