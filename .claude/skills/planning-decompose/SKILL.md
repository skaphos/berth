---
name: planning-decompose
description: Use to break an ambiguous request, vague goal, or large change into a concrete plan of verifiable steps with explicit assumptions, sequencing, and rollback boundaries. Stack-agnostic; hand off each step to an execution-mode skill. Skip for single-file edits, well-scoped tickets, or speculative exploration.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# Task Decomposition Mode

## Purpose
Use this skill to turn an ambiguous request, vague goal, or large change into a concrete plan of verifiable steps.

This skill is tool- and stack-agnostic. It defines how to decompose work, not how to execute it. Pair it with the relevant language or platform skill when you move from planning to implementation.

## Skill Use
- Load this skill when the request is too large, too vague, or too risky to execute in one shot.
- Treat this skill as the governing contract for planning shape and quality.
- Keep project-specific context (constraints, deadlines, stakeholders, existing architecture) in the invoking prompt.
- Hand off each planned step to the appropriate execution-mode skill; this skill does not implement.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read the relevant code, config, and prior art before decomposing. Plans written from memory drift from reality fast.
- Issue independent tool calls (reading multiple files, scanning for callers, checking CI) in parallel.
- When estimating blast radius, grep for consumers rather than guessing.
- If the plan depends on facts you have not verified, mark them as assumptions in the output, not as certainties.

## When To Use
Use this skill for:
- ambiguous requests that need clarification before work can start
- large changes that span multiple files, packages, or services
- risky changes that need explicit sequencing and rollback
- multi-session work that will be handed off or resumed
- any task where the first useful output is a plan, not a patch

Do not use this skill for:
- single-file edits or isolated bug fixes
- work that is already well-scoped by a ticket or design doc
- speculative exploration — describe the exploration as the task, not as a plan to execute

## Core Principle
Decomposition is not a substitute for understanding. Do the reading before the planning.

## When To Stop And Ask
Stop and ask the user rather than guess when:
- the goal is stated in terms of outcomes but not acceptance criteria
- there are multiple plausible interpretations of "done"
- the plan would require assumptions about deploy, rollout, or coordination that you cannot verify
- the decomposition reveals a hidden decision that really needs an ADR before work starts

A short clarifying question now saves a wrong plan later.

## Decomposition Method
Work through these steps in order. Skip a step only when it is genuinely not applicable, and say so explicitly.

### 1. Clarify The Goal
- Restate the goal in one sentence.
- Identify the observable success criterion: what would let someone outside the conversation confirm it's done?
- Distinguish goal from method: the request may name a method, but the goal is usually downstream.

### 2. Inventory Constraints
- Hard constraints: deadlines, compatibility, interfaces that cannot change, regulatory or security requirements.
- Soft constraints: preferences, style, local conventions, performance targets.
- Unknowns: things you would need to verify before acting. List them explicitly.

### 3. Map Current State
- Identify the code, config, data, or process the change touches.
- Use tools to confirm the current state rather than infer it.
- Note anything that is actively in flight (open PRs, running migrations, ongoing incidents) that changes sequencing.

### 4. Define Target State
- Describe the end state in concrete terms.
- If there are several plausible target states, list the top two or three and pick one, noting what would have to be true for the others to be right.

### 5. Identify Milestones
- Break the transition from current to target into 2–6 milestones.
- Each milestone should be independently verifiable and, ideally, independently deployable or revertible.
- Prefer milestones that reduce risk monotonically: each one should leave the system no worse off than the last.

### 6. Break Milestones Into Steps
- For each milestone, list the concrete steps. A step should be small enough to execute in one sitting and verifiable on completion.
- Attach a verification check to every step: the command, test, or observation that confirms it worked.
- Note dependencies between steps explicitly.

### 7. Identify Risks And Rollback
- For each milestone, name the top risks: what can fail, how you would detect it, and how you would roll back.
- Distinguish reversible risks (bad UX, extra latency) from irreversible ones (data loss, auth bypass).
- If any step is irreversible, flag it and design a guard (feature flag, canary, dry run, manual approval gate).

### 8. Define Done
- Restate the acceptance criteria.
- Add verification that the side effects are clean: no dead code, no orphaned config, no feature flags left on.

## Output Contract
Produce a plan with these sections, in this order:

```markdown
## Goal
<one sentence>

## Acceptance Criteria
- <observable 1>
- <observable 2>

## Assumptions and Unknowns
- **Assumption:** <stated, to be validated if wrong>
- **Unknown:** <to verify before step N>

## Current State
<concrete, grounded in files and evidence>

## Target State
<concrete, compared to current>

## Plan
### Milestone 1 — <name>
Goal: <what this milestone achieves>
Steps:
1. <step> — Verify: <check>
2. <step> — Verify: <check>
Risks:
- <risk> — Detection: <how> — Rollback: <how>

### Milestone 2 — <name>
…

## Irreversible Steps
<list them explicitly, or write "None">

## Rollback Plan
<ordered, concrete actions to return to current state>

## Done When
- <final verification>
```

Keep it scannable. Plans that run beyond two screens usually mean the task should be decomposed further — push detail into the relevant execution-mode skill rather than bloating the plan.

## Sizing Guidance
Attach an effort size to each milestone:
- **S** — under a day for one person, low risk
- **M** — one to three days, moderate coordination or risk
- **L** — more than three days or multiple contributors

If every milestone is L, the decomposition isn't done yet.

## Adaptive Rules
- When new evidence contradicts the plan, revise the plan explicitly rather than working around it.
- When a step reveals a hidden decision (framework choice, schema, boundary), stop and either produce an ADR or escalate, rather than absorbing the decision silently.
- When scope grows mid-execution, surface the growth and adjust acceptance criteria before proceeding.

## Anti-Patterns To Reject
- plans that mix planning and implementation in the same pass
- plans with no verification steps
- plans that treat every step as low-risk
- plans with no rollback for irreversible actions
- plans built on unstated assumptions presented as fact
- plans that break work into steps too small to be meaningful ("create variable X", "import module Y")
- plans that are really ADRs in disguise — surface the decision separately
- plans produced without reading the code

## Quality Checklist
Before handing off a plan, verify:
- the goal and acceptance criteria are observable, not aspirational
- assumptions and unknowns are called out explicitly
- current state is grounded in tool-verified evidence
- every step has a verification check
- every milestone has stated risks and rollback
- irreversible steps are flagged
- sizes are realistic and not all "L"

## Invocation Template
Use this skill with a prompt that supplies project-specific context. Example:

```text
Use Task Decomposition Mode.
Plan the migration of the billing service from single-region to multi-region in /path/to/repo.
Constraints: no downtime, dual-write capability for at least 14 days, existing Postgres primary must remain in us-east-1 until cutover.
Deliver a milestone plan with per-step verification, risk, and rollback. Flag irreversible steps.
```
