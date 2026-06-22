<!--
SPDX-FileCopyrightText: 2026 Skaphos
SPDX-License-Identifier: MIT
-->

# Scalability and Load Testing

This page documents the scaling model Berth is being designed to meet, the
load-testing plan that produces evidence for it, and the measurement results
once each phase lands. It is the durable artifact for the
"how big can Berth get?" conversation.

Status: **Phases 1–3 landing.** Store-level baselines for `mem` and `sqlite`
are measured (Phase 1; see [Results](#results)), the API server is
Prometheus-instrumented on a separate `/metrics` port (Phase 2), and the
API-level load driver with all four scenarios is built and in-process-tested
(Phase 3). A coord-only kind + Prometheus harness
([`test/load/fixtures/`](https://github.com/skaphos/berth/tree/main/test/load/fixtures)) stands up `k8s`- and
`sql`-backed API servers under one Prometheus, capturing latency **and** per-pod
CPU/memory. The first deployed runs are in (see
[Results](#phase-3-first-deployed-run-k8s-vs-sql-steady)) and
already surfaced a real bottleneck — the `k8s` backend is throttle-bound by the
client-go default `QPS=5/Burst=10`, not resource-bound. What remains is the QPS
fix + re-run, then the operator/injection phases.

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
[`internal/lease/storetest`](https://github.com/skaphos/berth/blob/main/internal/lease/storetest/benchmarks.go),
mirroring the existing `RunStoreConformance` and reusing the **same** store
factory (`*testing.B` satisfies `testing.TB`). Each store's test file calls it,
so one workload runs on every backend. This produces the **raw backend
ceiling** with no HTTP, auth, controller, or network noise.

The three sub-benchmarks the suite runs:

- `AcquireParallel` — N goroutines doing `Get`+CAS on a single hot key,
  bumping the fencing token each write so writers genuinely collide; measures
  CAS-conflict cost (the cold-start / standby herd).
- `RenewSteadyState` — a fleet of independent holders renewing distinct keys at
  full speed; the sustained renew ceiling that bounds steady-state QPS.
- `FailoverFanout` — an already-expired lease with M standbys racing to
  reclaim, exactly one winning; the acquire-after-expiry path. Per-iteration
  setup/teardown is excluded from the timed region.

Outputs are `benchstat`-comparable `-count`-batched text files committed under
[`docs/operations/benchmarks/`](benchmarks/) and produced by a `task bench`
recipe in `Taskfile.yml`.

- [x] `RunStoreBenchmarks` added to `internal/lease/storetest`.
- [x] Wired from `mem` (external `lease_test`, to avoid the `storetest`→`lease`
      import cycle) and the `sqlstore` tests (SQLite, plus integration-tagged
      Postgres and MariaDB).
- [x] Baseline numbers committed for `mem` and `sql` (SQLite); see
      [Results](#results). Postgres and MariaDB run via `task bench-integration`
      against the DSN-gated integration path (`BERTH_TEST_POSTGRES_DSN` /
      `BERTH_TEST_MYSQL_DSN`) — same env contract as the existing conformance
      integration tests.
- [ ] `k8s` backend benchmark. **Deferred:** the repo has no `envtest`
      harness yet, and the unit tests use a fake clientset whose map+reactor
      cost is not the real etcd ceiling. Standing up `envtest` (or running the
      suite against a kind cluster) is tracked as its own follow-up so the
      `k8s` number is honest rather than a fake-client artifact.
- [x] `task bench` (mem + sqlite, no infra) and `task bench-integration`
      (Postgres / MariaDB) recipes.

### Phase 2 — API-server Prometheus instrumentation { #phase-2-api-server-prometheus-instrumentation }

The API server previously exposed no metrics endpoint (only the operator did,
via controller-runtime on `:8080`). Phase 2 adds `promhttp` with the standard
RED metrics on the lease handlers and the backend store calls
(`internal/metrics`):

- `berth_apiserver_request_duration_seconds{route, method, status}`
- `berth_apiserver_requests_total{route, method, status}`
- `berth_apiserver_requests_inflight`
- `berth_apiserver_lease_outcomes_total{outcome}`
- `berth_lease_store_call_duration_seconds{op, backend, outcome}`

The `route` label is the matched mux pattern (templated, so cardinality is
bounded), and `store-call` `outcome` separates the expected control-flow
signals (`conflict`, `notfound`) from an unexpected backend `error`. The
request middleware is the outermost wrapper, so recorded status includes auth
rejections; the store wrapper is a transparent `lease.Store` decorator tagged
with the resolved `backend`.

The `lease_outcomes` counter (SKA-447) names a request's **semantic** result —
`acquired`, `held-by-other`, `renewed`, `released`, `conflict`, `unauthorized`,
`error` — which HTTP status alone cannot express: a denied contender
(`held-by-other`) is a `200`, so contention is invisible in the status-keyed
series. This is the signal for contention rate and auth-failure storms. Each
request is also access-logged once (method, path, route, status, latency,
correlation id, and the authenticated holder/tenant — never the token) via
`api.LoggingMiddleware`, the outermost wrapper; `/healthz` logs at debug to keep
liveness probes out of the steady-state log.

**Endpoint placement — decided: separate unauthenticated port.** `/metrics` is
served on its own plain-HTTP port (`--metrics-addr`, default `:8080`), off the
TLS/auth path, mirroring the operator's controller-runtime endpoint. This keeps
the scrape path's per-request cost out of the instrumented lease path and
matches the existing operational pattern; the cost is a new flag and a
NetworkPolicy to restrict the port to the monitoring stack. The endpoint
exposes no lease data — only RED and store-call series.

- [x] Endpoint placement decision (separate unauthenticated port; see above).
- [x] Handler middleware records request metrics (`api.MetricsMiddleware`).
- [x] Store wrapper records store-call metrics with backend label
      (`metrics.WrapStore`).
- [x] Helm chart wires the scrape target: `metrics.service` adds the port,
      `metrics.serviceMonitor.*` renders a `ServiceMonitor`, and
      `metrics.podAnnotations.enabled` is the annotation-based alternative.
      A NetworkPolicy restricting `:8080` to Prometheus is left to the
      deploying cluster's policy set (documented, not yet templated).

### Phase 3 — API-level load driver (`test/load/`)

A Go program that drives the API server through `pkg/client`. The thin
entrypoint lives at [`test/load`](https://github.com/skaphos/berth/tree/main/test/load) (run with `go run
./test/load`); the scenario logic and latency math live in
[`internal/load`](https://github.com/skaphos/berth/tree/main/internal/load) so they are unit-testable. It is a
test/dev tool, deliberately **not** a shipped binary (no `task build`, no image,
not released). Knobs:

```
--leases=2000      --pairs=8          --ttl=30s --heartbeat=10s
--scenario={steady,coldstart,failover,churn}
--duration=5m      --target=https://...   --concurrency=256
--store-backend={k8s,sql}        # informational tag, not a switch
--tenant=load-key  # API key id; scopes generated holders so authz admits them
--api-key-file=... --ca-file=... --metrics-addr=:9090
```

(`--ttl` is whole-second granularity — the API expresses TTL as an int32
`ttlSeconds`. `--heartbeat` is sub-second-capable; it only paces cadence.)

When the target enforces authentication (the deployed fixtures use
`--auth-mode=static-keys` with key id `load-key`), pass `--tenant=load-key` so
every generated holder is scoped `load-key/...`; the API server binds each holder
to the caller's tenant and otherwise returns 403. Leave `--tenant` empty for an
`--auth-mode=none` target, where holder authorization is bypassed.

Scenarios:

| Scenario | Models |
| --- | --- |
| `steady` | N held + N standby contending, characterize p50/p95/p99/p99.9 |
| `coldstart` | every `acquire` fired near-simultaneously (no concurrency cap — that is the scenario) |
| `failover` | the even-index half is left to expire for one TTL while the odd-index survivor half keeps renewing at heartbeat cadence; standby acquire-after-expiry latency measured against that concurrent renew load |
| `churn` | each heartbeat a `--churn-fraction` of holders release and a fresh holder re-acquires |

> **`failover` semantics changed (SKA-458).** The scenario previously let *all*
> leases expire and timed a reclaim burst against an otherwise idle backend — a
> valid cold mass-expiry worst-case, but an optimistically low reclaim latency.
> It now keeps the survivor half renewing throughout, so the reclaim is measured
> under realistic concurrent load. Do not compare `failover` artifacts from
> before this change 1:1 against ones after it.

Emits a JSON latency summary (per-op count/errors/min/mean/p50/p95/p99/p99.9) on
stdout, and — when `--metrics-addr` is set — its own
`berth_load_request_duration_seconds{op,result}` on `/metrics` so Prometheus can
scrape the driver mid-run.

#### Deployed harness (`test/load/fixtures/`)

This harness exists to answer three questions, and it captures the telemetry
for all three — latency alone is not enough:

1. **Size CPU/memory requests & plan scaling** — measure per-pod CPU and memory
   *usage* under load and compare against the configured requests/limits.
2. **Identify bottlenecks** — localize latency by comparing client-observed
   latency against the server's RED metrics
   (`berth_apiserver_request_duration_seconds`) and store-call latency
   (`berth_lease_store_call_duration_seconds{backend}`), plus resource
   saturation.
3. **Baseline future releases** — commit comparable artifacts (run params +
   latency + resource usage) so a later release can be measured against today.

The driver simulates every holder and standby itself from the host, so an
API-level run against a real store needs only the **coordination** cluster — no
operator/runner clusters. [`test/load/fixtures/`](https://github.com/skaphos/berth/tree/main/test/load/fixtures)
brings up a single kind cluster (`berth-load`) hosting:

- `berth-apiserver-k8s` — `k8s` backend on the cluster's own etcd;
- `berth-apiserver-sql` — `sql` backend on an in-cluster throwaway Postgres;
- a `kube-prometheus-stack` scraping both apiservers' ServiceMonitors **plus**
  kubelet/cAdvisor and kube-state-metrics, so per-pod CPU/memory usage and the
  configured requests/limits are both recorded (goal 1). Grafana, Alertmanager,
  and node-exporter stay off.

The driver reaches each API server over a real network path — kind
`extraPortMappings` publish fixed NodePorts (30443/30444/30090) to
`127.0.0.1:18443/18444/19090`. This deliberately avoids `kubectl port-forward`,
whose single proxied connection bottlenecks a load run and confounds the
measurement (goal 2). One Prometheus covers both backends because the `backend`
label on `berth_lease_store_call_duration_seconds` separates the store paths; a
`k8s`-backend run also surfaces real etcd store latency, practically covering
the Phase 1 "k8s row".

```sh
go -C tools tool task load-up
go -C tools tool task load-run BACKEND=k8s SCENARIO=steady LEASES=500 DURATION=60s
go -C tools tool task load-run BACKEND=sql SCENARIO=steady LEASES=500 DURATION=60s
go -C tools tool task load-down
```

`run.sh` writes one artifact per run to [`docs/operations/results/`](results/)
pairing the driver's latency summary with a Prometheus resource snapshot
(CPU avg/peak, memory peak, configured requests/limits) over the run window;
fold the measured numbers into the [Results](#results) tables. The harness is a
measurement rig, not a deployment reference (self-signed TLS, fixed NodePorts,
throwaway api key, emptyDir Postgres) — see
[`test/load/fixtures/README.md`](https://github.com/skaphos/berth/blob/main/test/load/fixtures/README.md).

- [x] Driver location decided: `test/load` entrypoint + `internal/load` logic
      (not a shipped `cmd/*` binary).
- [x] All four scenarios implemented, plus an in-process end-to-end test that
      runs each against the real `api.NewMux` + mem store over `httptest` (no
      external infra) — proves the driver works; `internal/load` is at ~94%
      coverage.
- [x] Coord-only kind + Prometheus harness wiring `k8s` + `sql` API servers to
      one Prometheus, capturing latency **and** per-pod CPU/memory
      (`test/load/fixtures/`, `task load-up`/`load-run`/`load-down`).
- [x] First live `k8s` + `sql` steady runs captured; see
      [Results](#phase-3-first-deployed-run-k8s-vs-sql-steady).
      Headline finding: the `k8s` backend is throttle-bound by client-go
      `QPS=5/Burst=10`, not resource-bound.
- [ ] Raise/expose the `k8s` store client-go QPS/Burst, then re-run for an
      unthrottled `k8s` curve and fold per-backend CPU-request guidance into the
      Phase 6 recommendation.

### Phase 4 — Operator-level load on the 3-cluster kind harness

Extends [`test/e2e/fixtures/up.sh`](https://github.com/skaphos/berth/blob/main/test/e2e/fixtures/up.sh) with a
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

Each phase appends a dated section with the rig description and the numbers.

### 2026-05-30 — Phase 1 store-level baseline (mem, SQLite)

Rig: AMD Ryzen 9 9950X3D (32 hardware threads), Go test `-benchtime=2s
-count=6`, in-process. SQLite is in-memory (`mode=memory&cache=shared`). Raw
`go test -bench` output is committed under
[`docs/operations/benchmarks/`](benchmarks/) (`mem.txt`, `sqlite.txt`) and is
`benchstat`-ready. Median of six runs:

| Benchmark | `mem` | `sqlite` (in-mem) | What it bounds |
| --- | --- | --- | --- |
| `AcquireParallel` (per `Get`+CAS op) | ~0.25 µs | ~19 µs | CAS-conflict cost on one hot key |
| `RenewSteadyState` (per renew op) | ~0.40 µs | ~21 µs | sustained holder renew ceiling |
| `FailoverFanout` (per 8-standby reclaim round) | ~39 µs | ~250 µs | acquire-after-expiry on failover |

Read-out against the [sizing arithmetic](#sizing-arithmetic) (combined target
~400 write req/s for 2,000 leases):

- **The store layer is not the bottleneck at the target.** A single-process
  renew ceiling of ~0.40 µs/op (`mem`) or ~21 µs/op (`sqlite`) is ~2.5M and
  ~47k renews/s respectively — both orders of magnitude above the ~200 req/s
  holder-renew load the 2,000-lease target implies.
- This is the expected shape: the real ceiling lives in the **per-request
  round-trip** — HTTP, auth, and the backend's network/durability cost — which
  Phases 2–4 measure. `mem`/`sqlite` here are the in-process floor every
  durable backend is compared against, not a production sizing.
- `sqlite` is ~50–80× slower than `mem` per op but still far above target; it
  remains single-writer (not on the production target) and is included only as
  a cheap, infra-free regression signal.

Pending for a complete Phase 1: `k8s` (etcd) and the DSN-backed Postgres /
MariaDB numbers, which carry real network and durability cost and are the ones
the sizing recommendation will actually rest on.

### 2026-05-30 — Phase 3 first deployed run (`k8s` vs `sql`, steady) { #phase-3-first-deployed-run-k8s-vs-sql-steady }

Rig: the `test/load/fixtures/` harness (coord-only kind, single-replica API
servers, etcd from the kind node, in-cluster Postgres 16), driver on the host
over the fixed-NodePort path. `steady` scenario, ttl 30s / heartbeat 10s, 10s
request timeout. Raw artifacts (latency + per-pod resource snapshot) under
[`docs/operations/results/`](results/). These are kind/laptop numbers — useful
for **shape and bottleneck**, not absolute capacity.

| Run | acquire p50 | acquire p95 | acquire err | renew p50 | CPU peak (req 0.1c) | mem peak (req 128Mi) |
| --- | --- | --- | --- | --- | --- | --- |
| `k8s`, 300 leases | 10,001 ms | 10,026 ms | 1184/1192 | 9.5 ms | 0.007 c | 20 MiB |
| `k8s`, 8 leases | 585 ms | 2,399 ms | 0/32 | 601 ms | 0.004 c | 13 MiB |
| `sql`, 300 leases | 6.3 ms | 1,655 ms | 60/1800 | 7.8 ms | 0.086 c | 38 MiB |

Read against the three harness goals:

- **Bottleneck (goal 2): the `k8s` backend is throttle-bound by client-go, not
  resource-bound.** Under 300 concurrent acquires the API server pod is
  *idle* (0.007 c, 20 MiB) yet nearly every acquire hits the 10s deadline. The
  pod's own logs are unambiguous: `"Waited before sending request"
  delay="10.0s" reason="client-side throttling, not priority and fairness"` on
  the `coordination.k8s.io` Lease calls. The default client-go limiter
  (`QPS=5, Burst=10`) caps the store, and because every heartbeat fires
  renew+contend for *all* leases at once, the in-flight count exceeds Burst=10
  at even ~8 leases (the 8-lease run is slow but error-free; the 300-lease run
  times out). The unthrottled store latency is ~1–2 ms (renew min 1.3 ms).
  **Root cause:** [`internal/k8s/client.go`](https://github.com/skaphos/berth/blob/main/internal/k8s/client.go)
  builds the clientset via `rest.InClusterConfig()` and never sets
  `cfg.QPS`/`cfg.Burst`, so they take client-go's defaults (5 / 10).
  **Action:** make them configurable and raise the defaults before the `k8s`
  backend can carry the 2,000-lease target.
- **`sql` scales far better on the same load:** acquire p50 6.3 ms vs 10 s, and
  it degrades gracefully (p95 ~1.6 s, 3% errors) instead of collapsing — no
  client-side throttle; a connection pool absorbs the burst.
- **Sizing (goal 1):** neither backend is memory-bound here (≤38 MiB against a
  128 MiB request — the request could drop). `sql` reaches **86% of its 0.1-core
  request** at 300 leases while `k8s` stays idle, so the per-backend CPU-request
  guidance differs: `sql` needs CPU headroom to scale; `k8s` needs QPS headroom,
  not cores. Re-run after the QPS fix to get a `k8s` curve that isn't
  throttle-clipped.

## Open Questions

- **Metrics endpoint placement** — **resolved (Phase 2): separate
  unauthenticated port** (`--metrics-addr`, default `:8080`), matching the
  operator and keeping the scrape path off the instrumented TLS/auth path. The
  remaining follow-up is a templated NetworkPolicy restricting `:8080` to the
  monitoring stack — documented but left to the deploying cluster's policy set
  for now.
- **SQL backend matrix.** `sqlstore_integration_test.go` already covers
  Postgres + MariaDB + SQLite for correctness; Phase 1 benchmarks reuse the
  same factory. SQLite runs infra-free in `task bench`; Postgres and MariaDB
  run via `task bench-integration` against the existing DSN env contract
  (`BERTH_TEST_POSTGRES_DSN` / `BERTH_TEST_MYSQL_DSN`) — there is no
  testcontainers wiring in the repo, so the earlier "testcontainers" framing
  was aspirational. The sizing recommendation only targets Postgres / MariaDB
  (SQLite is single-writer per the architecture doc).

- **`k8s` (etcd) benchmark needs a real backend.** The unit tests use a fake
  clientset; benchmarking it would measure the fake's map+reactor cost, not
  etcd. A faithful `k8s` number needs `envtest` (not yet wired in this repo) or
  a kind cluster. Tracked as a Phase 1 follow-up before the `k8s` row in
  [Results](#results) can be filled in.
- **Reporting cadence.** Once the harness is stable, a nightly CI job that
  re-runs Phase 1 benchmarks against `mem` and `sql` (sqlite + tc) and
  publishes `benchstat` deltas would catch regressions cheaply. Not in the
  Phase 0 scope.
