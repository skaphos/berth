# Phase 1 Data Model: State-Volume Trust and Marker Freshness

There is no persisted schema in this feature. The "data" is the contents of one
shared `emptyDir` and the classifications the webhook and the probe derive from
a pod spec. This document fixes those shapes so the admission rule and the
freshness test cannot drift apart.

## State volume (`berth-state`)

The trusted set. Every artifact here is written by an injected helper and, after
this change, is writable by nothing else.

| Artifact | Written by | Read by | Trust role |
|---|---|---|---|
| `holder` | init (`WriteAcquired`) and sidecar on reacquire | sidecar (`loadHandoff`) | Identity handoff between helpers |
| `token` | same | same | Fencing token handoff |
| `healthy` | init on successful acquire, then sidecar on every successful renew | `check` in the main container | The liveness signal. Its **mtime** is the freshness evidence. |
| `check` | init (`InstallCheckBinary`, copies own executable) | kubelet, exec'd in the main container | **The verifier itself.** In the trusted set, not a convenience — this is why the mount rule exists rather than a marker-signing scheme. |

**Invariant (new)**: for any admitted pod, every mount of this volume into a
workload-authored container is read-only. Injector-owned mounts on helper
containers remain writable.

**Invariant (preserved, load-bearing)**: `healthy` is created by the *init*
container's successful acquire — `WriteAcquired` ends in `MarkHealthy()`
(`state.go:40-50`), called from `acquire.go:49`. The one-TTL freshness bound
depends on this: it is why there is no startup window in which a legitimately
started workload has no marker. Implementation MUST NOT move marker creation to
the renew loop.

## Mount classification

The predicate the admission rule evaluates. Derived per `volumeMounts` entry,
per container.

| Field | Source | Notes |
|---|---|---|
| `container` | container name | Reported in the rejection message |
| `containerKind` | `containers` / `initContainers` / `ephemeralContainers` | Ephemeral arrives via the subresource (R3) |
| `volumeName` | `volumeMounts[].name` | Matches when equal to the injected state volume |
| `mountPath` | `volumeMounts[].mountPath` | Reported; the rule is **not** keyed on this |
| `readOnly` | `volumeMounts[].readOnly` | Absent/false means writable |
| `ownedByInjector` | whether the webhook itself added this mount | The discriminator; see below |

**Decision rule**: reject when `volumeName` matches the state volume **and**
`readOnly` is not true **and** `ownedByInjector` is false.

`ownedByInjector` is the subtle field. The webhook adds writable mounts to its
own helper containers, so "writable" alone cannot be the test. The available
discriminators, in preference order:

1. The container is one the injector is adding in this same admission pass
   (known directly, no inference).
2. The container name matches an injected helper's reserved name.

Preference 1 is exact for pod creation. Preference 2 is the fallback for the
ephemeral-containers path, where the injected helpers are already present in
the incoming pod. Implementation must not conflate the two.

## Freshness verdict

The typed result of the shared predicate (R5), returned to both callers.

| Variant | Meaning | `check` exit | Reported detail |
|---|---|---|---|
| `Healthy` | Marker present and newer than the bound | 0 | — |
| `Absent` | Marker does not exist | 1 | Path |
| `Stale` | Marker exists but mtime older than the bound | 1 | Observed age and the bound it exceeded |
| `Indeterminate` | Marker exists but cannot be stat'ed | 1 | Underlying error |

`Absent` and `Stale` are deliberately distinct: they are the two operational
stories FR-011a requires an operator to tell apart — an expected lease-loss
kill versus a dead-sidecar incident. `Indeterminate` fails closed; an
unreadable marker is not evidence of health.

**Boundary**: `Stale` when `age > bound`, `Healthy` when `age <= bound`. Stated
explicitly because the equality case is exactly what the FR-007 margin test
will probe.

## Rejection reason

Carried in the admission error (FR-002) and as the counter label (FR-011b).

| Reason | Condition | Message must name |
|---|---|---|
| `WritableStateMount` | Author-declared writable mount of the state volume | Container, volume, mount path, and the accepted resolutions |
| `WritableStateMountEphemeral` | Same, arriving via the ephemeral-containers subresource | Same, plus that the running pod is unaffected and the *debug request* was refused |

Counter labels are the reason and the admission path only — never pod or
namespace name, which would be unbounded under a controller retry loop (R6).
