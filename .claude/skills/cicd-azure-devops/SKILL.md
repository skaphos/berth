---
name: cicd-azure-devops
description: Use when designing, modifying, or reviewing Azure DevOps YAML pipelines — `azure-pipelines.yml`, templates under `pipelines/` or `.azure/`, `extends` templates, variable groups, Key Vault integration, service connections, environments and approvals, workload identity. Skip for Classic UI pipelines except to migrate them to YAML.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# Azure DevOps Pipelines Mode

## Purpose
Use this skill when designing, writing, modifying, or reviewing Azure DevOps (ADO) YAML pipelines. It covers pipeline structure, templates, variable groups, Key Vault integration, service connections, environments and approvals, `extends` templates, resources (repos, containers, pipelines), and agent pool choice.

This skill is the ADO specialization layer. Pair it with `cicd-core` for platform-agnostic principles and with `cicd-supply-chain` for release integrity.

## Skill Use
- Load this skill when the task is to change `azure-pipelines.yml`, pipeline templates, or ADO-specific pipeline configuration.
- Treat this skill as the governing contract for ADO-specific decisions (templates, service connections, environments, workload identity).
- Keep organization-specific conventions (naming, variable groups, approval policies) in the invoking prompt.
- Classic UI pipelines are out of scope except to migrate them to YAML.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read `azure-pipelines.yml` and every file under `pipelines/` or `.azure/` before proposing changes. ADO pipelines commonly use `extends` and `template:` across multiple files.
- Validate YAML with `az pipelines validate` or trigger a dry run on a feature branch — the expression engine has sharp edges that linters don't catch.
- Use `az pipelines run` and `az pipelines runs show` to trigger and inspect runs from the shell.
- For `extends` templates from another repo, check that repo out (via `resources.repositories`) and read the template locally before assuming behavior.

## YAML-First Rule
- Author every pipeline as YAML. Do not maintain Classic (UI) pipelines.
- If migrating from Classic, export to YAML, then rewrite using templates and `extends`. Raw exports are rarely clean.

## Pipeline Structure

### Top-Level Shape
```yaml
# azure-pipelines.yml
trigger:
  branches:
    include: [main]

pr:
  branches:
    include: [main]
  drafts: false

variables:
  - group: shared-build-vars           # Library variable group
  - name: buildConfiguration
    value: Release

stages:
  - stage: Build
    jobs:
      - template: pipelines/jobs/build.yml
  - stage: Test
    dependsOn: Build
    jobs:
      - template: pipelines/jobs/test.yml
  - stage: Publish
    dependsOn: Test
    condition: and(succeeded(), eq(variables['Build.SourceBranch'], 'refs/heads/main'))
    jobs:
      - template: pipelines/jobs/publish.yml
```

Rules:
- Use explicit `stages:` once the pipeline has more than one logical phase.
- `dependsOn` is explicit. Implicit ordering rots fast.
- `condition:` runs stages only when they should. The default runs every stage.
- Keep root `azure-pipelines.yml` thin — it orchestrates templates, not raw steps.

### Parameters vs. Variables
- **Parameters** (`parameters:`) are compile-time. They let you branch the pipeline structure itself (what jobs run, which templates get included). Use them for things that must be known before the pipeline graph is built.
- **Variables** (`variables:`) are runtime. They are string substitutions available to scripts. Use them for values that steps consume.
- Typed parameters (`type: boolean`, `type: object`, `type: string`, `values:`) catch errors at queue time.

```yaml
parameters:
  - name: environment
    type: string
    default: dev
    values: [dev, staging, prod]
  - name: runIntegrationTests
    type: boolean
    default: true
```

## Templates
Templates are the main factoring unit. Four kinds:

1. **Step templates** — shared step sequences. `steps: [ - template: steps/go-setup.yml ]`.
2. **Job templates** — whole jobs with runner, variables, steps. `jobs: [ - template: jobs/build.yml ]`.
3. **Stage templates** — multiple jobs sharing a gate. `stages: [ - template: stages/deploy.yml ]`.
4. **Extends templates** — the whole pipeline extends from a parent and only provides parameters. This is how organizations enforce pipeline standards.

### Extends For Organization Standards
`extends` is how you centralize compliance requirements (signed commits, required scans, approved agent pools) across teams:

```yaml
# azure-pipelines.yml in the consuming repo
resources:
  repositories:
    - repository: platform-templates
      type: git
      name: platform/pipeline-templates
      ref: refs/tags/v3.1.0

extends:
  template: pipelines/standard-build.yml@platform-templates
  parameters:
    language: go
    deployEnvironments: [dev, staging, prod]
```

- Pin `ref:` to a tag, not `refs/heads/main`. A template ref of `main` means any change to the platform repo runs unreviewed in your pipeline.
- Extends templates can restrict what callers do (`parameters:` with `values:` lists, forbidding `steps:` at the root). Use this to enforce organizational policy.

### Template Inputs
- Always type template parameters. Untyped `string` inputs are string-injection vectors.
- For object inputs that drive loops, validate with `values:` where the set is small and fixed.
- Templates called from a repo-relative path (no `@`) share the consuming repo's checkout. Templates from a referenced repo use that repo's contents.

## Variables, Variable Groups, And Key Vault

### Library Variable Groups
- Put environment-scoped values in Library variable groups, not inline `variables:` blocks.
- Link one variable group per environment (e.g., `app-dev`, `app-staging`, `app-prod`). Use conditional expressions to select.

### Key Vault Integration
- Link secrets from Key Vault into variable groups via the Key Vault tab. This projects Key Vault secrets as pipeline variables, resolved at run time, masked in logs.
- Prefer **workload identity federation** for the Key Vault service connection — no client secret on either side.
- Rotate Key Vault secrets without touching the pipeline; consumers re-resolve on next run.
- Never echo secrets to logs. `$(secretName)` is masked when the variable is marked secret; raw string concat in scripts can defeat masking.

### Expressions And Scoping
- Runtime expressions `$[ ... ]` evaluate at runtime; template expressions `${{ ... }}` evaluate at compile time. Mixing them silently is a common bug.
- Secrets are not available in template expressions. If you're trying to use a secret at compile time, you're doing something wrong.

## Service Connections And Workload Identity
- Prefer **workload identity federation** for every cloud service connection (Azure, AWS, GCP). Federated identity uses OIDC between ADO and the cloud, eliminating long-lived secrets.
- For the Azure Resource Manager connection type, use the "Workload Identity federation" authentication option. The service connection gets a Managed Identity or App Registration, and ADO authenticates via OIDC.
- For AWS, use `AWSShellScript@1` with `awsCredentials:` backed by an OIDC-federated role.
- Scope service connection permissions narrowly; do not grant Owner at the subscription level when Contributor on a resource group is enough.
- Restrict service connections to the specific pipelines that need them via "Security" → "Pipeline permissions."

## Agent Pools
- Microsoft-hosted agents (`vmImage:`) are the default. Pin the image (`ubuntu-24.04`, not `ubuntu-latest`).
- Self-hosted agents are a trust boundary. Do not run PR builds from forks on a self-hosted pool that has access to production credentials.
- Scaled-set agents (VMSS-backed) provide ephemerality — each job gets a fresh VM. Prefer this pattern for self-hosted needs.
- `demands:` let a job require specific agent capabilities. Use it sparingly; it fragments the pool.

## Environments And Approvals
- ADO Environments (under Pipelines → Environments) hold approvals, gates, and deployment history.
- Use `deployment:` jobs (not `job:`) when targeting an environment; they integrate with rollback, history, and checks.
- Attach **checks** to environments for pre-deploy gates: manual approval, required reviewers, business hours window, ServiceNow change ticket, security policy eval. These are enforced at the platform level, not in a script.
- One environment per deploy target (e.g., `prod-eus`, `prod-weu`). Shared environments hide blast radius.

```yaml
jobs:
  - deployment: DeployProd
    displayName: Deploy to Production
    environment: prod-eus
    pool:
      vmImage: ubuntu-24.04
    strategy:
      runOnce:
        deploy:
          steps:
            - checkout: none
            - download: current
              artifact: release
            - template: steps/deploy.yml
```

## Resources
- Declare external dependencies explicitly: other repos (`repositories:`), pipeline artifacts (`pipelines:`), container jobs (`containers:`), pipeline webhooks (`webhooks:`).
- Cross-repo dependencies must be pinned. A `repositories:` entry without a `ref:` pulls `main` by default, which is a compliance risk.

```yaml
resources:
  repositories:
    - repository: shared-templates
      type: git
      name: platform/templates
      ref: refs/tags/v4.2.0
  pipelines:
    - pipeline: upstream-build
      source: upstream-service-build
      trigger:
        branches: [main]
  containers:
    - container: ci-tools
      image: myregistry.azurecr.io/ci-tools:sha256:abc...
```

- Use `container:` jobs to run steps inside a specific image when runner state matters.

## Triggers
- Explicit `trigger:` and `pr:` blocks. The default (trigger on every branch) is rarely what you want.
- `pr.drafts: false` keeps draft PRs from burning agent time.
- Path filters (`paths.include` / `paths.exclude`) let a monorepo skip unaffected pipelines.
- Branch policies (under Repos → Policies) enforce the PR pipeline as a required check; that belongs in the repo settings, not the pipeline YAML.

## Artifacts
- Use `PublishPipelineArtifact@1` for artifacts that downstream jobs in the same pipeline consume. They're faster than Universal Packages.
- Use **Universal Packages** (`UniversalPackages@0`) for artifacts shared across pipelines or promoted through environments.
- Promote existing artifacts between stages with `downloadPipelineArtifact`; do not rebuild.
- For container images, push to ACR or a separate registry; don't treat pipeline artifacts as a substitute.

## Scripts And Shell Hygiene
- Use `bash` (`steps: - bash: | ...`) with `set -Eeuo pipefail` on every non-trivial script.
- Use `pwsh:` for Windows agents; use `bash:` when running on Linux, even when PowerShell Core is available. Portable is better than clever.
- Quote variables. `$(...)` and `${{ ... }}` in scripts are interpolated by ADO before the shell sees them — user-controlled input is a shell-injection vector.
- Prefer env-var indirection for untrusted input:

```yaml
- bash: |
    set -Eeuo pipefail
    echo "$PR_TITLE"
  env:
    PR_TITLE: $(System.PullRequest.PullRequestTitle)
```

## Caching
- Use `Cache@2` with explicit key and path:

```yaml
- task: Cache@2
  inputs:
    key: 'go | "$(Agent.OS)" | go.sum'
    path: $(HOME)/go/pkg/mod
    restoreKeys: |
      go | "$(Agent.OS)"
```

- Hash every input that invalidates correctness.
- Do not cache secrets, built artifacts you intend to publish, or anything under `$(System.DefaultWorkingDirectory)` that `git clean` would remove.

## Security Checklist
- Every service connection uses workload identity federation.
- Key Vault is the source of truth for secrets; no secrets in variable groups or YAML.
- `extends` templates live in a platform repo pinned by tag.
- Required reviewers and branch policies are configured in the repo (not the pipeline) for protected branches.
- Environment checks (approvals, business hours, change management) attached to prod environments.
- Self-hosted agents are ephemeral and segmented by trust boundary.
- Pipelines run from forks (if enabled) do not have access to production service connections.
- `System.PullRequest.*` variables are not interpolated directly into scripts.
- `actionlint`-equivalent for ADO: use `az pipelines validate` and a linter like `az-pipelines-linter` in pre-commit.

## Anti-Patterns To Reject
- Classic (UI) pipelines held alongside YAML
- `vmImage: ubuntu-latest` or `windows-latest` in production pipelines
- `extends` templates referenced by branch (`refs/heads/main`) instead of tag
- Service connections with long-lived client secrets when federation is available
- Broad service connection permissions (Owner at subscription level) for narrow pipelines
- Secrets stored in variable groups without Key Vault backing
- `variables:` at the root holding environment-specific values that should be in groups
- Untyped template parameters
- Script steps that interpolate `$(System.PullRequest.SourceBranch)` or similar directly
- Self-hosted pools without ephemerality running untrusted PR code
- Deployment steps in a `job:` when `deployment:` against an environment would enforce the gate
- `continueOnError: true` on checks that should block the stage
- Artifacts rebuilt at each stage rather than promoted

## Completion Criteria
Do not consider an ADO pipeline task complete until all applicable items are true:
- YAML-only (no Classic pipeline for the same path)
- `trigger:`, `pr:`, and path filters are explicit
- templates are extracted where logic is shared; `extends` is used where org policy requires
- service connections use workload identity federation
- secrets come from Key Vault via linked variable group, marked secret, not echoed
- deploy jobs target ADO Environments with appropriate checks
- agents are pinned images on Microsoft-hosted pools, or ephemeral self-hosted with trust segmentation
- scripts use `set -Eeuo pipefail`, quote variables, and env-var-indirect untrusted input
- caching keys hash all inputs; nothing sensitive is cached

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Azure DevOps Pipelines Mode together with cicd-core.
Refactor /path/to/repo/azure-pipelines.yml to extend from platform/pipeline-templates @ v3.1.0.
Replace the ServicePrincipal ARM service connection with workload identity federation.
Project secrets via Key Vault into the variable group; remove inline secrets.
Deploy stages target environments prod-eus and prod-weu with manual approval checks.
Pin Microsoft-hosted image to ubuntu-24.04. Validate with az pipelines validate.
```
