---
name: go-dev
description: Use when implementing or modifying Go code — feature work, bug fixes, targeted refactors, or cleanup tied to behavior change. Thin overlay; pair with `go-policy` (quality bar) and `go-workflow` (verification flow). Skip for documentation-only or test-only work (use `go-docs` / `go-test`).
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Development Mode

## Purpose
Use this skill when the task is to implement or modify Go code: feature work, bug fixes, targeted cleanup, or small refactors coupled to a concrete behavior change.

This skill is intentionally thin. It is the default execution mode for writing Go code and is meant to be used with:
- `policy.skill.md` for what good Go code looks like
- `workflow.skill.md` for how to approach and verify the work

## Skill Use
- Load this skill when the primary task is to change Go code.
- Use this skill together with Go policy and Go workflow when available.
- Treat this skill as the implementation-mode overlay, not as the full Go contract by itself.
- Follow repository-specific commands, generators, build tags, and CI conventions when they are explicit.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Invoke tools to read code, find references, and run tests; do not describe what you would do.
- Issue independent tool calls in parallel rather than sequentially.
- Run the relevant `go test` or static-analysis commands yourself — do not claim a change is verified without tool output.

## Mode Focus
- Deliver the requested behavior with the smallest viable change.
- Preserve package boundaries unless the task explicitly requires changing them.
- Prefer regression tests for bug fixes and failing tests first for new behavior when practical.
- Keep unrelated cleanup out of the patch unless it is necessary for correctness or to avoid making the design worse.

## Implementation Workflow
1. Classify the task and identify the affected entrypoints, packages, and symbols.
2. Use structural tooling to find definitions, references, and boundary crossings before editing.
3. Identify existing tests that cover the behavior and extend them when practical.
4. Make the smallest viable change.
5. Re-run verification proportional to the risk.
6. Re-check blast radius before considering the task complete.

## Default Verification
- At minimum, verify the touched package or relevant package set with `go test`.
- Use `go test -race` for concurrency, shutdown, shared-state, or lifecycle changes.
- Use repository-standard static analysis when it is part of normal workflow.
- Run broader package or full-repository verification when the change affects public APIs, shared packages, persistence, transport, or dependency wiring.

## Completion Criteria
Do not consider an implementation task complete until all applicable items are true:
- the requested behavior was implemented or corrected
- affected tests were added or updated when behavior changed
- verification was run or a concrete verification gap was reported
- no unnecessary boundary widening or abstraction was introduced
- remaining uncertainty and blast radius were stated explicitly

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Development Mode with Go Engineering Policy and Go Engineering Workflow.
Implement the queue retry backoff fix in /path/to/repo.
Keep the change scoped to the worker package.
Add or update regression coverage.
Verify the affected package and any impacted integration path.
```
