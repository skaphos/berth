# 4. The shared state volume is a trust boundary, not shared scratch space

- **Status**: accepted
- **Date**: 2026-08-09
- **Deciders**: Shawn Stratton
- **Consulted**: Skaphos team
- **Informed**: Berth contributors

Amends [ADR-0003](0003-sidecar-runtime-enforcement-by-container-kill.md),
which remains accepted: killing the main container is still the enforcement
mechanism, and `probe` is still the default. This ADR narrows the trust model
ADR-0003 assumed for the volume that carries that enforcement.

## Context and Problem Statement

ADR-0003 chose to enforce at-most-once by failing an injected liveness probe,
backed by a health marker on a shared `emptyDir`. It treated the volume as a
transport for state between the injected helpers and the probe, and did not
say who else may write to it.

Two verified defects came from that gap.

**The workload could write the volume (#96).** The webhook forced the
workload's state mount read-only only when its path equalled `StateDir`, and
`preflight` rejected only a *different* volume mounted there. A second mount of
the same `berth-state` volume at another path stayed writable. Both mounts
share one `emptyDir`, so a workload could recreate the marker after the sidecar
removed it.

**The verifier lives in the volume it verifies.** `InstallCheckBinary` copies
the running helper executable to `<StateDir>/check`, and the kubelet execs that
binary every two seconds. Write access therefore lets a workload replace the
*verifier*, not merely the marker — between probe invocations, with a two
second window.

That second point is what forces the shape of the fix. Any scheme that
authenticates the marker — signing it, embedding an expiry, adding a token —
is defeated by replacing the binary that would check it, and by replacing any
key shipped alongside it. Marker integrity cannot be established from inside a
volume the adversary can write.

**Enforcement also depended on a live sidecar (#98).** The probe passed on
marker existence alone, so every path to enforcement ran through the sidecar
deleting the file. A dead or crash-looping sidecar left the marker in place,
the workload passing liveness, and the lease free to expire under it.

## Decision Drivers

- Constitution X: safety must not depend on well-behaved clients, and must
  hold under arbitrary interleaving rather than on the happy path.
- Constitution IX: whatever the code does not guarantee must be stated, not
  implied.
- ADR-0003's own premise — that `probe` "preserves container isolation" — is
  only true if the workload cannot reach the artifacts the probe depends on.
- The probe runs inside the workload's container, which may be distroless and
  has no Berth configuration of its own.

## Considered Options

1. **Make the marker unforgeable** — sign it, or embed a validated expiry.
2. **Reserve the volume at admission** — refuse any workload-authored writable
   mount of it.
3. **Move enforcement state off the shared volume** — a separate channel the
   workload cannot reach at all.

## Decision Outcome

**Chosen: option 2, reserve the volume at admission.**

Admission rejects any Pod whose author-declared containers mount `berth-state`
writably, at any path, in either enforce mode, on Pod creation and on the
`pods/ephemeralcontainers` subresource. Injector-owned mounts keep write
access. Read-only author mounts remain allowed.

The volume's contents — holder, token, marker, and the `check` binary — are
now a **trusted set**: written only by injected helpers, readable by the
workload, writable by nothing else.

Freshness is layered on top: the probe fails when the marker is absent *or*
older than one lease TTL, so enforcement no longer depends on a live sidecar
acting. That check is only meaningful because option 2 makes the verifier
trustworthy — which is why the two shipped as one unit of work, in that order.

Because the rule is a safety control rather than a convenience mutation, the
chart's `failurePolicy` default moves from `Ignore` to `Fail`. Under `Ignore`
the rule lapsed silently for the duration of any webhook outage.

**Option 1 was rejected on the merits, not on cost.** It does not work here.
The verifier and any key ship inside the writable volume, so the attacker
replaces the checker rather than forging the input.

**Option 3 was rejected as disproportionate.** There is no channel available
to a distroless main container that does not involve the shared filesystem;
reaching for one would mean a network dependency or an exec-based probe, both
of which give up the properties ADR-0003 chose `probe` for.

### Consequences

**Good**

- At-most-once no longer depends on the workload behaving, which is the
  guarantee Berth exists to provide.
- The trust boundary is explicit and enforced at admission rather than assumed.
- A dead sidecar now stops its workload within one TTL instead of never.
- Probe failures distinguish "lease lost" from "sidecar dead", which were
  previously indistinguishable.

**Bad**

- Breaking. Pods that mount the state volume writably admitted before and do
  not now, with no opt-out and no grace period.
- The operator becomes a hard dependency for Pod creation in gated namespaces
  under `failurePolicy: Fail`.
- `signal` mode keeps a residual gap: the freshness backstop needs the
  container's single `livenessProbe` slot, and `preflight` steers users to
  `signal` precisely when that slot is taken. Those Pods still lose enforcement
  if their sidecar dies. Documented as a limitation and tracked for a watchdog
  mechanism.

**Neutral**

- The freshness bound reaches `check` as a command-line argument rather than
  configuration, keeping the probe path free of config, network, and
  filesystem dependencies beyond the marker itself.

## Links

- Supersedes the trust model in
  [ADR-0003](0003-sidecar-runtime-enforcement-by-container-kill.md); the
  enforcement mechanism there is unchanged.
- [Workload gating via injection](../workload-gating-injection.md)
- [Upgrade notes](../operations/upgrade-state-volume-trust.md)
- Issues #96 (probe enforcement bypass) and #98 (marker freshness).
