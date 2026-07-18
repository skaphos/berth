<!--
SPDX-FileCopyrightText: 2026 Rillan AI LLC
SPDX-License-Identifier: MIT
-->

# Load harness results

JSON latency summaries produced by the scalability load harness
(`test/load/fixtures/`, SKA-445). Each file is one run of the Phase 3 load
driver against a deployed Berth API server.

`test/load/fixtures/run.sh` writes files here named
`<scenario>-<backend>-<UTC>.json` (e.g. `steady-k8s-20260530T184500Z.json`).
Most runs are throwaway — **commit only curated, representative runs** that
back a number in [`../scalability.md`](../scalability.md), and note the
hardware/cluster they came from in the PR.

## Schema

Each file is the driver's `load.Summary` (see `internal/load/summary.go`)
**plus** a `resources` block that `run.sh` adds from a Prometheus query over the
run window:

```jsonc
{
  "scenario": "steady",        // steady | coldstart | failover | churn
  "backend": "k8s",            // store backend driven (k8s | sql)
  "leases": 500,
  "pairs": 8,
  "ttlMs": 30000,
  "elapsedMs": 60012.3,
  "ops": {                     // keyed by operation: acquire | renew | release
    "acquire": {
      "count": 4000,
      "errors": 0,
      "minMs": 0.8, "maxMs": 42.1, "meanMs": 3.2,
      "p50Ms": 2.7, "p95Ms": 7.9, "p99Ms": 14.0, "p999Ms": 31.5
    }
  },
  "resources": {               // API server pod, over [startTs, endTs]
    "window": { "startTs": 1748620800, "endTs": 1748620860, "seconds": 60 },
    "cpuCoresAvg": 0.31, "cpuCoresPeak": 0.58,   // cores (1.0 = one vCPU)
    "memBytesPeak": 96468992,
    "cpuRequestCores": 0.1, "cpuLimitCores": 1.0,
    "memRequestBytes": 134217728, "memLimitBytes": 268435456
  }
}
```

Latencies are **client-observed** (driver → HTTP → API server → store);
percentiles are nearest-rank. `resources` is what answers the sizing question:
compare `cpuCoresPeak`/`memBytesPeak` against `*RequestCores`/`*RequestBytes`. A
`null` resource field means the Prometheus query returned no samples (e.g.
kube-state-metrics not yet scraped). For finer server-side and store-call
breakdowns, query the harness Prometheus live during the run
(`berth_apiserver_request_duration_seconds`,
`berth_lease_store_call_duration_seconds{backend="…"}`).
