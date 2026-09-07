#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Skaphos
# SPDX-License-Identifier: MIT
set -euo pipefail

version="${1:?usage: release-images.sh VERSION}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "Invalid release image version: $version" >&2
  exit 1
fi
for image in berth-apiserver berth-operator berth-oidc-broker berth-acquire; do
  printf 'ghcr.io/skaphos/%s:v%s\n' "$image" "$version"
done
