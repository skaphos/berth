---
name: go-policy
description: Use as the Go standards layer (DRY/KISS/YAGNI, project structure with thin `cmd/*/main.go`, design priorities, review bar) for any Go implementation, review, refactor, migration, documentation, or audit work. Pair with `go-workflow` for execution discipline. Repo-specific stricter rules win when explicit and defensible.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Engineering Policy

## Purpose
Use this skill when Go work needs a clear definition of what good, correct, maintainable engineering looks like.

This skill is the standards layer for Go tasks. It defines coding rules, architectural expectations, review priorities, and quality bars. It does not define the execution workflow; pair it with `workflow.skill.md` for that.

## Skill Use
- Load this skill for Go implementation, review, refactor, migration, documentation, and testing work when standards matter.
- Treat this skill as the default Go quality contract unless the repository has stricter local rules.
- Repository-specific conventions may override this skill when they are explicit, coherent, and defensible.
- When this skill conflicts with casual convenience, follow this skill.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Invoke tools to check standards against real code; do not apply rules from memory to code you have not read.
- Issue independent tool calls in parallel rather than sequentially.
- Verify claims about API shape, error handling, or boundaries against the current source before asserting them.

## Core Principles

### DRY
- Extract repeated logic into named functions or methods when repetition is real and local.
- Prefer small, clear duplication over brittle abstraction.
- Use shared constants for meaningful repeated values and strings.

### KISS
- Choose the simplest solution that solves the actual problem.
- Prefer straightforward control flow over indirection.
- Split functions that mix unrelated responsibilities.

### YAGNI
- Do not add interfaces, extension points, or config knobs without a real consumer.
- Do not add speculative utility packages.
- Do not introduce plugin or provider patterns for a single implementation.

## Design Priorities
- Make runtime behavior easy to trace from entrypoint to side effect.
- Keep package boundaries explicit and directional.
- Separate transport, domain, persistence, and infrastructure concerns when the repository size justifies it.
- Prefer designs that are easy to test, observe, and operate.
- Choose operational clarity over abstraction density.

## Project Structure
- Keep `cmd/*/main.go` thin: parse config, wire dependencies, call into real application code, handle shutdown.
- Put non-public business logic in `internal/` by default.
- Use `pkg/` only for real external consumers.
- Group by domain concern once the codebase grows beyond a few packages.

## Package And Boundary Rules
- Keep packages cohesive with one primary reason to change.
- Prevent inward layers from importing outward layers.
- Do not import transport packages into service or domain packages.
- Make boundary crossings explicit in names and types.
- Avoid reusing persistence structs as transport DTOs unless the tradeoff is deliberate.

## Interfaces
- Define interfaces where they are consumed, not where they are implemented.
- Keep interfaces small.
- Accept interfaces and return concrete types when practical.
- Do not create interfaces only to mock concrete types.

## Types And Data Handling
- Start with types and invariants before implementation detail.
- Validate untrusted input at the boundary closest to ingress.
- Normalize once, then operate on validated forms.
- Be explicit about zero values, optional values, defaults, and partial updates.
- Prefer concrete types over `interface{}` or `any` in public APIs unless flexibility is genuinely required.

## Error Handling
- Return errors; do not hide them.
- Wrap errors with useful operation context when crossing meaningful boundaries.
- Handle errors at the boundary and avoid duplicate logging.
- Use sentinel errors or typed errors when callers need branching behavior.
- Do not string-match error text.

## Context
- Put `ctx context.Context` first when context is needed.
- Never store context in a struct.
- Propagate context through handlers, services, repositories, and external calls.
- Use context values only for cross-cutting concerns such as request or trace identifiers.

## Concurrency And Lifecycle
- Every goroutine must have an owner, shutdown path, and error handling strategy.
- Prefer explicit worker and queue boundaries over ad hoc concurrency.
- Select on `ctx.Done()` in long-running goroutines.
- Protect shared mutable state deliberately.
- Make timeout, retry, idempotency, and drain behavior explicit.

## Configuration
- Load configuration at startup into typed structs.
- Validate configuration before starting the application.
- Keep precedence explicit: flags, env vars, config file, defaults.
- Do not scatter `os.Getenv` reads across the codebase.

## Logging And Observability
- Use structured logging in production code.
- Log at meaningful operation boundaries.
- Include correlation identifiers when available.
- Never log secrets, tokens, or sensitive payloads by default.
- Write code so metrics and tracing can be added at meaningful boundaries.

## Security
- Treat all external input as untrusted.
- Validate inputs before database, file, template, shell, or outbound-network use.
- Keep secret access narrow and never expose secrets in logs or errors.
- Make authorization checks explicit.
- Be cautious with file paths, redirects, remote fetches, and proxy-like behavior.

## Handlers And Entrypoints
- Keep handlers in a consistent flow: decode, validate, call service, encode response.
- Inject dependencies through structs or constructors.
- Do not leak raw domain errors directly to clients.
- Use middleware for cross-cutting concerns.

## Graceful Shutdown
- Handle `SIGINT` and `SIGTERM`.
- Drain in-flight work before exit.
- Use explicit shutdown timeouts.
- Make background work participate in cancellation.
- Log the shutdown path clearly enough for operators to understand it.

## Refactoring Rules
- Refactor toward simpler control flow and clearer boundaries.
- Keep refactors incremental unless a larger redesign is explicitly requested.
- Make the smallest structural change that solves the real problem.
- Leave touched code more testable and observable than you found it.

## Review Standard
Evaluate Go changes in this order:
1. correctness
2. error handling
3. concurrency safety
4. API and boundary design
5. testability
6. simplicity
7. performance where it is obvious and material

## Shared Quality Checklist
Before considering Go work complete, verify all applicable items:
- behavior changes have relevant tests or an explicit gap explanation
- concurrency-sensitive changes are race-safe
- errors are handled at the right level
- no dead code or stray TODOs without tracking context were introduced
- naming follows Go conventions
- exported API surface has the needed documentation
- repository-standard vet or static analysis issues were not ignored silently

## Anti-Patterns To Reject
- god structs
- oversized interfaces
- public APIs built around `interface{}` or `any` without strong reason
- global mutable state
- `panic` for expected errors
- hidden goroutines with no lifecycle management
- low-level logging that forces duplicate logs upstream
- packages named `util`, `common`, or `helpers` without a narrow purpose
- clever abstractions that obscure control flow

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Engineering Policy with Go Engineering Workflow.
Apply Go standards while reviewing and updating /path/to/repo.
Keep boundaries explicit, avoid speculative abstractions, and prioritize correctness over convenience.
```
