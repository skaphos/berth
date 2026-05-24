# 2. Opt into injection via pod-template labels/annotations, not a wrapper CRD

- **Status**: proposed
- **Date**: 2026-05-24
- **Deciders**: Shawn Stratton
- **Consulted**: Skaphos team (pending SKA-437 design review)
- **Informed**: Berth contributors

## Context and Problem Statement

Having decided the injected path gates at the pod level (ADR-0001), an authoring
surface is needed: how does a user declare "inject the Berth helper into this
workload's pods, with this lease and these parameters"?

The mutation boundary is a Pod. Standard workload controllers (Deployment,
StatefulSet, DaemonSet, Job, CronJob) copy `spec.template.metadata` onto the
Pods they create, so a mutating admission webhook can select and configure pods
purely from pod-template metadata. The alternative is a new typed CRD that a
controller reconciles into workload mutations.

This is a hard-to-reverse API-surface decision: once users template a CRD or a
set of labels/annotations across their GitOps repos, changing the contract is
costly.

## Decision Drivers

- Native Kubernetes opt-in that every standard workload controller already
  propagates.
- GitOps-friendliness — users already template labels/annotations.
- Avoid a second control loop competing with workload controllers (consistent
  with ADR-0001).
- The injected path is deliberately independent of `BerthLease`; a new CRD would
  re-introduce a control-plane object on a path whose point is "no extra
  objects."

## Considered Options

- **A. Wrapper CRD** (e.g., `WorkloadGate`) reconciled by a controller that
  annotates or mutates the target workload.
- **B. Mutating admission webhook** keyed on a pod-template label
  (`berth.skaphos.io/inject: acquire`) plus `berth.skaphos.io/*` annotations for
  configuration.
- **C. Manual injection** — users add the init container and sidecar to their
  own manifests; no automation.

## Decision Outcome

Chosen option: **B, label/annotation contract consumed by a mutating webhook**,
because it is the native opt-in surface, propagates automatically through every
workload controller, and adds no API object or controller — while leaving a CRD
as an explicit, deferrable future option rather than a foreclosed one.

The contract uses one selection **label** (`berth.skaphos.io/inject: acquire`,
matchable by the webhook's `objectSelector`) and `berth.skaphos.io/*`
**annotations** for free-form configuration (`lease-name`, `mode`, `enforce`,
TTL, etc.). A CRD is revisited only if: annotation validation/defaulting proves
insufficient, a first-class active-system-selection primitive is added, or users
need a queryable status object that Pod events and helper logs cannot provide.

### Consequences

- **Positive**: No new API object or controller; native and GitOps-friendly; the
  webhook is the single mutation point; opt-in propagates for free via
  controllers; a CRD remains addable later without rework.
- **Negative**: Annotation-based configuration has weaker validation and
  defaulting than a typed CRD schema; there is no queryable status object, so
  "which pod holds lease X?" is answered only via pod events and helper logs;
  webhook availability and its failure policy (Fail vs. Ignore) become a
  correctness and operability concern.
- **Neutral**: CronJob opt-in must live two levels deep at
  `spec.jobTemplate.spec.template.metadata`, a documented mislabeling trap.

## Pros and Cons of the Options

### A. Wrapper CRD
- Good, because it offers typed validation, defaulting, and a status surface.
- Bad, because it adds a controller competing with workload controllers and a
  control-plane object on a path designed to avoid both.

### B. Label/annotation + mutating webhook
- Good, because it is the idiomatic, controller-propagated opt-in with zero new
  API objects.
- Bad, because validation is webhook-side only and there is no first-class
  status object.

### C. Manual injection
- Good, because it needs no webhook and is fully explicit.
- Bad, because it puts brittle init/sidecar wiring into every user's manifests
  and loses the central control point — defeating the "retrofit unmodifiable
  workloads" goal.

## Links

- Design: `docs/design/2026-05-workload-gating-injection-model.md`
  ("Decision Record: Labels/Annotations vs. Wrapper CRD")
- Related ADRs: [ADR-0001](0001-pod-level-gating-for-injected-singletons.md),
  [ADR-0003](0003-sidecar-runtime-enforcement-by-container-kill.md)
- Linear: SKA-274, SKA-437, SKA-439 (webhook), SKA-440 (Helm)
