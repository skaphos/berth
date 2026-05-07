---
name: operator-dev
description: Use when building or modifying Kubernetes operators in Go (kubebuilder, controller-runtime, achilles-sdk). Triggers on edits to API types, reconcilers, watch wiring, finalizers, status/conditions, webhooks, or controller-runtime manager setup. Pair with `go-policy` and `go-workflow`. Skip for ordinary Go work without controller scope.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Operator Development Guidance

## Purpose
Use this skill when building or modifying Kubernetes operators in Go. This skill is a controller-specific overlay and should be used together with `go/policy.skill.md` and `go/workflow.skill.md`.

This guidance is centered on `kubebuilder`, `controller-runtime`, and `achilles-sdk`. `operator-sdk` is not the primary workflow; if a repository already uses it, preserve local conventions rather than forcing a migration.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Invoke tools to read API types, reconcilers, watches, and generated CRDs before editing — do not describe behavior you have not traced.
- Run `make generate`, `make manifests`, `go test ./...`, and `envtest` yourself; do not claim a controller change is verified without tool output.
- Prefer structural tooling (LSP, `gopls`) for reference and implementation lookups across the reconciler, its owned resources, and tests.
- Issue independent tool calls (reading types, watches, RBAC markers, CRD manifests) in parallel.

## Scope
- CRD and API type design
- reconcilers and watch wiring
- status and condition management
- finalizers and deletion flow
- webhooks and validation/defaulting
- controller-runtime manager setup
- achilles-sdk integration where present

## Core Principles
- Treat reconciliation as a level-triggered convergence loop, not an imperative script.
- Make status truthful, minimal, and useful to operators.
- Keep spec, status, metadata, and external side effects clearly separated.
- Prefer idempotent writes and explicit ownership of created resources.
- Avoid hidden controller behavior in helper layers that obscure requeue, error, or condition semantics.

## Default Workflow
1. Inspect API types, CRD markers, reconciler flow, watches, predicates, and owned resources before editing.
2. Identify the contract change: API shape, reconciliation behavior, status semantics, or lifecycle behavior.
3. Make the smallest safe change to types, reconciliation logic, or generated artifacts.
4. Re-check generation, tests, and upgrade implications before considering the task done.

## Default Verification
- Run repository-standard generators for deep-copy, CRD, or manifests when needed.
- Prefer focused Go tests plus `envtest` for reconciliation behavior.
- Re-check finalizers, conditions, observed generation, and owner references for lifecycle correctness.
- Verify that controller behavior is safe across retries, duplicate events, and partial failure.

## Completion Criteria
- Reconciliation remains idempotent.
- Status and conditions reflect reality.
- API and CRD changes are intentional and migration-aware.
