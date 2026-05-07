---
name: kubernetes-docs
description: Use when documenting Kubernetes resources, deployment flows, overlay usage, environment setup, rollout/rollback runbooks, or controller migration notes. Cite manifest paths; document secret contracts, never secret content. Skip for code changes without a doc deliverable.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Documentation Guidance

## Purpose
Use this skill when documenting Kubernetes resources, deployment flows, operational runbooks, or platform conventions.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read the manifests, overlays, and patches before documenting them; do not write documentation from memory.
- Verify example commands by running `kustomize build`, `kubectl apply --dry-run=server`, or `kubeconform` where practical.
- Issue independent tool calls (reading multiple overlays, scanning for resources) in parallel.
- Cite the manifest path and resource name when describing behavior — paraphrased docs drift fastest.

## Scope
- Workload and service READMEs
- Overlay usage and environment documentation
- Deployment, rollback, and incident runbooks
- API or controller migration notes

## Rules
- Document what is actually deployed: kinds, namespaces, controllers, ingress paths, config dependencies, and operational expectations.
- Keep examples executable, such as `kubectl apply -k overlays/prod`.
- Explain rollout and rollback behavior where it matters.
- Do not document secrets content; document secret contracts and sourcing instead.
