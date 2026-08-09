# Tasks: State-Volume Trust and Marker Freshness

**Feature**: `002-state-volume-trust` | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

**Input**: spec.md, plan.md, research.md, data-model.md, contracts/, quickstart.md

**Revised 2026-08-09** after the analysis pass: adds the `failurePolicy: Fail`
flip (C1), end-to-end verification tasks (G1, G2), two missing verification
tasks (G3, G4), and settles the `signal`-mode watchdog sequencing (R2 tier 2).
Task IDs were renumbered to stay sequential in execution order.

## Format: `[ID] [P?] [Story] Description`

- **[P]** — parallelizable: different files, no dependency on an incomplete task
- **[US1] / [US2] / [US3]** — the user story the task serves

**Tests are included.** FR-011 requires regression tests that fail against the
pre-fix behavior, and the constitution's engineering constraints require a
covering test for every regression fix. This is not the optional-test case.

## Path Conventions

Repository root is the Go module root. Key paths: `internal/webhook/`,
`internal/acquire/`, `cmd/berth-acquire/`, `test/e2e/`,
`deploy/helm/berth-operator/`, `docs/`.

---

## Phase 1: Setup (Shared Infrastructure)

- [X] T001 Confirm a green baseline before changes: run `go -C tools tool task test`, `lint`, and `verify-generated` from the repository root and record that all three pass
- [X] T002 [P] Resolve the R5 open item: determine whether `State.IsHealthy()` has any non-test callers, searching `internal/` and `cmd/`; if it has none, note that removing it satisfies FR-009 trivially and adjust T030
- [X] T003 [P] Resolve the check-CLI skew risk: confirm how the current Cobra command in `cmd/berth-acquire/main.go` behaves on an unknown flag, since an old `check` receiving `--max-age` must not fail argument parsing (see contracts/check-cli.md); if it errors, record helper-image-before-webhook as a release-ordering requirement

---

## Phase 2: Foundational (Blocking Prerequisites)

These define the two shared vocabularies every later phase reuses. Complete
before starting any user story.

- [X] T004 [P] Define the rejection reason type (`WritableStateMount`, `WritableStateMountEphemeral`) in `internal/webhook/contract.go`, per data-model.md — consumed by US1 messages and the US3 counter
- [X] T005 [P] Define the freshness verdict type (`Healthy`, `Absent`, `Stale`, `Indeterminate`) and the shared age predicate in `internal/acquire/state.go`, per data-model.md and R5 — boundary is `age <= bound` healthy, `age > bound` stale

---

## Phase 3: User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) 🎯 MVP

**Goal**: Make the state volume reserved, so no workload-authored container can
write the health marker or the `check` binary.

**Independent test**: Submit pods and ephemeral containers across the
admission decision table in contracts/admission.md and assert exactly the
writable author-declared mounts are rejected, each naming container, volume,
and mount path — then confirm in a live cluster that an admitted workload
cannot modify the marker or the verifier.

### Tests for User Story 1

- [X] T006 [P] [US1] Test that a writable author-declared state-volume mount at a non-injected path is rejected, in `internal/webhook/inject_test.go` — must fail pre-fix
- [X] T007 [P] [US1] Test that a `readOnly: true` author-declared mount is admitted, in `internal/webhook/inject_test.go`
- [X] T008 [P] [US1] Test that injector-owned writable helper mounts are still admitted, in `internal/webhook/inject_test.go` — guards against keying the rule on "writable" alone
- [X] T009 [P] [US1] Test that a writable mount arriving via the ephemeral-containers subresource is rejected and a read-only one admitted, in `internal/webhook/inject_test.go` — must fail pre-fix
- [X] T010 [P] [US1] Test that the existing behaviours are unchanged: a writable mount at exactly `StateDir` is admitted and forced read-only, and a different volume at `StateDir` is still rejected, in `internal/webhook/inject_test.go`
- [X] T011 [P] [US1] Test both enforce modes (`probe` and `signal`) against the mount rule, in `internal/webhook/inject_test.go` — the rule is mode-independent
- [X] T012 [US1] **(G1)** End-to-end test in `test/e2e/` asserting that inside an *admitted* workload container, both `touch /berth/healthy` and overwriting `/berth/check` fail — admission tests prove the spec is rejected, this proves the runtime property the feature actually claims

### Implementation for User Story 1

- [X] T013 [US1] Implement mount classification in `preflight` in `internal/webhook/inject.go`: reject when the mount targets the state volume, is not read-only, and is not injector-owned, per data-model.md
- [X] T014 [US1] Implement the rejection message in `internal/webhook/inject.go` naming container, volume, and mount path, stating why and giving the resolutions, with no implication that an opt-out exists (FR-002)
- [X] T015 [US1] Apply the rule to the ephemeral-containers admission path in `internal/webhook/inject.go`, with wording that makes clear the running pod is healthy and the debug request was refused
- [X] T016 [US1] Register `pods/ephemeralcontainers` (`CREATE`) in `deploy/helm/berth-operator/templates/mutatingwebhookconfiguration.yaml` alongside the existing `pods` rule
- [X] T017 [US1] **(C1)** Change the shipped default to `failurePolicy: Fail` in `deploy/helm/berth-operator/values.yaml` and update the surrounding comment to state the trade — a webhook outage blocks pod creation for pods matching the selector (FR-001b)
- [X] T018 [US1] Bump `version` in `deploy/helm/berth-operator/Chart.yaml` — **minor**, since T016 and T017 change chart behavior additively and by default (FR-014)

**Checkpoint**: The bypass is closed and cannot lapse during a webhook outage. US2 is meaningless before this point, because `check` is only trustworthy once the volume is.

---

## Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2)

**Goal**: Make the probe test freshness, so enforcement survives the sidecar's
own death.

**Independent test**: With the sidecar stopped, the check begins failing once
the marker is one TTL old and the kubelet kills the workload; with the sidecar
renewing normally it never fails, at any point in the cycle.

### Tests for User Story 2

- [X] T019 [P] [US2] Test the freshness boundary in `internal/acquire/state_test.go`: `age == bound` healthy, `age > bound` stale, absent distinguished from stale — must fail pre-fix
- [X] T020 [P] [US2] Test that `State.IsHealthy()` and the `check` subcommand return the same verdict across an age table including the boundary, in `internal/acquire/state_test.go` (FR-009)
- [X] T021 [P] [US2] Test that a holder renewing successfully never trips the bound, across heartbeats from the TTL/3 default to just under the TTL, in `internal/acquire/renew_test.go` (FR-007, SC-003)
- [X] T022 [P] [US2] Test that freshness does not depend on cross-container clock agreement, in `internal/acquire/state_test.go` (FR-008)
- [X] T023 [P] [US2] Test that `check` exits 0/1 with the correct distinct stderr reason per contracts/check-cli.md, in `cmd/berth-acquire/main_test.go` (FR-011a)
- [X] T024 [P] [US2] Test that `check` without `--max-age` preserves presence-only behavior, in `cmd/berth-acquire/main_test.go` — the compatibility row from contracts/check-cli.md
- [X] T025 [P] [US2] Test that the init container's acquire leaves a fresh marker so a just-started pod is never killed for a missing first renewal, in `internal/acquire/acquire_test.go`
- [X] T026 [P] [US2] **(G4)** Test the `Indeterminate` verdict — an unreadable marker fails closed rather than passing — in `internal/acquire/state_test.go`
- [X] T027 [P] [US2] **(G3)** Test that `check` runs correctly with no environment and no config present, asserting the FR-010 dependency-free constraint that would otherwise erode silently, in `cmd/berth-acquire/main_test.go`
- [X] T028 [US2] **(G2)** End-to-end test in `test/e2e/` asserting that with the sidecar stopped, the workload is killed roughly one TTL later with no sidecar action, and that the probe failure event reports **stale** with the observed age (SC-002, FR-011a) — `deadsidecar_test.go`, passing in 22s against the three-cluster kind topology

### Implementation for User Story 2

- [X] T029 [US2] Add `--max-age` to the `check` subcommand in `cmd/berth-acquire/main.go`, calling the shared predicate and emitting the distinct reasons; absent flag preserves today's behavior
- [X] T030 [US2] Rewire `State.IsHealthy()` in `internal/acquire/state.go` onto the shared predicate — or remove it if T002 found no non-test callers
- [X] T031 [US2] Template `--max-age` into the injected probe command in `internal/webhook/inject.go`, resolved from the pod's TTL (R1)
- [X] T032 [US2] Inject the freshness-only backstop probe for `enforce: signal` where the main container's liveness slot is free, in `internal/webhook/inject.go` (FR-010a, tier 1 of R2)
- [X] T033 [US2] Record the residual `signal`-mode gap — pods whose liveness slot is occupied keep the #98 exposure — as a stated limitation in `docs/workload-gating-injection.md` (FR-010b). **Mandatory**, not conditional: tier 2 is deferred, so this is the only thing standing between users and a silent gap
- [X] T034 [US2] Open a tracked follow-up issue for the R2 tier-2 watchdog container, referencing FR-010b and this spec, so the deferred coverage is a scheduled commitment rather than an aspiration
- [X] T035 [US2] Bump `version` in `deploy/helm/berth-operator/Chart.yaml` **only if US2 ships as a separate release from US1** — under the incremental strategy below it does; if US1 and US2 are batched into one release, T018's bump covers both and this task is a no-op (FR-014)

**Checkpoint**: A dead sidecar now stops its workload within one TTL, in `probe` mode and in `signal` mode where the liveness slot is free.

---

## Phase 5: User Story 3 - A rejected pod tells its owner exactly what to change (Priority: P3)

**Goal**: Make the breaking change survivable and the enforcement explainable.

**Independent test**: An uninitiated workload owner can fix a rejected pod from
the error and docs alone; an operator can find affected workloads before
upgrading and see enforcement firing afterwards.

### Tests for User Story 3

- [X] T036 [P] [US3] Test the rejection counter increments per rejection with reason and admission-path labels and no pod/namespace labels, in `internal/webhook/inject_test.go` (FR-011b, R6)

### Implementation for User Story 3

- [X] T037 [US3] Register the rejection `CounterVec` on the operator's metrics registry, confirming first whether to extend `internal/metrics/metrics.go` or register locally in the operator — R6 notes the existing package serves the apiserver, so this is a decision to make before writing, not while writing
- [X] T038 [P] [US3] Document the reserved state volume and the freshness rule in `docs/workload-gating-injection.md` (FR-012)
- [X] T039 [P] [US3] Write upgrade notes in `docs/operations/` covering **both** breaking changes — rejected pod shapes, and the `failurePolicy` default moving from `Ignore` to `Fail` — including the `kubectl`/`jq` inventory recipe runnable against an un-upgraded cluster (FR-012a, FR-001b)
- [X] T040 [P] [US3] Document the new failure posture in `docs/workload-gating-injection.md`: the guarantee now holds unconditionally, at the cost of the operator being a hard dependency for pod creation in gated namespaces; update or close issue #103, which this change resolves for the gating webhook (R4)
- [X] T041 [US3] Write `docs/adr/0004-state-volume-is-reserved.md` superseding ADR-0003's trust model, and mark ADR-0003 superseded rather than editing it (FR-013)
- [X] T042 [P] [US3] Update `docs/architecture.md` and `docs/code-map.md` if the component descriptions no longer match

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T043 Verify every new regression test fails against pre-fix code by stashing only the implementation files and re-running, as done for #97 — a test that passes before the fix proves nothing (FR-011)
- [X] T044 Run the full gates from the repository root: `go -C tools tool task test`, `go test -race ./internal/webhook/... ./internal/acquire/...`, `task lint`, `task verify-generated`
- [X] T045 [P] Confirm the docs build clean with `mkdocs build --strict`, since docs CI gates on it
- [X] T046 [P] Cross-check the T039 inventory recipe against actual admission behavior in a scratch cluster — a recipe that disagrees with the webhook is worse than none. Verified twice: against synthetic pod JSON covering every row of the admission decision table, and against the live kind topology, where the shapes it selects are exactly the ones admission refuses
- [X] T047 Confirm exactly one correctly-sized `deploy/helm/berth-operator/Chart.yaml` bump is present for the release being cut; `task lint` will not catch a missed or doubled bump

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately. T002 and T003 are investigations whose answers change T030 and release ordering, so run them early.
- **Foundational (Phase 2)**: depends on Setup. Blocks all user stories.
- **User Story 1 (Phase 3)**: depends on Phase 2. **Blocks US2** — see below.
- **User Story 2 (Phase 4)**: depends on Phase 2 and **on US1**.
- **User Story 3 (Phase 5)**: depends on Phase 2; T036/T037 depend on US1's rejection path existing.
- **Polish (Phase 6)**: depends on all shipped stories.

### The US1 → US2 dependency is real

The template's usual assumption is that stories are independent. Here they are
not, and the spec makes the ordering normative: US2's freshness rule is
evaluated by `check`, which lives inside the volume US1 protects. Shipping US2
first would produce a rule an attacker can delete. **Do not parallelize US1 and
US2 across people as if they were independent.**

### Parallel Opportunities

- Phase 1: T002 and T003 together.
- Phase 2: T004 and T005 together — different packages.
- Phase 3: T006–T011 together (all admission cases). T012 is e2e and runs after the implementation tasks.
- Phase 4: T019–T027 together across `internal/acquire` and `cmd/berth-acquire`. T028 is e2e and runs after implementation.
- Phase 5: T038, T039, T040, T042 together — all documentation, different files.

## Parallel Example: User Story 1

```text
# Write the admission matrix together, then implement against it:
T006  writable at non-injected path      -> reject
T007  readOnly at non-injected path      -> admit
T008  injector-owned helper mounts       -> admit
T009  ephemeral subresource              -> reject / admit
T010  writable at StateDir               -> admit, forced read-only
T011  both enforce modes                 -> rule is mode-independent
# Then, after T013-T018 land:
T012  e2e: workload cannot write marker or replace check
```

## Implementation Strategy

### MVP (User Story 1 only)

Phases 1 → 2 → 3. This alone closes the security defect and is independently
shippable: the volume becomes reserved, the verifier becomes untamperable, the
rule no longer lapses during a webhook outage, and the bypass in #96 is gone.
#98 remains open, which is honest — the marker is still presence-only, and the
docs must not claim otherwise until US2 lands.

### Incremental Delivery

1. **US1** — closes #96. Ship it.
2. **US2** — closes #98 for `probe` and for `signal` where the liveness slot is
   free. Only meaningful after US1.
3. **US3** — makes the breaking change survivable. Ship *with* US1, not after
   it: T038/T039/T040 are what stop the upgrade surprising people, so treating
   them as follow-up work would land two breaking changes without guidance.

### Phase 1 investigation findings (T002, T003)

- **T002** — `State.IsHealthy()` has **no non-test callers**, but 10+ test call
  sites across `renew_test.go` and `state_test.go`. Removing it would churn
  those tests for no benefit, so T030 rewires it onto the shared predicate
  instead of deleting it. FR-009 is satisfied either way.
- **T003** — the skew risk in contracts/check-cli.md is **not reachable**
  through normal upgrade. Cobra does error on unknown flags
  (`SilenceErrors` suppresses printing, not parsing), but the probe command
  and the helper image both come from one `InjectorConfig` applied in a single
  admission pass, so a pod's `check` binary and probe command always match.
  No release-ordering requirement; only a pinned-stale `injection.helper.image`
  could produce the failing combination. Contract updated accordingly.

### Verified against a live cluster

T012 and T028 were briefly deferred on the mistaken belief that no cluster was
available. The repo ships one: `task e2e-up` brings up a three-kind-cluster
topology, `task e2e` runs the tagged suite against it, `task e2e-all` does the
whole cycle. Both tests are written and passing.

Two findings from actually running it, neither of which unit tests could have
produced:

- **The `pods/ephemeralcontainers` rule was verified by demonstration, not
  reasoning.** With the shipped `operations: ["CREATE", "UPDATE"]`, a
  `kubectl patch --subresource=ephemeralcontainers` carrying a writable
  `berth-state` mount is refused. Reverting the *live* webhook config to
  `["CREATE"]` and repeating the patch **admits it**, with the container
  keeping its writable mount — confirming the original CREATE-only
  registration was a real bypass and not a theoretical one.
- **The stale-marker path was observed end to end.** Repointing the sidecar's
  image at an unpullable tag stops it without needing exec (the helper image
  is distroless and has no shell). The workload is then killed by the kubelet
  with:

  ```
  Liveness probe failed: health marker stale: /berth/healthy not refreshed
  for 16s (limit 15s); the lease sidecar is not renewing
  ```

  The injected probe command was confirmed as
  `["/berth/check","check","/berth/healthy","--max-age","15s"]`, and the app
  container's `restartCount` rose while `berth-sidecar` stayed down.

### Notes

- **T017 is a cluster-behavior change, not a values tweak.** Flipping
  `failurePolicy` to `Fail` makes the operator a hard dependency for pod
  creation in gated namespaces. It is grouped with US1 because a control that
  fails open is not a control, but it needs its own line in the upgrade notes
  (T039) and its own reviewer attention.
- **T033 and T034 are a pair.** Tier 2 of R2 is deferred, so the documented
  limitation and the tracked follow-up together are what keep the deferral
  honest. Neither is optional.
- Analysis findings deliberately left open: FR-011b's "labelled enough"
  phrasing (pinned concretely in R6 and data-model.md), and the
  "workload-authored" versus "author-declared" terminology drift between
  plan.md and the other artifacts. Both are wording-level and do not affect
  execution.
