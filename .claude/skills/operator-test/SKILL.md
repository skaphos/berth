---
name: operator-test
description: Use when designing or writing tests for Kubernetes operators in Go — reconciler unit tests, envtest, fake-client, webhook validation/defaulting tests, and CRD generation checks. Pair with `go-test`, `go-policy`, `go-workflow`. Skip for non-controller Go tests.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Operator Test Strategy And Generation

## Purpose
Use this skill when designing or writing tests for Kubernetes operators built in Go.

Apply this skill with `go/policy.skill.md`, `go/workflow.skill.md`, and `go/test.skill.md` for base Go testing standards.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Run the tests you write. A new reconciler or envtest case is not done until `go test`, `envtest`, or the repo's runner has executed it and the result is known.
- Regenerate CRDs and deep-copy code via `make generate`/`make manifests` after API changes; do not rely on stale generated files.
- Issue independent tool calls (reading types, predicates, watch setup, existing tests) in parallel.
- Report failing envtest runs with the exact command and output that produced them.

## Test Layers
- pure Go unit tests for helpers, predicates, mapping functions, and condition logic
- focused reconciler tests for branching, retries, and error handling
- `envtest` for API server backed reconciliation behavior
- webhook tests for validation and defaulting
- generation checks for CRDs and manifests when code generation is part of the repo

## Guidance
- Prioritize reconciliation invariants: idempotency, ownership, finalizers, status updates, and event ordering resilience.
- Test status and conditions as part of the contract, not as incidental output.
- Prefer deterministic fake-client tests only for narrow logic; use `envtest` when API-server semantics matter.
- Cover deletion paths, not-found paths, partial creation, and requeue-after-error behavior.
- Treat CRD version changes and webhooks as high-risk areas requiring explicit coverage.
