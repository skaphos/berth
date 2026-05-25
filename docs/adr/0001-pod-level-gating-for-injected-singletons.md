# 1. Pod-level gating for injected singletons, not BerthLease scale actions

- **Status**: accepted
- **Date**: 2026-05-24
- **Deciders**: Shawn Stratton
- **Consulted**: Skaphos team
- **Informed**: Berth contributors

## Context and Problem Statement

Berth already has an **operator-as-holder** path (SKA-271): a per-cluster
operator holds the lease on a workload's behalf and activates the workload by
**scaling it** — it applies `AcquireAction` / `ReleaseAction` (`scale.replicas`,
see `api/v1alpha1/types.go`) on a `BerthLease` whose `target` references the
Deployment. This works because the operator sits *outside* the workload.

The injected-helper path (SKA-274) targets unmodifiable images where per-pod
admission and runtime enforcement are wanted. Here the helper runs **inside the
pod**, and by the time it runs the pod is already scheduled. There is nothing
for the helper to "scale to zero" — the pod exists.

The parent tickets originally framed the injected path as reusing scale actions
(SKA-437: "use the existing `BerthLease` `AcquireAction` + `ReleaseAction`";
SKA-274: "scale-up / scale-down via `AcquireAction`/`ReleaseAction`"). That is a
category error: the helper's vantage point is incompatible with controller-level
scaling. A decision is forced about how the injected path activates a workload.

## Decision Drivers

- The helper executes inside the pod; controller-level scaling is not available
  to it.
- No application or image change is permitted.
- The goal is per-pod admission (a "hold") and per-pod runtime enforcement, not
  whole-workload on/off.
- Avoid introducing a second control loop that competes with the workload
  controller (Deployment/StatefulSet/Job controllers).
- Reuse the existing lease RPCs (Acquire / Renew / Release) rather than the CRD
  action surface.

## Considered Options

- **A. Reuse `BerthLease` `AcquireAction`/`ReleaseAction` scaling** for the
  injected path (as the tickets originally framed it).
- **B. Pod-level gating** — an init container blocks pod startup until it
  acquires (the "hold"); a sidecar renews and enforces at runtime; the helper
  calls the lease API directly and needs no `BerthLease`.
- **C. Hybrid** — the injected helper writes a `BerthLease` status and an
  operator reacts by scaling.

## Decision Outcome

Chosen option: **B, pod-level gating**, because the helper's in-pod vantage point
makes scaling unavailable to it, and gating at the init container plus enforcing
from a sidecar delivers per-pod admission and at-most-once without a second
controller or any `BerthLease` object on the critical path.

A `BerthLease` may still coexist for observability or an operator-driven
workflow, but it is not required by, and not on the critical path of, the
injected path.

### Consequences

- **Positive**: No extra CRD or controller; no competition with workload
  controllers; works at per-pod granularity; aligns with how Kubernetes copies
  `spec.template.metadata` onto Pods; reuses the existing lease RPC trio.
- **Negative**: Every opted-in replica is a potential lease holder, so the
  fencing surface is larger than the one-operator-per-cluster path. Runtime
  singleton rolling updates can stall the new pod up to one TTL while the
  outgoing pod's lease frees. The parent tickets' acceptance criteria
  (SKA-437, SKA-274) must be corrected away from the scale-action framing.
- **Neutral**: Failover RTO is comparable to the operator path
  (≈ TTL + reacquire interval) plus kill latency; the reacquire-after-expiry
  logic is the same behavior fixed for the operator in SKA-436 and must not be
  reintroduced as a bug.

## Pros and Cons of the Options

### A. Reuse scale actions
- Good, because it reuses an existing, tested mechanism and the operator's
  failover path.
- Bad, because the helper cannot scale from inside the pod; the model does not
  fit the injected vantage point at all.

### B. Pod-level gating
- Good, because it matches the helper's actual position and needs no extra
  control-plane object.
- Bad, because it multiplies the number of potential holders and adds
  rolling-update stalls.

### C. Hybrid (helper writes status, operator scales)
- Good, because it keeps activation at the controller level.
- Bad, because it reintroduces a `BerthLease` dependency and a second control
  loop for a path whose whole point is "no extra objects, just gate the pod."

## Links

- Design: `docs/design/2026-05-workload-gating-injection-model.md`
- Supersedes the scale-action framing in SKA-437 and SKA-274 (tickets to be
  corrected)
- Related ADRs: [ADR-0002](0002-label-annotation-opt-in-over-crd.md),
  [ADR-0003](0003-sidecar-runtime-enforcement-by-container-kill.md)
- Linear: SKA-268, SKA-271, SKA-274, SKA-437, SKA-436
