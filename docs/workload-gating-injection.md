# Workload Gating via `berth-acquire` Injection

Berth can gate a workload on a lease **without modifying its container image**
by injecting a small `berth-acquire` helper into opted-in Pods through a
mutating admission webhook. A Pod opts in with a label; annotations on the pod
template tune the behavior.

This is a **fallback path**. The recommended way to run a cross-cluster
singleton is still [operator-as-holder](architecture.md#operator-reconcile-flow)
(a `BerthLease` with `clusterID`), which keeps lease ownership at one controller
per cluster. Reach for injection only when you cannot change the image or need
per-Pod startup gating.

> Design rationale and trade-offs live in
> [docs/design/2026-05-workload-gating-injection-model.md](design/2026-05-workload-gating-injection-model.md)
> and ADRs [0001](adr/0001-pod-level-gating-for-injected-singletons.md),
> [0002](adr/0002-label-annotation-opt-in-over-crd.md),
> [0003](adr/0003-sidecar-runtime-enforcement-by-container-kill.md). This page is
> the operator/user how-to.

## When to use which path

| You want… | Use |
| --- | --- |
| A no-code cross-cluster singleton you control | **Operator-as-holder** (`BerthLease` + `clusterID`) — recommended |
| Fine-grained holder/renewal logic in an app you maintain | **Direct integration** (the Berth Go client) |
| To gate an **unmodifiable** (legacy/vendor) image on a lease | **Injection** (this page) |
| Pod startup to *wait* for a lease, with no runtime guarantee | **Injection — `startup-gate`** |
| Exactly one Pod candidate to run behind a continuously renewed lease | **Injection — `runtime-singleton`** |

Injection creates **more potential lease holders** (every opted-in replica can
race to acquire) than operator-as-holder, so its split-brain surface is larger.
See [Operational caveats](#operational-caveats).

## Prerequisites

1. **The operator chart with injection enabled.** Install `berth-operator`
   with `injection.enabled=true`; see the
   [configuration reference](reference/configuration.md) for the full value
   surface. At minimum:

   ```yaml
   injection:
     enabled: true
     helper:
       repository: ghcr.io/skaphos/berth-acquire
       # tag defaults to the chart appVersion
     webhook:
       tls:
         certManager:
           enabled: true
           issuerRef: { name: berth-ca, kind: ClusterIssuer }
   ```

   The injected helper **reuses the operator's `berth.*` connection settings**
   (API server URL, CA bundle, server name, `clusterID`, insecure-skip-verify) —
   you do not configure them again under `injection`.

2. **A webhook serving certificate.** Either cert-manager (above) or a
   pre-created `kubernetes.io/tls` Secret via
   `injection.webhook.tls.existingSecret` plus
   `injection.webhook.tls.caBundle`.

3. **Auth for the injected helper.** Each injected Pod talks to the Berth API
   directly, so it needs a bearer token unless the API server runs with
   `--auth-mode=none`. The chart sets the **path** the helper reads via
   `injection.helper.apiKeyFile`; the **platform must mount a token at that
   path** in the workload Pod (see [Authenticating injected Pods](#authenticating-injected-pods)).

## Opt-in contract

All keys are under the `berth.skaphos.io/` prefix. The label and annotations
must live on the **pod template** — standard controllers copy
`spec.template.metadata` onto the Pods they create.

### Label (selection)

| Key | Required | Value | Meaning |
| --- | --- | --- | --- |
| `berth.skaphos.io/inject` | Yes | `acquire` | Opt the Pod in. Any other value is ignored (no mutation). |

The chart's default `injection.webhook.objectSelector` also matches this label,
so the webhook is only consulted for opted-in Pods.

### Annotations (configuration)

| Key | Required | Default | Notes |
| --- | --- | --- | --- |
| `berth.skaphos.io/lease-name` | **Yes** | — | Berth lease to acquire. |
| `berth.skaphos.io/lease-namespace` | No | Pod namespace | Lease namespace. |
| `berth.skaphos.io/mode` | No | `runtime-singleton` | `startup-gate` or `runtime-singleton`. |
| `berth.skaphos.io/holder-identity` | No | mode-specific | Overrides the computed holder identity. Advanced — a shared identity can defeat singleton enforcement. |
| `berth.skaphos.io/ttl-seconds` | No | chart default | Lease TTL. Must be `> 0`. |
| `berth.skaphos.io/heartbeat-interval-seconds` | No | derived from TTL | Renew interval. `> 0` and `< ttl-seconds`. |
| `berth.skaphos.io/release-on-shutdown` | No | `true` (runtime-singleton) | Best-effort `Release` on graceful shutdown. |
| `berth.skaphos.io/enforce` | No | `probe` | runtime-singleton only: `probe` or `signal`. See [Enforcement](#enforcement-runtime-singleton). |
| `berth.skaphos.io/enforce-grace-seconds` | No | from `terminationGracePeriodSeconds` | `signal` only: seconds between SIGTERM and SIGKILL. |

Operator-supplied defaults for `mode`, `enforce`, and `ttl-seconds` come from the
chart (`injection.defaults.*`); a per-Pod annotation overrides them.

## Modes

### `startup-gate`

Injects an **init container only**. It blocks until it acquires the lease, then
exits 0 so the main containers start. **Nothing renews the lease afterward** —
this is *not* runtime singleton enforcement. Good for startup ordering and for
run-to-completion Jobs that just need to win the lease before running.

### `runtime-singleton`

Injects the init "hold" **plus a native sidecar** (an init container with
`restartPolicy: Always`, so Jobs/CronJobs can still complete). The sidecar
renews the lease and, **on lease loss, actively stops the main container** and
keeps it gated until it re-acquires. This is the at-most-once mode.

A kubelet restart does **not** re-run init containers, so the sidecar is what
keeps a stopped main container gated after enforcement fires — "lease lost"
becomes a controlled crashloop, not an unguarded restart.

## Enforcement (`runtime-singleton`)

Selected by `berth.skaphos.io/enforce`:

- **`probe` (default)** — the webhook injects an `exec` liveness probe
  (`/berth/check /berth/healthy`) on each main container, backed by a shared
  `emptyDir`. The sidecar removes the health marker on lease loss; the kubelet
  kills the container. Native, preserves container isolation, no extra RBAC. The
  helper ships a static `check` binary onto the shared volume, so the probe does
  **not** require a shell in the target image.
- **`signal`** — the webhook sets `shareProcessNamespace: true`; the sidecar
  sends `SIGTERM`/`SIGKILL` to the main process on lease loss. More immediate,
  but every container in the Pod can then see and signal every other one (weaker
  isolation), and PID-1 signal handling varies by image. Reserve it for images
  where the probe cannot run.

Both are **best-effort within a bounded window**: there is detection + kill
latency between lease loss and the container stopping. The **fencing token**
remains the real boundary for any downstream resource that must reject a stale
holder.

## Examples

### Deployment — runtime singleton (legacy cross-cluster singleton)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout-service
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: checkout-service
        berth.skaphos.io/inject: acquire          # opt-in (copied onto Pods)
      annotations:
        berth.skaphos.io/lease-name: checkout-singleton
        berth.skaphos.io/mode: runtime-singleton
        berth.skaphos.io/enforce: probe
        berth.skaphos.io/ttl-seconds: "30"
    spec:
      containers:
        - name: app
          image: vendor/checkout:1.4.2             # unmodifiable image
```

### StatefulSet — runtime singleton

Identical opt-in on `spec.template.metadata`. The holder identity defaults to a
unique per-Pod value, so the stable StatefulSet Pod name is folded in; only one
ordinal runs behind the lease at a time.

### Job — startup gate (recommended for run-to-completion)

```yaml
apiVersion: batch/v1
kind: Job
spec:
  template:
    metadata:
      labels:
        berth.skaphos.io/inject: acquire
      annotations:
        berth.skaphos.io/lease-name: nightly-export
        berth.skaphos.io/mode: startup-gate         # gate start, then run to completion
    spec:
      restartPolicy: Never
      containers:
        - name: export
          image: vendor/export:2
```

`runtime-singleton` on a Job is allowed (the native sidecar lets the Job
complete), but `startup-gate` is usually the right fit — a Job should run to
completion once it has won the lease, not be killed mid-run on a transient renew
blip.

### CronJob — mind the deeper label path

CronJobs nest the pod template **two levels deeper**. Putting the label on the
CronJob or the JobTemplate metadata does nothing — it must be on
`spec.jobTemplate.spec.template.metadata`:

```yaml
apiVersion: batch/v1
kind: CronJob
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        metadata:                                   # <-- spec.jobTemplate.spec.template.metadata
          labels:
            berth.skaphos.io/inject: acquire
          annotations:
            berth.skaphos.io/lease-name: hourly-rollup
            berth.skaphos.io/mode: startup-gate
        spec:
          restartPolicy: Never
          containers:
            - name: rollup
              image: vendor/rollup:7
```

### What the webhook injects

For `runtime-singleton` with `enforce=probe`, the admitted Pod gains a
`berth-state` `emptyDir`, a blocking `berth-acquire` init container, a
`berth-sidecar` native sidecar, an injected liveness probe on each main
container, and the corresponding volume mounts:

```yaml
spec:
  volumes:
    - { name: berth-state, emptyDir: {} }
  initContainers:
    - name: berth-acquire           # blocking "hold"
      image: ghcr.io/skaphos/berth-acquire:<tag>
      volumeMounts: [{ name: berth-state, mountPath: /berth }]
    - name: berth-sidecar           # native sidecar: renew + enforce
      image: ghcr.io/skaphos/berth-acquire:<tag>
      restartPolicy: Always
      volumeMounts: [{ name: berth-state, mountPath: /berth }]
  containers:
    - name: app
      livenessProbe:
        exec: { command: ["/berth/check", "/berth/healthy"] }
        periodSeconds: 2
        failureThreshold: 1
      volumeMounts: [{ name: berth-state, mountPath: /berth, readOnly: true }]
```

## Authenticating injected Pods

The injected helper inherits the API server URL, CA, and TLS settings from the
operator, but the **bearer token must be present inside the workload Pod** at the
`injection.helper.apiKeyFile` path. The webhook does not mount it for you (it
cannot reach into arbitrary workload namespaces). Mount it yourself — for
example from a Secret you manage in the workload namespace:

```yaml
# in the workload pod template
spec:
  volumes:
    - name: berth-token
      secret:
        secretName: berth-api-key      # data key: token
  containers:
    - name: app
      volumeMounts:
        - { name: berth-token, mountPath: /var/run/berth, readOnly: true }
```

with the operator installed using `injection.helper.apiKeyFile=/var/run/berth/token`.
If the Berth API server runs with `--auth-mode=none`, no token is needed.

> **Known limitation (SKA-444).** Today the webhook sets the
> `BERTH_API_KEY_FILE` / `BERTH_CA_BUNDLE_FILE` environment variables on the
> injected helper containers but does **not** yet mount the token / CA file into
> them, so a Pod-template token mount reaches the workload container but not the
> helper. Until that is fixed, injected pods can authenticate only against an
> API server running `--auth-mode=none`, with TLS satisfied by system trust or
> `injection`-inherited `insecure-skip-verify` (both env-only, no file). Token
> and custom-CA auth for injected pods are tracked in SKA-444.

## Operational caveats

- **Rolling updates can stall up to one TTL (runtime singleton).** With a unique
  per-Pod holder identity, a new rollout Pod cannot acquire until the outgoing
  Pod's lease frees. Graceful termination releases it; `SIGKILL`/OOM/node-drain
  do not, so the new Pod blocks at its init "hold" for up to the TTL. Keep the
  TTL modest (15–30s) for workloads that roll often.
- **Failover RTO ≈ TTL + reacquire interval + kill latency.**
- **More holders, less visibility.** Every opted-in replica is a potential
  holder, so the enforce/re-gate/release code runs in many more places than one
  operator per cluster. This is the central reason injection is a fallback.
- **`startup-gate` provides no runtime guarantee** once the init container exits.
- **`failurePolicy`.** The chart defaults the webhook to `Ignore` so an outage
  cannot block unrelated Pod creation; because the object selector already scopes
  admission to opted-in Pods, set `injection.webhook.failurePolicy=Fail` if you
  want opted-in Pods to refuse to start while the webhook is unavailable.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Opted-in Pod stuck in `Init` | Another candidate holds the lease (expected for a standby), or the helper can't reach/authenticate to the API server | Check `kubectl logs <pod> -c berth-acquire`; verify the token mount and API URL. |
| Main container CrashLoopBackOff right after start (probe mode) | The injected `exec` probe can't run, or the marker is absent | Confirm the helper's static `check` binary is on the shared volume; for images where the probe can't run, switch to `berth.skaphos.io/enforce: signal`. |
| Webhook never fires (no injection) | Label not on the **pod template**, CronJob label at the wrong depth, or the Pod is in a skipped namespace | Put the label on `spec.template.metadata` (CronJob: `spec.jobTemplate.spec.template.metadata`); the release and `injection.controlPlaneNamespaces` namespaces are never mutated. |
| Pod create rejected with a webhook error | A reserved name/volume collision (`berth-acquire`/`berth-sidecar`/`berth-state`), an existing `livenessProbe` in probe mode, or a foreign volume already mounted at the state dir | Read the admission error — the webhook rejects these up front with a clear message; rename the conflicting resource or switch enforcement mode. |
| `x509`/TLS errors calling the webhook | `caBundle` doesn't match the serving cert | Use cert-manager (auto-injects the CA) or set `injection.webhook.tls.caBundle` to the serving CA. |
| Multiple replicas all run | A shared `holder-identity` override in runtime-singleton | Remove the override; let the per-Pod identity default apply. |

## See also

- [Architecture — runtime components and failure behavior](architecture.md)
- [Configuration reference — `injection.*` values](reference/configuration.md)
- [Design: workload gating via init-container injection](design/2026-05-workload-gating-injection-model.md)
