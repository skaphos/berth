<!--
SPDX-FileCopyrightText: 2026 Rillan AI LLC
SPDX-License-Identifier: MIT
-->

# Berth scalability load harness

A self-contained, instrumented harness that runs the Phase 3 load driver
(`test/load`) against **real, deployed** Berth API servers so the predicted
tables in [`docs/operations/scalability.md`](../../../docs/operations/scalability.md)
can be replaced with measured numbers (SKA-445).

It is separate from the e2e correctness harness in `test/e2e/fixtures/`: that
one verifies cross-cluster behaviour across coord + east + west; this one is
**coord-only** and exists to push API load and capture latency/throughput.

## Topology

```
            host
        go run ./test/load  ──────────────┐  (simulates every holder + standby)
              127.0.0.1:18443/18444        │  https, --insecure-skip-tls-verify
                                           ▼  (kind extraPortMappings → NodePort)
  ┌─────────────────────── kind: berth-load ───────────────────────┐
  │   :30443/:30444 NodePort                                        │
  │   berth-apiserver-k8s ──► coordination.k8s.io Lease (etcd)      │
  │   berth-apiserver-sql ──► berth-load-postgres (in-cluster)      │
  │          │  │                                                   │
  │          ▼  ▼  ServiceMonitor (metrics :8080)                   │
  │   kube-prometheus-stack ── + kubelet/cAdvisor + kube-state-     │
  │   (operator + Prometheus,    metrics (per-pod CPU/mem usage)    │
  │    :30090 → 127.0.0.1:19090)                                    │
  └─────────────────────────────────────────────────────────────────┘
```

Both API servers expose the Phase 2 metrics endpoint; one Prometheus scrapes
both, plus kubelet/cAdvisor and kube-state-metrics for per-pod CPU/memory usage
and configured requests/limits. The `backend` label on
`berth_lease_store_call_duration_seconds` (`k8s` vs `sql`) separates the two
store paths in a single Prometheus.

The driver reaches each server over a real network path (kind
`extraPortMappings` publishing fixed NodePorts to `127.0.0.1`), **not**
`kubectl port-forward` — a single proxied forward would bottleneck the run and
distort the very numbers this harness exists to measure.

A load run against the **k8s** backend also surfaces real etcd store latency,
which practically covers the Phase 1 "k8s row" without a separate benchmark.

## Prerequisites

`kind`, `kubectl`, `helm`, `docker`, `openssl`, and a Go toolchain. A running
Docker daemon with enough headroom for a single-node kind cluster plus
Prometheus and Postgres.

## Usage

```sh
# bring up cluster + Prometheus + Postgres + both API servers
go -C tools tool task load-up

# drive a scenario (BACKEND k8s|sql, SCENARIO steady|coldstart|failover|churn)
go -C tools tool task load-run BACKEND=k8s SCENARIO=steady LEASES=500 DURATION=60s
go -C tools tool task load-run BACKEND=sql SCENARIO=steady LEASES=500 DURATION=60s

# tear everything down
go -C tools tool task load-down
```

The scripts can also be invoked directly (`./up.sh`, `./run.sh`, `./down.sh`);
`run.sh` reads its configuration from the environment (see its header).

## Where results land

`run.sh` writes one artifact per run to
`docs/operations/results/<scenario>-<backend>-<UTC>.json`, pairing the driver's
latency summary with a Prometheus resource snapshot (CPU avg/peak, memory peak,
and the configured requests/limits) over the run window — the latency answers
"how fast", the resources answer "how big to size it". Most runs are throwaway;
commit only curated, representative runs (see that directory's README).

## Reaching Prometheus

`load-up` prints `PROM_URL` (`http://127.0.0.1:19090`). Open it and query e.g.:

- `berth_apiserver_requests_total`
- `histogram_quantile(0.95, sum by (le) (rate(berth_apiserver_request_duration_seconds_bucket[1m])))`
- `berth_lease_store_call_duration_seconds{backend="k8s"}` / `{backend="sql"}`

## Not production-grade

Self-signed 1-day TLS, host-side `kubectl port-forward` exposure, a fixed
throwaway api key (`load-key`), and an emptyDir Postgres with a weak fixed
password. This is a measurement rig, not a deployment reference — see
`deploy/helm/` for that.
