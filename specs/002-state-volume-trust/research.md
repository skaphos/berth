# Phase 0 Research: State-Volume Trust and Marker Freshness

Resolves the open questions in [spec.md](./spec.md) that the plan depends on.
Each entry states the decision, why, and what was rejected.

---

## R1 — How does `check` learn the freshness bound without configuration?

**Decision**: Pass the bound as an argument in the injected probe command,
alongside the marker path the webhook already passes:

```text
/berth/check check /berth/healthy --max-age 30s
```

`check` compares `time.Since(stat.ModTime())` against `--max-age`. When the
flag is absent it preserves today's presence-only behavior, so a `check`
binary from an older helper image and a newer probe spec (or the reverse) both
degrade to something well-defined rather than crashing.

**Rationale**: The webhook already resolves the TTL when it builds the pod, and
already templates the marker path into the probe command
(`inject.go:477-484`). Passing one more argument reuses a channel that exists,
adds no config parsing, no env lookup, and no file reads — satisfying FR-010's
requirement that `check` stay dependency-free in a distroless container.

**Alternatives considered**:
- *Embed a deadline in the marker contents.* Rejected. It makes the marker a
  parsed format rather than a flag, which means version skew between the
  writer and reader becomes a correctness problem. It also invites the
  signed-marker thinking the spec already ruled out: content-based schemes are
  worthless while the verifier shares the volume.
- *Read the TTL from an env var in the main container.* Rejected. The webhook
  would have to inject `BERTH_*` env into the *workload's* containers, which
  is a far more invasive mutation than adding a probe argument, and it is
  visible to the workload as apparent configuration it does not own.
- *Compile a constant into `check`.* Rejected. TTL is per-lease; a constant is
  either wrong for most leases or must be conservative enough to be useless.

---

## R2 — A `signal`-mode backstop that does not consume the liveness slot

**Context**: FR-010a requires a dead-sidecar backstop in `signal` mode.
FR-010b records why it cannot always be a liveness probe: `preflight` sends
users to `signal` precisely when the container already defines one
(`inject.go:247-249`), and Kubernetes permits exactly one per container.

**Decision**: Two tiers, in this order.

1. **Where the main container's liveness slot is free** — inject the same
   freshness-only probe as `probe` mode. Enforcement stays signal-driven; the
   probe exists solely as the dead-sidecar net.
2. **Where the slot is occupied** — recommend a **watchdog container**: a
   second injected container running a new `berth-acquire watchdog`
   subcommand that polls marker freshness and, when the marker goes stale,
   signals the workload exactly as the sidecar's `signalEnforcer` would.

The watchdog is feasible specifically because `signal` mode *already* sets
`shareProcessNamespace: true` (`inject.go:472-475`) to let the sidecar signal
the workload. Any container in that pod can do the same. The backstop needs no
new pod-level capability — it reuses one the mode already requires.

**Rationale**: The occupied-slot case is not a corner: it is the *documented
reason* users end up in `signal` mode at all, so leaving it uncovered would
mean the majority of signal-mode pods keep #98 in full. The watchdog is
independent of the sidecar's process, which is the whole point — a backstop
sharing the sidecar's fate is not a backstop.

**This is the plan's most consequential proposal and is flagged for the plan
review gate.** It adds a container to signal-mode pods. If that cost is
unacceptable, the fallback is tier 1 only, plus an explicit documented
limitation per FR-010b — which is a smaller change and still an improvement
over today.

**Alternatives considered**:
- *A liveness probe on the sidecar container itself.* Rejected as insufficient.
  It catches a wedged-but-alive sidecar (kubelet restarts it), but not a
  sidecar in `CrashLoopBackOff`, which is the scenario US2 actually describes.
  Worse, it interacts badly with **#109**: on restart, `loadHandoff` sets
  `expiresAt = now + TTL` optimistically (`renew.go:53-57`), so a restarted
  sidecar will not self-fence for a full TTL even when the lease expired long
  ago. Relying on sidecar restart as the backstop would inherit that bug.
- *Overwrite the workload's existing `livenessProbe`.* Rejected outright.
  Silently replacing a user's health check is a worse defect than the one
  being fixed.
- *`startupProbe`.* Rejected — it stops running once startup succeeds, so it
  cannot detect a sidecar that dies later.
- *`readinessProbe`.* Rejected — it removes the pod from Service endpoints but
  does not stop the workload, so it does not deliver at-most-once.

---

## R3 — Which admission paths must the rule cover?

**Decision**: `CREATE` on `pods`, plus `CREATE` on the
`pods/ephemeralcontainers` subresource. `UPDATE` on `pods` is **not** needed
for the mount rule.

**Rationale**: A pod's `volumes` and its containers' `volumeMounts` are
immutable after creation; the mutable paths are the ephemeral-containers
subresource (which may mount volumes the pod already declares) and a small set
of fields irrelevant here. So creation plus ephemeral containers is the
complete set of ways a writable state-volume mount can enter a pod.

Verified against the shipped configuration, which registers only:

```yaml
# deploy/helm/berth-operator/templates/mutatingwebhookconfiguration.yaml:33-35
apiVersions: ["v1"]
operations: ["CREATE"]
resources: ["pods"]
```

**Note for implementation**: the ephemeral-containers admission request
carries the *whole pod* with the proposed ephemeral containers appended, so the
same predicate over "author-declared containers" applies with no new logic —
only the rule registration and the rejection wording differ (the spec's edge
case requires the message make clear the pod is healthy and the *debug
request* was refused).

**Alternatives considered**:
- *Also register `UPDATE` on pods.* Rejected as unnecessary given mount
  immutability, and it would put the webhook in the path of every pod status
  write — a significant and pointless load increase.

---

## R4 — `failurePolicy: Ignore` and the value of US1

**Finding**: US1 is a security control enforced by a webhook whose shipped
default is `failurePolicy: Ignore`
(`deploy/helm/berth-operator/values.yaml:157`). While the webhook is
unavailable, offending pods admit unenforced.

**Decision**: **Do not change the default in this unit of work.** Instead:

1. Document the dependency explicitly — US1's guarantee holds only while the
   webhook is reachable, and operators who want it unconditionally must set
   `failurePolicy: Fail`.
2. Add a pointed cross-reference to **#103**, which is exactly this question,
   so the two are not resolved independently.

**Rationale**: Flipping to `Fail` means a webhook outage blocks pod creation in
scope — a real availability trade, and one that belongs to the operator, not
to this spec. It is also separable: the mechanism US1 introduces is identical
under either policy. Constitution VII prefers read-only degradation over
blindness, and `Ignore` is the failure mode aligned with that; the tension
with Principle X is genuine, which is why it is documented rather than
silently resolved.

**Raised for the maintainer at the plan gate**, since it materially bounds
what US1 delivers, and reasonable people would want `Fail` once the webhook
carries an at-most-once control.

**Alternatives considered**:
- *Flip to `Fail` here.* Rejected as out of scope and as a larger operational
  change than the defect fix warrants — but it is a legitimate choice if the
  maintainer prefers it, and would be a one-line values change plus a chart
  version bump.
- *Say nothing.* Rejected. Shipping a security control while leaving its
  fail-open default undocumented is precisely the silent divergence
  Constitution IX forbids.

---

## R5 — Keeping the two health tests from disagreeing (FR-009)

**Decision**: Extract the freshness predicate into one exported function in
`internal/acquire` — taking a marker path and a max age, returning a typed
result that distinguishes *absent* from *stale* (with the observed age). Both
`State.IsHealthy()` and the `check` subcommand call it. Add a test asserting
the two callers return the same verdict across a table of ages including the
exact boundary.

**Rationale**: FR-009 forbids divergence, and today the two paths are
independent `os.Stat` calls in different packages
(`state.go:99-102`, `cmd/berth-acquire/main.go:113-119`) — the exact shape
that drifts. Sharing the predicate rather than the caller preserves FR-010:
the `check` path still constructs no config.

The typed result also feeds FR-011a directly: the distinct reason the probe
must report is a property of the predicate, not something the caller
reconstructs.

**Alternatives considered**:
- *Have `check` shell out to the sidecar.* Rejected — no network or IPC is
  available to a distroless main container, and it would make the probe depend
  on the very process it exists to check.
- *Delete `State.IsHealthy()`.* Deferred to implementation. It may turn out to
  have no remaining callers outside tests, in which case removing it satisfies
  FR-009 trivially. Worth checking before building the shared predicate.

---

## R6 — Where the rejection counter lives (FR-011b)

**Decision**: Register a `CounterVec` on the operator's controller-runtime
metrics registry, labelled by rejection reason and admission path
(`pods` vs `pods/ephemeralcontainers`). Do **not** label by pod or namespace
name.

**Rationale**: The repo already has an established Prometheus pattern
(`internal/metrics/metrics.go:50-70` builds `CounterVec`s and registers them),
so this follows house style. Omitting pod/namespace labels avoids unbounded
cardinality — a rejected pod may be recreated by a controller in a hot loop,
which would otherwise mint a new series per attempt.

**Note**: the existing `internal/metrics` package serves the **apiserver**;
the webhook runs in the **operator**, which exposes metrics through
controller-runtime. Implementation should confirm whether to extend the shared
package or register locally in the operator rather than assuming either.

**Alternatives considered**:
- *Kubernetes Events on the rejected pod.* Rejected as the primary signal —
  a rejected pod does not exist, so there is nothing to attach an event to.
  The synchronous admission error is the per-request explanation (FR-002); the
  counter is the fleet-wide one.
