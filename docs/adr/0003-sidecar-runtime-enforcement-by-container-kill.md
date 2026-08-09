# 3. Enforce at-most-once by killing the main container; probe default, signal opt-in

- **Status**: accepted — trust model amended by
  [ADR-0004](0004-state-volume-is-a-trust-boundary.md)
- **Date**: 2026-05-24
- **Deciders**: Shawn Stratton
- **Consulted**: Skaphos team
- **Informed**: Berth contributors

## Context and Problem Statement

In runtime-singleton mode (ADR-0001), the injected sidecar must enforce
at-most-once for an image that cannot be modified to react to lease loss. Two
facts force the design:

1. Berth's acquire semantics renew a lease when the incoming holder matches the
   current unexpired holder (`internal/lease/manager.go`: `case cur.Holder ==
   holder && !cur.Expired(now)` returns the same fencing token with
   `Acquired: true`). So multiple pods sharing a holder identity can all "hold"
   the same lease — runtime singleton must use a unique per-pod identity, and
   when a pod loses the lease, *something* must stop its main container.
2. The helper is a separate container. A container cannot, by default, stop the
   process of another container in the same pod.

The original draft deferred the kill to "future work" and only logged on loss.
For unmodifiable images that gives no real enforcement, so a concrete mechanism
must be decided now.

## Decision Drivers

- No application or image change.
- A real (if bounded) at-most-once guarantee, not log-only hope.
- Preserve container isolation where possible.
- Work for distroless/scratch images that have no shell.
- Not break Job/CronJob completion.

## Considered Options

- **A. `probe`** — the webhook injects an `exec` liveness probe
  (`cat /berth/healthy`, or a static check binary on the shared volume) on each
  main container, backed by a shared `emptyDir`. The sidecar removes the marker
  on lease loss; the kubelet kills the container.
- **B. `signal`** — the webhook sets `shareProcessNamespace: true`; the sidecar
  finds the main process and sends `SIGTERM` then `SIGKILL`.
- **C. Log-only / rely on the app to react** (the original draft's deferral).
- **D. Sidecar deletes the Pod via the Kubernetes API.**

## Decision Outcome

Chosen option: **support A and B, defaulting to A (`probe`)**, selected by the
`berth.skaphos.io/enforce` annotation; reject C and D as the enforcement
primitive.

`probe` is the default because the kubelet performs the kill and container
isolation is preserved. `signal` is the opt-in for images where probe injection
is not viable (no shell and the static check binary cannot run, or a liveness
probe is already in use). The sidecar is injected as a **native sidecar**
(`restartPolicy: Always` init container) so Jobs still complete, and it keeps the
main container gated after a kill — holding the marker absent or re-signalling
until it re-acquires — because a kubelet restart does **not** re-run completed
init containers.

Enforcement is **best-effort within a bounded window**: there is a detection +
kill latency between lease loss and the container stopping. This strengthens
at-most-once at the process level but does not replace the fencing token, which
remains the real boundary for any downstream resource that must reject a stale
holder.

### Consequences

- **Positive**: Real at-most-once enforcement with no image change; `probe`
  keeps isolation and lets the kubelet own the kill; `signal` covers shell-less
  images; native sidecar preserves Job/CronJob completion.
- **Negative**: Bounded best-effort, not a hard mutex — a stale holder can run
  for the detect+kill window, so downstream systems still need token-aware
  fencing. `signal` requires `shareProcessNamespace: true`, letting every
  container in the pod see and signal every other one (weaker isolation).
  Restart re-gating adds sidecar complexity (it must hold the gate until
  reacquire). `probe` cannot be validated at admission because the webhook
  cannot introspect image contents, so a probe that can never pass will
  crashloop the container.
- **Update (SKA-449)**: `signal` enforcement is scoped by the
  `berth.skaphos.io/signal-target` annotation (env `BERTH_SIGNAL_TARGET`),
  matched against process `comm`/executable basename, so only the workload
  process is signaled and co-located sidecars are spared. A later operator-audit
  fix made the target **required** for `signal` in `runtime-singleton`: the
  webhook rejects an unscoped signal Pod at admission and a directly-run helper
  fails the same validation, rather than falling back to the broad
  signal-everything behavior. `probe` stays the default.
- **Neutral**: `enforce-grace-seconds` tunes the SIGTERM→SIGKILL delay for
  `signal`; a static check binary on the shared volume keeps `probe` viable for
  distroless/scratch images.

## Pros and Cons of the Options

### A. probe (injected liveness probe + marker)
- Good, because the kubelet does the kill and container isolation is preserved;
  no extra RBAC.
- Bad, because it depends on the probe being able to run in the target image and
  cannot be validated at admission.

### B. signal (shareProcessNamespace + SIGKILL)
- Good, because it is immediate and works without a probe-capable image.
- Bad, because it weakens pod isolation and PID-1 signal semantics vary by
  image.

### C. Log-only / app reacts
- Good, because it is trivial to implement.
- Bad, because it provides no enforcement for the exact workloads this path
  exists to serve (unmodifiable images) — rejected.

### D. Sidecar deletes the Pod via the API
- Good, because it removes the whole pod, not just a container.
- Bad, because it needs pod-delete RBAC per workload, races with controllers,
  and is far heavier than gating the main container — rejected.

## Links

- Design: `docs/design/2026-05-workload-gating-injection-model.md`
  ("Active Enforcement (v1)", "Restart Re-Gating")
- Code: `internal/lease/manager.go` (same-holder acquire/renew semantics)
- Related ADRs: [ADR-0001](0001-pod-level-gating-for-injected-singletons.md),
  [ADR-0002](0002-label-annotation-opt-in-over-crd.md)
- Linear: SKA-274, SKA-437, SKA-438 (helper), SKA-439 (webhook), SKA-436
