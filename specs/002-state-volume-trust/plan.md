# Implementation Plan: State-Volume Trust and Marker Freshness

**Branch**: `002-state-volume-trust` | **Date**: 2026-08-08 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-state-volume-trust/spec.md`

## Summary

Close the two defects that break `runtime-singleton` at-most-once enforcement,
in the order the spec makes normative:

1. **US1 (#96)** — make the state volume *reserved*. Admission rejects any pod
   that grants a workload-authored container write access to it, on pod
   creation and on the `pods/ephemeralcontainers` subresource. This is the
   only mechanism that works, because the probe's verifier (`check`) lives
   inside the volume being protected, so marker-signing schemes are
   self-defeating.
2. **US2 (#98)** — make the probe test *freshness*, not just presence: a
   marker whose mtime is older than one lease TTL fails the check. Extended to
   `enforce: signal` as a dead-sidecar backstop, subject to the liveness-slot
   limit in FR-010b.
3. **US3** — make the resulting breaking change survivable: a rejection
   message that names what to fix, a pre-upgrade `kubectl`/`jq` recipe, a
   rejection counter, and a superseding ADR.

## Technical Context

**Language/Version**: Go 1.26.5 (`go.mod`)

**Primary Dependencies**: controller-runtime (operator/webhook), `k8s.io/api`
v0.36.3, Cobra (`berth-acquire` CLI), Prometheus client_golang v1.24.1

**Storage**: N/A for this feature. The unit of state is a shared `emptyDir`
(`berth-state`) holding `holder`, `token`, `healthy`, and `check`.

**Testing**: `go test` in-package; webhook admission tests in
`internal/webhook`; helper/enforcement tests in `internal/acquire`; race
enabled in CI (`task test`, `task lint`, `task verify-generated`)

**Target Platform**: Linux runtime components (injected helpers, operator);
main containers may be distroless with no shell

**Project Type**: Kubernetes operator + injected sidecar/init helper + Go
client library

**Performance Goals**: No throughput target. The relevant latency is
*detection*: a dead sidecar must stop the workload within one TTL plus the
probe period (`periodSeconds: 2`, `failureThreshold: 1`).

**Constraints**:
- `check` runs inside the main container and MUST construct no config, do no
  network I/O, and depend on nothing beyond the state volume (FR-010).
- Freshness MUST NOT depend on cross-container clock agreement (FR-008);
  mtime via the shared volume gives one node kernel clock.
- A container has exactly one `livenessProbe` slot, and `preflight` routes
  users to `signal` precisely when theirs is occupied (FR-010b).
- Any `deploy/helm/<chart>/` change bumps that chart's version (FR-014).

**Scale/Scope**: Two packages carry the change (`internal/webhook`,
`internal/acquire`) plus `cmd/berth-acquire`, the operator chart, `docs/`, and
one new ADR.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment |
|---|---|
| I. Explicit State Over Implicit Behavior | **Pass.** Today "the workload must not write the state volume" is an undocumented assumption; this makes it a declared, enforced rule. |
| III. Deterministic, Reconstructible Operation | **Pass.** Admission is a pure function of the pod spec; freshness is a pure function of mtime and TTL. |
| IV. Kubernetes-Native, Never Obscured | **Pass.** Uses admission and probes directly; adds no external orchestration. |
| VI. Explainable Reconciliation, Evidence-Grade Audit | **Pass, and load-bearing.** FR-011a/011b exist because "liveness probe failed" alone cannot distinguish an expected failover from a dead-sidecar incident. |
| VII. Read-Only Degradation Over Blindness | **⚠ See gate finding below.** |
| IX. Technical Precision, Honest Scope | **Pass.** FR-010b requires the residual signal-mode gap be stated, not implied. |
| X. Coordination Safety Is Non-Negotiable | **⚠ See gate finding below.** Regression tests that fail pre-fix are mandated by FR-011. |

### Gate finding: the security control is fail-open

Principle X states safety "MUST NOT depend on timing, sweep loops, or
well-behaved clients." US1 is enforced by the gating webhook, and the shipped
chart default is:

```yaml
# deploy/helm/berth-operator/values.yaml:157
failurePolicy: Ignore
```

So while the webhook is unavailable, the rule does not apply and an offending
pod admits unenforced. An attacker who can submit pods and can disrupt (or
merely wait out) the webhook defeats US1 without touching any of the code this
plan changes.

This is not a defect introduced here — it is pre-existing, and it is tracked
as **#103** — but it becomes materially more important once the webhook is
carrying an at-most-once security control rather than only a convenience
mutation.

**This gate does not block Phase 0**, because the resolution is a decision for
the maintainer rather than a design question, and it is separable from the
mechanism. It is carried into Complexity Tracking and raised in the completion
report. Options are analysed in [research.md](./research.md) (R4).

### Post-Design Re-Check (after Phase 1)

Re-evaluated against the artifacts in `research.md`, `data-model.md`, and
`contracts/`. No new violations; two items sharpened:

- **Principle IX (Honest Scope) — strengthened.** R2 turned the signal-mode gap
  from "document it" into a concrete two-tier design, and
  `contracts/check-cli.md` surfaced a compatibility matrix that was invisible
  at spec time: an *old* `check` binary receiving the new `--max-age` flag must
  not exit non-zero on argument parsing, or the rollout itself kills healthy
  workloads. That is a release-ordering constraint, now recorded rather than
  assumed.
- **Principle X (Coordination Safety) — unchanged but bounded.** The design
  satisfies it *given a reachable webhook*. R4's fail-open finding is the
  boundary, and it is documented in the admission contract rather than left to
  the reader.

Design did not introduce new dependencies, new persisted state, or new public
API surface beyond one optional CLI flag.

## Project Structure

### Documentation (this feature)

```text
specs/002-state-volume-trust/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── admission.md     # What admission accepts and rejects
│   └── check-cli.md     # berth-acquire check exit codes and messages
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
internal/webhook/
├── inject.go            # preflight mount rule (FR-001), ephemeral path
│                        # (FR-001a), signal-mode backstop probe (FR-010a)
├── contract.go          # admission wiring for the new subresource
└── inject_test.go       # admission matrix (SC-001)

internal/acquire/
├── state.go             # IsHealthy gains freshness (FR-006, FR-009)
├── enforce.go           # unchanged mechanism; marker writes stay on renew
└── state_test.go        # freshness boundary and clock-independence tests

cmd/berth-acquire/
└── main.go              # check subcommand: freshness + distinct reasons
                         # (FR-006, FR-010, FR-011a)

deploy/helm/berth-operator/
├── templates/mutatingwebhookconfiguration.yaml  # add ephemeralcontainers rule
├── values.yaml                                  # (see R4 re: failurePolicy)
└── Chart.yaml                                   # version bump (FR-014)

docs/
├── workload-gating-injection.md   # reserved volume, freshness, signal gap
├── reference/configuration.md     # any new surface
└── adr/0004-*.md                  # supersedes ADR-0003's trust model (FR-013)
```

**Structure Decision**: No new packages. The change lands in the two packages
that already own the two halves of the trust boundary — `internal/webhook`
decides what may be admitted, `internal/acquire` decides what counts as
healthy — plus the `cmd/berth-acquire` entrypoint that the probe execs. This
keeps `cmd/*` thin per the constitution's engineering constraints.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| Two health-test implementations remain (`State.IsHealthy` and the `check` subcommand) | `check` must run in a distroless main container with no config (FR-010); `IsHealthy` runs in the sidecar with full config available | Collapsing them into one call site was rejected: the probe path must stay dependency-free, so the shared piece is the freshness *predicate*, not the caller. FR-009 requires they cannot disagree — enforced by a shared function plus a test asserting agreement. |
| Security control remains fail-open by default (`failurePolicy: Ignore`) | Pre-existing chart default (#103); changing it makes a webhook outage block all pod creation in scope | Flipping to `Fail` unilaterally was rejected as out of scope for this spec and a materially larger operational change. Carried as a gate finding for the maintainer — see R4. |
| `signal`-mode backstop coverage is partial | A container has one `livenessProbe` slot and `preflight` routes users to `signal` exactly when it is taken | Overwriting a user's own probe was rejected outright — silently replacing a workload's health check is a worse defect than the one being fixed. See R2 for the alternative that closes the remainder. |
