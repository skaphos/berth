---
name: go-docs
description: Use when generating, reviewing, or maintaining Go documentation — `doc.go` and package docs, exported symbol comments, READMEs, ADRs, API docs, runbooks, changelogs, onboarding. Read source first; document actual behavior, not intent. Skip for logic review, security audit, architecture grading, or test design.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 2.1.0 -->
# Go Documentation Mode

## Purpose
Use this skill when generating, reviewing, or maintaining documentation for a Go codebase.

Apply this skill with:
- `policy.skill.md` for standards that affect public APIs and maintainability
- `workflow.skill.md` for code-grounded, tool-first execution

This mode is for documentation work that materially changes developer understanding: package docs, symbol docs, README files, ADRs, API docs, runbooks, changelogs, and onboarding materials.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read the source before documenting it; do not write documentation from memory or assumption.
- Verify examples by running them (or `go build`/`go vet` where applicable) when the repository treats examples as testable.
- Issue independent tool calls (reading multiple files, checking multiple symbols) in parallel.
- When documenting behavior, cite the file and symbol you read — do not paraphrase without grounding.

## When To Use
Use this skill for:
- `doc.go` and package documentation
- exported symbol comments
- README updates
- ADRs
- API docs
- runbooks
- changelogs
- onboarding documentation
- documentation gap analysis

Do not use this skill for:
- logic review
- security audit
- broad architecture grading
- test design work

## Operating Stance
- Read the code before documenting it.
- Document actual behavior, not intent or aspiration.
- Prefer concise, scannable docs over commentary-heavy prose.
- Write for operators and engineers who may be reading under time pressure.

## Documentation Rules

### Package Documentation
- Non-trivial packages should have a `doc.go` file with a package comment.
- Start package comments with `Package <name>`.
- Describe what the package does, not its internal implementation details.
- Include a runnable example when the package has a primary usage path and the repository supports examples.

### Exported Symbols
- Every exported function, type, method, constant, or variable should have a doc comment when the package exposes it as API.
- Start the first sentence with the symbol name.
- Document invariants, zero-value behavior, side effects, concurrency safety, and notable error conditions when relevant.
- Do not add noise comments that restate the obvious.

### README Files
A Go project README should usually include:
1. project name and one-line description
2. overview
3. quickstart
4. build and run
5. configuration
6. testing
7. API or package reference link
8. architecture overview
9. contributing
10. license

Rules:
- do not duplicate godoc in the README
- keep quickstart short and executable
- update the README when build, config, or project structure changes

### ADRs
Use:
- `Status`
- `Context`
- `Decision`
- `Consequences`
- `Alternatives Considered`

Rules:
- keep ADRs concise
- store them in the repository's decisions directory
- supersede accepted ADRs with new ADRs rather than rewriting history

### API Docs
- Keep API docs in sync with handlers, request/response types, and auth requirements.
- Document method, path, request, response, status codes, authentication, and authorization.
- Prefer generated or code-near canonical sources when the repository uses them.

### Runbooks
Each runbook should cover:
1. purpose
2. prerequisites
3. symptoms
4. diagnosis
5. resolution
6. escalation
7. post-incident checks

Rules:
- use numbered operational steps
- include actual commands or queries
- include expected outputs when practical

### Changelogs
- Follow the repository's changelog format; `Keep a Changelog` is a strong default.
- Group entries under user-visible categories such as `Added`, `Changed`, `Fixed`, and `Security`.
- Do not treat internal cleanup as a public changelog item unless it changes user-visible behavior.

## Evidence Rules
- Verify docs against source, config, tests, and handlers.
- Verify defaults by reading config loading code, not existing docs.
- Verify API behavior by reading code or generated specs, not stale prose.
- Label planned work explicitly as planned or proposed.

## Quality Checklist
Before considering documentation work complete, verify:
- symbol and package names are current
- examples compile when the repository treats them as testable examples
- configuration docs match real defaults
- API docs match actual handlers and types
- docs do not describe removed files, packages, flags, or behavior

## Anti-Patterns To Reject
- stale documentation
- aspirational documentation presented as implemented behavior
- comment duplication
- undocumented public APIs
- orphaned docs with no navigation path
- screenshots of text where searchable text should exist

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Go Documentation Mode with Go Engineering Policy and Go Engineering Workflow.
Update package documentation and README content for /path/to/repo/internal/auth.
Ground every statement in code or config.
Verify examples and defaults before finalizing the docs.
```
