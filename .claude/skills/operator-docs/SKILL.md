---
name: operator-docs
description: Use when documenting Kubernetes operator behavior — CRDs and API contracts, reconciliation semantics, ownership, status/conditions, finalizers, webhook semantics, install/upgrade/rollback guides, operational runbooks. Read API types and generated CRDs before writing. Skip for code-only changes without doc deliverable.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.2.0 -->
# Kubernetes Operator Documentation Guidance

## Purpose
Use this skill when documenting Kubernetes operator behavior, CRD contracts, reconciliation semantics, operational procedures, or upgrade guidance.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read API types, CRD manifests, reconciler code, and status builders before documenting behavior — do not write from memory.
- Verify CRD field defaults and validation by reading the generated `config/crd/bases/*.yaml`, not by paraphrasing code comments.
- Issue independent tool calls (reading types, conditions, webhooks, sample CRs) in parallel.
- Cite the Go type and field path when documenting behavior; stale documentation drifts fastest from generated API surface.

## Scope
- CRD and API documentation
- controller behavior and ownership documentation
- install, upgrade, and rollback guides
- troubleshooting and operational runbooks
- webhook and status semantics documentation

## Rules
- Document the API as users experience it: required fields, defaults, status, conditions, and lifecycle semantics.
- Explain what the controller owns, what it watches, and what side effects it produces.
- Keep examples grounded in actual CRD fields and real reconciliation behavior.
- Treat generated CRDs and manifests as artifacts to explain, not as the only source of documentation.
