# Operator workload admission and cleanup

Operator-managed workloads require `workloadManagement.enabled: true` in the
operator chart and a serving certificate configured through
`injection.webhook.tls`. This TLS configuration is shared with helper injection;
`injection.enabled` can remain false. Lease-only BerthLeases need neither webhook
nor TLS. Chart 0.8.0 defaults to lease-only operation until admission is enabled.

The chart installs separate, fail-closed registrations for managed workloads.
They do not inherit injection's failure policy or opt-in selectors. They inspect
workload and Pod requests across namespaces, except the operator's own namespace,
which cannot contain managed targets. Admission outages can therefore delay
unmanaged controller requests too. The operator's namespace is excluded so its
own recovery does not depend on its webhook. In managed mode, readiness checks
local webhook availability; a central API outage must not remove the admission
service's endpoints or prevent local cleanup.

## Supported resources

Create a BerthLease **before** creating its target, in the application's namespace.
The target must be a new Kubernetes object admitted under the installed guard.
An existing target cannot become admitted through an annotation or UPDATE.
Managed controllers cannot adopt existing unadmitted Pods or controllers;
create their Pods from the admitted template instead.

| Target | Acquire action | Required release action |
| --- | --- | --- |
| `apps/v1` Deployment, StatefulSet, ReplicaSet | `scale.replicas` >= 0 | `scale.replicas: 0` |
| `batch/v1` CronJob | `suspend: false` | `suspend: true` |

Arbitrary target kinds, missing/empty actions and nonzero release replicas are
rejected. Lease name, holder identity, target reference and release action are
immutable. Delete and fully drain the old resource before changing those
identities. There may be only one BerthLease for a target.

Admission stamps the lease name/UID and scheduling gate into controller
Pod templates, including CronJob Job templates. Final target CREATE admission
records the API-assigned target UID in status before allowing storage. Dry runs
do not write status. If a create is subsequently denied or aborted, its unused
UID can be replaced by a later CREATE while no activation/cleanup is active.
Do not grant ordinary workload users write access to BerthLease status.

## Activation and stopping

The operator persists central holder, fencing token and conservative expiry
before activating the target. Every Pod starts with the Berth scheduling gate.
The operator records the stored Pod UID before removing that gate. Only its
configured Kubernetes service account may ungate a registered Pod. Prebound
`spec.nodeName` Pods are rejected; binding requests must carry the Pod UID.

Expiry, loss of ownership and deletion commit a `Stopping` phase in the same
status object used for Pod registration. Optimistic updates prevent a stale
heartbeat or registration from overwriting that transition. Cleanup scales
controllers to zero or suspends CronJobs, suspends/deletes their active Jobs,
and normally deletes their Pods. Even registered Pending/gated Pods must be
confirmed stopped: an earlier ungate request may still be in flight.

The central lease is released and the finalizer removed only after registered
Pods have terminated/disappeared and the observed Jobs are gone. Cleanup errors
retain identity, token and finalizer for retry. Late creates remain gated and
carry the old lease UID; Pod/Job reconciliation removes inert arrivals after the
lease is deleted. Recreating the same lease name cannot authorize old Pods.
During standby, harmless gated Pods can await a future acquisition of the same
lease incarnation; scheduling gates do not provide exactly-once Job semantics.

Central RPCs and local reconciliation have deadlines. The local deadline never
extends past the earlier of the request start plus TTL and the server-reported
expiry. API errors do not preserve workload activation beyond that recorded
deadline. Process termination still takes detection time plus Kubernetes grace
and kubelet processing. A stopped operator, unreachable local API/kubelet, force
deletion, or administrator removal of admission/status protections defeats this
controller-based guarantee. Enforce fencing tokens at downstream storage for
strict rejection of stale writes.

Registration is capped at 1,024 concurrent nonterminal Pod UIDs per BerthLease;
further Pods remain gated until completed entries are pruned. Managed mode adds
Pod/Job watches, direct Kubernetes reads and status writes. Re-measure capacity
for this mode rather than assuming prior lease-only scalability measurements.

## Upgrade and rollback

1. Stop producers and pause/suspend targets. Drain all old Pods and active Jobs,
   including orphaned work; confirm processes have stopped. Do not force-delete
   objects as a substitute for stopping their processes.
2. Delete old workload targets and BerthLeases after draining, allowing existing
   cleanup/finalizers to complete. Preserve the central lease store and fencing
   token history.
3. Upgrade the CRD, operator image and chart together. Helm does not upgrade CRDs
   from a chart's `crds/` directory automatically; apply the reviewed bundled CRD
   explicitly. Enable workload management with a valid TLS Secret/CA bundle or
   cert-manager issuer. Confirm both required webhook registrations are ready.
4. Create replacement BerthLeases in an application namespace, then recreate
   their targets. Confirm target UID admission records, gated Pods, registered
   Pod UIDs and eventual activation. Do not copy old status or UID annotations.

Do not disable admission, downgrade the operator, delete status records or
remove finalizers while managed work is active. Rollback requires another full
drain and recreation under the older deployment contract. Lease-only resources
are unaffected by target migration.

## Verification

Run `go test -race ./internal/operator ./cmd/operator` for lifecycle and admission
regressions. Set `KUBEBUILDER_ASSETS` to Kubernetes envtest assets to include the
real API-server/etcd admission, dry-run, UID and conflict tests.

`TestManagedExecution` is an opt-in execution test for a **disposable kind node**.
It installs test admission and CRDs, rejects normal ReplicaSet adoption of an
unadmitted running orphan, runs Deployment and CronJob containers,
and checks kubelet/runtime termination using `crictl` and the recorded process
PID. It requires `BERTH_TEST_KUBECONFIG` and must never target a shared cluster.
See [ADR-0005](../adr/0005-register-managed-pods-before-scheduling.md) for the
ordering and tradeoffs behind the design.
