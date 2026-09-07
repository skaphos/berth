#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Skaphos
# SPDX-License-Identifier: MIT
set -euo pipefail

version="${1:?usage: package-release-charts.sh VERSION OUTPUT_DIR}"
output="${2:?usage: package-release-charts.sh VERSION OUTPUT_DIR}"
root="$(cd "$(dirname "$0")/.." && pwd)"
images="$("$root/scripts/release-images.sh" "$version")"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
printf '%s\n' "$images" > "$output/images.txt"

for chart in berth-apiserver berth-operator; do
  helm package "$root/deploy/helm/$chart" --version "$version" \
    --app-version "v$version" --destination "$output"
done

# Render the packaged artifacts with every optional Berth image enabled.
# Do not override repositories or tags: that would hide broken defaults.
helm template release-api "$output/berth-apiserver-$version.tgz" \
  --set replicaCount=1,store.backend=mem,auth.mode=none \
  --set tls.certManager.enabled=false,tls.existingSecret=release-test \
  > "$output/apiserver.yaml"
helm template release-operator "$output/berth-operator-$version.tgz" \
  --set clusterID=release-test,berth.apiServer=https://berth.invalid \
  --set injection.enabled=true,injection.webhook.tls.existingSecret=release-test \
  --set injection.webhook.tls.caBundle=dGVzdA== \
  --set sidecarBroker.enabled=true,sidecarBroker.oidc.issuerURL=https://issuer.invalid \
  --set sidecarBroker.oidc.clientID=release-test,sidecarBroker.oidc.clientSecret.secretName=release-test \
  > "$output/operator.yaml"

LC_ALL=C sort -u "$output/images.txt" > "$output/expected-images.txt"
# Includes image fields and the operator's injected-helper-image argument.
grep -hEo 'ghcr.io/skaphos/berth-[[:alnum:]-]+:[^"[:space:]]+' \
  "$output/apiserver.yaml" "$output/operator.yaml" | LC_ALL=C sort -u \
  > "$output/rendered-images.txt"
diff -u "$output/expected-images.txt" "$output/rendered-images.txt"
echo "Packaged chart image references match all four release images."
