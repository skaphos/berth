<!--
SPDX-FileCopyrightText: 2026 Skaphos
SPDX-License-Identifier: MIT
-->

# Scalability and Load Testing

This page documents the scaling model Berth is being designed to meet, the
load-testing plan that produces evidence for it, and the measurement results
once each phase lands. It is the durable artifact for the
"how big can Berth get?" conversation.

Status: **plan, not measured.** Phases 1 onward fill in the numbers.

## Target Workload

The current sizing target is:

| Dimension | Value |
| --- | --- |
| Applications (distinct leases) | up to **2,000** |
| Regions | **8** |
| Region topology | pairs (active + standby), so one lease ↔ 2 clusters competing |
| Default lease parameters | `ttlSeconds: 30`, `heartbeatIntervalSeconds: 10` |
| Backend store | `k8s` (primary), `sql` (alternative for runner-local topologies) |

This is one concrete target, not a hard limit. The harness in this page is
parameterized so the same scenarios can be re-run for larger or smaller fleets.

## Sizing Arithmetic

Numbers below are derived from the lease-lifecycle behavior described in
[Architecture](../architecture.md) and the default lease parameters above. They
are the **predicted** load the harness must validate against; measured numbers
land in [Results](#results) once Phase 1+ run.

Per held lease, with `ttl=30s`, `heartbeat=10s`:

- Holder issues one `renew` every `heartbeatIntervalSeconds` = **0.1 req/s**.
- Standby in the paired cluster issues one `acquire` (denied) at roughly the
  same cadence while waiting = **0.1 req/s**.

For 2,000 leases × 1 holder + 1 standby each:

| Metric | Steady-state estimate |
| --- | --- |
| Holder `renew` QPS to API | ~**200 req/s** |
| Standby `acquire` (denied) QPS | ~**200 req/s** |
| Combined API write QPS | ~**400 req/s** |
| Backend write rate (`k8s`: etcd `Lease` updates) | ~400 writes/s |
| Backend write rate (`sql`: row CAS updates) | ~400 tx/s |
| `BerthLease` CRs across tenant clusters | up to **4,000** (2,000 per cluster in a pair) |

Bounded events:

| Event | Load shape |
| --- | --- |
| Cold start | up to 2,000 simultaneous `acquire` calls inside the first heartbeat window. |
| Region-pair failover | up to ~**250 standby-to-holder transitions** inside one TTL (one region of the 8 = ~250 of the 2,000 leases). |
| Operator restart | up to 2,000 reconciles enqueued; bounded by controller-runtime workqueue parallelism. |

### Where the load actually lives

Three independent surfaces, sized differently:

1. **API server** — the central rendezvous. Steady-state QPS above; CPU/RAM
   sized for the chosen backend's per-request cost.
2. **Backend store** — almost certainly the ceiling.
    - `k8s` backend: every renew is an etcd write on a single
      `coordination.k8s.io/v1.Lease` object. The coordination cluster's etcd is
      the bottleneck; this is the primary thing Phase 1 measures.
    - `sql` backend: every renew is an indexed CAS update; Postgres and MariaDB
      scale horizontally on the API side because the DB handles concurrency,
      but DB connection-pool sizing matters. SQLite is single-writer (see
      [Architecture: Storage Backends](../architecture.md#storage-backends))
      and is not on this target.
3. **Operator** — per tenant cluster, ~2,000 `BerthLease` reconciles on a
   `min(heartbeat, ttl/3)` cadence. Controller-runtime workqueue depth and
   reconcile p99 are the signals.

## Load-Testing Plan

A phased plan. Each phase produces a usable signal on its own; later phases
build on earlier ones.

### Phase 0 — Sizing model (this document)

- [x] Target workload captured.
- [x] Steady-state, cold-start, and failover arithmetic written down.
- [x] Phased checklist published.

### Phase 1 — Store-level benchmarks via `storetest`

Adds `RunStoreBenchmarks(b, newStore)` to
[`internal/lease/storetest`](../../internal/lease/storetest/storetest.go),
mirroring the existing `RunStoreConformance`. Each store's test file calls it
against the same factory, so the same workload runs on `mem`, `k8s` (envtest),
and `sql` (testcontainers: Postgres, MariaDB, SQLite). This produces the **raw
backend ceiling** with no HTTP, auth, controller, or network noise.

Benchmarks:

- `BenchmarkAcquireParallel` — N goroutines contending for a single lease;
  measures CAS-conflict cost.
- `BenchmarkRenewSteadyState` — pre-populated holders renewing at heartbeat
  cadence; sustained QPS + per-op latency.
- `BenchmarkFailoverFanout` — one holder lets its lease expire; M standbys
  race; measures the acquire-after-expiry latency distribution.

Outputs are `benchstat`-comparable text files committed under
`docs/operations/benchmarks/` and a `task bench` recipe in `Taskfile.yml`.

- [ ] `RunStoreBenchmarks` added to `internal/lease/storetest`.
- [ ] Wired from `memstore_test.go`, `k8sstore_test.go`, and `sqlstore` tests.
- [ ] Baseline numbers committed for `mem`, `k8s` (envtest), `sql` (Postgres,
      MariaDB, SQLite).
- [ ] `task bench` recipe.

### Phase 2 — API-server Prometheus instrumentation

The API server currently exposes no metrics endpoint (only the operator does,
via controller-runtime on `:8080`). Phase 2 adds `promhttp` with the standard
RED metrics on the lease handlers and the backend store calls:

- `berth_apiserver_request_duration_seconds{route, method, status}`
- `berth_apiserver_requests_total{route, method, status}`
- `berth_apiserver_requests_inflight`
- `berth_lease_store_call_duration_seconds{op, backend, outcome}`

Endpoint placement (separate unauthenticated port vs. same TLS port behind
auth) is deferred until Phase 1 numbers exist, since the decision affects how
much per-request cost the scrape path itself adds.

- [ ] Endpoint placement decision (re-asked after Phase 1).
- [ ] Handler middleware records request metrics.
- [ ] Store wrapper records store-call metrics with backend label.
- [ ] Helm chart wires the scrape target (`ServiceMonitor` or annotations).

### Phase 3 — API-level load driver (`test/load/`)

A Go binary that drives the API server using `pkg/client`. Knobs:

```
--leases=2000      --pairs=8          --ttl=30s --heartbeat=10s
--scenario={steady,coldstart,failover,churn}
--duration=10m     --target=https://...
--store-backend={k8s,sql}        # informational tag, not a switch
```

Scenarios:

| Scenario | Models |
| --- | --- |
| `steady` | N held + N standby, characterize p50/p95/p99/p99.9 |
| `coldstart` | 2,000 simultaneous `acquire` inside a 30s window |
| `failover` | half the holders stop renewing; standby acquire latency distribution |
| `churn` | random holder restarts at a configurable rate |

Emits Prometheus scrape on `/metrics` and a JSON summary suitable for CI
assertions.

- [ ] Driver binary at `cmd/berth-load/` (or `test/load/`, TBD).
- [ ] Each scenario implemented and reproducible.
- [ ] Run against both `k8s` and `sql` backends; results in
      `docs/operations/results/`.

### Phase 4 — Operator-level load on the 3-cluster kind harness

Extends [`test/e2e/fixtures/up.sh`](../../test/e2e/fixtures/up.sh) with a
`load` mode that creates N `BerthLease` CRs across `east` and `west`. Measures:

- controller-runtime workqueue depth (already exported on operator `:8080`)
- reconcile p99 (already exported)
- end-to-end "BerthLease created" → `status.leaseState=held` latency
- failover wallclock when an operator pod is killed

This is the only phase that exercises the full reconcile + finalizer + CRD
path under sustained load.

- [ ] `up.sh load` mode and teardown.
- [ ] Metric capture (Prometheus + dashboards or JSON snapshots).
- [ ] Results table for 100 / 500 / 1,000 / 2,000 CRs per cluster.

### Phase 5 — Injection-path load

Scales a Deployment per tenant cluster with the
`berth.skaphos.io/inject: acquire` opt-in label. Distinct load shape from
operator-as-holder: every Pod is a client, so the API-server fan-out factor is
much larger. Validates the sizing for users who adopt the injection path (see
[Workload gating via injection](../workload-gating-injection.md)).

- [ ] Driver Deployment + scaler.
- [ ] Pod-acquire latency capture from API metrics.
- [ ] Results table at named replica counts.

### Phase 6 — Publish the sizing recommendation

This document is rewritten in place with measured numbers replacing
predictions: per-backend throughput at named load, recommended API server
CPU/RAM per 1,000 leases, recommended coordination-cluster etcd sizing (for
`k8s`) or DB sizing (for `sql`), failover-time SLO under named load, and a
`task load` recipe.

- [ ] Replace "predicted" tables with "measured" tables.
- [ ] Recommended deployment shape for the 2,000-app / 8-region / pairs target.
- [ ] `task load` runs the full scenario set against an instrumented target.

## Results

> Empty until Phase 1+ land. Each phase appends a dated section with the rig
> description and the numbers.

## Open Questions

- **Metrics endpoint placement** (deferred from Phase 2). Separate
  unauthenticated port matches the operator pattern but adds a flag and a
  NetworkPolicy concern; same TLS port behind auth needs Prometheus to hold a
  bearer token. Re-asked after Phase 1 produces baseline cost data.
- **SQL backend matrix.** `sqlstore_integration_test.go` already covers
  Postgres + MariaDB + SQLite for correctness; Phase 1 benchmarks should run
  against all three but the sizing recommendation only targets Postgres /
  MariaDB (SQLite is single-writer per the architecture doc).
- **Reporting cadence.** Once the harness is stable, a nightly CI job that
  re-runs Phase 1 benchmarks against `mem` and `sql` (sqlite + tc) and
  publishes `benchstat` deltas would catch regressions cheaply. Not in the
  Phase 0 scope.
