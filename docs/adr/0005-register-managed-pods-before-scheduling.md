# 5. Register managed Pods before scheduling

- **Status**: accepted
- **Date**: 2026-09-07
- **Deciders**: Shawn Stratton

## Context and Problem Statement

The operator holds central leases and activates workload controllers through
`internal/operator/reconciler.go` and `actions.go`. Changing desired replicas or
suspending a CronJob does not serialize against an in-flight Job or Pod create.
An empty Pod list cannot establish that no further executable Pod will appear.
The operator must preserve cleanup responsibility through central API errors,
status conflicts, deletion and delayed Kubernetes operations.

## Decision Drivers

- Retain Deployment, StatefulSet, ReplicaSet and CronJob targets.
- Preserve operator-owned central leases and support ordinary init containers.
- Account for executable Pod UIDs before relinquishing cleanup responsibility.
- Keep lease-only operation independent of admission infrastructure.

## Considered Options

- Scheduling gates with durable Pod UID registration before ungating.
- Admission-side execution reservations with commit/abort reconciliation.
- Move central ownership into workload helpers.

Suspension plus an empty list and a webhook that only reads the current lease
state cannot close the admission-to-storage race.

## Decision Outcome

Use scheduling gates. Every managed Pod is created gated. Persist its UID in
BerthLease status before removing the gate. Serialize registration and Stopping
through optimistic updates of that same resource. Terminate every registered
UID, including Pending/gated Pods, before clearing records or releasing the
central lease. A delayed ungate request cannot recreate a deleted UID.

Managed targets must be created under required admission after their BerthLease.
Final CREATE admission records the actual target UID before storage; a failed
or aborted create may leave a harmless pending UID. An annotation alone is not
an admission record. Immutable lease UID bindings and independently resolved
owner references prevent late children from inheriting a recreated lease name.

### Consequences

- **Positive**: Handles delayed creates without per-workload binaries or changing
  central lease ownership; supports CronJobs and their active Jobs/Pods.
- **Negative**: Managed workloads require fail-closed admission and TLS. Existing
  targets need draining and recreation; arbitrary target kinds and unsafe
  release actions are rejected. Registration introduces status contention and
  bounded per-lease Pod bookkeeping.
- **Negative**: Controller and kubelet availability remain necessary. Termination
  grace and detection latency remain; downstream fencing is needed to reject
  stale writes. Force deletion is not proof of process termination.
- **Neutral**: Lease-only resources and injected helper ownership remain separate.

Admission reservations could avoid scheduling gates but require resolving
requests that were admitted and never committed. Helpers provide workload-local
expiry handling but change ownership and workload compatibility. Revisit this
decision if operator unavailability must itself fence execution.

## Links

- [ADR-0001](0001-pod-level-gating-for-injected-singletons.md): separate injected
  helper and operator ownership paths; its injected-path decision is unchanged.
