---
name: go-test
description: Use when designing, writing, reviewing, or refactoring Go tests — stdlib testing or Ginkgo, table-driven tests, race detector, fuzz, benchmarks, golden files, integration/e2e, regression tests, coverage analysis. Run the tests you write before claiming done. Pair with `go-policy` and `go-workflow`. Skip for production-code review or documentation.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Test Mode

## Purpose
Use this skill when designing, writing, reviewing, or refactoring tests for a Go codebase.

Apply this skill with:
- `policy.skill.md` for standards that define maintainable Go code
- `workflow.skill.md` for tool-first execution and verification discipline

This mode exists because test work has its own decision rules, fixtures, verification patterns, and failure modes.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Run the tests you write. A new or updated test is not done until `go test` has executed it and the result is known.
- Use structural tools to locate the code under test and its callers; do not guess at signatures or coverage.
- Issue independent tool calls (test discovery, coverage reads, symbol lookups) in parallel.
- Report flaky or timing-sensitive failures with the command and output that produced them, not paraphrased.

## When To Use
Use this skill for:
- writing new tests
- designing a test strategy
- analyzing coverage gaps
- refactoring flaky or brittle tests
- writing regression tests for bugs
- adding integration, e2e, fuzz, benchmark, or golden-file coverage
- reviewing test quality

Do not use this skill for:
- broad production-code review outside test concerns
- documentation work
- security audit work

## Test Philosophy
- Test behavior, not implementation details.
- Test at the right level for the risk.
- Treat tests as production code.
- Prefer deterministic evidence over incidental timing or environment luck.
- A failing test is useful only when it fails for the right reason.

## Choosing Test Type

### Unit Tests
Use for:
- branching logic
- validation
- parsing and transformation
- state-machine behavior
- error-path coverage

### Table-Driven Tests
Use for:
- many input-output combinations
- validation matrices
- parser and formatter coverage
- boundary case expansion

### Integration Tests
Use for:
- database behavior
- HTTP or gRPC integration
- queue or file-system interaction
- multi-package wiring

### End-To-End Tests
Use sparingly for:
- critical request flows
- smoke coverage
- deploy-time or contract confirmation

### Fuzz Tests
Use for:
- parsers
- validators
- serializers
- untrusted input processing

### Benchmark Tests
Use for:
- performance-sensitive code
- allocation-sensitive paths
- comparing concrete alternatives

### Golden File Tests
Use for:
- complex rendered output
- code generation output
- CLI formatting
- report generation

### BDD With Ginkgo/Gomega
Use when:
- the repository already uses it
- the behavior benefits from nested shared context

Prefer standard `testing` when:
- table-driven or direct assertions are clearer
- introducing BDD would add ceremony without payoff

## Test Design Rules
- Prefer public-behavior coverage over private implementation coupling.
- Use `t.Run` for named cases and selective execution.
- Use `t.Helper()` in helpers.
- Use `t.Cleanup` for teardown tied to test lifecycle.
- Use `t.Parallel()` only when shared state and external resources make it safe.
- Prefer fakes and stubs over heavy mock frameworks unless call sequencing is the real behavior under test.
- Do not mock types you do not own; wrap them behind your own interface first when necessary.

## Concurrency And Lifecycle Testing
- Run concurrent or shared-state tests with the race detector.
- Use explicit synchronization, not `time.Sleep`, for correctness.
- Test cancellation, timeout, shutdown, and drain behavior when code depends on them.
- Verify goroutine ownership and completion in lifecycle-sensitive code.

## Coverage And Verification
- Coverage quality matters more than percentage.
- Prioritize branches, failure paths, boundary cases, and cancellation behavior.
- Use integration coverage where the risk lies in wiring or external interaction.
- Treat a missing regression test after a bug fix as a gap unless there is a concrete reason it cannot be added.

## Typical Commands
When the repository does not define a stricter workflow, prefer:
- `go test ./...`
- `go test -race ./...` for concurrency or lifecycle-sensitive changes
- `go test -coverprofile=coverage.out ./...` when coverage evidence is needed
- `ginkgo run -r --race --cover` when Ginkgo is the primary repository workflow

## Anti-Patterns To Reject
- flaky time-based synchronization
- tests coupled to implementation call order without real need
- broad mocks replacing simple fakes
- no regression coverage after bug fixes
- golden files updated without reviewing the diff
- treating high line coverage as proof of meaningful test quality

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Test Mode with Go Engineering Policy and Go Engineering Workflow.
Add regression coverage for the queue retry logic in /path/to/repo/internal/worker.
Prefer standard testing and table-driven cases.
Cover cancellation, invalid payloads, and retry exhaustion.
Verify with the relevant package tests and race detection if shared state is involved.
```
