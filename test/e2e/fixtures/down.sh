#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
#
# Tear down the Berth e2e topology. Safe to run when clusters don't
# exist — kind itself returns 0 in that case after a warning.

set -euo pipefail

for cluster in berth-e2e-coord berth-e2e-east berth-e2e-west; do
  kind delete cluster --name "$cluster" || true
done

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
rm -rf "$REPO_ROOT/.tmp/e2e"
