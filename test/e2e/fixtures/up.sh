#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Skaphos
# SPDX-License-Identifier: MIT
#
# Bring up the Berth e2e topology: three kind clusters wired so the
# east + west operators reach the coord-hosted API server via its
# control-plane container IP + Service NodePort.
#
# The kind clusters share the default `kind` docker network, so the
# east/west operator pods can reach the coord control-plane container
# directly on the docker bridge.

set -euo pipefail

cd "$(dirname "$0")"
FIXTURES_DIR="$PWD"
REPO_ROOT="$(cd ../../.. && pwd)"
TMP_DIR="${REPO_ROOT}/.tmp/e2e"
mkdir -p "$TMP_DIR"

COORD_CLUSTER=berth-e2e-coord
EAST_CLUSTER=berth-e2e-east
WEST_CLUSTER=berth-e2e-west
NAMESPACE=berth-system
COORD_NS=berth-coordination

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

log "creating kind clusters (parallel)"
for cfg in kind-coord.yaml kind-east.yaml kind-west.yaml; do
  kind create cluster --config "$FIXTURES_DIR/$cfg" --wait 90s &
done
wait

log "building local images"
(
  cd "$REPO_ROOT"
  docker build -f Dockerfile.apiserver -t berth-apiserver:e2e .
  docker build -f Dockerfile.operator -t berth-operator:e2e .
)

log "loading images into each cluster"
for cluster in "$COORD_CLUSTER" "$EAST_CLUSTER" "$WEST_CLUSTER"; do
  kind load docker-image berth-apiserver:e2e --name "$cluster" &
  kind load docker-image berth-operator:e2e --name "$cluster" &
done
wait

log "generating throwaway TLS material + API key"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$TMP_DIR/tls.key" -out "$TMP_DIR/tls.crt" \
  -days 1 -subj "/CN=berth-apiserver.${NAMESPACE}.svc" >/dev/null 2>&1

API_KEY="e2e-key"
API_KEY_HASH="$(printf '%s' "$API_KEY" | sha256sum | cut -d' ' -f1)"
printf '%s:%s\n' "$API_KEY" "$API_KEY_HASH" > "$TMP_DIR/api-keys"

log "installing coord cluster: namespaces + secrets + apiserver"
kubectl --context "kind-$COORD_CLUSTER" create namespace "$NAMESPACE"
kubectl --context "kind-$COORD_CLUSTER" create namespace "$COORD_NS"
kubectl --context "kind-$COORD_CLUSTER" -n "$NAMESPACE" create secret tls berth-apiserver-tls \
  --cert="$TMP_DIR/tls.crt" --key="$TMP_DIR/tls.key"
kubectl --context "kind-$COORD_CLUSTER" -n "$NAMESPACE" create secret generic berth-api-keys \
  --from-file=api-keys="$TMP_DIR/api-keys"

helm --kube-context "kind-$COORD_CLUSTER" install berth-apiserver \
  "$REPO_ROOT/deploy/helm/berth-apiserver" \
  -n "$NAMESPACE" \
  -f "$FIXTURES_DIR/apiserver-values.yaml" \
  --wait --timeout 3m

log "discovering apiserver address reachable from runner clusters"
COORD_IP="$(docker inspect "${COORD_CLUSTER}-control-plane" \
  --format '{{ .NetworkSettings.Networks.kind.IPAddress }}')"
NODE_PORT="$(kubectl --context "kind-$COORD_CLUSTER" -n "$NAMESPACE" \
  get svc berth-apiserver -o jsonpath='{.spec.ports[0].nodePort}')"
API_URL="https://${COORD_IP}:${NODE_PORT}"
echo "$API_URL" > "$TMP_DIR/apiserver-url"
echo "coord apiserver reachable at $API_URL"

install_operator() {
  local cluster="$1"
  local cluster_id="$2"
  kubectl --context "kind-$cluster" create namespace "$NAMESPACE"
  kubectl --context "kind-$cluster" -n "$NAMESPACE" create secret generic berth-api-key \
    --from-literal=api-key="$API_KEY"
  helm --kube-context "kind-$cluster" install berth-operator \
    "$REPO_ROOT/deploy/helm/berth-operator" \
    -n "$NAMESPACE" \
    -f "$FIXTURES_DIR/operator-values.yaml" \
    --set "clusterID=$cluster_id" \
    --set "berth.apiServer=$API_URL" \
    --wait --timeout 3m
}

log "installing east operator"
install_operator "$EAST_CLUSTER" cluster-east

log "installing west operator"
install_operator "$WEST_CLUSTER" cluster-west

log "topology ready"
printf 'API_URL=%s\nAPI_KEY=%s\n' "$API_URL" "$API_KEY" > "$TMP_DIR/env"
cat "$TMP_DIR/env"
