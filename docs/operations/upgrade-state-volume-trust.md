# Upgrade notes: reserved state volume and marker freshness

This release changes injection behaviour in two ways that can stop workloads
from starting. Read this before upgrading `berth-operator`.

Both changes exist to close verified defects in `runtime-singleton`
enforcement — a workload could subvert its own at-most-once guarantee, and a
dead sidecar could leave a workload running unleased.

## Breaking change 1 — the state volume is reserved

Pods whose own containers mount the injected `berth-state` volume **writably**
are now refused at admission, at any mount path, in either enforce mode. This
applies to Pod creation and to ephemeral containers (`kubectl debug`).

Previously such a Pod was admitted: the webhook simply added a read-only mount
alongside the writable one. Because both mounts share a single `emptyDir`, the
workload could still recreate the health marker after the sidecar removed it,
and could replace the `check` binary the liveness probe execs.

**There is no opt-out.** A rejected Pod is rejected on every admission event,
including re-admission after eviction or rescheduling.

Read-only mounts remain allowed, and a writable mount at exactly the state dir
is still repaired to read-only on Pod creation rather than refused.

### Find affected workloads before upgrading

Run this against your **un-upgraded** cluster. It lists Pods that declare a
writable mount of `berth-state` in a container the injector does not own:

```bash
kubectl get pods -A -o json | jq -r '
  .items[]
  | select(.metadata.labels["berth.skaphos.io/inject"] == "acquire")
  | . as $pod
  | (.spec.containers + (.spec.initContainers // []) + (.spec.ephemeralContainers // []))[]
  | select(.name != "berth-acquire" and .name != "berth-sidecar")
  | . as $c
  | (.volumeMounts // [])[]
  | select(.name == "berth-state" and (.readOnly != true))
  | "\($pod.metadata.namespace)/\($pod.metadata.name)\tcontainer=\($c.name)\tpath=\(.mountPath)"
'
```

Empty output means nothing in the cluster is affected.

This is a best-effort inventory, not the enforcement path itself — the webhook
is authoritative. Two caveats worth knowing:

- It inspects **running Pods**, so a workload that is scaled to zero, or a
  CronJob that has not fired recently, will not appear. Check your Pod
  templates too.
- A writable mount at exactly the state dir is listed but will **not** be
  rejected; it is repaired. Treat those as informational.

### Fixing an affected workload

Either mark the mount `readOnly: true`, or give the container its own volume
for whatever data it was writing. The state volume has no supported use for
workload data.

## Breaking change 2 — `failurePolicy` now defaults to `Fail`

The chart previously shipped `injection.webhook.failurePolicy: Ignore`.
Installations that never overrode it will inherit `Fail` on upgrade.

**What changes:** while the webhook is unavailable, Pods matching the
injection selectors will not be created. Before, they were created without
injection — unprotected, but running.

This is deliberate. The webhook now carries a safety rule, and a rule that
lapses during an outage is not a rule. But it does make the operator a hard
dependency for Pod creation in gated namespaces, which is worth knowing before
it happens rather than during an incident.

Inspection is unaffected: lease inventory, holders, and status stay readable
throughout an outage.

To keep the previous behaviour:

```yaml
injection:
  webhook:
    failurePolicy: Ignore
```

Understand what that buys and costs: gated workloads keep starting during a
webhook outage, but they start unprotected, and the reserved-volume rule does
not apply to them.

## Non-breaking: the liveness probe now tests freshness

The injected probe fails when the health marker is absent **or** has not been
refreshed within one lease TTL. A correctly-renewing holder never trips it —
validation already requires the heartbeat to be shorter than the TTL.

No action is needed. Pods admitted before the upgrade keep their existing
probe command and behaviour until they are recreated.

The probe failure now says which condition failed, and a stale result reports
the observed age and the bound. See
[Workload gating via injection](../workload-gating-injection.md) for what the
two mean operationally.

## Verifying after upgrade

Confirm the webhook is registered for both admission paths:

```bash
kubectl get mutatingwebhookconfiguration -l app.kubernetes.io/name=berth-operator \
  -o jsonpath='{.items[*].webhooks[*].rules[*].resources}'
# expect: ["pods","pods/ephemeralcontainers"]
```

Watch for the rule firing across the fleet:

```bash
# on the operator's metrics endpoint
berth_webhook_admission_rejections_total{reason="writable_state_mount",path="pods"}
```

A non-zero, climbing counter after upgrade usually means a controller is
retrying a Pod template you missed in the inventory step above.
