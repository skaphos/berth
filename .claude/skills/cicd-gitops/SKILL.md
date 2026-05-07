---
name: cicd-gitops
description: Use when designing or modifying GitOps deploy workflows with Argo CD or Flux — `Application`/`ApplicationSet`, `Kustomization`/`HelmRelease`, config-repo structure, PR-driven promotion, progressive delivery, drift detection, secret management, RBAC. Pair with `cicd-core`, `cicd-supply-chain`, `kubernetes-dev`. Triggers on edits to Argo/Flux manifests or config-repo layout.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# GitOps Mode

## Purpose
Use this skill when designing, reviewing, or modifying GitOps deployment workflows. It covers pull-based deployment, desired-state design, PR-driven promotion, progressive delivery, secret management, drift detection, and RBAC for deployments.

This skill is platform-agnostic across the two dominant tools (**Argo CD** and **Flux**), with notes where they diverge. Pair it with `cicd-core` for pipeline principles, `cicd-supply-chain` for artifact integrity, and `kubernetes-dev` for workload manifests.

## Skill Use
- Load this skill when the task is to design a deploy model, structure a config repo, write Argo CD `Application`/`ApplicationSet` manifests, write Flux `Kustomization`/`HelmRelease` manifests, or reason about promotion.
- Treat this skill as the governing contract for deploy shape, safety, and RBAC.
- Keep organization-specific context (tool choice, cluster topology, target environments, secret store) in the invoking prompt.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Read the config repo (not just the app repo) before proposing changes. In GitOps, the config repo is the source of truth and where most deploy bugs live.
- Run `kustomize build` / `helm template` on the rendered output; verify the change produces the intended manifest before committing.
- For Argo CD: use `argocd app diff` and `argocd app get` to compare desired vs. live state. For Flux: `flux diff kustomization`, `kustomize build | kubectl diff -f -`.
- Issue independent tool calls (reading multiple environments, scanning for references, checking sync status) in parallel.
- Never `kubectl apply` directly in production — any change must go through Git. If the investigation requires a live-cluster action, do it read-only (`kubectl get`, `kubectl describe`).

## Core Principles
- **Git is the source of truth.** If it isn't in Git, it isn't deployed. If Git changes, the cluster converges.
- **Pull-based, not push-based.** The cluster's in-cluster controller pulls and applies; CI does not `kubectl apply` across network boundaries with long-lived credentials.
- **Desired state, not imperative procedure.** The repo declares what should be; the controller reconciles how to get there.
- **Promotion is a PR.** Moving an artifact from dev to staging to prod is a PR that edits the config repo, reviewed like any other code change.
- **Rollback is a git revert.** No separate rollback tooling; the revert PR is the rollback.
- **Small blast radius per application.** One Argo CD `Application` / Flux `Kustomization` per deployable unit, scoped to a single namespace by default.

## Repository Structure
Separate **app** and **config** repos for non-trivial deployments. Monorepos are defensible for small orgs but make access control and PR reviews messier.

```
config-repo/
  clusters/
    prod-eus/
      kustomization.yaml
      infra/
        argocd/
        external-secrets/
        cert-manager/
      apps/
        myapp/
    staging-eus/
    dev-eus/
  apps/
    myapp/
      base/
        kustomization.yaml
        deployment.yaml
        service.yaml
      overlays/
        dev/
        staging/
        prod/
  platform/
    appsets/
    projects/
```

Rules:
- `base/` holds the manifest; `overlays/<env>/` holds environment-specific patches.
- Commit rendered manifests only when your GitOps tool requires it (rare). Let Argo CD or Flux render Kustomize/Helm at apply time.
- Cluster-scoped configuration (`clusters/<cluster>/`) selects which apps run where.

## Argo CD

### Application Model
One `Application` per deployable unit:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: myapp-prod-eus
  namespace: argocd
spec:
  project: prod
  destination:
    name: prod-eus
    namespace: myapp
  source:
    repoURL: https://github.com/org/config-repo
    targetRevision: HEAD
    path: apps/myapp/overlays/prod
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
    syncOptions:
      - CreateNamespace=false
      - ServerSideApply=true
      - PrunePropagationPolicy=foreground
    retry:
      limit: 5
      backoff:
        duration: 30s
        factor: 2
        maxDuration: 10m
```

Rules:
- `project:` scopes what sources, destinations, and RBAC apply. Use projects as the coarse-grained RBAC boundary.
- `automated.prune: true` removes resources when they're deleted from Git. Without it, deletions are one-way.
- `selfHeal: true` reverts out-of-band changes — correct for GitOps, but verify no admission controller mutations are getting reverted in a loop.
- `ServerSideApply=true` is the modern default; it gives meaningful conflict errors and respects other field managers.
- Pin `targetRevision` to a commit or tag for production. Use a branch only for preview environments.

### ApplicationSet
`ApplicationSet` generates `Application`s from a template. Use for multi-cluster, multi-tenant, or PR-preview patterns.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: myapp
  namespace: argocd
spec:
  generators:
    - matrix:
        generators:
          - clusters:
              selector:
                matchLabels:
                  env: prod
          - git:
              repoURL: https://github.com/org/config-repo
              revision: HEAD
              directories:
                - path: apps/myapp/overlays/prod-*
  template:
    metadata:
      name: 'myapp-{{name}}-{{path.basename}}'
    spec: ...
```

- Matrix generator combines cluster + path enumeration. Good for "deploy this app to every prod cluster."
- PR generator deploys a preview for each open PR. Scope destination namespaces per PR and auto-prune on close.

### Sync Waves And Hooks
- Annotate resources with `argocd.argoproj.io/sync-wave: "<n>"` to control apply order. CRDs at -1, namespaces at 0, operators at 1, apps at 2.
- Pre/post-sync hooks (`argocd.argoproj.io/hook: PreSync`) run as Kubernetes Jobs. Use them for migrations, but idempotency is your responsibility.

### Argo CD RBAC
- `AppProject` limits what an `Application` can reference: source repos, destinations, resource kinds, roles.
- Apply the principle of least privilege: a project for `prod-apps` does not include `default` namespace or the `argocd` namespace as destinations.
- Argo CD's own RBAC controls who can trigger syncs. Production sync from the UI should require a specific role, not a shared admin account.

## Flux

### Kustomization + HelmRelease
Flux splits GitOps into source controllers (`GitRepository`, `HelmRepository`, `OCIRepository`) and apply controllers (`Kustomization`, `HelmRelease`).

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: config
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/org/config-repo
  ref:
    tag: prod/v1.42.0
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: myapp-prod
  namespace: flux-system
spec:
  interval: 5m
  path: ./apps/myapp/overlays/prod
  prune: true
  sourceRef:
    kind: GitRepository
    name: config
  targetNamespace: myapp
  timeout: 5m
  wait: true
  healthChecks:
    - apiVersion: apps/v1
      kind: Deployment
      name: myapp
      namespace: myapp
  retryInterval: 2m
```

Rules:
- Pin source by tag (`ref.tag`) or semver range (`ref.semver`) for production. `ref.branch: main` is fine for dev.
- `prune: true` is the Flux equivalent of Argo CD's prune; same semantics.
- `wait: true` with `healthChecks` blocks the reconcile as ready until resources report healthy — required for ordered multi-`Kustomization` deploys.
- `dependsOn:` expresses ordering across Kustomizations (infra first, then apps).

### Flux RBAC And Impersonation
- Flux's `Kustomization` can impersonate a ServiceAccount per namespace (`spec.serviceAccountName`), letting you enforce per-tenant RBAC on what the GitOps controller can apply.
- Separate flux namespaces for platform vs. tenant deploys when multi-tenancy matters.

## Progressive Delivery
- Use **Argo Rollouts** or **Flagger** for canary, blue/green, and progressive traffic shifting. Base `Deployment` rolling updates are not progressive delivery.
- Define analysis (metrics, success rate, latency) as `AnalysisTemplate` / `Canary` resources; don't script traffic shifts from CI.
- Progressive delivery is independent of GitOps: the config repo declares the `Rollout`; the controller manages traffic.
- Abort-on-failure: every progressive-delivery spec should have explicit failure conditions and an auto-rollback path.

## Secret Management
You cannot put plaintext secrets in Git. Pick one and apply it consistently:

- **External Secrets Operator (ESO)**: `ExternalSecret` references a cloud secret store (AWS Secrets Manager, GCP Secret Manager, Azure Key Vault, Vault). ESO projects the value into a `Secret`. Strong fit for multi-cloud.
- **SOPS**: encrypt secrets at rest with age or KMS keys; Flux has native SOPS decryption support. Argo CD needs a plugin.
- **Sealed Secrets**: cluster-specific encrypted `SealedSecret` CRDs. Simple but tied to the cluster's Sealed Secrets controller key.
- **Secrets Store CSI Driver**: project secrets as mounted files without creating `Secret` objects. Rotation is automatic but adds complexity.

Rules:
- Never stage plaintext secrets even "temporarily." A secret committed to Git must be treated as compromised and rotated.
- Rotate secret-store credentials on a schedule. The secret store itself is the long-lived credential.
- Do not project secrets via `env` when file mounts work. `env` leaks via process listings and crash dumps.

## Promotion Patterns
Promotion between environments is a PR that updates the desired version.

### Image Tag Update (CI-Driven)
1. CI builds the image, tags it with a commit SHA, publishes SBOM and signature.
2. CI opens a PR against the config repo updating `images:` in `apps/myapp/overlays/dev/kustomization.yaml` to the new SHA.
3. The PR merges on review. Argo CD / Flux reconciles dev.
4. A separate PR (manual or bot-authored) promotes the same tag to staging, then prod.

Rules:
- Dev can auto-promote; staging and prod require human review.
- Do not point prod at `latest` or a floating tag. The tag in Git is the source of truth for what's deployed.
- A promotion PR that lands is the deploy event. There is no separate "push deploy" step.

### Image Automation (Flux)
Flux Image Automation Controllers can auto-update Git when a new image tag matching a policy appears. Useful for dev environments; rarely acceptable for prod.

### Argo CD Image Updater
Similar pattern for Argo CD. Same tradeoffs.

## Drift Detection And Remediation
- `selfHeal: true` (Argo CD) and the default Flux reconcile both revert out-of-band changes. Verify this doesn't fight admission-controller mutations.
- For resources that legitimately change outside GitOps (HPA-managed replicas, cert-manager-updated TLS secrets), use `ignoreDifferences:` (Argo CD) or `fieldManagers` (Flux) to exempt specific fields, not whole resources.
- Alert on drift events. A drift being silently reverted is fine; a drift that keeps recurring is a bug.

## Multi-Cluster Patterns
- Prefer one GitOps controller per cluster (in-cluster). Central "hub" controllers exist (Argo CD can manage many clusters) but increase blast radius and credential sprawl.
- Cluster bootstrap: one `Application` / `Kustomization` deploys the GitOps controller itself. Everything else is a child of that.
- Cluster identity: label each cluster (`env`, `region`, `tier`) so `ApplicationSet` generators target cleanly.

## Bootstrap And Day 0
- Bootstrap is a one-time operation: install the GitOps controller, point it at the config repo, let it take over.
- Store the bootstrap manifests in the config repo so "recreate the cluster" is a scripted operation.
- Bootstrap credentials (the first SSH key or token for the config repo) are short-lived: rotate to an in-cluster credential (ServiceAccount with repo access, OIDC to the Git provider) as soon as possible.

## Observability
- Expose GitOps controller metrics to Prometheus. Track sync success rate, sync duration, reconcile age, drift events.
- Wire alerts for: sync failures, Applications/Kustomizations stuck in `OutOfSync`/`Drifted`, repository fetch failures, controller pod restarts.
- Record `kubectl events` for deploy-critical namespaces; they're the paper trail when a controller reconcile goes wrong.

## Security Checklist
- Config repo has branch protection, signed commits, required reviews on main.
- GitOps controller has least-privilege RBAC: namespace-scoped when possible, no `cluster-admin`.
- Secrets are encrypted at rest (SOPS) or pulled from a secret store (ESO, CSI).
- No plaintext secrets in any commit, historical or current. Run a secret scanner on the config repo.
- Admin endpoints (Argo CD UI, Flux webhooks) are behind auth and network policy.
- Argo CD SSO uses OIDC; do not use the built-in local admin account in production.
- Progressive delivery analysis templates have explicit failure modes; a stuck canary should auto-rollback, not sit.

## Anti-Patterns To Reject
- `kubectl apply -f ...` to production from CI, bypassing GitOps
- Production `Application`/`Kustomization` pointing at `HEAD` or `main` instead of a pinned ref
- Plaintext secrets in any commit
- One Application/Kustomization covering multiple unrelated workloads
- `automated.prune: false` on production Applications (silently leaks resources)
- `selfHeal: false` in GitOps mode (defeats the point)
- Promotion by retagging an image rather than updating Git
- CI directly modifying cluster state, bypassing the config repo
- Shared admin account for GitOps UI sync operations
- Flux `Kustomization` without a `serviceAccountName` in multi-tenant clusters
- Broad `AppProject` permissions (all destinations, all kinds) used by tenant Applications
- Manual edits to cluster state "just this once" — file a PR even for emergency changes; the revert is the audit trail

## Completion Criteria
Do not consider a GitOps task complete until all applicable items are true:
- the config repo is the source of truth; no manual cluster state outside of emergency break-glass
- every Application/Kustomization is scoped to one namespace (or has explicit cluster-scope justification)
- production refs are pinned to commits or tags
- prune and self-heal are both on (or each is justified off)
- secrets are stored via SOPS, ESO, or equivalent — never plaintext
- promotion is a PR; no out-of-band deploys
- GitOps controller RBAC is scoped; no `cluster-admin`
- drift is observable (metrics + alerts); progressive delivery has auto-rollback

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use GitOps Mode together with kubernetes-dev.
Design the Argo CD application structure for myapp across dev, staging, and prod-eus/prod-weu.
Use an ApplicationSet with matrix generator: clusters labeled env=prod × overlays/prod-*.
Production targetRevision pinned to tag; dev follows main.
Secrets via ExternalSecretsOperator from Azure Key Vault. Progressive delivery with Argo Rollouts, canary 10/25/50/100 with SLO analysis.
Report RBAC boundaries: AppProjects and destination scoping.
```
