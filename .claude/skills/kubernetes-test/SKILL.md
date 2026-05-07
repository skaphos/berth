---
name: kubernetes-test
description: Use when designing validation or test strategy for Kubernetes manifests — `kubeconform`, `kubectl apply --dry-run=server`, policy tools (`conftest`, `kyverno test`, `gator`), render diffs, deprecated-API checks. Covers Deployments, StatefulSets, Services, Ingress, HPAs, PDBs, RBAC, storage. Skip for ordinary manifest edits without a test deliverable.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Test Strategy And Generation

## Purpose
Use this skill when designing validation and test strategy for Kubernetes manifests and platform repositories.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Run `kustomize build`, `kubeconform`, `kubectl apply --dry-run=server`, and policy tools (`conftest`, `kyverno test`, `gator`) directly. A new validation is not done until the commands have executed.
- Issue independent tool calls (rendering multiple overlays, scanning for deprecated APIs, checking policy bundles) in parallel.
- Report failures with the exact command and output that produced them.
- When testing policy changes, render every affected overlay — do not assume a policy rule covers edge cases without verifying.

## Test Layers
- Static schema validation with `kubectl`, `kubeconform`, or `kubeval`
- Render validation for Kustomize or generated manifests
- Policy checks for RBAC, security context, image policy, and namespace rules
- Diff-based deployment review before apply
- Live-cluster smoke tests only when static validation is insufficient

## Guidance
- Cover default overlays and the exact environment combinations users deploy.
- Prioritize rollout-critical objects: Deployments, StatefulSets, Services, Ingress, HPAs, PDBs, RBAC, and storage resources.
- Test for deprecated API versions and immutable field changes.
- Prefer deterministic manifest validation before integration tests against a cluster.
