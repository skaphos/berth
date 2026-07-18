#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
#
# Run one load scenario against a harness brought up by up.sh and archive a
# single result artifact under docs/operations/results/ that pairs:
#   - client-observed latency      (the load driver's JSON summary)
#   - server-side resource usage    (CPU/memory of the API server pod over the
#                                     run window, vs its configured requests),
#                                     pulled from the harness Prometheus.
# Together these serve the harness's three goals (SKA-445): size CPU/memory
# requests, localize bottlenecks, and baseline future releases.
#
# Configuration comes from the environment (the Taskfile passes these as Task
# vars; defaults below let it run standalone):
#   BACKEND      k8s | sql            (which API server to drive; default k8s)
#   SCENARIO     steady|coldstart|failover|churn   (default steady)
#   LEASES       number of leases      (default 500)
#   PAIRS        region pairs (label)  (default 8)
#   TTL          lease TTL             (default 30s)
#   HEARTBEAT    renew cadence         (default 10s)
#   DURATION     run length            (default 60s)
#   CONCURRENCY  max in-flight burst   (default 256)

set -euo pipefail

cd "$(dirname "$0")"
REPO_ROOT="$(cd ../../.. && pwd)"
ENV_FILE="${REPO_ROOT}/.tmp/load/env"
RESULTS_DIR="${REPO_ROOT}/docs/operations/results"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "harness env not found: $ENV_FILE (run 'task load-up' first)" >&2
  exit 1
fi
# shellcheck source=/dev/null
source "$ENV_FILE"

BACKEND="${BACKEND:-k8s}"
SCENARIO="${SCENARIO:-steady}"
LEASES="${LEASES:-500}"
PAIRS="${PAIRS:-8}"
TTL="${TTL:-30s}"
HEARTBEAT="${HEARTBEAT:-10s}"
DURATION="${DURATION:-60s}"
CONCURRENCY="${CONCURRENCY:-256}"

case "$BACKEND" in
  k8s) TARGET="$API_URL_K8S" ;;
  sql) TARGET="$API_URL_SQL" ;;
  *) echo "unknown BACKEND=$BACKEND (want k8s or sql)" >&2; exit 2 ;;
esac

mkdir -p "$RESULTS_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${RESULTS_DIR}/${SCENARIO}-${BACKEND}-${STAMP}.json"
SUMMARY="$(mktemp)"
trap 'rm -f "$SUMMARY"' EXIT

echo "driving ${BACKEND} backend at ${TARGET} — scenario=${SCENARIO} leases=${LEASES} duration=${DURATION}" >&2

START_TS="$(date -u +%s)"
go -C "$REPO_ROOT" run ./test/load \
  --target="$TARGET" \
  --store-backend="$BACKEND" \
  --scenario="$SCENARIO" \
  --leases="$LEASES" \
  --pairs="$PAIRS" \
  --ttl="$TTL" \
  --heartbeat="$HEARTBEAT" \
  --duration="$DURATION" \
  --concurrency="$CONCURRENCY" \
  --api-key="$API_KEY" \
  --insecure-skip-tls-verify \
  | tee "$SUMMARY"
END_TS="$(date -u +%s)"

# --- resource snapshot from Prometheus over the run window ------------------
# promq <query> -> first scalar value at END_TS, or "null".
WINDOW=$(( END_TS - START_TS )); [[ "$WINDOW" -lt 5 ]] && WINDOW=5
POD_RE="berth-apiserver-${BACKEND}-.*"
NS="berth-system"

promq() {
  curl -sg "${PROM_URL}/api/v1/query" \
    --data-urlencode "query=$1" \
    --data-urlencode "time=${END_TS}" 2>/dev/null \
    | jq -r '[.data.result[]?.value[1] | tonumber] | if length == 0 then "null" else max end' 2>/dev/null \
    || echo "null"
}

CPU_AVG=$(promq "avg_over_time( sum by (pod) ( rate(container_cpu_usage_seconds_total{namespace=\"${NS}\",pod=~\"${POD_RE}\",container!=\"\",container!=\"POD\"}[30s]) )[${WINDOW}s:10s] )")
CPU_PEAK=$(promq "max_over_time( sum by (pod) ( rate(container_cpu_usage_seconds_total{namespace=\"${NS}\",pod=~\"${POD_RE}\",container!=\"\",container!=\"POD\"}[30s]) )[${WINDOW}s:10s] )")
MEM_PEAK=$(promq "max_over_time( sum by (pod) ( container_memory_working_set_bytes{namespace=\"${NS}\",pod=~\"${POD_RE}\",container!=\"\",container!=\"POD\"} )[${WINDOW}s:5s] )")
CPU_REQ=$(promq "sum by (pod) ( kube_pod_container_resource_requests{namespace=\"${NS}\",pod=~\"${POD_RE}\",resource=\"cpu\"} )")
MEM_REQ=$(promq "sum by (pod) ( kube_pod_container_resource_requests{namespace=\"${NS}\",pod=~\"${POD_RE}\",resource=\"memory\"} )")
CPU_LIM=$(promq "sum by (pod) ( kube_pod_container_resource_limits{namespace=\"${NS}\",pod=~\"${POD_RE}\",resource=\"cpu\"} )")
MEM_LIM=$(promq "sum by (pod) ( kube_pod_container_resource_limits{namespace=\"${NS}\",pod=~\"${POD_RE}\",resource=\"memory\"} )")

# Merge latency summary + resource snapshot into one artifact.
jq -n \
  --slurpfile s "$SUMMARY" \
  --argjson startTs "$START_TS" --argjson endTs "$END_TS" --argjson window "$WINDOW" \
  --argjson cpuAvg "${CPU_AVG:-null}" --argjson cpuPeak "${CPU_PEAK:-null}" \
  --argjson memPeak "${MEM_PEAK:-null}" \
  --argjson cpuReq "${CPU_REQ:-null}" --argjson memReq "${MEM_REQ:-null}" \
  --argjson cpuLim "${CPU_LIM:-null}" --argjson memLim "${MEM_LIM:-null}" \
  '$s[0] + { resources: {
      window: { startTs: $startTs, endTs: $endTs, seconds: $window },
      cpuCoresAvg: $cpuAvg, cpuCoresPeak: $cpuPeak,
      memBytesPeak: $memPeak,
      cpuRequestCores: $cpuReq, cpuLimitCores: $cpuLim,
      memRequestBytes: $memReq, memLimitBytes: $memLim
  } }' > "$OUT"

echo "summary written to ${OUT}" >&2
jq -r '"  latency  renew p95=\(.ops.renew.p95Ms // "n/a")ms  acquire p95=\(.ops.acquire.p95Ms // "n/a")ms  errors(acq/renew)=\(.ops.acquire.errors // 0)/\(.ops.renew.errors // 0)",
        "  resource cpu avg=\(.resources.cpuCoresAvg // "n/a")c peak=\(.resources.cpuCoresPeak // "n/a")c (req \(.resources.cpuRequestCores // "n/a")c)  mem peak=\((.resources.memBytesPeak // 0)/1048576 | floor)MiB (req \((.resources.memRequestBytes // 0)/1048576 | floor)MiB)"' "$OUT" >&2
echo "Prometheus: ${PROM_URL}" >&2
