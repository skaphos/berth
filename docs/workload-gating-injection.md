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
   `--auth-mode=none`. Configure `injection.helper.apiKeyFile` together with
   `injection.helper.apiKeySecret` and the **webhook mounts the token into the
   helper containers**. The Secret must exist in every namespace that runs an
   opted-in workload (see
   [Authenticating injected Pods](#authenticating-injected-pods)).

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
| `berth.skaphos.io/signal-target` | **Required** for `signal` in `runtime-singleton` | _(none)_ | `signal` only: scope enforcement to the workload process, matched by `comm` or executable basename (e.g. `nginx`). The webhook **rejects** a `runtime-singleton` Pod that requests `enforce: signal` without it — see the hazard note under [Enforcement](#enforcement-runtime-singleton). |

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

  > **Multi-sidecar hazard.** An unscoped signal enforcer signals **every**
  > process in the shared PID namespace except PID 1 and berth's own — so on
  > lease loss it would also `SIGTERM`/`SIGKILL` co-located sidecars (service
  > mesh, log shippers, metrics agents), not just the gated workload. Because
  > of this, the webhook now **rejects** a `runtime-singleton` Pod that requests
  > `enforce: signal` without `berth.skaphos.io/signal-target`, and a
  > directly-run helper fails the same validation at startup. Set it to the
  > workload's process name (`comm`, or the executable basename; the kernel
  > truncates `comm` to 15 bytes, which the matcher accounts for) to bound the
  > blast radius. `probe` remains the recommended default precisely because it
  > has no such cross-container reach.

Both are **best-effort within a bounded window**: there is detection + kill
latency between lease loss and the container stopping. The **fencing token**
remains the real boundary for any downstream resource that must reject a stale
holder.

### The state volume is reserved

The shared `emptyDir` (`berth-state`) is not general-purpose scratch space. It
holds the holder identity, the fencing token, the health marker — and the
`check` binary the liveness probe execs. Because the *verifier* lives in the
same volume it verifies, a workload with write access could replace `check`
itself, not merely recreate the marker.

Admission therefore **rejects any Pod whose own containers mount `berth-state`
writably**, at any path, in either enforce mode. This applies to Pod creation
and to ephemeral containers (`kubectl debug`) attached to a running gated Pod.

- Read-only mounts are fine — read access is not a bypass.
- The injected helper containers keep write access; they have to.
- A writable mount at exactly the state dir is repaired to read-only on Pod
  creation rather than refused, since that shape is unambiguous.
- There is no annotation or value that permits the rejected shape.

If a workload needs scratch space, give it its own volume.

### Marker freshness

The probe fails when the marker is **absent** *or* when it has not been
refreshed within one lease TTL. Freshness matters because presence alone only
detects enforcement the sidecar performs: if the sidecar is OOM-killed or
crash-looping, nothing removes the marker, and the workload would keep passing
liveness while its lease expired and a standby took over.

The two failures are reported distinctly, because they mean different things:

| Probe failure | Meaning |
|---|---|
| `health marker absent` | The sidecar removed it — the lease was lost. Expected failover. |
| `health marker stale` | Nobody removed it and nobody refreshed it — the sidecar is dead or wedged. An incident. |

The stale message reports the observed age and the bound it exceeded. Both
surface on the Pod's probe-failure event.

A correctly-renewing holder never trips this: validation requires the heartbeat
to be strictly shorter than the TTL, so a healthy sidecar refreshes the marker
well inside the bound.

### Known limitation — `signal` mode and a dead sidecar

`signal` enforcement is carried out by the sidecar, so a dead sidecar cannot
signal anything. Where a main container defines **no** `livenessProbe` of its
own, the webhook injects the freshness probe as a backstop and the behaviour
matches `probe` mode.

Where the container **already defines a `livenessProbe`**, it does not. A
container may have only one, and overwriting the workload's own health check
would be a worse failure than the gap. That case is exactly why `preflight`
steers people to `signal` in the first place, so it is not rare.

**For those Pods, a dead sidecar still leaves the workload running unleased.**
If you need the guarantee unconditionally, use `enforce: probe` and let Berth
own the liveness slot. Closing this for `signal` requires a separate watchdog
mechanism, tracked in
[#142](https://github.com/skaphos/berth/issues/142).

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

The injected helper inherits the API server URL and TLS settings from the
operator. The **webhook mounts the bearer token and CA bundle into the injected
helper containers for you** — you do not mount them in the workload Pod
template, and the workload's own containers never receive them.

Configure the operator with the in-Pod path *and* the source object it should
mount there. Each pair is set together or not at all; the operator refuses to
start with only one half:

```yaml
injection:
  helper:
    # Path the helper reads inside the workload Pod.
    apiKeyFile: /var/run/berth/api-key
    # Secret the webhook mounts at that path.
    apiKeySecret:
      name: berth-api-key
      key: api-key            # default: token

    # Only when TLS is not already satisfied by system trust.
    caBundleFile: /var/run/berth-ca/ca.crt
    caBundleConfigMap:
      name: berth-ca
      key: ca.crt
```

If the Berth API server runs with `--auth-mode=none`, leave all four unset; no
token is needed.

> [!IMPORTANT]
> **The Secret and ConfigMap must already exist in every namespace that runs an
> opted-in workload.** The webhook mounts them by name from the Pod's own
> namespace — it does not create or copy them, because it cannot safely
> replicate credentials into arbitrary namespaces. A Pod whose namespace is
> missing the Secret will fail to start with a mount error, which is a clearer
> failure than the alternative but still a failure.
>
> Distributing that Secret is yours to arrange (an external secrets operator, a
> namespace-provisioning controller, or whatever your platform already uses for
> per-namespace credentials).

The auth mounts are read-only and land only on the `berth-acquire` init
container and the `berth-sidecar` sidecar. They are separate from the shared
`berth-state` volume, which is reserved and read-only for workload containers
— see [The state volume is reserved](#the-state-volume-is-reserved).

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
- **`failurePolicy` defaults to `Fail`.** The webhook enforces a safety rule,
  not only a convenience mutation: it reserves the state volume so a workload
  cannot forge the health marker or replace the probe's `check` binary. Under
  `Ignore` that rule silently lapses for the duration of any webhook outage,
  which is indistinguishable from not having it.

  The cost is real and worth planning for: **while the webhook is unavailable,
  opted-in Pods will not start.** The operator becomes a hard dependency for
  Pod creation in gated namespaces. The namespace and object selectors scope
  this to Pods opting into injection, and read paths — lease inventory,
  holders, status — are unaffected.

  Set `injection.webhook.failurePolicy=Ignore` only if you would rather a gated
  workload start unprotected than not start at all. Note that this reverses the
  guarantee: enforcement is then best-effort with respect to webhook
  availability.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Opted-in Pod stuck in `Init` | Another candidate holds the lease (expected for a standby), or the helper can't reach/authenticate to the API server | Check `kubectl logs <pod> -c berth-acquire`; verify the API URL, and that the token Secret named by `injection.helper.apiKeySecret` exists **in this Pod's namespace**. |
| Opted-in Pod fails to start with a volume/mount error naming a Secret or ConfigMap | The token Secret or CA ConfigMap does not exist in the workload's namespace | The webhook mounts them by name from the Pod's own namespace and never copies them there; create the object in that namespace (see [Authenticating injected Pods](#authenticating-injected-pods)). |
| Main container CrashLoopBackOff right after start (probe mode) | The injected `exec` probe can't run, or the marker is absent | Confirm the helper's static `check` binary is on the shared volume; for images where the probe can't run, switch to `berth.skaphos.io/enforce: signal`. |
| Webhook never fires (no injection) | Label not on the **pod template**, CronJob label at the wrong depth, or the Pod is in a skipped namespace | Put the label on `spec.template.metadata` (CronJob: `spec.jobTemplate.spec.template.metadata`); the release and `injection.controlPlaneNamespaces` namespaces are never mutated. |
| Pod create rejected with a webhook error | A reserved name/volume collision (`berth-acquire`/`berth-sidecar`/`berth-state`), an existing `livenessProbe` in probe mode, a foreign volume already mounted at the state dir, or a **writable mount of the reserved `berth-state` volume** | Read the admission error — the webhook rejects these up front with a clear message naming the container, volume, and path; rename the conflicting resource, mark the mount `readOnly: true`, use a separate volume, or switch enforcement mode. |
| `kubectl debug` refused on a healthy gated Pod | The debug container mounts `berth-state` writably | Attach with the mount marked `readOnly: true`, or without mounting `berth-state` at all — the Pod itself is unaffected. |
| `x509`/TLS errors calling the webhook | `caBundle` doesn't match the serving cert | Use cert-manager (auto-injects the CA) or set `injection.webhook.tls.caBundle` to the serving CA. |
| Multiple replicas all run | A shared `holder-identity` override in runtime-singleton | Remove the override; let the per-Pod identity default apply. |

## See also

- [Architecture — runtime components and failure behavior](architecture.md)
- [Configuration reference — `injection.*` values](reference/configuration.md)
- [Design: workload gating via init-container injection](design/2026-05-workload-gating-injection-model.md)
