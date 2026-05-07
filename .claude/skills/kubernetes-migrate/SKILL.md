---
name: kubernetes-migrate
description: Use when planning Kubernetes manifest migrations — deprecated API version transitions, controller swaps, workload identity changes, storage class / PVC / StatefulSet transitions, label/selector changes that affect routing, security policy adoption that can block existing pods at admission. Skip for ordinary manifest edits or rollout debugging.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Migration Planning

## Purpose
Use this skill when planning Kubernetes manifest migrations: API version changes, controller swaps, workload identity changes, storage transitions, or platform policy adoption.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Inventory affected resources with `kubectl api-resources`, `kubectl explain`, and repo-wide searches before proposing a plan.
- Diff rendered manifests between source and target with `kustomize build` or `kubectl diff`; inspect the output directly.
- Issue independent tool calls (scanning overlays, checking CRDs, inspecting controller versions) in parallel.
- For storage, selector, or immutable-field changes, verify the required cutover path by reading controller behavior rather than assuming.

## Migration Concerns
- Deprecated API versions and removed fields
- Controller changes that alter rollout or ownership behavior
- Label and selector changes that can break service routing
- Storage class, PVC, or StatefulSet changes with data implications
- Security policy changes that can block existing pods at admission time

## Planning Rules
- Separate compatibility work from behavioral refactors.
- Identify which resources can roll forward safely and which need cutover planning.
- Provide ordered steps for manifests, controllers, and cluster prerequisites.
- Call out rollback blockers, especially for storage and selector changes.
