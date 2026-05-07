---
name: operator-audit
description: Use for a phased, evidence-based deep audit of a Kubernetes operator codebase — reconciler correctness, CRD/webhook/status models, lifecycle and upgrade safety. The user must invoke this explicitly and supply repo path + phase. Skip for ordinary controller work or narrow bug hunts.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.3.0 -->
# Kubernetes Operator Audit Deep Dive

## Purpose
Use this skill for a phased, evidence-based audit of a Kubernetes operator codebase.

Apply this skill with:
- `go/policy.skill.md` for general Go evaluation standards
- `go/workflow.skill.md` for tool-first execution discipline

This skill overlays Go audit discipline with controller-specific concerns around APIs, reconciliation, status, finalizers, webhooks, and lifecycle safety.

## Skill Use
- Load this skill when the user explicitly wants a deep operator audit.
- Treat this skill as the governing operator-audit contract for the turn or session.
- Keep repository-specific scope, focus areas, and exclusions in the invoking prompt.
- Execute only the requested phase and stop at the phase boundary.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Every factual claim in an audit must come from a tool invocation, not inference. Read the reconciler, API type, watch, or generated artifact before writing the finding.
- Prefer structural or type-aware tooling (LSP, `gopls`) for reconciler call graphs and owner-reference chains; fall back to text search only when it is not available.
- Issue independent tool calls (API scans, controller inventories, CRD manifest reads, RBAC marker greps) in parallel.
- If evidence cannot be gathered (generated code hidden, envtest unavailable), record it under `UNREVIEWED/INACCESSIBLE` rather than guessing.

## When To Use
Use this skill for:
- deep operator audits
- reconciler correctness and lifecycle reviews
- CRD, webhook, or status model reviews
- operability and upgrade-safety assessment for controllers
- evidence-backed controller refactor or CRD evolution planning

Do not use this skill for:
- ordinary Go implementation work without operator-audit scope
- narrow bug hunts without broader audit intent
- one-change review without repository-level operator analysis

## Required Inputs
The invoking prompt must provide:
- repository path or scope
- exact phase to execute

Recommended inputs:
- focus controllers, APIs, or webhook packages
- exclusions
- how to treat generated code and generated manifests
- previous phase artifacts or `STATE_SNAPSHOT` when continuing

If scope or phase is missing, stop and ask.

## Operating Stance
- Prefer evidence over intuition.
- Describe controller behavior as implemented, not as intended.
- Stay phase-disciplined.
- Separate generic Go issues from controller-specific design issues.
- Treat API types, reconcilers, predicates, watches, webhooks, tests, generated artifacts, and manifests as first-class evidence.

## Evidence Rules
- Every factual claim must be anchored to a file path and, when applicable, a type, reconciler, controller, webhook, test, or generated artifact.
- Mark any non-provable conclusion as `INFERENCE`.
- List inaccessible or unreviewed material under `UNREVIEWED/INACCESSIBLE` with impact notes.
- Do not imply runtime certainty unless it is supported by code, tests, generated artifacts, manifests, or docs.

## Output Contract
- Output only Markdown.
- Machine-readable artifacts must be fenced `csv` or `json`.
- If a hard requirement cannot be met, output exactly:

```text
ERROR: <short reason>
BLOCKED_BY: <what is missing>
```

## Chunking And Continuation Rules
- Work only on the requested phase.
- Stop at the end of the phase boundary.
- Chunk large artifacts rather than compressing them inaccurately.
- When a phase is too large for one response, emit the current chunk, preserve artifact part names, and set `NEXT` to the exact remaining step or artifact part.
- If required information is missing, stop and identify exactly what is missing instead of guessing.
- End every response with:

```text
STATE_SNAPSHOT: (max 8 bullets)
- <bullet>

NEXT: <exact next phase name>
```

## General Audit Method
1. Establish accessible scope and obvious exclusions.
2. Build a fast inventory of APIs, versions, controllers, watches, predicates, webhooks, generated artifacts, and operational assets.
3. Read the files relevant to the current phase before making conclusions.
4. Build inventories or evidence tables before evaluative claims.
5. Preserve phase boundaries strictly.

## Phase Gate Rules
- Phase 1 may inventory and describe, but must not recommend.
- Phase 2 may account and index, but must not recommend or grade.
- Phase 3 may assess reconciliation design, ownership, and lifecycle boundaries, but must not produce detailed remediation plans.
- Phase 4 may produce prioritized findings with fixes, but must not assign overall grades.
- Phase 5 may synthesize, grade, prioritize, and plan.

## Phase Rules

### PHASE 1 - Inventory + Operator Surface
Produce:
- repository inventory grouped by API package, controller package, webhook package, generated artifacts, and operational assets
- one-line purpose for each API version, controller, webhook, watch setup, generated manifest set, and test area
- entrypoint summary: manager setup, controller registration, webhook registration, and generated output where evidenced
- totals and `UNREVIEWED/INACCESSIBLE`

### PHASE 2 - API + Controller Accounting
Produce exactly:
- `controller_index.csv`
- `api_index.csv`

Rules:
- include one row per reconciler, controller registration, webhook handler, and notable watch or predicate boundary
- include one row per CRD type, version, status surface, condition model, and conversion or defaulting boundary where applicable
- chunk outputs to 500 rows max per file part
- leave ownership or runtime fields blank when precision is not supportable and note `INFERENCE`

### PHASE 3 - Reconciliation + Lifecycle Boundaries
Using phase 1 and 2 evidence:
- describe operator architecture as implemented
- map reconciliation flow, ownership boundaries, and side-effect boundaries
- assess idempotency, retry behavior, ownership of created resources, and requeue semantics
- assess status and conditions for truthfulness, freshness, and operator usability
- assess deletion behavior, finalizers, cleanup ordering, and stuck-resource risk
- assess upgrade safety: CRD evolution, defaulting, conversion, and backwards compatibility boundaries

### PHASE 4 - Safety + Operability Findings
Review:
- leader election, concurrency, rate limiting, metrics, events, and log quality
- webhook safety, validation and defaulting placement, and generated-artifact drift risk
- controller failure isolation, retry behavior, and unsafe side effects

Output findings grouped by `P0`, `P1`, and `P2`, each with:
- file path
- type, controller, or webhook
- evidence
- concrete fix

### PHASE 5 - Synthesis
Produce:
- overall grade `A-F`
- subgrades for API design, reconciliation, status model, lifecycle safety, operability, and docs/DX
- anchored justification
- prioritized recommendations with `P0`, `P1`, and `P2`
- effort sizing `S`, `M`, `L`

## Completion Rule
An audit response is incomplete if it:
- mixes phases
- makes unsupported claims
- omits required artifacts
- grades before synthesis
- recommends fixes before the proper phase
- omits the continuation footer

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Kubernetes Operator Audit Deep Dive with the Go policy and workflow skills.
Audit /path/to/operator.
Execute PHASE 3 - Reconciliation + Lifecycle Boundaries.
Focus on idempotency, finalizers, condition truthfulness, and CRD upgrade safety.
Summarize generated code instead of expanding it.
```
