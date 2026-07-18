#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
#
# Bring up the Berth scalability load harness: a single kind cluster
# ("berth-load") hosting
#   - a k8s-backed API server (coordination.k8s.io Lease store on coord etcd)
#   - a sql-backed API server (in-cluster throwaway Postgres)
#   - a minimal kube-prometheus-stack scraping both via ServiceMonitors
#
# The Phase 3 load driver runs from the HOST (go run ./test/load) and reaches
# each API server through the control-plane container IP + Service NodePort.
# Because the driver simulates every holder/standby itself, no runner clusters
# are needed — this is coord-only by design (SKA-445).
#
# Not production-grade: self-signed TLS, NodePort exposure, a fixed throwaway
# api key, emptyDir Postgres. It exists to produce real, instrumented numbers.

set -euo pipefail

cd "$(dirname "$0")"
FIXTURES_DIR="$PWD"
REPO_ROOT="$(cd ../../.. && pwd)"
TMP_DIR="${REPO_ROOT}/.tmp/load"
mkdir -p "$TMP_DIR"

CLUSTER=berth-load
CTX="kind-${CLUSTER}"
NAMESPACE=berth-system
COORD_NS=berth-coordination
MON_NS=monitoring
PROM_RELEASE=kube-prometheus-stack
IMAGE=berth-apiserver:load

log() { printf '\n=== %s ===\n' "$*"; }

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 1
  }
}

require kind
require kubectl
require helm
require docker
require openssl

log "creating kind cluster ${CLUSTER}"
kind create cluster --config "$FIXTURES_DIR/kind-load.yaml" --wait 90s

log "building + loading berth-apiserver image"
(
  cd "$REPO_ROOT"
  docker build -f Dockerfile.apiserver -t "$IMAGE" .
)
kind load docker-image "$IMAGE" --name "$CLUSTER"

log "installing kube-prometheus-stack (operator + Prometheus only)"
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
helm repo update prometheus-community >/dev/null
kubectl --context "$CTX" create namespace "$MON_NS"
# Installs the monitoring.coreos.com CRDs (incl. ServiceMonitor) the
# apiserver charts need, so this must precede the apiserver installs.
helm --kube-context "$CTX" install "$PROM_RELEASE" prometheus-community/kube-prometheus-stack \
  -n "$MON_NS" \
  -f "$FIXTURES_DIR/prometheus-values.yaml" \
  --wait --timeout 6m

log "generating throwaway TLS material + API key"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$TMP_DIR/tls.key" -out "$TMP_DIR/tls.crt" \
  -days 1 -subj "/CN=berth-apiserver.${NAMESPACE}.svc" >/dev/null 2>&1

API_KEY="load-key"
API_KEY_HASH="$(printf '%s' "$API_KEY" | sha256sum | cut -d' ' -f1)"
printf '%s:%s\n' "$API_KEY" "$API_KEY_HASH" > "$TMP_DIR/api-keys"

log "creating namespaces + shared secrets"
kubectl --context "$CTX" create namespace "$NAMESPACE"
kubectl --context "$CTX" create namespace "$COORD_NS"
kubectl --context "$CTX" -n "$NAMESPACE" create secret tls berth-apiserver-tls \
  --cert="$TMP_DIR/tls.crt" --key="$TMP_DIR/tls.key"
kubectl --context "$CTX" -n "$NAMESPACE" create secret generic berth-api-keys \
  --from-file=api-keys="$TMP_DIR/api-keys"

log "deploying in-cluster Postgres for the sql backend"
kubectl --context "$CTX" apply -f "$FIXTURES_DIR/postgres.yaml"
# DSN the sql apiserver mounts as a file. sslmode=disable: throwaway, in-cluster.
PG_DSN="postgres://berth:berth@berth-load-postgres.${NAMESPACE}.svc:5432/berth?sslmode=disable"
kubectl --context "$CTX" -n "$NAMESPACE" create secret generic berth-sql-dsn \
  --from-literal=dsn="$PG_DSN"
kubectl --context "$CTX" -n "$NAMESPACE" rollout status deploy/berth-load-postgres --timeout 3m

log "installing berth-apiserver-k8s (coordination Lease backend)"
helm --kube-context "$CTX" install berth-apiserver-k8s \
  "$REPO_ROOT/deploy/helm/berth-apiserver" \
  -n "$NAMESPACE" \
  -f "$FIXTURES_DIR/apiserver-k8s-values.yaml" \
  --wait --timeout 3m

log "installing berth-apiserver-sql (Postgres backend)"
helm --kube-context "$CTX" install berth-apiserver-sql \
  "$REPO_ROOT/deploy/helm/berth-apiserver" \
  -n "$NAMESPACE" \
  -f "$FIXTURES_DIR/apiserver-sql-values.yaml" \
  --wait --timeout 3m

log "pinning NodePorts to the host-mapped ports"
# kind-load.yaml publishes fixed NodePorts (30443/30444/30090) to the host via
# extraPortMappings, giving the load driver a real network path instead of a
# single proxied port-forward (which would bottleneck the run). The apiserver
# chart does not template a nodePort, so patch the https port (index 0) to the
# pinned value; Prometheus pins its own via prometheus-values.yaml.
patch_nodeport() { # svc nodePort
  kubectl --context "$CTX" -n "$NAMESPACE" patch svc "$1" --type=json \
    -p "[{\"op\":\"replace\",\"path\":\"/spec/ports/0/nodePort\",\"value\":$2}]" >/dev/null
}
patch_nodeport berth-apiserver-k8s 30443
patch_nodeport berth-apiserver-sql 30444

API_URL_K8S="https://127.0.0.1:18443"
API_URL_SQL="https://127.0.0.1:18444"
PROM_URL="http://127.0.0.1:19090"

wait_url() { # url
  local url="$1" i
  for i in $(seq 1 40); do
    if curl -sk "$url" -o /dev/null --max-time 2 2>/dev/null; then return 0; fi
    sleep 0.5
  done
  echo "endpoint did not become reachable: $url" >&2
  return 1
}

log "waiting for host-mapped endpoints"
wait_url "${API_URL_K8S}/healthz"
wait_url "${API_URL_SQL}/healthz"
wait_url "${PROM_URL}/-/ready"

{
  printf 'API_URL_K8S=%s\n' "$API_URL_K8S"
  printf 'API_URL_SQL=%s\n' "$API_URL_SQL"
  printf 'PROM_URL=%s\n' "$PROM_URL"
  printf 'API_KEY=%s\n' "$API_KEY"
} > "$TMP_DIR/env"

log "harness ready"
cat "$TMP_DIR/env"
printf '\nRun a scenario with:  task load-run BACKEND=k8s SCENARIO=steady\n'
printf 'Prometheus UI:        %s\n' "$PROM_URL"
