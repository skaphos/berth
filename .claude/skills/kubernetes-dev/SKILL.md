---
name: kubernetes-dev
description: Use when writing, modifying, or reviewing Kubernetes manifests, Kustomize bases or overlays, and platform-facing resource definitions. Covers selectors/labels, probes, resources, security context, NetworkPolicy, PDBs, rollout strategy. Verify with `kustomize build` and `kubectl apply --dry-run=server`. Skip for chart packaging (use `helm-dev`) or controllers (use `operator-dev`).
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.3.0 -->
# Kubernetes Development Guidance

## Purpose
Use this skill when writing, modifying, or reviewing Kubernetes manifests, workload configuration, and platform-facing resource definitions.

## Skill Use
- Load this skill for raw manifests, Kustomize bases or overlays, and repository-managed Kubernetes objects.
- Favor explicit, reviewable manifests over abstraction layers that hide important workload behavior.
- Preserve stable object identity unless the task explicitly requires replacement.
- Match the repository's existing tooling (Kustomize, kpt, server-side apply, GitOps) rather than introducing new mechanisms.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Invoke tools to read manifests, overlays, and patches; do not describe what you would do.
- Run `kustomize build`, `kubectl diff`, or `kubectl apply --dry-run=server` yourself — do not claim a change is safe without tool output.
- Issue independent tool calls (reading multiple overlays, inspecting related RBAC/ConfigMaps) in parallel.
- Before changing selectors, labels, or immutable fields, grep for consumers (Services, HPAs, NetworkPolicies, PDBs) to scope blast radius.

## Core Principles
- Keep selectors, labels, ports, probes, resources, and security context obvious in the manifest.
- Prefer namespace-scoped, least-privilege changes. Escalate scope only when required.
- Model production concerns directly: readiness, liveness, disruption tolerance, rollout strategy, and observability hooks.
- Avoid hidden coupling across overlays, namespaces, and controllers.
- Secure by default: a manifest that does not explicitly set security context, resource requests, or a NetworkPolicy should be treated as incomplete.

## Default Workflow
1. Inspect the target workload, related ConfigMaps/Secrets, RBAC, and any overlay or patch chain.
2. Identify whether the change affects rollout behavior, identity, or API compatibility.
3. Make the smallest safe manifest change.
4. Validate rendered output with `kustomize build` and server-side schema with `kubectl apply --dry-run=server`.
5. Re-check disruption safety: PDBs, topology spread, replica count, and rollout strategy.

## Manifest Style
- Use API versions supported by the target cluster. Do not write manifests against removed or deprecated versions.
- Set both `kind` and `apiVersion` on every object; do not rely on defaults.
- Use fully qualified labels and annotations. The `app.kubernetes.io/*` recommended label set (`name`, `instance`, `version`, `component`, `part-of`, `managed-by`) is the default for workloads.
- Keep `metadata.name` stable across renames; rename via a new resource plus deprecation rather than in-place edits on immutable-selector fields.
- Prefer `resources`, `securityContext`, and `probes` as required fields on every Pod template, not optional polish.
- Avoid string-templating YAML by hand. Use Kustomize patches, strategic-merge, or structured tools rather than unstructured text substitution.

## Structured YAML And Kustomize
Prefer structured tools over ad-hoc YAML editing:

- Use `kustomize build path/` (or `kubectl kustomize path/`) for composition; do not hand-merge overlays.
- For per-environment differences, use `kustomization.yaml` with `patches`, `patchesStrategicMerge`, or `patchesJson6902` (selected by change kind).
- Prefer strategic-merge patches for additive changes and JSON-6902 patches for precise index-level edits.
- Use `components` in Kustomize for cross-cutting concerns (security-baseline, observability sidecars) that apply to many overlays.
- Commit rendered manifests to a release branch only if the GitOps tooling requires it; otherwise let Kustomize render at apply time.
- For programmatic manipulation (operators, release tooling, scripted generation), prefer a structured YAML library such as `sigs.k8s.io/kustomize/kyaml` over string templates — it preserves comments, ordering, and formatting, and catches schema errors early.
- Keep `kustomization.yaml` declarative. Avoid `configMapGenerator`/`secretGenerator` flags that inject time-dependent hashes unless the rollout depends on them.
- Use server-side apply (`kubectl apply --server-side`) when multiple controllers or humans touch the same objects; it produces meaningful conflict errors instead of silent overwrites.

## Labels, Selectors, And Ownership
- Pod `metadata.labels` and `spec.selector.matchLabels` must match. Changing a Deployment selector is effectively a new Deployment — plan a cutover.
- Scope Service selectors narrowly. A Service that matches more pods than expected silently routes traffic to them.
- Use `app.kubernetes.io/instance` to distinguish releases when multiple copies of the same workload coexist.
- When adopting resources into a controller (Helm release, GitOps, operator), set ownership annotations and labels before applying, not after.

## Workload Design

### Replicas, Disruption, And Rollout
- Set `replicas` explicitly. Do not rely on defaults.
- Any workload with `replicas > 1` must define a `PodDisruptionBudget` covering voluntary disruption. Use `minAvailable` for stateful workloads; `maxUnavailable` for stateless ones.
- Set `strategy` explicitly. For stateless workloads, `RollingUpdate` with conservative `maxUnavailable` and `maxSurge` is the default. For stateful workloads, use the controller-native strategy (`OnDelete` or `RollingUpdate` with `partition` for StatefulSets).
- For workloads that must survive node, zone, or region loss, add `topologySpreadConstraints` (see below).

### Topology Spread Constraints
Topology spread constraints are the default mechanism for "don't pile all my pods on one node or in one zone."

- For multi-replica workloads, include at least a hostname spread and a zone spread. Example:

```yaml
spec:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: kubernetes.io/hostname
      whenUnsatisfiable: ScheduleAnyway
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: myapp
          app.kubernetes.io/instance: myapp-prod
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: ScheduleAnyway
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: myapp
          app.kubernetes.io/instance: myapp-prod
```

Rules:
- `labelSelector` must match the Pod's labels, not the Deployment's labels. Use both `app.kubernetes.io/name` and `app.kubernetes.io/instance` to avoid cross-release interference.
- Default to `whenUnsatisfiable: ScheduleAnyway`. `DoNotSchedule` is a stronger guarantee but will leave pods Pending if the cluster cannot satisfy it — reserve it for hard requirements.
- Use `matchLabelKeys: [pod-template-hash]` (Kubernetes 1.27+) to scope a constraint to a single rollout revision; this avoids old and new pods counting against the same skew budget during deploys.
- `minDomains` (Kubernetes 1.27+) lets you require a minimum number of topology domains (zones) before scheduling — useful for HA workloads.
- Prefer `topologySpreadConstraints` over `podAntiAffinity` for new work. Anti-affinity is coarser and harder to tune.

### Probes
- `readinessProbe` is required for any workload that serves traffic; without it, rollouts route to unready pods.
- `livenessProbe` should detect hung states the process cannot recover from on its own — not transient slowness. If a liveness probe would kill a healthy-but-busy pod, it's wrong.
- `startupProbe` is required for slow-starting containers; without it, liveness failures will restart them during initialization.
- Use `httpGet` with a dedicated health endpoint when the application supports it; otherwise `tcpSocket`. Avoid `exec` probes for chatty containers.
- Set `periodSeconds`, `failureThreshold`, and `timeoutSeconds` deliberately. Default values are rarely right for real workloads.

### Resources And QoS
- Always set `resources.requests` for both CPU and memory. Requests drive scheduling and are the baseline for HPA.
- Set `resources.limits` for memory. CPU limits are context-dependent: they can cause throttling under bursty load and may be harmful for latency-sensitive workloads — decide deliberately.
- Aim for Guaranteed QoS (equal requests and limits) only when the workload needs it; Burstable is the default for most services.
- HPA and VPA cannot coexist on the same metric for the same workload without coordination. Use one, and document why.

### Autoscaling
- When adding an HPA, also set a sensible replica floor and ceiling (`minReplicas`, `maxReplicas`).
- HPA requires `resources.requests` to be set on the target workload.
- Prefer `behavior` (Kubernetes 1.18+) to control scale-up and scale-down rates. Scale-down stabilization prevents flapping.

## Security Baseline
Every pod spec should adopt a Pod Security Standards (PSS) baseline. The defaults below target the **Restricted** profile; relax only when the workload genuinely requires it, and document the reason.

```yaml
spec:
  automountServiceAccountToken: false   # set true only when the pod calls the kube API
  serviceAccountName: myapp              # never use the default service account
  securityContext:                        # pod-level
    runAsNonRoot: true
    runAsUser: 10001
    runAsGroup: 10001
    fsGroup: 10001
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: registry.example.com/myapp@sha256:...   # pin by digest, not tag
      imagePullPolicy: IfNotPresent
      securityContext:                     # container-level
        allowPrivilegeEscalation: false
        privileged: false
        readOnlyRootFilesystem: true
        runAsNonRoot: true
        capabilities:
          drop: ["ALL"]
      resources:
        requests:
          cpu: "100m"
          memory: "128Mi"
        limits:
          memory: "256Mi"
      volumeMounts:
        - name: tmp
          mountPath: /tmp
  volumes:
    - name: tmp
      emptyDir: {}
```

### Pod Security Standards
- Apply Pod Security Admission labels on every namespace: at minimum `pod-security.kubernetes.io/enforce=baseline`, and prefer `restricted` for application namespaces.
- Use `audit` and `warn` labels at a tighter level than `enforce` during rollout so you can surface violations before promoting them.

### Service Accounts And Tokens
- Every workload has its own ServiceAccount. Do not reuse `default`, and do not share ServiceAccounts across workloads.
- Set `automountServiceAccountToken: false` on every pod that does not call the Kubernetes API.
- For workloads that do need API access, scope RBAC to the namespace when possible and use `Role`/`RoleBinding` rather than cluster-scoped permissions.
- For cloud identity, prefer workload identity federation (IRSA, GKE Workload Identity, Azure Workload Identity) over static credentials.

### Containers And Filesystem
- `runAsNonRoot: true` and an explicit `runAsUser > 0` on every container. Do not rely on the image default.
- `readOnlyRootFilesystem: true` on every container. Mount `emptyDir` volumes at `/tmp`, `/var/run`, or wherever the app genuinely needs to write.
- `allowPrivilegeEscalation: false` on every container.
- Drop all capabilities (`capabilities.drop: ["ALL"]`) and add back only the specific capabilities the app requires.
- Never set `privileged: true` unless there is a documented, reviewed justification (CNI, device plugin, node-level component).
- `hostNetwork`, `hostPID`, `hostIPC`, `hostPath` mounts, and host ports are all escape hatches. They should be absent by default and justified when present.
- Pin images by digest (`@sha256:...`) for production workloads. Tags drift silently.
- Set a `seccompProfile` (`RuntimeDefault` as the baseline; `Localhost` with a custom profile for hardened workloads).

### Secrets
- Do not commit secret values. Reference Secrets by name; source them from an external secret store (Vault, Cloud Secret Manager, External Secrets Operator, Secrets Store CSI Driver) wherever the platform supports it.
- Never expose secrets via `env` when the app can read files; mount them as files so rotation takes effect without a pod restart.
- Label Secrets with owner and purpose; stale secrets are a common exfiltration vector.

### Network Exposure
- Default-deny NetworkPolicy per application namespace: one NetworkPolicy that denies all ingress and egress, then explicit allow rules per workload.
- Every Service that accepts external traffic should be scoped: `LoadBalancer` and `NodePort` are public by default — prefer `ClusterIP` plus an ingress controller with TLS.
- For Ingress, require TLS. Use cert-manager or the platform-standard certificate flow. Do not hardcode certificates in manifests.
- Explicit `externalTrafficPolicy: Local` when preserving client IP matters; understand the availability tradeoff before setting it.

### Example Default-Deny NetworkPolicy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: myapp
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
```

Then add explicit allow policies per workload (DNS egress, metrics scraping, inter-service traffic).

## RBAC
- Grant the minimum verbs on the minimum resource names.
- Prefer `Role`/`RoleBinding` over `ClusterRole`/`ClusterRoleBinding`. Cluster scope should be justified.
- Do not grant `*` on `*` outside of platform-level controllers that genuinely need it.
- Audit `ClusterRole` aggregation labels — they can grant more than intended.
- Never bind a ServiceAccount to `cluster-admin` unless the workload is a platform controller with a documented need.

## ConfigMaps And Secrets
- Project configuration as files when possible; env vars leak via logs and process listings.
- When renaming a ConfigMap or Secret key, update all consumers in the same change; dangling references fail at pod start with confusing errors.
- Use `immutable: true` on Secrets and ConfigMaps that should never change in place; forces a new name on edit and surfaces unintended mutation.

## StatefulSets, Storage, And Data
- StatefulSet changes that touch `serviceName`, `volumeClaimTemplates`, or `selector` are breaking. Plan cutover and rollback before applying.
- `storageClassName` is effectively immutable on a PVC. Changing it requires a new PVC and migration.
- `allowVolumeExpansion` must be true on the StorageClass before an expansion will succeed.
- Back up before applying any change to a StatefulSet's volume claim template.

## Jobs And CronJobs
- Set `backoffLimit`, `ttlSecondsAfterFinished`, and `activeDeadlineSeconds` deliberately. Defaults create long-lived failed jobs and log noise.
- For CronJobs, set `concurrencyPolicy` explicitly (`Forbid` for non-idempotent work, `Allow` for parallel-safe work) and `successfulJobsHistoryLimit` / `failedJobsHistoryLimit` to manage history.
- Job pods must follow the same security baseline as long-running workloads.

## Observability
- Prometheus metrics endpoint should be on a dedicated port and gated by NetworkPolicy to the scrape source.
- Include `ServiceMonitor` or the equivalent for the cluster's observability stack.
- Structure logs (JSON). Include pod name, namespace, and correlation identifiers when available.
- Annotate workloads for the cluster's log routing conventions rather than relying on defaults.

## Default Verification
- Run `kustomize build path/` and inspect the rendered output.
- Validate schema with `kubectl apply --dry-run=server -f -` or `kubeconform -summary` for offline checks.
- Run `kubectl diff` against the target cluster when safe.
- Run the cluster's policy validator (`kyverno test`, `conftest`, `gator test`) when policies are defined in the repo.
- Re-check selectors, immutable fields, disruption budgets, topology spread, and controller ownership for upgrade safety.

## Completion Criteria
Do not consider a Kubernetes manifest task complete until all applicable items are true:
- rendered manifests validate against the cluster's admission stack
- every pod template has resources, probes, and a securityContext meeting the security baseline
- every multi-replica workload has a PDB and topology spread constraints
- NetworkPolicy coverage is present or the gap is deliberately documented
- RBAC is scoped narrowly and uses a dedicated ServiceAccount
- images are pinned and the SA token is not auto-mounted unless needed
- the change does not break selectors, disruption budgets, or StatefulSet identity

## Anti-Patterns To Reject
- Using the `default` ServiceAccount
- Missing `resources.requests`
- Missing or default-value probes
- `runAsUser: 0` (root) without explicit justification
- `privileged: true`, `hostNetwork: true`, `hostPath` mounts without documented need
- `capabilities.add: ["ALL"]` or broad capability grants
- Images pinned by `:latest` or floating tags
- NetworkPolicies absent on application namespaces
- RBAC wildcards (`*` on `*`) outside platform components
- Deployment selectors that accidentally include labels from a sibling deployment
- Multi-replica workloads without PDB or topology spread
- Secrets referenced via `env` when the app can read files
- Hand-edited rendered manifests that bypass Kustomize composition
- Changing `immutable` fields (selector, serviceName, storageClassName) in place

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Kubernetes Development Guidance.
Add a new worker Deployment in /path/to/manifests/overlays/prod.
Three replicas, hostname + zone topology spread, PDB with minAvailable=2.
Apply the restricted Pod Security Standards baseline: non-root, read-only root FS, drop all capabilities, no SA token mount.
Scope network egress to the queue and metrics endpoints with a NetworkPolicy.
Pin the image by digest. Verify with kustomize build and kubectl apply --dry-run=server.
```
