# Tasks: State-Volume Trust and Marker Freshness

**Feature**: `002-state-volume-trust` | **Plan**: [plan.md](./plan.md) | **Spec**: [spec.md](./spec.md)

**Input**: spec.md, plan.md, research.md, data-model.md, contracts/, quickstart.md

## Format: `[ID] [P?] [Story] Description`

- **[P]** — parallelizable: different files, no dependency on an incomplete task
- **[US1] / [US2] / [US3]** — the user story the task serves

**Tests are included.** FR-011 requires regression tests that fail against the
pre-fix behavior, and the constitution's engineering constraints require a
covering test for every regression fix. This is not the optional-test case.

## Path Conventions

Repository root is the Go module root. Key paths: `internal/webhook/`,
`internal/acquire/`, `cmd/berth-acquire/`, `deploy/helm/berth-operator/`,
`docs/`.

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Confirm a green baseline before changes: run `go -C tools tool task test`, `lint`, and `verify-generated` from the repository root and record that all three pass
- [ ] T002 [P] Resolve the R5 open item: determine whether `State.IsHealthy()` has any non-test callers, searching `internal/` and `cmd/`; if it has none, note that removing it satisfies FR-009 trivially and adjust T020
- [ ] T003 [P] Resolve the check-CLI skew risk: confirm how the current Cobra command in `cmd/berth-acquire/main.go` behaves on an unknown flag, since an old `check` receiving `--max-age` must not fail argument parsing (see contracts/check-cli.md); if it errors, record helper-image-before-webhook as a release-ordering requirement

---

## Phase 2: Foundational (Blocking Prerequisites)

These define the two shared vocabularies every later phase reuses. Complete
before starting any user story.

- [ ] T004 [P] Define the rejection reason type (`WritableStateMount`, `WritableStateMountEphemeral`) in `internal/webhook/contract.go`, per data-model.md — consumed by US1 messages and the US3 counter
- [ ] T005 [P] Define the freshness verdict type (`Healthy`, `Absent`, `Stale`, `Indeterminate`) and the shared age predicate in `internal/acquire/state.go`, per data-model.md and R5 — boundary is `age <= bound` healthy, `age > bound` stale

---

## Phase 3: User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) 🎯 MVP

**Goal**: Make the state volume reserved, so no workload-authored container can
write the health marker or the `check` binary.

**Independent test**: Submit pods and ephemeral containers across the
admission decision table in contracts/admission.md and assert exactly the
writable author-declared mounts are rejected, each naming container, volume,
and mount path.

### Tests for User Story 1

- [ ] T006 [P] [US1] Test that a writable author-declared state-volume mount at a non-injected path is rejected, in `internal/webhook/inject_test.go` — must fail pre-fix
- [ ] T007 [P] [US1] Test that a `readOnly: true` author-declared mount is admitted, in `internal/webhook/inject_test.go`
- [ ] T008 [P] [US1] Test that injector-owned writable helper mounts are still admitted, in `internal/webhook/inject_test.go` — guards against keying the rule on "writable" alone
- [ ] T009 [P] [US1] Test that a writable mount arriving via the ephemeral-containers subresource is rejected and a read-only one admitted, in `internal/webhook/inject_test.go` — must fail pre-fix
- [ ] T010 [P] [US1] Test that the existing behaviours are unchanged: a writable mount at exactly `StateDir` is admitted and forced read-only, and a different volume at `StateDir` is still rejected, in `internal/webhook/inject_test.go`
- [ ] T011 [P] [US1] Test both enforce modes (`probe` and `signal`) against the mount rule, in `internal/webhook/inject_test.go` — the rule is mode-independent

### Implementation for User Story 1

- [ ] T012 [US1] Implement mount classification in `preflight` in `internal/webhook/inject.go`: reject when the mount targets the state volume, is not read-only, and is not injector-owned, per data-model.md
- [ ] T013 [US1] Implement the rejection message in `internal/webhook/inject.go` naming container, volume, and mount path, stating why and giving the resolutions, with no implication that an opt-out exists (FR-002)
- [ ] T014 [US1] Apply the rule to the ephemeral-containers admission path in `internal/webhook/inject.go`, with wording that makes clear the running pod is healthy and the debug request was refused
- [ ] T015 [US1] Register `pods/ephemeralcontainers` (`CREATE`) in `deploy/helm/berth-operator/templates/mutatingwebhookconfiguration.yaml` alongside the existing `pods` rule
- [ ] T016 [US1] Bump `version` in `deploy/helm/berth-operator/Chart.yaml` — minor, since this is additive admission behavior (FR-014)

**Checkpoint**: The bypass is closed. US2 is meaningless before this point, because `check` is only trustworthy once the volume is.

---

## Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2)

**Goal**: Make the probe test freshness, so enforcement survives the sidecar's
own death.

**Independent test**: With the sidecar stopped, the check begins failing once
the marker is one TTL old; with the sidecar renewing normally it never fails,
at any point in the cycle.

### Tests for User Story 2

- [ ] T017 [P] [US2] Test the freshness boundary in `internal/acquire/state_test.go`: `age == bound` healthy, `age > bound` stale, absent distinguished from stale — must fail pre-fix
- [ ] T018 [P] [US2] Test that `State.IsHealthy()` and the `check` subcommand return the same verdict across an age table including the boundary, in `internal/acquire/state_test.go` (FR-009)
- [ ] T019 [P] [US2] Test that a holder renewing successfully never trips the bound, across heartbeats from the TTL/3 default to just under the TTL, in `internal/acquire/renew_test.go` (FR-007, SC-003)
- [ ] T020 [P] [US2] Test that freshness does not depend on cross-container clock agreement, in `internal/acquire/state_test.go` (FR-008)
- [ ] T021 [P] [US2] Test that `check` exits 0/1 with the correct distinct stderr reason per contracts/check-cli.md, in `cmd/berth-acquire/main_test.go` (FR-011a)
- [ ] T022 [P] [US2] Test that `check` without `--max-age` preserves presence-only behavior, in `cmd/berth-acquire/main_test.go` — the compatibility row from contracts/check-cli.md
- [ ] T023 [P] [US2] Test that the init container's acquire leaves a fresh marker so a just-started pod is never killed for a missing first renewal, in `internal/acquire/acquire_test.go`

### Implementation for User Story 2

- [ ] T024 [US2] Add `--max-age` to the `check` subcommand in `cmd/berth-acquire/main.go`, calling the shared predicate and emitting the distinct reasons; absent flag preserves today's behavior
- [ ] T025 [US2] Rewire `State.IsHealthy()` in `internal/acquire/state.go` onto the shared predicate — or remove it if T002 found no non-test callers
- [ ] T026 [US2] Template `--max-age` into the injected probe command in `internal/webhook/inject.go`, resolved from the pod's TTL (R1)
- [ ] T027 [US2] Inject the freshness-only backstop probe for `enforce: signal` where the main container's liveness slot is free, in `internal/webhook/inject.go` (FR-010a, tier 1 of R2)
- [ ] T028 [US2] Implement the `signal`-mode watchdog for pods whose liveness slot is occupied — **GATED on approval of R2 tier 2 at the plan review**; if declined, skip and complete T029 instead
- [ ] T029 [US2] If T028 is declined, record the residual `signal`-mode gap as a stated limitation in `docs/workload-gating-injection.md` (FR-010b) — this task is mandatory unless T028 ships
- [ ] T030 [US2] Bump `version` in `deploy/helm/berth-operator/Chart.yaml` if the probe templating changed chart output (FR-014)

**Checkpoint**: A dead sidecar now stops its workload within one TTL.

---

## Phase 5: User Story 3 - A rejected pod tells its owner exactly what to change (Priority: P3)

**Goal**: Make the breaking change survivable and the enforcement explainable.

**Independent test**: An uninitiated workload owner can fix a rejected pod from
the error and docs alone; an operator can find affected workloads before
upgrading and see enforcement firing afterwards.

### Tests for User Story 3

- [ ] T031 [P] [US3] Test the rejection counter increments per rejection with reason and admission-path labels and no pod/namespace labels, in `internal/webhook/inject_test.go` (FR-011b, R6)

### Implementation for User Story 3

- [ ] T032 [US3] Register the rejection `CounterVec` on the operator's metrics registry, confirming first whether to extend `internal/metrics/metrics.go` or register locally in the operator (R6 notes the existing package serves the apiserver)
- [ ] T033 [P] [US3] Document the reserved state volume and the freshness rule in `docs/workload-gating-injection.md` (FR-012)
- [ ] T034 [P] [US3] Write upgrade notes marking the change breaking, including the `kubectl`/`jq` inventory recipe runnable against an un-upgraded cluster, in `docs/operations/` (FR-012a)
- [ ] T035 [P] [US3] Document the `failurePolicy: Ignore` dependency — US1 holds only while the webhook is reachable — and cross-reference issue #103, in `docs/workload-gating-injection.md` (R4)
- [ ] T036 [US3] Write `docs/adr/0004-state-volume-is-reserved.md` superseding ADR-0003's trust model, and mark ADR-0003 superseded rather than editing it (FR-013)
- [ ] T037 [P] [US3] Update `docs/architecture.md` and `docs/code-map.md` if the component descriptions no longer match

---

## Phase 6: Polish & Cross-Cutting Concerns

- [ ] T038 Verify every new regression test fails against pre-fix code by stashing only the implementation files and re-running, as done for #97 — a test that passes before the fix proves nothing (FR-011)
- [ ] T039 Run the full gates from the repository root: `go -C tools tool task test`, `go test -race ./internal/webhook/... ./internal/acquire/...`, `task lint`, `task verify-generated`
- [ ] T040 [P] Confirm the docs build clean with `mkdocs build --strict`, since docs CI gates on it
- [ ] T041 [P] Cross-check the T034 inventory recipe against actual admission behavior in a scratch cluster — a recipe that disagrees with the webhook is worse than none
- [ ] T042 Confirm the `deploy/helm/berth-operator/Chart.yaml` bump is present and correctly sized; `task lint` will not catch a missed bump

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately. T002 and T003 are investigations whose answers change T025 and release ordering, so run them early.
- **Foundational (Phase 2)**: depends on Setup. Blocks all user stories.
- **User Story 1 (Phase 3)**: depends on Phase 2. **Blocks US2** — see below.
- **User Story 2 (Phase 4)**: depends on Phase 2 and **on US1**.
- **User Story 3 (Phase 5)**: depends on Phase 2; T031/T032 depend on US1's rejection path existing.
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
- Phase 3: T006–T011 together (all test cases, same file but independent cases; coordinate if one person).
- Phase 4: T017–T023 together across `internal/acquire` and `cmd/berth-acquire`.
- Phase 5: T033, T034, T035, T037 together — all documentation, different files.

## Parallel Example: User Story 1

```text
# Write the admission matrix together, then implement against it:
T006  writable at non-injected path      -> reject
T007  readOnly at non-injected path      -> admit
T008  injector-owned helper mounts       -> admit
T009  ephemeral subresource              -> reject / admit
T010  writable at StateDir               -> admit, forced read-only
T011  both enforce modes                 -> rule is mode-independent
```

## Implementation Strategy

### MVP (User Story 1 only)

Phases 1 → 2 → 3. This alone closes the security defect and is independently
shippable: the volume becomes reserved, the verifier becomes untamperable, and
the bypass in #96 is gone. #98 remains open, which is honest — the marker is
still presence-only, and the docs must not claim otherwise until US2 lands.

### Incremental Delivery

1. **US1** — closes #96. Ship it.
2. **US2** — closes #98. Only meaningful after US1.
3. **US3** — makes the breaking change survivable. Ship *with* US1, not after
   it: T033/T034/T035 are what stop the upgrade surprising people, so treating
   them as follow-up work would land the breakage without the guidance.

### Notes

- T028 is gated on the plan review of R2 tier 2 (the watchdog container). T029
  is its fallback and is mandatory if T028 does not ship — the residual gap
  must not be left silent (FR-010b, Constitution IX).
- The fail-open `failurePolicy` finding (R4) is deliberately not a code task.
  It is documented by T035 and raised for the maintainer; changing the default
  belongs to #103.
