---
name: go-migrate
description: Use when planning or executing Go migrations — Go version upgrade, dependency replacement, framework swap, package or service extraction, schema/API transitions, legacy modernization with compatibility constraints. Prefer reversibility over speed. Skip for greenfield code or isolated bug fixes without a migration dimension.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Migration Mode

## Purpose
Use this skill when planning or executing migrations in a Go codebase.

Apply this skill with:
- `policy.skill.md` for design and boundary standards
- `workflow.skill.md` for tool-first discovery and verification

This mode exists for migrations that materially change tooling, dependencies, architecture, schema, or API shape and therefore require explicit impact analysis, sequencing, and rollback planning.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Use tools to inventory call sites, imports, and schema references before proposing a migration plan — do not estimate blast radius from memory.
- Issue independent tool calls (listing affected files, reading config, checking CI) in parallel.
- Run `go build`, `go test`, and static analysis after each migration step and report actual output, not expected output.
- If the migration touches persistence, run schema or data verification against the target state rather than inferring correctness.

## When To Use
Use this skill for:
- Go version upgrades
- dependency replacement
- framework migrations
- package or architecture restructuring
- service extraction
- database migration planning
- API version transitions
- legacy modernization with compatibility constraints

Do not use this skill for:
- greenfield implementation
- ordinary code review
- isolated bug fixes without a migration dimension

## Operating Stance
- Prefer reversibility over speed.
- Describe the current system as implemented before proposing the target state.
- Do not migrate and opportunistically redesign at the same time.
- Every migration plan must answer: what can fail, how do we detect it, and how do we roll it back?

## Migration Planning Process
1. Inventory the current surface area: packages, imports, call sites, APIs, schemas, entrypoints, and consumers.
2. Define the target state and what must remain compatible during transition.
3. Identify blast radius, sequencing constraints, and rollback boundaries.
4. Choose incremental steps that are independently verifiable.
5. Define verification for each step before making changes.
6. Remove temporary compatibility layers once migration is complete.

## Migration Types

### Go Version Upgrades
- inspect the current `go` directive and any toolchain directive
- read release notes for each intermediate Go version
- check CI, Docker images, and pinned tooling versions
- run repository-standard tests and static analysis after the upgrade
- adopt new language features only after the version upgrade is stable

### Dependency Migrations
- map old import paths, call sites, types, and behavior contracts
- prefer drop-in replacement when behavior is truly compatible
- otherwise use adapters or temporary interfaces at the consumption boundary
- do not leave dual-dependency coexistence in place indefinitely

### Framework Migrations
- inventory handlers, middleware, route behavior, and dependency wiring
- verify middleware order and error-handling semantics
- compare route tables or API behavior before and after

### Architecture Migrations
- map current package relationships before moving code
- introduce boundaries incrementally
- verify dependency direction after each meaningful move
- keep temporary aliases or shims only as long as needed for safe transition

### Database Migrations
- separate schema migration from data migration
- plan expand/migrate/contract for zero-downtime constraints
- define rollback behavior explicitly
- never depend on production startup to run migrations automatically

### API Migrations
- choose a versioning strategy and apply it consistently
- preserve backward compatibility unless a new version is being introduced
- document deprecation windows and consumer migration signals

## Execution Rules
- Keep each migration step small and independently verifiable.
- Run verification after each meaningful step.
- Preserve backward compatibility during the transition when consumers depend on it.
- Use temporary adapters, shims, or dual-write behavior only when the migration truly requires it.
- Remove temporary migration scaffolding after the cutover.

## Verification Expectations
- Verify the affected package set on every step.
- Use broader verification for API, persistence, dependency, or architecture shifts.
- Use race detection for concurrency or lifecycle-sensitive migrations.
- Run repository-standard lint or static analysis when part of normal workflow.

## Anti-Patterns To Reject
- migrating and refactoring simultaneously without need
- changing behavior during a dependency swap unless explicitly intended
- skipping rollback planning
- one-shot big bang migrations when incremental steps are possible
- leaving compatibility layers in place indefinitely
- claiming migration safety without verification evidence

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Migration Mode with Go Engineering Policy and Go Engineering Workflow.
Plan and execute the logrus-to-slog migration in /path/to/repo.
Identify impacted packages, middleware, and field-name differences.
Keep the migration incremental and reversible.
Define verification and rollback for each step.
```
