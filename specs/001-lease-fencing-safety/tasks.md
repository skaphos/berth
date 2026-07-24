# Tasks: Lease Fencing and Isolation Safety

**Input**: Design documents from `specs/001-lease-fencing-safety/`
**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/)

**Tests**: included — the spec (FR-009) and the Berth constitution make a
regression test per defect and store-boundary interleaving coverage
mandatory. Regression tests are written with or before the code they guard,
and each must demonstrably fail against the pre-fix logic.

**Organization**: phases 3–5 map to the spec's user stories (US1 = #90,
US2 = #92/#93, US3 = #94). Phases 2+3 form one compile unit (the `Store`
signature change and the manager threading land together); each *phase* ends
with the tree compiling and the suite green.

## Implementation notes (deviations from the plan as written)

All 29 tasks are complete; four executed differently than drafted:

- **T002/T015** — the interim version-predicated `Delete` was skipped:
  since all phases landed in one implementation pass, the `Store` interface
  went straight to its final shape (no `Delete` at all). The intermediate
  step existed only to keep the tree green between separately-landed
  phases, which did not apply.
- **T008 (finding)** — the mem and k8s backends were never wired into the
  conformance suite at all (only SQLite ran it). Fixed:
  `internal/lease/conformance_test.go` now runs the full suite against
  `MemStore`, and the new exported `storetest.RunStoreSafetyRegressions`
  sub-suite runs against the k8s fake as well (the fake clientset ignores
  context cancellation, so the full suite cannot run there).
- **T011** — the trace direction in the drafted task (stall the renewer,
  land the reclaim first) is an interleaving that was already safe pre-fix
  (the reclaim bumps the token). The implemented test
  (`TestReclaimRacingRenewGrantsExactlyOne`) stalls the *reclaimer* so the
  renew lands first — the exact issue #90 double-grant order.
- **T012** — verified by temporarily restoring the pre-fix token-CAS
  predicate in `MemStore.Put`: six regression tests fail (the #90 store
  interleaving, #92 monotonicity, #93 scan-stale GC at both store and
  enforcer level, the manager trace, and the tombstone lifecycle), then
  the predicate was restored and the suite is green.
- **T027** — the e2e harness drives leases exclusively through
  `BerthLease` CRs, where Kubernetes itself enforces DNS-label namespaces;
  an invalid-key request is unrepresentable on that path and fencing
  tokens are not surfaced to the harness, so no e2e change was made.
  Coverage lives at the store, manager, and HTTP-handler boundaries.

## Phase 1: Setup

- [X] T001 Confirm green baseline on the branch point: run `go -C tools tool task test` and `go -C tools tool task lint`; record any pre-existing failures so they are not attributed to this feature

## Phase 2: Foundational — version-CAS store contract (blocks US1, US2)

Contract: [contracts/store-contract.md](contracts/store-contract.md), decisions D1/D6/D7 in [research.md](research.md).

- [X] T002 In `internal/lease/store.go`: add `Version int64` to `Record` (store-owned, doc per data-model), change `Put` to `Put(ctx, expectedVersion int64, record *Record)` (0 = create→stores 1; else CAS, stores expected+1; caller's `record.Version` ignored), change `Delete` to `Delete(ctx, key Key, expectedVersion int64)` (interim — removed in T015), and rewrite the contract comments including `ErrConflict`
- [X] T003 [P] In `internal/lease/memstore.go`: implement version CAS for `Put`/`Delete` (compare stored `Version`, write `expected+1`); update `internal/lease/memstore_test.go`
- [X] T004 [P] In `internal/lease/sqlstore/dialect.go` and `sqlstore.go`: add `version bigint NOT NULL DEFAULT 1` to all three schemas plus additive idempotent migration statements (Postgres `ADD COLUMN IF NOT EXISTS`; MySQL via `information_schema` probe; SQLite via `PRAGMA table_info` probe); update insert/update/delete SQL to `version` predicates with `version = version + 1` on update; scan/return `Version`; update `internal/lease/sqlstore/sqlstore_test.go`
- [X] T005 [P] In `internal/lease/k8sstore.go`: persist `Version` in a `berth.skaphos.io/version` annotation (missing annotation reads as 1 for legacy objects); `Put`/`Delete` check the annotation against `expectedVersion` and keep the existing `resourceVersion`-guarded Update/Delete; update `internal/lease/k8sstore_test.go`
- [X] T006 In `internal/lease/manager.go`: thread the observed `cur.Version` into every `Put`/`Delete` call in `Acquire`, `Renew`, `Release` (absent → expected 0); `Renew`'s CAS is now the version, not the token — behavior contract per [contracts/store-contract.md](contracts/store-contract.md)
- [X] T007 In `internal/lease/ttl.go`: GC `Delete` predicate becomes the `Version` captured at scan; update `internal/lease/ttl_test.go`
- [X] T008 In `internal/lease/storetest/storetest.go` and `benchmarks.go`: update conformance to the new signatures and add version-discipline cases (create stores 1; each successful Put stores expected+1; stale expectedVersion → `ErrConflict`; Get/List return Version); update `internal/lease/membench_test.go`
- [X] T009 Run `go -C tools tool task test` — tree compiles, full suite green with the new signatures before any behavior work continues

## Phase 3: User Story 1 — Failover never yields two holders (P1, #90)

**Goal**: a stale renew and a concurrent reclaim can never both succeed.

**Independent test**: storetest interleaving cases + manager-level trace test below; both fail when `internal/lease` is reverted to token-CAS.

- [X] T010 [P] [US1] In `internal/lease/storetest/storetest.go`: ABA regression — read a record at version v, land an interfering write, then assert `Put(expected=v)` with a *same-token renew-shaped* record returns `ErrConflict`, in both landing orders (spec US1 scenarios 1–2)
- [X] T011 [P] [US1] In `internal/lease/manager_test.go`: reproduce the issue #90 trace with a stalling `Store` wrapper — A's renew passes checks and stalls before Put; B reclaims (token bump); A's write lands and must lose; assert A gets `Acquired=false` and B is sole holder; add a concurrent acquire/renew/release single-holder invariant test (spec US1 scenario 3), race detector on
- [X] T012 [US1] Verify regression quality: temporarily revert the manager/store predicate change (e.g. `git stash` of the fix hunks or a token-CAS toggle in a scratch branch), confirm T010/T011 fail, restore; note the verification in the PR description

**Checkpoint**: `go -C tools tool task test` green; #90 provably closed at both store and manager boundaries.

## Phase 4: User Story 2 — Fencing tokens never repeat for a key (P2, #92 + #93)

**Goal**: release/expiry/GC never reset or reuse tokens; GC can never touch a live lease. Design D2/D3/D4 in [research.md](research.md).

- [X] T013 [US2] In `internal/lease/manager.go`: `Release` writes a tombstone (`Put(cur.Version, {Holder: "", FencingToken: cur token})`) instead of deleting, keeping idempotent-nil race semantics; `Acquire` treats `cur.Holder == ""` as available and issues `cur.FencingToken + 1` with the `math.MaxInt32` refuse-to-wrap check; `Renew` on a tombstone reports the lease lost
- [X] T014 [US2] In `internal/lease/ttl.go`: `collect` skips tombstones and converts records expired beyond grace into tombstones via `Put(scannedVersion, tombstone)`; `ErrConflict` → skip (live); update log wording ("tombstoned" not "deleted"); update `internal/lease/ttl_test.go` including the multi-replica concurrent-sweep case
- [X] T015 [US2] Remove `Delete` from the `Store` interface in `internal/lease/store.go` and delete its implementations and direct tests in `memstore.go`, `k8sstore.go`, `sqlstore/sqlstore.go` + `dialect.go` (drop `deleteSQL`), `storetest/storetest.go`, `storetest/benchmarks.go` — no hard-delete path remains
- [X] T016 [P] [US2] In `internal/lease/storetest/storetest.go`: #92 regression — acquire→release→reacquire and expire→tombstone→reacquire cycles produce strictly increasing, never-repeating tokens (spec US2 scenarios 1–2)
- [X] T017 [P] [US2] In `internal/lease/storetest/storetest.go`: #93 regression — capture version via List, release + fresh reacquire, then the GC-shaped tombstone `Put` with the scanned version must return `ErrConflict` and leave the live lease intact (spec US2 scenarios 3–4)
- [X] T018 [US2] In `internal/lease/manager_test.go`: tombstone semantics unit tests — release is idempotent on tombstone; renew/release by the old holder against a tombstone fail cleanly; acquire-from-tombstone preserves monotonicity and the ceiling error message

**Checkpoint**: suite green; #92/#93 closed structurally (no delete path), regression-verified as in T012.

## Phase 5: User Story 3 — Distinct keys never share a stored object (P3, #94)

**Goal**: key→storage-object mapping is injective; invalid keys rejected at the boundary. Design D5 in [research.md](research.md); contract [contracts/http-api.md](contracts/http-api.md).

- [X] T019 [P] [US3] Create `internal/lease/validate.go` + `validate_go_test.go` (or fold into `store.go`/`manager_test.go` per package convention): `ValidateKey(key Key) error` using `k8s.io/apimachinery/pkg/util/validation` — namespace RFC 1123 DNS label, name RFC 1123 DNS subdomain, `len(ns)+1+len(name) ≤ 253`; table-driven tests covering dots, uppercase, underscores, empty, length bounds, and the issue's `("a","b.c")` vs `("a.b","c")` pair
- [X] T020 [US3] In `internal/lease/manager.go`: `Acquire`/`Renew`/`Release` call `ValidateKey` before any store access; add manager tests for the error path
- [X] T021 [US3] In `internal/api/leases.go`: validate the key in `handleAcquire`/`handleRenew`/`handleRelease` after `authorize` and return 400 naming the field and allowed format; add handler tests in `internal/api/leases_test.go` for the 400s, the authz-before-validation ordering, and unchanged success responses
- [X] T022 [P] [US3] In `internal/lease/k8sstore_test.go`: injectivity property test — over generated `ValidateKey`-accepted key pairs, distinct keys always produce distinct `k8sLeaseName` values, and every produced name is a valid k8s object name

**Checkpoint**: suite green; a dotted-namespace request can no longer reach any store.

## Phase 6: Polish & cross-cutting

- [X] T023 [P] Update `docs/concepts.md`: fencing token monotonic across the key's entire lifetime (release/expiry/GC included); tombstone behavior and the out-of-band-deletion caveat; cumulative int32 ceiling note; key format rules
- [X] T024 [P] Update `docs/architecture.md`: GC is a tombstoning sweep; replace the token-CAS correctness claim with the never-reused-version argument; note `Store.Delete` removal
- [X] T025 [P] Update `docs/reference/api.md`: key validation rules and 400 responses; fencing-token guarantee as delivered; check `docs/reference/configuration.md` for SQL-migration notes (`migrate=off` needs the manual `ALTER TABLE`, per research D6)
- [X] T026 In `internal/lease/sqlstore/sqlstore_integration_test.go`: run conformance + the new regressions against real Postgres and MySQL, and add an upgrade-in-place case — create the pre-change schema, run `migrate=auto`, verify legacy rows behave as `Version = 1` and are safely CAS'd
- [X] T027 Check `test/e2e/lease_test.go`: if the harness exercises release→reacquire, assert token monotonicity end-to-end; add an invalid-namespace 400 assertion
- [X] T028 Final gates: `go -C tools tool task test`, `lint`, `verify-generated`; confirm no `deploy/helm/` file changed (if any did, bump that chart's `Chart.yaml` version in the same change); walk [quickstart.md](quickstart.md) sections 3–4 manually against a local apiserver
- [X] T029 Prepare the PR: reference issues #90 #92 #93 #94 (`Fixes #…` each), summarize the T012-style regression verification, signed + DCO commits throughout

## Dependencies

```text
Phase 1 (T001)
  └─► Phase 2 (T002 → {T003,T004,T005} → T006 → T007 → T008 → T009)
        └─► Phase 3 US1 ({T010,T011} → T012)          — MVP: the critical #90 fix
              └─► Phase 4 US2 (T013 → T014 → T015 → {T016,T017} → T018)
                    └─► Phase 5 US3 ({T019,T022} → T020 → T021)   — independent of US1/US2 content,
                    │                                               sequenced after to avoid manager.go conflicts
                    └─► Phase 6 ({T023,T024,T025} ∥ T026 → T027 → T028 → T029)
```

- US1 depends on the foundational contract (Phases 2+3 are one compile unit).
- US2 depends on US1's predicate (tombstone writes CAS on version) and removes `Delete` only after GC/release stop using it.
- US3 is logically independent (touches `validate.go` + API layer) and can be developed in parallel by another contributor; it is sequenced last here only because T020 edits `manager.go`.

## Parallel opportunities

- Phase 2: T003, T004, T005 (three backends, disjoint files) after T002.
- Phase 3: T010 and T011 (different files).
- Phase 4: T016 and T017 (same file, separate subtests — parallel if split across contributors, else sequential edits).
- Phase 5: T019 and T022 before the wiring tasks.
- Phase 6: T023, T024, T025 docs in parallel with T026.

## Implementation strategy

MVP = Phases 1–3: the critical double-holder fix (#90), independently
shippable and regression-proven. Phases 4–5 complete the milestone's
remaining three issues; Phase 6 makes the docs honest and runs the full
gate. Suggested PR shape: a single PR for the store-layer work (#90/#92/#93
are one root-cause family per the spec) with #94 either included or as a
small follow-up PR — decide at T029 based on final diff size.
