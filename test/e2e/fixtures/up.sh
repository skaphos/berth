#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
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
  # berth-acquire is injected into opted-in workload pods by the operator's
  # injection webhook (exercised by injection_test.go).
  docker build -f Dockerfile.acquire -t berth-acquire:e2e .
)

log "loading images into each cluster"
for cluster in "$COORD_CLUSTER" "$EAST_CLUSTER" "$WEST_CLUSTER"; do
  kind load docker-image berth-apiserver:e2e --name "$cluster" &
  kind load docker-image berth-operator:e2e --name "$cluster" &
  # Only the runner clusters inject; coord doesn't need the helper image, but
  # loading everywhere keeps the loop simple and the cost is negligible.
  kind load docker-image berth-acquire:e2e --name "$cluster" &
done
wait

log "generating throwaway TLS material + per-cluster API keys"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$TMP_DIR/tls.key" -out "$TMP_DIR/tls.crt" \
  -days 1 -subj "/CN=berth-apiserver.${NAMESPACE}.svc" >/dev/null 2>&1

# Each runner cluster is its own tenant: with static-key auth the tenant IS the
# key id (internal/auth/static.go), and lease requests are holder-authorized —
# the request holder must equal the caller's tenant or be scoped "<tenant>/..."
# (internal/tenant/authorizer.go). The operator's holder is its --cluster-id, so
# the key id must equal (or own) that cluster id or every acquire returns 403.
# We therefore mint one key per cluster with key id == cluster id. Distinct
# tenants contending for the same lease name is exactly the documented
# cross-cluster failover model (docs/architecture.md "Authorization"). The
# bearer token reuses the key id for test simplicity.
: > "$TMP_DIR/api-keys"
for cid in cluster-east cluster-west; do
  hash="$(printf '%s' "$cid" | sha256sum | cut -d' ' -f1)"
  printf '%s:%s\n' "$cid" "$hash" >> "$TMP_DIR/api-keys"
done

log "generating injection webhook serving cert (self-signed; SAN = webhook Service DNS)"
# The MutatingWebhookConfiguration's caBundle is the cert itself (self-signed),
# so the API server trusts the operator's webhook server. CN/SAN must match the
# webhook Service DNS: <release>-injection.<ns>.svc.
WEBHOOK_SVC="berth-operator-injection.${NAMESPACE}.svc"
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$TMP_DIR/webhook.key" -out "$TMP_DIR/webhook.crt" \
  -days 1 -subj "/CN=${WEBHOOK_SVC}" \
  -addext "subjectAltName=DNS:berth-operator-injection,DNS:berth-operator-injection.${NAMESPACE},DNS:${WEBHOOK_SVC},DNS:${WEBHOOK_SVC}.cluster.local" \
  >/dev/null 2>&1
WEBHOOK_CA_B64="$(base64 < "$TMP_DIR/webhook.crt" | tr -d '\n')"

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
  # The operator authenticates as its own tenant: bearer token == key id ==
  # cluster id, so its --cluster-id holder is owned by that tenant (see the
  # per-cluster key generation above).
  kubectl --context "kind-$cluster" create namespace "$NAMESPACE"
  kubectl --context "kind-$cluster" -n "$NAMESPACE" create secret generic berth-api-key \
    --from-literal=api-key="$cluster_id"
  # Webhook serving cert for the injection webhook (operator-values enables it).
  kubectl --context "kind-$cluster" -n "$NAMESPACE" create secret tls berth-operator-injection-tls \
    --cert="$TMP_DIR/webhook.crt" --key="$TMP_DIR/webhook.key"
  helm --kube-context "kind-$cluster" install berth-operator \
    "$REPO_ROOT/deploy/helm/berth-operator" \
    -n "$NAMESPACE" \
    -f "$FIXTURES_DIR/operator-values.yaml" \
    --set "clusterID=$cluster_id" \
    --set "berth.apiServer=$API_URL" \
    --set "injection.webhook.tls.existingSecret=berth-operator-injection-tls" \
    --set "injection.webhook.tls.caBundle=$WEBHOOK_CA_B64" \
    --wait --timeout 3m
}

log "installing east operator"
install_operator "$EAST_CLUSTER" cluster-east

log "installing west operator"
install_operator "$WEST_CLUSTER" cluster-west

log "topology ready"
# Per-cluster keys (token == key id == cluster id); each operator authenticates
# as its own tenant. No single shared API key any more.
printf 'API_URL=%s\nEAST_API_KEY=%s\nWEST_API_KEY=%s\n' \
  "$API_URL" "cluster-east" "cluster-west" > "$TMP_DIR/env"
cat "$TMP_DIR/env"
