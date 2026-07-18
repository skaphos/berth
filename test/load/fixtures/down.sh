#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: MIT
#
# Tear down the Berth load harness. Safe to run when the cluster does not
# exist — kind returns 0 after a warning.

set -euo pipefail

kind delete cluster --name berth-load || true

REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
rm -rf "$REPO_ROOT/.tmp/load"
