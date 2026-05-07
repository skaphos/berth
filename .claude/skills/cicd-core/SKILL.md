---
name: cicd-core
description: Use as the platform-agnostic CI/CD layer for any pipeline (GitHub Actions, Azure DevOps, GitLab, Jenkins, Buildkite, CircleCI, Tekton). Defines stage ordering, fast-feedback discipline, secret hygiene, immutable artifacts, least-privilege credentials, observable failure. Pair with a platform-specific skill (`cicd-github-actions`, `cicd-azure-devops`) or use alone for pre-platform pipeline design.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# CI/CD Core Principles

## Purpose
Use this skill when designing, reviewing, or refactoring any CI/CD pipeline, regardless of platform (GitHub Actions, Azure DevOps, GitLab CI, Jenkins, Buildkite, CircleCI, Tekton, or custom runners).

This skill is the platform-agnostic layer. It defines what good pipelines look like: reproducibility, feedback speed, secret discipline, artifact integrity, and least-privileged credentials. Pair it with a platform-specific skill (`cicd-github-actions`, `cicd-azure-devops`) and, for deployment, with `cicd-gitops`.

## Skill Use
- Load this skill alongside a platform-specific CI/CD skill, or on its own when the task is platform-independent pipeline design.
- Treat this skill as the governing contract for pipeline shape and quality.
- Keep platform and repository specifics in the invoking prompt.
- When this skill conflicts with casual convenience, follow this skill.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read the existing pipeline files (`.github/workflows/*.yml`, `azure-pipelines.yml`, `.gitlab-ci.yml`, `Jenkinsfile`) before proposing changes — pipelines drift fast.
- Run the pipeline locally where possible (`act` for Actions, `gh workflow run`, `az pipelines run`, `gitlab-runner exec`) to verify behavior before merging.
- Issue independent tool calls (reading multiple workflows, inspecting cache config, checking secrets) in parallel.
- For any pipeline change, verify in a feature branch first; do not alter the default-branch pipeline in place without a rehearsal run.

## Core Principles
- **Fast feedback first.** Cheap, high-signal checks run before expensive ones. Fail the pipeline at the earliest step that can detect the problem.
- **Reproducibility beats cleverness.** A pipeline that succeeds on one runner and fails on the next is broken, even if it passes most of the time.
- **Least privilege by default.** Jobs get the narrowest credentials, permissions, and network access that let them succeed.
- **Immutable artifacts.** What passes CI is what deploys. No rebuilding between promotion stages.
- **Secrets stay out of logs and layers.** Assume every log is public and every artifact is tamper-inspectable.
- **Observable failure.** When a job fails, the output tells you what, where, and how to reproduce. Silent failures are worse than loud ones.

## Pipeline Shape

### Stages And Ordering
Typical pipeline order, cheapest-first:

1. **Static checks** — formatter, linter, type-checker, license/SPDX compliance, commit-message checks, secret scan. Seconds, not minutes.
2. **Unit tests** — fast, hermetic, no external services.
3. **Build** — compile, package, create container image or equivalent artifact.
4. **Integration tests** — services, databases, test doubles for externals.
5. **Security scans** — SAST, dependency vuln scan, container scan, IaC scan.
6. **Artifact publication** — registry push, SBOM + signature, provenance.
7. **Deployment (CD)** — per-environment, gated where appropriate.

Rules:
- A failure at stage N should prevent stage N+1 from running.
- Parallelize jobs within a stage; serialize across stages.
- Do not mix deploy steps into CI; deployment belongs in CD (pull-based GitOps is preferred — see `cicd-gitops`).
- For monorepos, scope stages by path changes so unaffected jobs skip.

### Job Sizing
- Each job should have a single, nameable purpose. "Test" is too broad; "unit tests", "integration tests", "e2e tests" are right.
- Jobs under 30 seconds have overhead that dominates; consider merging trivial jobs.
- Jobs over 15 minutes should be split or parallelized — long jobs slow the feedback loop and are expensive to retry.
- Timeouts on every job. A missing timeout is a queue-blocking incident waiting to happen.

### Branch And Trigger Model
- Run the full pipeline on pull requests; the PR check is the unit of review.
- On push to default branch, run the full pipeline and publish artifacts.
- On tag, run the release pipeline (which overlaps with default-branch CI but adds signing, provenance, and promotion).
- Do not run the full pipeline on every branch push — scope by branch name or PR state.
- Schedule nightly or weekly runs for things that detect drift: dependency CVEs, flaky tests, base image updates, certificate expiry.

## Reproducibility
- Pin the runner OS version (`ubuntu-24.04`, not `ubuntu-latest`). `latest` rolls under you.
- Pin language toolchain versions in repo metadata (`go.mod`'s `go` directive, `.python-version`, `.tool-versions`, `Dockerfile` `ARG` values) and have the pipeline read from that file rather than duplicate the version.
- Pin third-party actions/tasks by SHA (for actions) or exact version (for marketplace tasks). See the platform-specific skills for syntax.
- Commit a lockfile (`go.sum`, `uv.lock`, `package-lock.json`, `Cargo.lock`, `poetry.lock`). The pipeline's install step must fail if the lockfile would change, not silently update it.
- If the build depends on network state (package mirrors, upstream registries), consider a repository cache or vendor directory for durability.

## Caching
- Cache only what is genuinely expensive to recompute and safe to share across runs.
- Cache key must include every input that invalidates the cache: OS version, toolchain version, lockfile hash, build flags.
- Separate restore-only keys from save keys when builds frequently invalidate but reads are still useful.
- Never cache secrets, credentials, or built artifacts that should be versioned independently.
- Measure cache effectiveness. A cache with a <50% hit rate may be costing more than it saves.

## Secrets And Credentials
- Secrets come from the platform's secret store (Actions secrets, ADO variable groups + Key Vault, Vault, cloud secret manager). Never from env vars in the repo, never from committed files.
- Prefer **OIDC federation** (GitHub OIDC → AWS/GCP/Azure, ADO workload identity federation) over long-lived credentials. See platform skills.
- Scope secrets to the minimum set of jobs that need them. A global repository secret available to every job is a spill waiting to happen.
- Mask secrets in logs (platform default) and verify the mask works for multi-line or structured values.
- Rotate on a schedule appropriate to the credential's blast radius. Static service-account keys are rarely acceptable; if used, set explicit expiry.
- For PR runs from forks, do **not** expose production secrets. Use a separate, narrower credential set for fork builds (or skip jobs that need privileged creds).

## Artifacts
- An artifact produced by CI must be immutable: same hash, same contents, every time it's referenced downstream.
- Publish artifacts with a clear identity: commit SHA, build number, semantic version. Avoid mutable labels (`latest`) in production pipelines unless paired with an immutable tag.
- Promote existing artifacts between environments; do not rebuild per environment.
- Attach SBOM, signature, and provenance to every published artifact. See `cicd-supply-chain`.

## Retries And Flakes
- Do not blanket-retry a flaky job. Retries hide the flake and eventually cause a real failure to be ignored.
- Retry only transient, well-characterized failures (network blips on artifact download, registry 5xx). Scope the retry tightly.
- Flaky tests are bugs. Quarantine them (skip with issue link) and fix them, don't re-run until green.
- Record failure classes to detect drift (e.g., DNS failures spike when a provider has an outage).

## Matrix And Fan-Out
- Matrix strategies are for genuine cross-platform or cross-version coverage. Do not matrix over trivial variations.
- Fail-fast or fail-slow is a deliberate choice: fail-fast for fast iteration, fail-slow for cross-platform validation where knowing the failure pattern matters.
- Trim the matrix for PR runs and expand it for main/release. A 9-cell matrix on every PR burns CI time.

## Deployment (CD)
- Prefer **pull-based GitOps** over push-based deploys from CI. It narrows the blast radius of a compromised CI runner.
- When push-based CD is unavoidable, the CI job that deploys must have a credential scoped to exactly one environment.
- Environment gates (required approvals, protected branches, deployment windows) belong at the platform level, not in a custom script.
- Every deploy records: who, when, what (artifact hash), to which environment, and the pipeline run URL. Make the record durable.
- Blue/green, canary, or progressive delivery belongs in the deployment system (Argo Rollouts, Flagger, service mesh) — not the CI pipeline.

## Observability
- Publish a summary at the end of each run: job results, notable durations, artifact locations. GitHub's `GITHUB_STEP_SUMMARY`, ADO's `##vso[task.uploadsummary]`, and equivalents exist for a reason.
- Annotate failed lines with the platform's annotation mechanism so failures surface in the PR review, not only in logs.
- Export metrics to track pipeline health: duration, success rate, queue time, cache hit rate. A pipeline whose duration doubles silently is itself an incident.
- Structured logs when possible; for long-running jobs, periodic progress heartbeats prevent "is it stuck?" investigations.

## Common Anti-Patterns
- `*-latest` runner images in production CI
- Unpinned third-party actions/tasks (pinned by floating tag instead of SHA or exact version)
- `set -e` missing from bash scripts that chain multiple commands
- Piping to shell (`curl ... | bash`) without checksum or signature verification
- Secrets in `env:` blocks at the workflow level where every job inherits them
- Jobs that rebuild an artifact at every promotion stage
- Long workflows with no timeout
- Retries wrapped around flaky steps instead of fixing the flake
- Manual deployment steps triggered from the CI UI
- CI runners with broad, long-lived cloud credentials
- Duplicate logic across workflows that should be a reusable workflow or template
- `continue-on-error` on failing checks, rendering the check advisory
- Matrix strategies that test near-identical combinations
- Dynamic runner selection based on user input from PRs
- Builds that depend on network state (upstream mirrors, public package indexes) without caching or vendoring
- CI/CD as the source of truth for deployed state (instead of Git)

## Completion Criteria
Do not consider a pipeline task complete until all applicable items are true:
- stage ordering is cheapest-first and short-circuits correctly
- every job has an explicit timeout and named purpose
- runner, toolchain, and third-party action/task versions are pinned
- secrets are scoped to the jobs that need them; OIDC used where available
- caching keys include all inputs that invalidate correctness
- artifacts are immutable and promoted rather than rebuilt
- PR runs from forks cannot access production credentials
- failure paths are observable (annotations, summary, metrics)

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use CI/CD Core Principles together with cicd-github-actions.
Design a pipeline for /path/to/repo (Go service, builds a distroless container, deploys via Argo CD).
Stages: static checks, unit, build, integration, security scans, publish.
Pin runner and toolchain via go.mod. Use OIDC for the registry. Publish SBOM and cosign signature.
Deployment is out of scope for this pipeline — the image is consumed by a separate GitOps repo.
```
