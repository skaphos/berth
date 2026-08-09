# Feature Specification: State-Volume Trust and Marker Freshness

**Feature Branch**: `002-state-volume-trust`

**Created**: 2026-08-08

**Status**: Draft

**Input**: User description: "Fix the runtime-singleton enforcement defects
tracked in GitHub issues #96 (probe enforcement is bypassable via a second
read-write mount of the state volume) and #98 (the health marker has no
freshness check, so a dead sidecar leaves the workload running unleased).
Decision taken with the maintainer: for #96, reject offending pods outright at
admission — no opt-out annotation and no warn-then-enforce grace period —
accepting that pods which admit today may fail on upgrade. Scope note added
during analysis: the exploit is broader than #96 as filed, because the probe's
`check` binary lives inside the same shared volume, so marker-integrity
schemes alone cannot close it."

## Context

Berth's `runtime-singleton` mode promises **at-most-once**: the injected
sidecar renews the lease and, on loss, actively stops the main container
(`docs/workload-gating-injection.md`). With the default `enforce: probe`, that
promise is carried entirely by one shared `emptyDir`:

- the webhook injects a liveness probe `exec`ing
  `<StateDir>/check check <StateDir>/healthy`, `periodSeconds: 2`,
  `failureThreshold: 1` (`internal/webhook/inject.go:477-488`);
- the sidecar creates the marker on every successful renew and removes it on
  lease loss (`internal/acquire/enforce.go:33-34`, driven from
  `internal/acquire/renew.go`);
- the helper copies its own executable to `<StateDir>/check` so the probe works
  in a distroless image with no shell (`internal/acquire/state.go:107-116`).

Two verified defects break that carrier:

- **#96 (high, security)**: the webhook forces the workload's state mount
  read-only **only** for the mount whose path equals `StateDir`
  (`inject.go:489-500`), and `preflight` rejects only a *different* volume
  mounted at `StateDir` (`inject.go:253-257`). Nothing rejects a **second
  mount of the same `berth-state` volume at another path**, which stays
  writable. Because both mounts share one `emptyDir`, a workload with such a
  mount can recreate `healthy` after the sidecar removes it — and, since the
  verifier lives in the same volume, can also **replace the `check` binary
  itself** between probe invocations. At-most-once is defeated in any
  environment that policy-enforces the inject label.
- **#98 (high)**: the probe passes on marker **existence** alone — `check` is
  a bare `os.Stat` with no content or age test
  (`cmd/berth-acquire/main.go:113-119`), as is `State.IsHealthy()`
  (`state.go:99-102`). All enforcement therefore depends on a *live* sidecar
  to delete the file. If the sidecar dies or crash-loops, the marker persists,
  the workload keeps passing liveness, nobody renews, the lease expires, and a
  standby starts — two live instances for the whole sidecar-down window.

The two are one subject: **what the probe is entitled to trust about the state
volume**. They are specified together because #96 determines whether #98 is
worth anything — a freshness rule evaluated by a binary an attacker can
replace protects nothing — and because both amend the trust model recorded in
ADR-0003 (`docs/adr/0003-sidecar-runtime-enforcement-by-container-kill.md`).

Ordering is therefore normative, not stylistic: **US1 before US2**.

## Clarifications

### Session 2026-08-08

- Q: What is the freshness bound for the health marker? → A: One lease TTL
  since the last successful renew, measured from the marker's mtime.
- Q: Should the mount rule extend beyond CREATE on pods to cover ephemeral
  containers (`kubectl debug`)? → A: Yes — extend the webhook to the
  `pods/ephemeralcontainers` subresource and apply the same rejection.
- Q: What observability should the change require? → A: Both — a distinct
  failure reason for a stale versus absent marker, and a metric counting
  admission rejections.
- Q: How should operators find workloads the new rule will reject, before
  upgrading? → A: A documented `kubectl`/`jq` recipe in the upgrade notes,
  runnable against an un-upgraded cluster.
- Q: US2 fixes the dead-sidecar hole for `probe` mode only — what about
  `signal` mode? → A: Add a backstop there too, rather than documenting the
  gap: inject a freshness-only liveness probe in `signal` mode as a
  dead-sidecar net, where the pod admits one.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) — #96

A platform team enforces the injection label across a namespace, so every
workload there is gated by Berth. An application author — malicious, or merely
trying to write scratch data — adds a second `volumeMounts` entry pointing at
the injected `berth-state` volume at a path of their choosing. The system must
make this impossible to admit: whatever the workload does inside its
containers, it must not be able to influence the artifacts the probe depends
on.

**Why this priority**: this is the at-most-once guarantee itself, defeated by
a pod-spec edit any workload author can make, in exactly the deployment shape
(label-enforced injection) the feature is sold for. Constitution X: safety
must not depend on well-behaved clients.

**Independent Test**: submit pods carrying a second mount of the injected
state volume — writable, read-only, at various paths, via `volumeMounts` on
main containers, on the injected helpers, and on unrelated sidecars — and
assert admission rejects exactly the ones that would grant the workload write
access, with a message naming the offending container, volume, and mount path.

**Acceptance Scenarios**:

1. **Given** a pod opted into `runtime-singleton` with `enforce: probe`,
   **When** it declares a writable mount of the state volume at any path other
   than the injected read-only mount, **Then** admission rejects it and names
   the offending container, volume, and mount path.
2. **Given** the same pod with that extra mount marked `readOnly: true`,
   **When** it is admitted, **Then** injection proceeds — read access is not a
   bypass, and refusing it would break legitimate debugging.
3. **Given** a pod whose only state-volume mount is the one the injector owns,
   **When** it is admitted, **Then** the mount is read-only in every main
   container and the pod is otherwise unchanged from today's behavior.
4. **Given** a workload container that is admitted, **When** it attempts to
   create, modify, or delete `healthy` or `check` at any path visible to it,
   **Then** the write fails and enforcement is unaffected.
5. **Given** a pod using `enforce: signal` rather than `probe`, **When** it
   declares extra state-volume mounts, **Then** the rule still applies — the
   sidecar's holder/token state is not workload-writable in either mode.
6. **Given** an admitted, running, gated pod, **When** someone attaches an
   ephemeral container (for example via `kubectl debug`) that mounts the
   state volume writably, **Then** the request is rejected — the rule is not
   confined to pod creation.

---

### User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2) — #98

An operator's sidecar is OOM-killed, or crash-loops on a transient startup
failure (for example `NewFileTokenSource` failing when the broker token file
is briefly absent, which exits non-zero and can back off up to five minutes).
Nobody is renewing the lease. The workload must stop on its own, without
needing the sidecar to come back and remove the marker.

**Why this priority**: it converts enforcement from "depends on a live sidecar
acting" to "depends on the sidecar continuing to prove liveness", which is the
only form that survives the sidecar's own failure. It is P2 because it is
worthless until US1 lands: a stale-marker rule is evaluated by `check`, which
US1 is what makes trustworthy.

**Independent Test**: with the sidecar stopped, assert the probe begins failing
once the marker is one TTL old, and that it does not fail while the sidecar is
renewing normally — including across a renew that fails transiently but within
the TTL.

**Acceptance Scenarios**:

1. **Given** a healthy holder, **When** the sidecar stops updating the marker
   for longer than the freshness bound, **Then** the probe fails and the
   kubelet kills the main container.
2. **Given** a healthy holder renewing normally, **When** the probe runs at any
   point between renewals, **Then** it passes — a correctly-renewing sidecar
   never trips the freshness rule, including at the maximum interval between
   two successful renewals.
3. **Given** a sidecar whose renewals are failing transiently but whose lease
   has not expired, **When** the probe runs, **Then** it passes until the
   lease expiry passes, matching the existing past-expiry self-fence rather
   than pre-empting it.
4. **Given** a pod that has just started, **When** the probe runs before the
   first renewal completes, **Then** the workload is not killed for a marker
   that was never yet written by a renewal.
5. **Given** clock skew between the sidecar and the main container, **When**
   the probe evaluates freshness, **Then** the outcome does not depend on the
   two clocks agreeing.
6. **Given** a pod using `enforce: signal` whose containers define no
   `livenessProbe` of their own, **When** its sidecar dies, **Then** the
   injected freshness-only backstop kills the workload on the same bound as
   `probe` mode — signal enforcement is otherwise unchanged.
7. **Given** a pod using `enforce: signal` whose container already defines a
   `livenessProbe`, **When** the backstop cannot be injected, **Then** the
   resulting coverage gap is surfaced explicitly rather than silently
   admitted as if protected.

---

### User Story 3 - A rejected pod tells its owner exactly what to change (Priority: P3)

An operator upgrades Berth. A workload that admitted yesterday now fails
admission because of US1. The person who sees the failure is usually the
application owner, not the platform team that enabled injection. They must be
able to resolve it from the error and the docs alone, without reading Berth's
source.

**Why this priority**: US1 is a deliberate breaking change with no opt-out, so
the quality of the rejection path is what makes it survivable. It is separable
from the mechanism and can ship alongside it. Constitution IX: state plainly
what changed and what the limitation is.

**Independent Test**: read the rejection message and the docs as an
uninitiated workload owner and confirm both the cause and the fix are stated;
verify the upgrade note exists in the release documentation.

**Acceptance Scenarios**:

1. **Given** a pod rejected by the US1 rule, **When** the owner reads the
   admission error, **Then** it names the offending container, volume, and
   mount path, says why the mount is refused, and states the accepted
   resolutions.
2. **Given** an operator planning an upgrade, **When** they read the release
   and gating documentation, **Then** the change is called out as breaking,
   with a way to find affected workloads before upgrading.
3. **Given** the published gating documentation, **When** an author designs a
   pod that needs scratch space, **Then** the docs state that the state volume
   is reserved and that a separate volume is the supported approach.

---

### Edge Cases

- **The verifier is in the blast radius.** `check` is copied into the shared
  volume (`state.go:107-116`) and re-`exec`ed by the kubelet every two
  seconds. Any scheme that authenticates the *marker* while leaving the
  *verifier* writable is defeated by replacing the verifier; this is why US1
  is a mount-admission rule and not a marker-signing rule.
- **`check` must stay dependency-free.** It runs inside the main container,
  which may be distroless, and deliberately constructs no config or client
  (`cmd/berth-acquire/main.go:107-119`). A freshness bound must reach it
  without new configuration plumbing or filesystem lookups beyond the marker.
- **Two implementations of "healthy".** `State.IsHealthy()` and the `check`
  subcommand independently `os.Stat` the marker. A freshness rule applied to
  only one of them leaves a divergent second opinion.
- **First-write race — already covered, and must stay that way.** The init
  container's successful acquire writes the marker: `WriteAcquired` ends in
  `MarkHealthy()` (`state.go:40-50`), called from `acquire.go:49`. So the
  marker exists with a fresh mtime before the main container starts, and the
  first renew refreshes it one heartbeat later — under a one-TTL bound there
  is no startup gap to bridge. This is load-bearing for the chosen bound, so
  the plan must not "simplify" marker creation out of the acquire path.
- **Legitimately slow renewals.** A one-TTL bound tolerates the worst-case
  gap between two successful renewals, because validation already requires
  the heartbeat to be strictly less than the TTL — so a healthy holder can
  never age its own marker past the bound. The margin is widest at the
  TTL/3 default and narrowest as the heartbeat approaches the TTL; the plan
  should confirm the boundary case is still safe rather than assume it.
- **Pods that already run with an extra writable mount.** They are admitted
  today and will be rejected after upgrade, including on unrelated
  re-admission events such as eviction and rescheduling. There is no
  grandfathering by decision.
- **The injected helpers themselves** need write access to the state volume;
  the rule must distinguish mounts the injector owns from mounts the pod
  author declared, rather than keying on "writable" alone.
- **Non-injected pods** that happen to declare a volume named `berth-state`
  are not gated by Berth at all and must not be affected.
- **Ephemeral containers are a second admission path.** `kubectl debug` adds
  them through the `pods/ephemeralcontainers` subresource, which the current
  `CREATE`-on-`pods` registration never sees, and they may mount volumes the
  pod already declares. This is a privilege escalation over `kubectl exec`,
  which only ever observes the read-only mount.
- **Rejecting an ephemeral container is a different user experience** from
  rejecting a pod: the pod keeps running and a debugging session is refused.
  The message must make clear that the pod is healthy and the *debug request*
  was denied, so an operator does not read it as a workload failure.
- **The liveness slot is single-occupancy.** A container may define exactly
  one `livenessProbe`, and `preflight` routes users to `signal` precisely
  because theirs is already taken. The `signal`-mode backstop (FR-010a) can
  therefore only be injected where that slot is free; assuming otherwise
  would silently overwrite a user's own health check, which is a worse defect
  than the one being fixed.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Admission MUST reject any pod subject to injection that grants a
  workload-authored container write access to the state volume, at any mount
  path. (#96)
- **FR-001a**: The rule MUST cover every admission path that can introduce
  such a mount, not pod creation alone. The webhook currently registers
  `operations: ["CREATE"]` on `resources: ["pods"]`; it MUST also intercept
  the `pods/ephemeralcontainers` subresource, so an ephemeral container
  attached to a running gated pod cannot mount the state volume writably.
- **FR-002**: The rejection MUST name the offending container, volume, and
  mount path, and MUST state the accepted resolutions. There is no annotation,
  label, or configuration value that permits the rejected shape — the decision
  is to reject outright.
- **FR-003**: Mounts the injector itself creates for the helper containers are
  unaffected; the rule distinguishes injector-owned mounts from
  author-declared ones.
- **FR-004**: A read-only author-declared mount of the state volume remains
  permitted.
- **FR-005**: For every admitted pod, no workload container may create,
  modify, or delete any artifact the probe depends on — at minimum the health
  marker and the `check` binary.
- **FR-006**: The liveness check MUST fail when the health marker's
  modification time is older than one lease TTL, in addition to failing when
  the marker is absent. (#98)
- **FR-007**: The freshness bound MUST NOT fail a holder whose sidecar is
  renewing successfully, at any point in the cycle, including the maximum
  gap between two successful renewals permitted by configuration. At the
  default heartbeat of TTL/3 this leaves roughly a threefold margin;
  configuration MUST NOT be able to erase it, since validation already
  requires the heartbeat to be strictly less than the TTL.
- **FR-008**: The freshness evaluation MUST NOT depend on agreement between
  the sidecar's clock and the main container's clock. Comparing the marker's
  modification time against the reader's own clock satisfies this: both
  containers observe one node kernel clock through the shared volume.
- **FR-009**: The freshness rule MUST apply identically to both implementations
  of the health test (`State.IsHealthy()` and the `check` subcommand); they
  MUST NOT be able to disagree.
- **FR-010**: `check` MUST remain runnable in a distroless main container with
  no configuration, no network, and no dependency beyond the state volume.
- **FR-010a**: `enforce: signal` MUST also get a dead-sidecar backstop, not
  only `probe`. Where the pod permits it, the webhook injects a
  freshness-only liveness probe in `signal` mode — enforcement stays
  signal-driven; the probe exists solely so a dead sidecar cannot leave the
  workload running unleased.
- **FR-010b**: That backstop is not universally injectable, and the spec does
  not pretend otherwise. `preflight` steers users to `signal` precisely when a
  container already defines its own `livenessProbe`
  (`internal/webhook/inject.go:247-249`), and Kubernetes permits only one per
  container. For those pods the plan MUST either identify a mechanism that
  does not consume the liveness slot, or record the residual gap as a stated
  limitation — it MUST NOT be left silent. (Constitution IX)
- **FR-011**: Regression tests MUST cover the #96 bypass (including the
  verifier-replacement variant) and the #98 stale-marker window, and MUST fail
  against the pre-fix behavior.
- **FR-011a**: A failed liveness check MUST state which condition failed —
  marker absent, or marker stale — and a stale result MUST report the
  observed age and the bound it exceeded. An operator MUST be able to tell an
  expected lease-loss kill from a dead-sidecar kill without reading Berth's
  source. (Constitution VI)
- **FR-011b**: The webhook MUST expose a counter of admissions rejected by the
  FR-001 rule, labelled enough to identify what is being rejected and through
  which admission path, so operators can see whether the new rule is firing
  across a fleet during and after upgrade.
- **FR-012**: Documentation MUST describe the delivered behavior: the state
  volume is reserved and workload-writable mounts of it are refused; the probe
  tests freshness as well as presence; and the upgrade is breaking.
- **FR-012a**: The upgrade notes MUST include a `kubectl`/`jq` recipe that
  lists pods declaring a writable mount of the state volume, runnable against
  an **un-upgraded** cluster so operators can assess exposure before
  installing the change. The recipe MUST be kept consistent with the FR-001
  predicate, and the docs MUST state that it is a best-effort inventory
  rather than the enforcement path itself.
- **FR-013**: ADR-0003's trust model MUST be brought in line with the
  delivered behavior — amended by a superseding ADR, not rewritten — since it
  is the record of why enforcement is carried by a shared volume.
- **FR-014**: Any change under `deploy/helm/<chart>/` MUST bump that chart's
  `Chart.yaml` version in the same change.

### Key Entities

- **State volume**: the shared `emptyDir` (`berth-state`) carrying the holder
  identity, fencing token, health marker, and `check` binary. After this
  change it is *reserved*: writable only by injector-owned mounts.
- **Health marker**: the file whose presence — and, after this change,
  freshness — the liveness probe tests. Rewritten on every successful renew.
- **Check binary**: the helper executable copied into the state volume and
  `exec`ed by the probe inside the main container. Part of the trusted set,
  not merely a convenience.
- **Freshness bound**: the maximum age of a marker that still counts as
  healthy — one lease TTL, measured from the marker's modification time —
  conveyed to `check` without new configuration plumbing.
- **Injector-owned mount**: a `volumeMounts` entry the webhook itself adds, as
  distinct from one present in the submitted pod spec.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: No pod spec admits that leaves the health marker or `check`
  binary writable by a workload container — demonstrated over an admission
  test matrix spanning mount path, `readOnly`, container kind, both enforce
  modes, and both admission paths (pod creation and the
  `pods/ephemeralcontainers` subresource).
- **SC-002**: With the sidecar stopped and the state volume untouched, the
  liveness check begins failing once the marker is one TTL old, and the
  workload is killed without any action by the sidecar — bounding the
  two-live-instances window at one TTL plus the probe period, rather than
  the unbounded sidecar-down window it is today.
- **SC-003**: Over a sustained normal run, a correctly-renewing holder records
  zero freshness-induced probe failures.
- **SC-004**: The new regression tests fail against the pre-fix behavior and
  pass after; the full suite passes race-enabled.
- **SC-005**: The rejection message alone is sufficient for a workload owner
  to correct the pod spec, and published docs contain no claim about probe
  enforcement stronger than what the tests demonstrate.
- **SC-006**: Given a killed workload, an operator can determine from
  cluster-visible signals alone — probe failure reason and metrics, without
  reading Berth's source — whether the cause was lease loss or a stale
  marker, and can see a fleet-wide count of admissions rejected by the new
  rule.

## Assumptions

- Rejecting the offending pod shape outright is the maintainer's decision,
  taken with the trade-off understood: pods that admit today may fail on
  upgrade, with no opt-out and no grace period. Any softening (opt-out
  annotation, warn-then-enforce) would be a separate, documented decision,
  and was explicitly declined for this unit of work.
- Workloads legitimately needing scratch space can declare their own volume;
  the state volume has no supported use for workload data. No migration
  tooling is provided beyond guidance for finding affected pods.
- Read access to the state volume from a workload container is not itself a
  vulnerability at this severity: the marker is a presence flag and the
  fencing token is already returned to the holder by the API. The defect
  being fixed is write access. Narrowing read access would be a separate,
  independently-motivated change.
- #96 and #98 are one unit of work: same trust boundary, same ADR, and #98's
  mechanism is only sound once #96 has landed. They are specified together and
  MUST ship in that order.
- The lease TTL is the correct basis for the freshness bound, consistent with
  the existing past-expiry self-fence; no new user-facing tunable is assumed
  necessary, and adding one would be a plan-phase decision.
- Issue #97 (unbounded renew RPCs) shipped separately in #138 and is assumed
  present: the freshness bound's worst case is reasoned about on the basis
  that a renew attempt now returns within one heartbeat.
- `enforce: signal` shares the state volume for holder/token handoff. The US1
  mount rule applies to both modes. The US2 freshness rule was initially
  scoped to `probe`; per the 2026-08-08 clarification it extends to `signal`
  as a dead-sidecar backstop (FR-010a), bounded by the injectability limit in
  FR-010b. Signal-mode coverage is therefore expected to be partial in this
  unit of work, and whatever remains uncovered must be stated in the docs
  rather than left implied.
