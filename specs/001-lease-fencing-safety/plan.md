# Implementation Plan: Lease Fencing and Isolation Safety

**Branch**: `fix-90-lease-fencing` (spec dir `001-lease-fencing-safety`) | **Date**: 2026-07-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-lease-fencing-safety/spec.md`

## Summary

Close the four verified lease-safety defects (#90, #92, #93, #94) with one
coherent store-layer change: (1) move the conditional-write predicate off the
fencing token onto a store-maintained per-record **version** that changes on
every write, (2) replace deletion with **tombstones** so fencing tokens and
versions are never reused across a key's lifetime, (3) turn the TTL GC into a
tombstoning sweep whose predicate is therefore never-reused, and (4) validate
lease keys at the API boundary (namespace = DNS label, name = DNS subdomain)
so the key→storage-object mapping is injective. Full decision record in
[research.md](research.md).

## Technical Context

**Language/Version**: Go 1.26 (`go.mod`; toolchain pinned via `tools/`)

**Primary Dependencies**: stdlib `net/http` + `database/sql`;
`k8s.io/client-go` / `k8s.io/apimachinery` (Lease objects, validation
helpers); SQL drivers `pgx/v5`, `go-sql-driver/mysql`, `modernc.org/sqlite`

**Storage**: pluggable `internal/lease.Store` — in-memory map, Kubernetes
`coordination.k8s.io/v1` Lease objects, SQL (Postgres/MySQL/SQLite)

**Testing**: stdlib `go test` with race detector via
`go -C tools tool task test`; shared conformance suite
`internal/lease/storetest` run by all backends; SQL integration tests;
`test/e2e` harness

**Target Platform**: Linux server (apiserver, operator); CLI is
cross-platform and unaffected

**Project Type**: single Go repository — API server + operator + CLI +
public Go client

**Performance Goals**: no added round-trips on the lease hot path (acquire /
renew / release stay one read + one conditional write); GC sweep stays one
List + one conditional write per expired record

**Constraints**: linearizable per-key store operations (existing contract);
in-place upgrade with pre-existing records (FR-011); HTTP success responses
unchanged; signed + DCO commits; regression test per fixed defect

**Scale/Scope**: ~6 packages touched (`internal/lease`, `sqlstore`,
`storetest`, `internal/api`, docs); no CRD or chart behavior changes expected

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` (Berth v1.0.0).*

| Principle | Verdict | Notes |
|---|---|---|
| I. Explicit state | PASS | Tombstone is an explicit, persisted record state (`Holder == ""`), not a convention; version is explicit per-record state. |
| II. Git as boundary | PASS | Schema/contract changes land in-repo (dialect schema, contract comments); no out-of-band state. |
| III. Deterministic | PASS | Predicates are exact-match CAS; no timing-dependent correctness (that is the defect being removed). |
| IV. Kubernetes-native | PASS | k8s backend keeps Lease objects + resourceVersion optimistic concurrency; adds only an annotation. |
| V. Compose, don't trap | PASS | No new cross-tool dependencies; `Store` stays a small interface. |
| VI. Explainable | PASS | GC logs gain tombstone wording; conflict outcomes remain distinct (`ErrConflict`/`ErrNotFound`). |
| VII. Read-only degradation | PASS | Unchanged: List/Get paths untouched by mutation-path changes. |
| VIII. Topology | N/A | No topology modeling in scope. |
| IX. Honest docs | PASS | FR-010 forces docs to match delivered guarantees; the current overclaim (#92) is being fixed code-first. |
| X. Coordination safety | PASS | This feature *is* principle X: interleaving-safe CAS, monotonic tokens, tenant-injective keys, store-boundary regression tests. |
| Store contract constraint | PASS | Contract doc (`store.go`), all three backends, and `storetest` change in the same unit of work. |
| Testing constraint | PASS | Regression test per defect (D7); race-enabled CI already default. |
| Generated artifacts | PASS | No API-type changes → no codegen churn; `verify-generated` stays green. |
| Helm constraint | WATCH | No chart file changes expected; if any chart file is touched at implement time, bump that chart's version in the same change. |
| Docs constraint | PASS | D8 enumerates the doc updates shipped with the change. |

**Post-Phase-1 re-check**: PASS — the design artifacts introduce no new
projects, dependencies, or contract deviations. Complexity Tracking stays
empty.

## Project Structure

### Documentation (this feature)

```text
specs/001-lease-fencing-safety/
├── spec.md
├── plan.md              # This file
├── research.md          # Phase 0: decisions D1–D9
├── data-model.md        # Phase 1: record/key/version/tombstone model
├── quickstart.md        # Phase 1: validation guide
├── contracts/
│   ├── store-contract.md
│   └── http-api.md
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
internal/lease/
├── store.go             # Record.Version, rewritten Store contract (Put CAS on version; Delete removed), ValidateKey
├── manager.go           # thread version CAS; Release → tombstone write; acquire-from-tombstone token bump
├── memstore.go          # version CAS, tombstone-aware
├── k8sstore.go          # version annotation, tombstone objects, Delete removed
├── ttl.go               # tombstoning sweep, skip tombstones
├── storetest/
│   ├── storetest.go     # version-CAS conformance + #90/#92/#93 regression cases
│   └── benchmarks.go    # updated signatures
├── sqlstore/
│   ├── dialect.go       # version column, additive migration statements, tombstone-safe SQL
│   └── sqlstore.go      # version scan/args, Delete removed
internal/api/
└── leases.go            # ValidateKey enforcement (400s) in acquire/renew/release handlers
docs/
├── concepts.md          # fencing guarantee as delivered; tombstones; ceiling note
├── architecture.md      # GC correctness argument
└── reference/api.md     # key format rules, 400 responses
test/e2e/                # exercise validation + release/reacquire monotonicity if harness reaches it
```

**Structure Decision**: existing single-repo layout; all changes are in the
lease packages, the HTTP handler layer, and docs. No new packages except
none — `ValidateKey` lives in `internal/lease` beside `Key`.

## Complexity Tracking

No constitution violations to justify — table intentionally empty.
