---
name: go-workflow
description: Use as the Go execution layer — tool-first discovery, structural tooling (gopls), parallel tool calls, evidence over inference, truth hierarchy. Default execution contract for understanding/modifying/debugging/reviewing Go. Pair with `go-policy` (standards) and any Go mode skill.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Engineering Workflow

## Purpose
Use this skill when the task is to understand, modify, debug, refactor, review, or validate Go code and you need a reliable workflow for moving from request to verified result.

This skill is the execution layer for Go work. It pairs with `policy.skill.md`, which defines what good code looks like. This skill defines how to approach the work, which tools to prefer, how to verify results, and how to report uncertainty.

## Skill Use
- Load this skill when the task involves understanding, modifying, debugging, refactoring, reviewing, or validating Go code.
- Use this skill together with Go policy and any task-specific Go mode when available.
- Treat this skill as the default execution contract unless the repository has stricter local workflow requirements.
- Prefer repository-specific commands, build tags, and tooling conventions when they are explicit.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Invoke tools to gather evidence; do not narrate or infer what a tool would show.
- Prefer structural or type-aware tooling (LSP, `gopls`) when available; fall back to text search only when it is not.
- Issue independent tool calls in parallel rather than sequentially.
- If a claim can be verified by running a command, reading a file, or inspecting a symbol, verify it before asserting it.

## Operating Stance
- Use tools before explanation when the tool can resolve the question.
- Read repository evidence before proposing changes.
- Prefer structural and executable signals over textual interpretation.
- Treat reasoning as a fallback layer, not a primary source of truth.
- Do not summarize behavior that has not been traced through code, tool output, or both.

## Core Principle
- Do not trust inference when verification is possible.

## Truth Hierarchy
When evaluating correctness, use this order of authority:
1. compiler and type system
2. test results
3. static analysis results
4. runtime signals such as logs and metrics
5. repository policy and guidance
6. LLM reasoning

## Tool Precedence
When multiple approaches are possible, prefer them in this order:
1. structural tooling such as `gopls` definitions, references, implementations, symbols, and rename-safe operations
2. Go-native repository tooling such as `go list`, `go build`, `go test`, `go vet`
3. repository-standard analysis tools such as `staticcheck`, `golangci-lint`, generators, or validation scripts
4. runtime evidence such as logs, metrics, traces, and reproduced failures
5. text search and file reads for supplemental discovery
6. reasoning when stronger evidence is unavailable

## Evidence Types
Use these categories when reasoning or reporting:
- structural evidence: definitions, references, imports, signatures, package edges
- executable evidence: compiler output, test results, static analysis output
- runtime evidence: logs, traces, metrics, reproduced failures
- repository evidence: CI config, scripts, build tags, docs tied to implementation
- inference: reasoning not yet confirmed by stronger evidence

## Repository Workflow Discovery
Before running default commands, inspect the repository for its canonical workflow:
- `go.mod`
- `Makefile`, `Taskfile.yml`, `magefiles`, or scripts
- CI definitions
- lint or static-analysis configuration
- build tags and integration-test conventions

Prefer repository-standard commands over generic defaults when they are explicit and relevant.

## Task Classification
Before acting, classify the task into one primary mode:
- understand code
- modify code
- add new functionality
- debug failure
- refactor
- review
- validate or verify

If a task spans multiple modes, choose the dominant mode first and sequence the others explicitly.

## Global Workflow Rules

### Identify The Source Of Truth
- Use structural tooling for code understanding when available.
- Prefer explicit types, signatures, call paths, and tool output over inferred intent.
- Confirm assumptions against code and tool output before acting.

### Minimize Blast Radius
- Identify affected symbols, packages, entrypoints, and consumers before changing code.
- Distinguish public API changes from internal implementation changes.
- Prefer the smallest viable change that solves the task.

### Require Verification
- Do not consider code correct until it has been verified by the relevant tools.
- The minimum acceptable verification for most code changes is:
  - compile or package-level test evidence
  - relevant tests passing
  - relevant static analysis when part of repository workflow
- Increase verification depth with risk.

### State Uncertainty Explicitly
- If evidence is incomplete, say what is known, what is unknown, and what must be verified next.
- Do not present guesses as conclusions.
- If evidence is insufficient, stop and identify the missing information or validation step.

## Minimum Required Tool Use By Mode

### Understand Code
Must use:
- structural lookup for definitions, references, implementations, or symbol boundaries when available
- `go list` or equivalent for package and module boundaries

May use:
- text search for comments, config keys, and supplemental discovery

### Modify Code
Must use:
- structural lookup before editing
- relevant tests after the change
- compile or test evidence for the affected package set

### Add New Functionality
Must use:
- existing test discovery
- failing or coverage-driving tests first when practical
- compile or test verification after implementation

### Debug Failure
Must use:
- reproduction or concrete failing evidence when practical
- code-path tracing
- post-fix regression verification

### Refactor
Must use:
- structural lookup before broad edits
- tests before and after meaningful steps
- compile or test confirmation that intended behavior is preserved

### Review
Must use:
- diff inspection
- affected symbol or package tracing
- verification evidence review when available

### Validate Or Verify
Must use:
- the relevant compiler, test, and analysis commands, not code inspection alone, when executable verification is possible

## Workflow By Mode

### Understand Code
1. Identify the relevant entrypoint, boundary, or starting symbol.
2. Trace definitions, references, and implementations.
3. Follow call flow across package boundaries.
4. Identify core types and data flow before summarizing behavior.
5. Summarize runtime path from input to side effect.

### Modify Code
1. Identify affected symbols and packages.
2. Determine whether the change affects public API, private implementation, or both.
3. Identify existing tests and update or extend them when practical.
4. Make the smallest viable change.
5. Re-run verification.
6. Re-check blast radius.

### Add New Functionality
1. Identify the correct boundary for the new behavior.
2. Reuse existing package structure unless there is a real reason not to.
3. Start from types and behavior contracts.
4. Add a failing test first when practical.
5. Implement the minimum viable code.
6. Verify behavior, placement, and observability implications.

### Debug Failure
1. Reproduce the failure deterministically when possible.
2. Identify the failing entrypoint, test, request path, or runtime boundary.
3. Trace inputs, transformations, and outputs.
4. Find the earliest point where reality diverges from expectation.
5. Fix the root cause.
6. Add or update regression coverage.

### Refactor
1. Confirm current behavior is covered or add coverage first.
2. Identify the structural problem explicitly.
3. Make small, incremental changes.
4. Re-run tests after meaningful steps.
5. Stop when the structural problem is solved.

### Review
1. Identify what changed and why.
2. Check whether the change matches the stated task.
3. Check correctness first.
4. Check edge cases, failure paths, cancellation, and blast radius where relevant.
5. Check boundary placement, API impact, and verification coverage.
6. Check style and consistency last.

### Validate Or Verify
1. Identify the risks introduced by the change.
2. Choose verification depth proportional to those risks.
3. Run the relevant commands and checks.
4. Confirm success criteria explicitly.
5. Report remaining uncertainty.

## Default Verification Profile
When repository-specific commands are not provided, prefer:
- affected-package verification first:
  - `go test ./path/to/affected/package`
- broader confirmation when blast radius is non-trivial:
  - `go test ./...`
- race detection for concurrency, lifecycle, shared-state, or shutdown changes:
  - `go test -race ./...`
- static analysis when present in repository workflow:
  - `go vet ./...`
  - `staticcheck ./...`

## Typical Risk Levels

### Low Risk
Examples:
- comments
- doc updates
- rename with strong structural-tool support and no behavior change

Typical checks:
- compile or focused tests if applicable

### Moderate Risk
Examples:
- behavior change in one package
- handler change
- refactor with existing test coverage

Typical checks:
- relevant package tests or `go test ./...`
- repository-standard static analysis

### High Risk
Examples:
- concurrency changes
- interface or API changes
- shutdown behavior
- persistence or transport changes
- dependency changes

Typical checks:
- full test suite
- race detection
- static analysis
- integration tests when applicable

## Fallback Rules
- If `gopls` or equivalent structural tooling is unavailable, fall back to `go list`, `go doc`, `rg`, and careful file tracing.
- If tests cannot be run, state exactly why and identify the pending verification gap.
- If runtime reproduction is unavailable, anchor the analysis to the strongest available structural and executable evidence.

## Escalation Rules
- If structural understanding is uncertain, get compiler-visible or type-aware evidence.
- If behavior is uncertain, reproduce the behavior or run tests.
- If safety is uncertain, increase verification depth.
- If evidence conflicts, report the conflict and resolve it using the truth hierarchy.
- If required information is missing, stop and identify exactly what is missing.

## Output Contract
When responding to a Go task, include the following when relevant:
- task classification
- current understanding
- plan
- affected packages, symbols, or boundaries
- risks or blast radius
- verification steps
- remaining uncertainty

Prefer concise structure over narrative sprawl.

## Completion Criteria
Do not consider a Go task complete until all applicable items are true:
- the task was correctly classified and scoped
- affected code paths were identified
- the requested change or analysis was completed
- relevant verification was run or explicitly identified as pending
- unresolved uncertainty was disclosed
- the result is supported by evidence rather than inference alone

## Anti-Patterns To Reject
- guessing when tooling can verify
- using grep as the primary structural tool when type-aware tooling is available
- making broad edits without understanding references and boundaries
- mixing refactor, feature work, and debugging implicitly
- claiming correctness without compile or test evidence when executable verification is possible
- widening interfaces or abstractions without need
- presenting tentative analysis as established fact
- skipping regression coverage after fixing a bug
- leaving verification implicit

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Engineering Workflow and Go Engineering Policy.
Understand the queue consumer flow in /path/to/repo.
Trace entrypoint to side effects.
Identify affected packages and boundaries.
Prefer actual tool output over inference.
Report risks, missing information, and verification steps.
```
