---
name: cicd-github-actions
description: Use when designing, modifying, or reviewing GitHub Actions — files under `.github/workflows/` or `action.yml`. Covers reusable workflows, action pinning by SHA, OIDC cloud auth, job permissions, concurrency, matrix strategies, caching, environments, and `pull_request_target` safety. Pair with `cicd-core` and `cicd-supply-chain`.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# GitHub Actions Mode

## Purpose
Use this skill when designing, writing, modifying, or reviewing GitHub Actions workflows. It covers workflow structure, reusable workflows, action pinning, permissions, OIDC cloud auth, concurrency, matrix strategies, caching, environments, and workflow security.

This skill is the GitHub Actions specialization layer. Pair it with `cicd-core` for platform-agnostic pipeline principles and with `cicd-supply-chain` for release integrity.

## Skill Use
- Load this skill when the task is to change files in `.github/workflows/` or `action.yml` definitions.
- Treat this skill as the governing contract for Actions-specific decisions (permissions, triggers, action pinning, OIDC).
- Keep repository-specific workflow context in the invoking prompt.
- Match existing workflow conventions when they are explicit and defensible.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read every workflow in `.github/workflows/` before proposing changes — reusable workflows and shared composite actions are often hidden from a single-file view.
- Validate workflow syntax with `actionlint` and shell snippets with `shellcheck`. Run these before committing.
- Run workflows locally with `act` when feasible to reproduce failures without burning minutes on the runner.
- Use `gh workflow run` and `gh run view --log-failed` to trigger and inspect runs directly instead of describing what the UI would show.
- For third-party actions, look up the exact commit SHA — do not trust floating tags.

## Workflow Structure

### Triggers
- Use minimal, specific triggers. Prefer `on: pull_request` + `on: push: branches: [main]` over broad event patterns.
- For fork-safe PR builds, `pull_request` runs with read-only repo access and no repository secrets — this is the default and correct behavior.
- **Avoid `pull_request_target`** unless you have a specific, reviewed need. It runs with the base repo's secrets against the fork's code, which is a known supply-chain footgun. If you must use it, check out the merge commit, not the head, and never execute untrusted fork code under it.
- `workflow_dispatch` is the right trigger for manual operations; pair it with typed `inputs:` and environment protection.
- `schedule:` runs on the default branch only. Use it for drift detection (dependency CVEs, cert expiry), not primary CI.

### Permissions
- Set `permissions:` at the workflow or job level, never rely on defaults.
- Start from `permissions: {}` (deny everything) and add the minimum each job needs. Common scopes: `contents: read`, `id-token: write` (for OIDC), `packages: write`, `pull-requests: write`, `checks: write`.
- Workflow-level permissions are inherited; job-level permissions override. Prefer narrower job-level grants.
- `GITHUB_TOKEN` expires at job end; it cannot be exfiltrated as a long-lived credential, but it can still damage the repo if misused. Treat it as a secret.

### Concurrency
- Add `concurrency:` to any workflow that should cancel prior in-progress runs when a new commit lands on the same branch:

```yaml
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
```

- Do **not** cancel in-progress runs for deploy workflows — cancelling a half-finished deploy leaves partial state. Use `cancel-in-progress: false` for deploys, or a dedicated concurrency group per environment.

### Jobs And Steps
- Name every job and step. Unnamed steps appear as the command in the UI, which makes failures hard to scan.
- Use `if:` to skip whole jobs or steps; prefer `if: github.event_name == 'pull_request'` to branching inside a script.
- Depend on jobs with `needs:`; don't chain logic through artifacts when `needs:` will do.
- Use `timeout-minutes:` on every job; Actions default is 360 minutes, which will hold up a queue.

## Pinning Actions
- **Pin third-party actions by commit SHA**, with the intended version in a trailing comment:

```yaml
- uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4.2.2
- uses: docker/build-push-action@471d1dc4e07e5cdedd4c81371e2a01d5bbb69c4a  # v6.15.0
```

- SHA pinning prevents a malicious or accidental tag move from running attacker-controlled code in your pipeline.
- First-party `actions/*` published by GitHub can be pinned by major tag (`@v4`) if your threat model accepts it — it's still safer than nothing. Be consistent per repo.
- Use Dependabot's `github-actions` ecosystem to raise PRs that update pinned SHAs:

```yaml
# .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
```

- Avoid composite actions from small third-party authors unless you have read the code. "It has 3 stars" is not a security review.

## OIDC For Cloud Auth
Prefer OIDC over long-lived cloud credentials.

### AWS
```yaml
permissions:
  id-token: write
  contents: read
steps:
  - uses: aws-actions/configure-aws-credentials@e3dd6a429d7300a6a4c196c26e071d42e0343502  # v4.0.2
    with:
      role-to-assume: arn:aws:iam::123456789012:role/gh-oidc-role
      aws-region: us-east-1
```

Pair with an IAM role whose trust policy restricts `token.actions.githubusercontent.com` to a specific repo and ref.

### GCP
Use Workload Identity Federation via `google-github-actions/auth@v2`. Scope the Workload Identity Pool to `repository:` and, ideally, `repository_ref:` conditions.

### Azure
Use `azure/login@v2` with OIDC. Configure the Entra application's federated credential to match `repo:<org>/<repo>:ref:refs/heads/<branch>` or `repo:<org>/<repo>:environment:<env>`.

### Rules For OIDC
- Federated identity conditions should include repo **and** ref or environment. A condition of "any workflow in any ref" gives workflow attackers the keys.
- Rotate federated credentials by updating the OIDC subject claim conditions, not by rotating secrets.
- Grant OIDC roles the minimum cloud permissions — least-privilege still applies.

## Reusable Workflows vs. Composite Actions
- **Reusable workflow** (`workflow_call`): full job with runner, permissions, secrets. Use when you need to encapsulate a whole job (build, publish, deploy) for many callers. Lives at `.github/workflows/*.yml`.
- **Composite action** (`action.yml` with `using: composite`): a sequence of steps that runs inside a job. Use for shared step patterns (set up Go with caching, run linters). Lives at `.github/actions/<name>/action.yml` or a dedicated repo.
- Consolidate duplicated logic into one or the other. Copy-pasted setup across five workflows is how subtle drift accumulates.

Reusable workflow outline:

```yaml
# .github/workflows/reusable-go-build.yml
on:
  workflow_call:
    inputs:
      go-version:
        required: false
        type: string
        default: stable
    secrets:
      REGISTRY_TOKEN:
        required: true
    outputs:
      image-digest:
        value: ${{ jobs.build.outputs.digest }}

jobs:
  build:
    runs-on: ubuntu-24.04
    timeout-minutes: 20
    permissions:
      contents: read
      id-token: write
    outputs:
      digest: ${{ steps.push.outputs.digest }}
    steps:
      ...
```

Caller:

```yaml
jobs:
  build:
    uses: ./.github/workflows/reusable-go-build.yml
    secrets:
      REGISTRY_TOKEN: ${{ secrets.REGISTRY_TOKEN }}
```

## Matrix Strategies
- Express matrix dimensions that matter (OS, Go version, arch). Do not matrix over arbitrary test names.
- Use `include:` to add specific combinations and `exclude:` to remove known-broken combinations.
- `fail-fast: false` when you need to see all cell failures (cross-platform validation). Default `fail-fast: true` for PR iteration.
- Limit matrix size on PRs via an `if:` that restricts to a representative subset; expand on `push: main` and `tag`.

## Caching
- Prefer the official `actions/setup-*` caches (`cache: true` on setup-go, setup-node, setup-python). They handle keying correctly.
- For custom caches, hash every input that invalidates correctness:

```yaml
- uses: actions/cache@0400d5f644dc74513175e3cd8d07132dd4860809  # v4.2.3
  with:
    path: ~/.cache/go-build
    key: go-build-${{ runner.os }}-${{ hashFiles('**/go.sum') }}-${{ hashFiles('**/*.go') }}
    restore-keys: |
      go-build-${{ runner.os }}-${{ hashFiles('**/go.sum') }}-
      go-build-${{ runner.os }}-
```

- Do not cache build output as if it were input; it creates non-deterministic builds.
- Never cache secrets, `.netrc`, cloud credentials, or tokens.

## Artifacts
- Use `actions/upload-artifact` with `if-no-files-found: error` so a silent path mistake fails the job.
- Set `retention-days` deliberately; Actions keeps artifacts far too long by default.
- Prefer publishing artifacts to a registry (OCI, package index) over Actions artifacts for anything that downstream systems depend on.
- For SBOM, signature, and provenance, use `actions/attest-build-provenance` (GitHub's native attestation) or cosign — see `cicd-supply-chain`.

## Environments
- Use `environment:` on deploy jobs to attach approvals, wait timers, and protected secrets.
- Each environment has its own secrets and variables; this is the right boundary for environment-scoped credentials.
- `environment.url` surfaces the deployed URL in the PR and deployment list.
- Protection rules (required reviewers, wait timer, branch restriction) are the platform-enforced gate. A script-level gate is not equivalent.

## Scripts And Shell Hygiene
- Use `bash -Eeuo pipefail` for non-trivial shell blocks. `set -euo pipefail` at the top also works:

```yaml
- name: Do the thing
  shell: bash
  run: |
    set -Eeuo pipefail
    ...
```

- Quote variables. `${{ github.event.pull_request.title }}` can contain arbitrary characters and is a shell-injection vector if interpolated into a script.
- Prefer environment variables over template interpolation for user-controlled values:

```yaml
env:
  PR_TITLE: ${{ github.event.pull_request.title }}
run: |
  echo "$PR_TITLE"
```

- Run `shellcheck` in CI on committed shell scripts. The `ludeeus/action-shellcheck` action is fine.

## Self-Hosted Runners
- Self-hosted runners are a supply-chain attack surface. Do not use them for public repositories unless they are ephemeral and isolated.
- Use a job queue (actions-runner-controller, Philips labs runners) that spawns a fresh container/VM per job. Never run on a persistent host with repo clone caches.
- Segment runners by trust boundary: production-release runners do not run PR builds from forks.

## Workflow Security Checklist
- No `pull_request_target` executing fork code.
- Third-party actions pinned by SHA.
- `permissions: {}` defaults or explicit minimum scopes.
- Secrets scoped by environment, not at workflow root.
- Scripts quote user-controlled interpolations; `${{ ... }}` not embedded in shell without env-var indirection.
- `GITHUB_TOKEN` scope narrowed (`contents: read` by default).
- No sensitive data in `outputs:` (outputs are stored in run metadata).
- `actionlint` and `shellcheck` on committed workflows and scripts.
- Dependabot enabled for `github-actions`.

## Anti-Patterns To Reject
- Unpinned third-party actions
- `pull_request_target` with direct fork-code checkout
- Workflow-level `env:` with secrets that every job inherits
- Shell steps that interpolate `${{ github.event.* }}` directly
- `continue-on-error: true` on required checks
- Long-lived cloud credentials when OIDC is available
- Duplicated setup logic across many workflows
- Caching without hashing all relevant inputs
- Self-hosted persistent runners for public repositories
- `${{ secrets.GITHUB_TOKEN }}` passed into third-party actions that could exfiltrate it
- `timeout-minutes` missing from long-running jobs
- `cancel-in-progress: true` on deploy workflows
- Running deployments from CI instead of a dedicated environment with approvals
- Reusable workflows that take `ref` or `script` inputs from PR-controlled values

## Completion Criteria
Do not consider an Actions workflow task complete until all applicable items are true:
- triggers are explicit and scoped
- `permissions:` are declared at the narrowest appropriate level
- third-party actions are SHA-pinned with version comment
- `concurrency` is set appropriately (cancel for CI, serialize for deploy)
- secrets are environment-scoped where possible; OIDC used for cloud auth
- shell blocks use `set -Eeuo pipefail` and quote user-controlled values
- `actionlint` and `shellcheck` pass
- timeouts and retention-days are set deliberately

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use GitHub Actions Mode together with cicd-core.
Refactor /path/to/repo/.github/workflows/ so the duplicated Go setup across ci.yml and release.yml moves into a reusable workflow.
Pin every third-party action by SHA. Enforce permissions: {} at workflow root and grant narrowly per job.
Use OIDC for the AWS ECR push. Keep pull_request runs fork-safe.
Verify with actionlint and a test PR run.
```
