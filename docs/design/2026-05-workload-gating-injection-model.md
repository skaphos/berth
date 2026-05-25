# Workload Gating via Init-Container Injection (Design)

- **Status**: Accepted (rev. 2026-05-25) — decisions ratified in ADRs [0001](../adr/0001-pod-level-gating-for-injected-singletons.md), [0002](../adr/0002-label-annotation-opt-in-over-crd.md), [0003](../adr/0003-sidecar-runtime-enforcement-by-container-kill.md)
- **Design ticket**: [SKA-437](https://linear.app/rillan/issue/SKA-437) (this document is its deliverable)
- **Track**: [SKA-274](https://linear.app/rillan/issue/SKA-274) — children [SKA-438](https://linear.app/rillan/issue/SKA-438) (helper), [SKA-439](https://linear.app/rillan/issue/SKA-439) (webhook), [SKA-440](https://linear.app/rillan/issue/SKA-440) (Helm), [SKA-441](https://linear.app/rillan/issue/SKA-441) (docs/test)
- **Parent Epic**: [SKA-268](https://linear.app/rillan/issue/SKA-268) (Cross-cluster singleton Deployment via Berth lease)
- **Related**: [SKA-271](https://linear.app/rillan/issue/SKA-271) (operator-as-holder), [SKA-436](https://linear.app/rillan/issue/SKA-436) (cross-cluster failover-after-expiry fix)

## Context and Problem

Berth provides distributed lease coordination so that only one instance of a workload is active across multiple clusters at a time.

There are three primary ways users can participate in lease holding:

1. **Direct integration** — Application code uses the Berth Go client (or CLI) to acquire/release leases.
2. **Operator-as-holder** (SKA-271) — The per-cluster Berth operator holds the lease on behalf of a `BerthLease` target (Deployment, StatefulSet, etc.) and applies scale or suspend actions. No changes to the application image.
3. **Injected helper (this design)** — For workloads where the image cannot be changed (legacy, vendor, unmaintained), a small `berth-acquire` helper is injected via a mutating admission webhook. The helper participates in the lease using pod-template labels/annotations.

This document defines the contract, mechanisms, and trade-offs for approach #3.

### Primary Motivations

- **Legacy / vendor / unmaintained images**: The container image cannot reasonably be modified to embed lease client logic.
- **Active/warm standby + controlled failover**: In addition to automatic leader-election style singletons, operators may need a way for automation or humans to *select* which standby becomes active and trigger a deliberate, clean whole-system failover. This draft reserves space for that behavior but does not claim it exists until the release-and-selection protocol is designed.

## Goals

- Provide a safe, minimal, label/annotation-driven opt-in mechanism.
- Support two explicitly different injected modes:
  - **Startup gate**: block pod startup until an initial lease acquire succeeds, with no runtime lease guarantee after the init container exits.
  - **Runtime singleton**: block startup, keep a helper sidecar running, renew the lease while the pod is active, **actively stop the main container if the lease is lost**, and release best-effort on shutdown. Active enforcement (the kill) is in scope for v1, not deferred.
- Preserve a path for external actors (automation, CLI, humans) to drive explicit failover by selecting the active system once the runtime singleton release-and-selection protocol is defined.
- Reuse as much of the existing Berth lease model, auth, and client machinery as possible.
- Keep the mutation surface narrow (pod templates only) and safe.

## Non-Goals (v1)

- New high-level CRD for workload gating.
- Automatic traffic steering or readiness gating driven by the helper (future work).
- Full replacement of operator-as-holder for most users.

## Activation Model: Pod-Level Gating, Not Scale Actions

This is the most important architectural decision in this design, and it
diverges from the earlier framing in the parent tickets.

The operator-as-holder path (SKA-271) activates a workload by **scaling it**:
the operator holds the lease on the workload's behalf and applies
`AcquireAction` / `ReleaseAction` (`scale.replicas`) on a `BerthLease` to bring
the Deployment up or down. That works because the operator sits *outside* the
workload.

The injected path is fundamentally different: the helper runs **inside the
pod**, and by the time it runs the pod is already scheduled. There is nothing
to "scale to 0" — the pod exists. So the injected path gates at the pod level
instead:

- **Init container** holds the pod at startup until it acquires the lease (the
  "hold").
- **Sidecar** keeps the lease and, on loss, **stops the main container** (the
  enforced "only once").

Consequently, **the injected path does not use `AcquireAction` /
`ReleaseAction` and does not require a `BerthLease` object at all.** The helper
talks to the Berth API lease model directly. A `BerthLease` may still exist and
participate for observability or for an operator-driven workflow, but it is not
on the injected path's critical path.

> **Ticket correction required.** SKA-437 ("Initial workload transition
> mechanism — use the existing `BerthLease` `AcquireAction` + `ReleaseAction`")
> and SKA-274 ("Initial implementation will use scale-up / scale-down via
> `AcquireAction`/`ReleaseAction`") describe the *operator-as-holder* mechanism,
> not this one. Their acceptance criteria for the injected path should be
> updated to "pod-level gating: init-container hold + sidecar enforcement,"
> reusing the lease RPCs (Acquire / Renew / Release) directly rather than CRD
> scale actions.

## Comparison of Lease-Holding Approaches

**Key Decision Factors** (the lenses that matter most for choosing a path):

- **Risk profile** — split-brain window size, fencing surface area, and number of potential concurrent holders
- **Active system selection & controlled failover** — ability for automation or humans to deliberately select which instance becomes active and drive a clean handoff
- **Decision guidance** — clear trade-offs and recommendations for when to use each approach

### Summary Comparison

| Aspect                               | Direct App Integration                          | Operator-as-Holder (SKA-271)                          | Injected `berth-acquire` (this design)                          |
|--------------------------------------|-------------------------------------------------|-------------------------------------------------------|-----------------------------------------------------------------|
| App / image changes required         | Yes                                             | No                                                    | No                                                              |
| Requires a `BerthLease` object       | No — app calls the lease API directly           | Yes — CRD with a `target` ref drives the operator     | No — the helper calls the lease API directly (a `BerthLease` may coexist for observability but is off the critical path) |
| Holder identity cardinality          | Per-process / per-replica                       | One per cluster (the operator)                        | Startup gate: short-lived during init, may be workload-level. Runtime singleton: one unique holder per candidate pod |
| Activation mechanism                 | App code acquires and self-gates                | Operator scales the whole target via `AcquireAction`/`ReleaseAction` | Pod-level: init container gates start; runtime-singleton sidecar additionally enforces at runtime |
| **Runtime enforcement on lease loss** | App must detect loss and stop itself           | Operator scales target to `releaseAction.replicas` (typically 0) | Startup gate: **none** after init exits. Runtime singleton: sidecar **kills the main container** (`enforce=probe`/`signal`) and holds it gated until it re-acquires |
| Primary failover trigger             | Application-driven                              | TTL expiry → operator reacquires & scales             | Startup gate: none after start. Runtime singleton: TTL expiry → standby pod's init/sidecar acquires; (future) explicit selection |
| Active system selection              | Application logic                               | First-to-acquire wins                                 | First-to-acquire in v1; deliberate external selection remains a designed extension |
| Overlap / split-brain window         | App-dependent                                   | TTL window at controller level (one target toggled)   | TTL window **plus** per-pod detection+kill latency; bounded best-effort, not a hard mutex |
| Failover RTO                         | App-defined                                     | ≈ TTL + reacquire interval                            | ≈ TTL + reacquire interval + kill latency                       |
| Run-to-completion (Job/CronJob)      | App-controlled                                  | Poor fit — operator toggles replicas, not per-run gating | Good fit via startup-gate; native sidecar lets runtime-singleton Jobs still complete |
| Rolling-update behavior              | App-defined                                     | Normal rollout                                        | Runtime singleton can stall the new pod up to one TTL waiting to acquire |
| **Risk profile** (split-brain, fencing surface) | Depends heavily on application correctness     | Lower — very small number of holders (one operator per cluster) | Higher — many pods can become holders; larger fencing surface, though runtime enforcement bounds the overlap window |
| GitOps & operational ergonomics      | High (application owns the integration)         | Lowest day-to-day burden                              | Medium (labels + webhook + config discovery)                    |
| Best suited for                      | Actively maintained services that need custom behavior | Most no-code cross-cluster singletons (current recommended path) | Unmodifiable legacy/vendor singleton pods, startup dependency ordering, or future deliberate active/warm handoff |

**Future capabilities** (not committed in v1 of the injected path):
- Richer helper signaling (readiness influence, explicit "I am chosen" handoff protocol)
- Pod readiness / traffic steering integration driven from the lease
- Optional higher-level CRD wrapper over the label/annotation contract

### Recommendation

- **Default / preferred path** for new or actively maintainable workloads: **Operator-as-holder**.
- Use **Direct integration** when the application needs fine-grained control or custom holder/renewal logic.
- Use the **injected path** when you truly cannot change the container image, when startup ordering against a Berth lease is sufficient, or when this design grows first-class support for deliberate, externally-driven active system selection and controlled failover.

The rest of this document and the parent tickets (SKA-268, SKA-271, SKA-274) expand on the risk and selection differences.

## Key Decision Factors

This section expands on the three lenses that should drive most path selection decisions.

### Risk Profile

The dominant risk difference between the approaches is the **number of independent entities that can hold a lease** and therefore the surface area over which correct fencing behavior must be implemented and maintained.

- **Direct integration** places the burden entirely on application developers. If the application correctly uses the fencing token on shutdown, crashes, and restarts, the risk is manageable. Many applications get this wrong at least some of the time.
- **Operator-as-holder** minimizes the number of holders to one long-lived process per cluster. The operator is under our control, has good operational visibility, and performs best-effort release on deletion using the fencing token. This is currently the lowest-risk no-code path.
- **Injected runtime singleton** (v1) creates the largest number of potential holders — every replica of every opted-in workload can attempt to acquire. Each injected helper must correctly capture and use the fencing token on graceful shutdown, OOM kills, node drains, etc. A single buggy or incomplete release path across many pods creates a larger window for split-brain or delayed failover.
- **Injected startup gate** has a different risk profile: once the init container exits, nothing renews the lease. It is not runtime singleton enforcement and must not be documented as one.

The injected path does not inherently have worse fencing *mechanics* (it reuses the same client and token), but it has more places where those mechanics must be applied correctly with less operational visibility. This risk is real and is one of the main reasons the path is positioned as a fallback rather than the default.

### Active System Selection & Controlled Failover

Not all "failover" is the same. There is an important distinction between *automatic recovery after failure* and *deliberate, externally driven handoff*.

- **Direct integration** gives the application full control. It can implement whatever selection or handoff logic it wants (including external signals), at the cost of embedding that logic in every participating binary.
- **Operator-as-holder** is designed around automatic recovery: the first operator that can acquire the lease after the previous holder dies or releases wins. There is currently no first-class mechanism for an external system or human to say "I want cluster B to become active now, force cluster A to release."
- **Injected runtime singleton** is the place to add the second model. In v1, it should only claim first-to-acquire behavior unless a concrete release-and-select protocol is implemented. External automation, another controller, or a human operator may later select a particular standby instance (or cluster) and cause the current holder to release cleanly so the chosen candidate can acquire.

If your use case requires the ability to *choose* the active system rather than just waiting for failover, the injected path needs the runtime singleton mode plus a first-class selection protocol. Until that protocol exists, direct integration is the only path with application-specific selection logic.

### Decision Guidance

Use the following as a starting point (not a rigid rule):

- Choose **Operator-as-holder** by default for cross-cluster singleton or active/standby Deployments and StatefulSets where the image can remain untouched.
- Choose **Direct integration** when the owning team wants (and is capable of) tight control over holder identity, renewal strategy, and fencing.
- Choose the **injected startup gate** when startup should wait for Berth lease availability but no runtime singleton guarantee is required.
- Choose the **injected runtime singleton** when the image is immutable (legacy, vendor, third-party) and exactly one pod candidate should run behind a continuously renewed Berth lease.

The comparison table earlier in this section and the parent Linear tickets provide additional context for edge cases.

## Injected Modes

The injected helper path is split into two modes with different guarantees.
They share the same webhook and helper binary, but they must not be described
as interchangeable.

### Startup Gate

Startup gate mode injects an init container only.

Lifecycle:

1. The init container calls `Acquire` for the configured lease.
2. If the acquire succeeds, the init container exits 0.
3. Kubernetes starts the main containers.
4. No Berth component remains in the pod to renew or release the lease.

Guarantee:

- The main containers do not start until the configured lease is available at
  pod startup time.
- The lease may expire after the init container exits.
- This mode does not provide runtime singleton enforcement.

Use cases:

- Startup ordering.
- Short-lived jobs where acquiring before start is enough.
- Legacy workloads where a brief admission gate is useful but ongoing Berth
  coordination is intentionally out of scope.

Holder identity:

- Startup gate may use a workload-level identity by default because it is only
  proving startup admission.
- If multiple pods share the same holder identity, they can all pass the gate
  through same-holder renewal semantics. That is acceptable only because this
  mode does not claim singleton runtime behavior.

### Runtime Singleton

Runtime singleton mode injects both an init container and a sidecar.

The sidecar is injected as a **native sidecar** (an init container with
`restartPolicy: Always`) so it starts before the main containers, runs
alongside them, and — critically — does not block Job/CronJob completion.

Lifecycle:

1. The init container calls `Acquire`, writes the fencing token and holder
   identity to a shared volume, and writes the health marker so the main
   container's injected liveness probe passes.
2. The sidecar starts before the main containers and continuously calls `Renew`
   using the holder identity and fencing token.
3. **If renew reports the lease is lost, the sidecar actively stops the main
   container** using the configured enforcement mechanism (see "Active
   Enforcement" below), keeps it stopped (marker held red / repeated signal),
   and attempts to re-`Acquire`. The main container is only allowed to run
   again once the sidecar re-acquires.
4. On graceful shutdown, the sidecar performs a best-effort `Release`.

#### Active Enforcement (v1)

Because the target image cannot be modified, the helper cannot rely on the
application to react to lease loss. The sidecar enforces the at-most-once
guarantee at the pod level. Two mechanisms are supported, selected by the
`berth.skaphos.io/enforce` annotation:

- **`probe` (default)** — The webhook injects an `exec` liveness probe on each
  main container (`cat /berth/healthy`) backed by a shared `emptyDir` volume.
  The sidecar writes the marker on acquire and removes it on lease loss; the
  kubelet then kills the container. The sidecar keeps the marker absent until it
  re-acquires, so a kubelet-restarted container stays gated (init containers do
  **not** re-run on a same-pod container restart — see "Restart Re-Gating").
  Native, preserves container isolation, no extra RBAC.
- **`signal`** — The webhook sets `shareProcessNamespace: true`; the sidecar
  finds the main process and sends `SIGTERM` then `SIGKILL`, re-signalling on
  every restart until it re-acquires. More immediate, but every container in the
  pod can see and signal every other one (weaker isolation) and PID-1 signal
  semantics vary by image. Reserved for cases where probe injection is not
  viable (no shell in the image, or a liveness probe is already in use).

Both are **best-effort within a bounded window**: there is a detection +
kill latency between lease loss and the main container actually stopping. This
strengthens at-most-once at the process level but does **not** replace the
fencing token, which remains the real boundary for any downstream resource that
must reject a stale holder.

#### Restart Re-Gating

A kubelet container restart (from a failed probe or a signal kill) does **not**
re-run completed init containers. The sidecar is therefore responsible for
keeping the main container gated after it stops it: it must hold the probe
marker absent (or keep re-signalling) until it has re-acquired the lease, at
which point it restores the marker and lets the main container run. This turns
"lease lost" into a controlled crashloop rather than an unguarded restart.

Guarantee:

- At most one candidate holder runs its main container at a time, subject to the
  store's linearizability, the TTL window, and the enforcement detection+kill
  latency.
- Runtime enforcement lasts only while the sidecar is running and renewing. If
  the sidecar itself dies, the native-sidecar `restartPolicy: Always` restarts
  it; until it is back and has re-confirmed the lease, the marker stays red.
- Strong end-to-end fencing for downstream systems still requires token-aware
  behavior on those systems; pod-level enforcement bounds, but does not
  eliminate, the overlap window.

Use cases:

- Legacy/vendor singleton pods where the application image cannot embed Berth
  client logic.
- A future active/warm handoff flow, once the explicit selection protocol is
  designed and implemented.

Holder identity:

- Runtime singleton must use a unique candidate identity by default. For Pods,
  the default should include namespace, owner reference lineage where
  discoverable, pod name, and cluster identity when available.
- Workload-level holder identities are dangerous in this mode. With Berth's
  current same-holder acquire semantics, multiple pods sharing a holder
  identity can all renew the same lease and all become active.
- Workload-level or cluster-level holder identities may be allowed later only
  with additional protocol support that prevents multiple pods from passing the
  gate under the same holder.

## Proposed Label / Annotation Contract (v1)

This section defines a precise, spec-like contract for the labels and annotations used to opt Pods into the injected `berth-acquire` path.

This contract is the v1 opt-in surface chosen **in lieu of a CRD** — see
[ADR-0002](../adr/0002-label-annotation-opt-in-over-crd.md) and the Decision
Record below.

All keys live under the `berth.skaphos.io/` prefix.

### Label Keys (Selection & Intent)

| Key                  | Type   | Required | Valid Values (v1)     | Description |
|----------------------|--------|----------|-----------------------|-------------|
| `berth.skaphos.io/inject` | string | Yes | `acquire` | Opt-in marker. The webhook will only consider Pods whose pod template labels are copied onto the created Pod. |

Future label (not in v1): `berth.skaphos.io/workload-type`
(`singleton` | `active-warm`).

**Webhook behavior on invalid `inject` label value**: Skip mutation (do not fail the Pod).

### Annotation Keys (Behavior & Configuration)

All annotations are optional unless marked required.

| Key                        | Type    | Required | Default                                      | Format / Constraints                  | Description |
|----------------------------|---------|----------|----------------------------------------------|---------------------------------------|-------------|
| `berth.skaphos.io/lease-name` | string | **Yes** | — | Non-empty DNS label-compatible string | Name of the Berth API lease to acquire. |
| `berth.skaphos.io/lease-namespace` | string | No | Pod's own namespace | Valid namespace name | Namespace of the Berth API lease. |
| `berth.skaphos.io/mode` | string | No | `runtime-singleton` | `startup-gate` \| `runtime-singleton` | Selects the injected helper mode. |
| `berth.skaphos.io/holder-identity` | string | No | Mode-specific default | Non-empty string | Identity used for Acquire/Renew/Release. See "Holder Identity Defaulting". |
| `berth.skaphos.io/ttl-seconds` | int32 | No | Chart/helper default | > 0 | TTL used for Acquire/Renew calls. |
| `berth.skaphos.io/heartbeat-interval-seconds` | int32 | No | Derived from TTL | > 0 and < TTL | Runtime singleton renew interval. Ignored by startup gate after init exits. |
| `berth.skaphos.io/release-on-shutdown` | string | No | `true` in runtime singleton, `false` in startup gate | `true` \| `false` | Whether the sidecar should attempt a best-effort Release on SIGTERM / graceful shutdown. |
| `berth.skaphos.io/enforce` | string | No | `probe` | `probe` \| `signal` | Runtime singleton only. How the sidecar stops the main container on lease loss. `probe`: injected exec liveness probe + shared health marker (kubelet kills). `signal`: `shareProcessNamespace` + SIGTERM/SIGKILL. Ignored by startup gate. |
| `berth.skaphos.io/enforce-grace-seconds` | int32 | No | Derived from pod `terminationGracePeriodSeconds` | ≥ 0 | `signal` mode only. Seconds between SIGTERM and SIGKILL to the main process. |

#### Auth / API Server Discovery (Dual Support)

The contract deliberately does **not** define a single mandated way to reach the Berth API server. Two supported models exist in v1:

- **Direct to central Berth API server**: The helper expects standard Berth client configuration (API URL, credentials, CA bundle) to be present in the environment or via well-known file paths. The user (or platform) is responsible for populating this (via Secret, ConfigMap, projected volumes, etc.).
- **Via local broker**: When a local OIDC / token broker is deployed alongside the Berth operator in the cluster, the helper may obtain tokens and routing information through that local component instead of talking directly to the central API.

**Guidance**:
- Use the local broker path when the platform wants to own auth surface and reduce secret distribution to workloads.
- Use direct central when teams need full control or the local broker is not present / not trusted for this workload.

The helper must support both modes. Exact environment variables, file paths, and token acquisition mechanisms for each mode will be defined during implementation of SKA-438.

### Holder Identity Defaulting

Holder identity defaults are mode-specific because Berth's current acquire
semantics renew a lease when the incoming holder matches the current holder.
That behavior is useful for a long-lived holder, but unsafe if multiple pods
share the same holder identity in runtime singleton mode.

**Startup gate default (v1)**:
- Derive from the owning controller of the Pod when available:
  `namespace` + `kind` + `name`.
- Example: `prod:deployment:checkout-service`.
- This may allow multiple pods from the same workload to pass startup if they
  share the holder identity. That is acceptable only because startup gate does
  not provide runtime singleton behavior.

**Runtime singleton default (v1)**:
- Derive a unique candidate identity from cluster identity, namespace, owner
  reference lineage where discoverable, and pod name.
- Example: `east:prod:deployment:checkout-service:pod:checkout-service-7f6c9b9d8d-j4n8x`.
- If cluster identity is not configured, the webhook or helper must still
  include pod identity so replicas do not share a holder by accident.

**Override behavior**:
- If the `berth.skaphos.io/holder-identity` annotation is present on the Pod
  template, it **completely replaces** the default.
- Overrides in runtime singleton mode are advanced usage. A shared holder
  identity can defeat singleton enforcement unless another mechanism prevents
  multiple pods from passing the gate.

This design intentionally does not make workload-level holder identity the
runtime singleton default.

### Validation & Error Behavior (Webhook)

The webhook must enforce the following in v1:

- If `berth.skaphos.io/inject=acquire` is present but `berth.skaphos.io/lease-name` is missing or empty → **Fail the Pod** (or configurable failure policy).
- Unknown values for `berth.skaphos.io/mode` → reject the Pod in fail-closed mode, or skip mutation only when the Helm-configured failure policy is explicitly `Ignore`.
- Unknown values for `berth.skaphos.io/enforce` → reject the Pod (same fail-policy handling as `mode`).
- `enforce=probe` assumes the main container image can run the marker check (a shell or a static check binary the webhook injects via the shared volume). The webhook **cannot** introspect image contents at admission, so it cannot reject a shell-less image; it must document that a probe that can never pass will crashloop the main container, and recommend `enforce=signal` for such images. Injecting a small static check binary onto the shared volume (so the probe does not depend on a shell in the target image) is the preferred way to keep `probe` viable for distroless/scratch images and should be the default helper behavior.
- `enforce=signal` requires `shareProcessNamespace: true`; the webhook sets it and must reject the Pod if a conflicting explicit `shareProcessNamespace: false` is already set rather than silently overriding it.
- Negative or zero `berth.skaphos.io/ttl-seconds` / `berth.skaphos.io/heartbeat-interval-seconds` → reject the Pod.
- Runtime singleton mode with an explicitly shared holder identity should warn
  through events/logs and may be rejected once the exact validation rule is
  defined.
- The webhook should never mutate Pods belonging to the Berth control plane itself.

More detailed validation rules and failure policy configuration (per-namespace or via webhook configuration) will be defined as part of the webhook implementation (SKA-439).

The exact final set of keys, value formats, and validation rules will be locked during the design review of this document and the implementation of the helper and webhook.

## Webhook Design

- Mutates created Pods whose labels include the opt-in label. Standard
  workload controllers copy `spec.template.metadata.labels` onto created Pods.
  The opt-in label/annotations must therefore live on the pod template:
  - Deployment / StatefulSet / DaemonSet / Job: `spec.template.metadata`.
  - **CronJob**: `spec.jobTemplate.spec.template.metadata` (two levels deeper —
    a common mislabeling trap; call it out in docs and examples).
- Never mutates objects in the Berth control-plane namespaces.
- Idempotent: re-mutation of an already-injected pod is a no-op or safe update.
- Failure policy: configurable (Fail vs. Ignore) via Helm values.
- Object and namespace selectors supported for blast-radius control.

The webhook will inject:
- Startup gate: init container image + command/args.
- Runtime singleton: init container plus a **native sidecar** (init container
  with `restartPolicy: Always`) so Jobs/CronJobs can still complete.
- A shared `emptyDir` volume (`/berth`) for the fencing token, holder identity,
  and the health marker.
- `enforce=probe`: an injected `exec` liveness probe on each main container, plus
  a static check binary on the shared volume so the probe does not require a
  shell in the target image.
- `enforce=signal`: `shareProcessNamespace: true` on the pod spec.
- Environment variables and volume mounts for auth/config.
- Any required service account token or projected volumes.

## Helper Binary (`berth-acquire`) Lifecycle

**Startup gate mode**:
- Blocks until successful `Acquire`.
- Exits 0 so the main container can start.
- Does not renew the lease after exit.
- Does not release the lease on pod shutdown because no helper process remains.
- On failure (policy dependent): crash or exit non-zero.

**Runtime singleton mode**:
- Init container blocks until successful `Acquire`, then writes the fencing
  token, holder identity, and health marker to the shared volume.
- Sidecar performs continuous heartbeats while the main container runs.
- If `Renew` reports lease loss, the sidecar **enforces** per `enforce`: removes
  the marker (`probe`) or signals the main process (`signal`), records a clear
  log/event, keeps the main container stopped, and retries `Acquire`. It
  restores the marker / stops signalling only after re-acquiring. (See
  "Restart Re-Gating".)
- The sidecar reuses the same reacquire-after-expiry logic the operator uses;
  see SKA-436 for the failover-after-TTL-expiry bug this must not reintroduce.
- On SIGTERM / shutdown: best-effort `Release`.

**Explicit Release for Controlled Failover**:
- Runtime singleton mode is the only injected mode where controlled failover
  can make sense.
- The helper may later support being told to release the lease so another
  candidate can acquire.
- The current Berth API requires `holder` plus `fencingToken` to release a
  lease. Any external release/selection design must define how automation
  learns those values without weakening fencing semantics.
- The design must define the exact mechanism for "I am the chosen active
  system" signaling to a standby before claiming deliberate active system
  selection as a v1 behavior.

Deeper coupling (the helper influencing pod readiness, traffic steering, or
`BerthLease` target actions) is deferred.

## Configuration Discovery (GitOps Friendly)

Preferred patterns (in priority order):
1. Projected volumes / mounted Secrets/ConfigMaps (recommended for GitOps).
2. Downward API + well-known paths.
3. Annotation values (less preferred for secrets).
4. Environment variables injected by the webhook from chart defaults.

The helper reuses the existing Berth Go client auth stack (static token, token file, OIDC broker output, TLS CA).

## Workload Examples

### Deployment — runtime singleton (legacy cross-cluster singleton)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout-service
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: checkout-service
        berth.skaphos.io/inject: acquire        # opt-in (copied onto Pods)
      annotations:
        berth.skaphos.io/lease-name: checkout-singleton
        berth.skaphos.io/mode: runtime-singleton
        berth.skaphos.io/enforce: probe
        berth.skaphos.io/ttl-seconds: "30"
    spec:
      containers:
        - name: app
          image: vendor/checkout:1.4.2   # unmodifiable image
```

### StatefulSet — runtime singleton

Identical opt-in on `spec.template.metadata`. Holder identity defaults to a
unique per-pod value, so the stable StatefulSet pod name is folded into the
identity; only one ordinal runs behind the lease at a time.

### Job — startup gate (recommended for run-to-completion)

```yaml
apiVersion: batch/v1
kind: Job
spec:
  template:
    metadata:
      labels:
        berth.skaphos.io/inject: acquire
      annotations:
        berth.skaphos.io/lease-name: nightly-export
        berth.skaphos.io/mode: startup-gate     # gate start; let the Job run/complete
    spec:
      restartPolicy: Never
      containers:
        - name: export
          image: vendor/export:2
```

Runtime singleton on a Job is allowed (native sidecar lets the Job complete),
but startup-gate is usually the right fit: the Job should run to completion once
it has won the lease, not be killed mid-run on a transient renew blip.

### CronJob — note the deeper label path

```yaml
apiVersion: batch/v1
kind: CronJob
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        metadata:                                # <-- spec.jobTemplate.spec.template.metadata
          labels:
            berth.skaphos.io/inject: acquire
          annotations:
            berth.skaphos.io/lease-name: hourly-rollup
            berth.skaphos.io/mode: startup-gate
        spec:
          restartPolicy: Never
          containers:
            - name: rollup
              image: vendor/rollup:7
```

### Resulting mutated Pod (runtime singleton, `enforce=probe`)

```yaml
spec:
  volumes:
    - name: berth-state
      emptyDir: {}
  initContainers:
    - name: berth-acquire            # blocking init: the "hold"
      image: ghcr.io/skaphos/berth-acquire:<tag>
      args: ["acquire", "--lease=checkout-singleton", "--ttl=30s"]
      volumeMounts:
        - {name: berth-state, mountPath: /berth}
    - name: berth-sidecar            # native sidecar: renew + enforce
      image: ghcr.io/skaphos/berth-acquire:<tag>
      restartPolicy: Always
      args: ["renew", "--lease=checkout-singleton", "--enforce=probe"]
      volumeMounts:
        - {name: berth-state, mountPath: /berth}
  containers:
    - name: app
      image: vendor/checkout:1.4.2
      livenessProbe:                 # injected: kubelet kills on lease loss
        exec:
          command: ["/berth/check", "/berth/healthy"]
        periodSeconds: 2
        failureThreshold: 1
      volumeMounts:
        - {name: berth-state, mountPath: /berth, readOnly: true}
```

(With `enforce=signal`, drop the injected probe, set
`spec.shareProcessNamespace: true`, and the sidecar signals the main PID.)

## Decision Record: Labels/Annotations vs. Wrapper CRD

> Recorded as [ADR-0002](../adr/0002-label-annotation-opt-in-over-crd.md). The
> "Proposed Label / Annotation Contract" section above is the concrete opt-in
> surface chosen here in lieu of a CRD.

**Decision (v1): use the label/annotation contract on the pod template; do not
introduce a wrapper CRD yet.**

Rationale:

- The mutation boundary is a Pod; labels/annotations on the pod template are the
  native Kubernetes way to opt in and are copied onto Pods by every standard
  workload controller. A CRD would add a second control loop competing with the
  workload controller for no v1 benefit.
- The injected path deliberately does **not** depend on `BerthLease`; adding a
  new CRD here would re-introduce a control-plane object on a path whose whole
  point is "no extra objects, just gate the pod."
- GitOps users already template labels/annotations; no new API to learn.

Revisit a wrapper CRD when (and only when) any of these become true:

- The contract needs validation/defaulting richer than a webhook on annotations
  can express cleanly.
- We add a first-class "active system selection" primitive (see Open Questions)
  that benefits from its own spec/status.
- Users need a queryable, status-bearing object ("which pod holds lease X?")
  that the Pod + events cannot satisfy.

This is recorded as a decision, not a non-goal: the CRD is deferred with
explicit re-open criteria, and may be extracted into an ADR if pursued.

## Operational Considerations

- **Rolling updates stall up to one TTL (runtime singleton).** With unique
  per-pod holder identity, a new rollout pod cannot acquire until the outgoing
  pod's lease frees. Graceful termination releases it (`release-on-shutdown:
  true`), but `SIGKILL` / OOM / node drain do not — the new pod then blocks at
  its init "hold" for up to the TTL. Keep TTL modest (e.g. 15–30s) for workloads
  that roll often, and document the expected stall.
- **Failover RTO ≈ TTL + reacquire interval**, matching the operator path. The
  sidecar's reacquire-after-expiry path is the same logic SKA-436 fixed for the
  operator; SKA-438 must carry that behavior (and a regression test) so a lost
  holder is actually succeeded.
- **More holders than the operator path.** Every opted-in replica is a potential
  holder, so the enforcement code (kill + re-gate + release) runs in many more
  places with less operational visibility than one operator per cluster. This is
  the central reason the path remains a fallback.

## Security & Safety Considerations

- Webhook must not grant excessive permissions to injected pods.
- Runtime singleton holder identity is per-candidate by default — this
  increases the number of potential holders compared with operator-as-holder.
- Startup gate mode does not provide runtime singleton enforcement after the
  init container exits.
- `enforce=signal` requires `shareProcessNamespace: true`, which lets every
  container in the pod see and signal every other container's processes and
  read `/proc`. Prefer `enforce=probe` (kubelet does the kill, isolation
  preserved); reserve `signal` for images where the probe cannot run.
- Enforcement is bounded best-effort: a detection+kill window exists between
  lease loss and the main container stopping. Fencing token usage remains
  critical for safe release on deletion/shutdown.
- The design must document the larger split-brain window compared with the operator-as-holder path and recommended mitigations.

## Observability

- The helper should emit structured logs and (optionally) metrics around acquire/renew/release/failover events.
- Users must be able to answer "which pod currently holds lease X?" when using this path.
- Events on the Pod and helper logs are required signals. Events on
  `BerthLease` remain useful when a CRD participates in the workflow, but the
  injected helper talks to the Berth API lease model directly.

## Open Questions & Future Work

- Exact shape of the "active system selection" primitive (annotation? separate API? lease status update?).
- Whether the helper should expose a readiness endpoint or file that the main container can use.
- Richer workload integration beyond scale (readiness gates, traffic routing).
- CRD wrapper in a later phase.
- Multi-lease pods and complex holder-identity strategies.

## Decision Records

The architecturally significant decisions in this design are recorded as ADRs
(immutable once accepted; supersede rather than edit):

- [ADR-0001](../adr/0001-pod-level-gating-for-injected-singletons.md) — pod-level
  gating, not `BerthLease` scale actions.
- [ADR-0002](../adr/0002-label-annotation-opt-in-over-crd.md) — label/annotation
  opt-in via a mutating webhook, in lieu of a wrapper CRD.
- [ADR-0003](../adr/0003-sidecar-runtime-enforcement-by-container-kill.md) —
  enforce at-most-once by killing the main container; `probe` default, `signal`
  opt-in.

## References

- Linear: SKA-268, SKA-271, SKA-274, SKA-437 (and children 438–441)
- `api/v1alpha1/types.go` — `BerthLease`, `LeaseAction`, `ScaleAction`
- `docs/architecture.md` — current failure and workload action behavior
- Existing Berth Go client auth and lease package

---

**Resolved in this revision**:
- Active enforcement (sidecar kills the main container on lease loss) is now a
  v1 behavior of runtime singleton, via `enforce=probe` (default) / `signal`.
- Activation model clarified: pod-level gating, not `AcquireAction`/
  `ReleaseAction` scaling — parent tickets need their AC corrected to match.
- Added workload examples, mutated pod spec, and the labels-vs-CRD decision.

**Still open**:
- Review with team and lock the final key/value set.
- Flesh out the explicit external "active system selection" failover mechanism
  (still future; see Open Questions).
- Decide on final home for the comparison matrix (here vs. user docs in SKA-441).
- Confirm the static probe-check binary approach for distroless/scratch images.

This document will be updated as the design progresses. Major decisions may be extracted into separate ADRs in `docs/adr/`.
