# Getting Started

This tutorial takes you from an empty machine to a **single workload that runs
in exactly one of two clusters at a time, with automatic failover** — Berth's
core job. You will stand up a throwaway three-cluster topology with `kind`,
deploy the same workload and the same `BerthLease` to two "runner" clusters,
watch one win, and then force a failover and watch the other take over.

Everything here uses the repository's local end-to-end harness, so the commands
are copy-pasteable and known to work.

!!! warning "This is a learning sandbox, not a production install"
    The harness uses self-signed TLS, a `NodePort` Service, static-key auth, and
    `insecureSkipVerify` on the operators. It exists to demonstrate behavior on
    one machine. For real deployments, follow the Helm instructions in the
    [project README](https://github.com/skaphos/berth#quickstart) and the
    [Configuration reference](reference/configuration.md) instead.

## What you will build

```
                 berth-e2e-coord  (coordination cluster)
                 ┌──────────────────────────────────┐
                 │  berth-apiserver  (k8s Lease store)│
                 └──────────────┬─────────────────────┘
                                │  acquire / renew / release (HTTPS)
              ┌─────────────────┴──────────────────┐
              │                                      │
   berth-e2e-east                          berth-e2e-west
   ┌────────────────────┐                  ┌────────────────────┐
   │ berth-operator      │                 │ berth-operator      │
   │   --cluster-id=     │                 │   --cluster-id=     │
   │     cluster-east    │                 │     cluster-west    │
   │ demo-app Deployment │                 │ demo-app Deployment │
   └────────────────────┘                  └────────────────────┘
```

Both runner clusters get an identical `BerthLease` named `demo-lease` and an
identical `demo-app` Deployment. The two operators compete for one central
lease. The winner scales its `demo-app` to 2 replicas; the loser keeps `demo-app`
at 0. If the winner disappears, the lease expires and the standby takes over.

## Prerequisites

You need these tools on your `PATH`:

| Tool | Why |
| --- | --- |
| [`kind`](https://kind.sigs.k8s.io/) | Creates the three local clusters. |
| `kubectl` | Inspects the clusters. |
| [`helm`](https://helm.sh/) | Installs the API server and operator charts. |
| `docker` | Builds the Berth images and runs the `kind` nodes. |
| `openssl` | Mints the harness's throwaway TLS material. |
| Go 1.26+ | Runs the `task` build orchestration (declared as a Go tool, so no separate install). |

Clone the repository and work from its root:

```bash
git clone https://github.com/skaphos/berth.git
cd berth
```

!!! note "Resource footprint"
    Three `kind` clusters plus image builds run comfortably in ~4 CPU and ~6 GB
    of memory. Make sure Docker has that headroom before you start.

## Step 1 — Bring up the topology

One command creates all three clusters, builds and loads the Berth images, mints
TLS material, and installs the API server and both operators:

```bash
go -C tools tool task e2e-up
```

This takes about two minutes. When it finishes it prints the discovered API
server URL and the demo API key:

```text
=== topology ready ===
API_URL=https://172.18.0.4:3xxxx
EAST_API_KEY=cluster-east
WEST_API_KEY=cluster-west
```

Three `kind` contexts now exist. Confirm they are reachable:

```bash
kubectl --context kind-berth-e2e-coord get nodes
kubectl --context kind-berth-e2e-east  get nodes
kubectl --context kind-berth-e2e-west  get nodes
```

!!! tip "What just got installed"
    - **coord** runs `berth-apiserver` in the `berth-system` namespace with the
      Kubernetes `Lease` backend (`--store-backend=k8s`), storing authoritative
      lease state as `coordination.k8s.io/Lease` objects in the
      `berth-coordination` namespace.
    - **east** and **west** each run a `berth-operator` with a distinct
      `--cluster-id` (`cluster-east` / `cluster-west`), pointed at the coord API
      server.
    - each operator authenticates with **its own static key whose key id equals
      its `--cluster-id`**, so it is its own tenant. With `static-keys` auth the
      tenant is the key id and lease calls are holder-authorized — the operator's
      holder (its `--cluster-id`) must be owned by that tenant, or acquires are
      rejected `403`. The two clusters are therefore distinct tenants contending
      for one lease name, the cross-cluster model from
      [Authorization](architecture.md#authorization).

Check that the control-plane pods are running:

```bash
kubectl --context kind-berth-e2e-coord -n berth-system get pods
kubectl --context kind-berth-e2e-east  -n berth-system get pods
kubectl --context kind-berth-e2e-west  -n berth-system get pods
```

## Step 2 — Deploy the workload to both runner clusters

`demo-app` is a trivial Deployment (the `pause` container — it starts fast and
does nothing) that starts at **0 replicas**. Berth, not you, decides when it
scales up. Apply it to **both** runner clusters:

```bash
cat <<'EOF' | tee /tmp/demo-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-app
  namespace: berth-system
spec:
  replicas: 0
  selector:
    matchLabels:
      app: demo-app
  template:
    metadata:
      labels:
        app: demo-app
    spec:
      containers:
        - name: pause
          image: registry.k8s.io/pause:3.9
      terminationGracePeriodSeconds: 1
EOF

kubectl --context kind-berth-e2e-east apply -f /tmp/demo-app.yaml
kubectl --context kind-berth-e2e-west apply -f /tmp/demo-app.yaml
```

!!! note "Why `berth-system`?"
    This tutorial reuses the namespace the harness already created. In a real
    deployment you would target your application's own namespace; the operator
    watches all namespaces, and just needs RBAC to scale the target there.

## Step 3 — Apply the same lease to both clusters

The `BerthLease` is the declarative request: *"keep `demo-app` at 2 replicas on
whichever cluster currently holds `demo-lease`, and at 0 everywhere else."*
Apply the **identical** manifest to both runner clusters:

```bash
cat <<'EOF' | tee /tmp/demo-lease.yaml
apiVersion: berth.skaphos.io/v1alpha1
kind: BerthLease
metadata:
  name: demo-lease
  namespace: berth-system
spec:
  leaseName: demo-lease
  # Placeholder — each operator overrides this with its own --cluster-id.
  holderIdentity: unset
  ttlSeconds: 15
  heartbeatIntervalSeconds: 5
  semantics: at-most-once
  target:
    apiVersion: apps/v1
    kind: Deployment
    name: demo-app
  acquireAction:
    scale:
      replicas: 2
  releaseAction:
    scale:
      replicas: 0
EOF

kubectl --context kind-berth-e2e-east apply -f /tmp/demo-lease.yaml
kubectl --context kind-berth-e2e-west apply -f /tmp/demo-lease.yaml
```

The two operators now race to acquire `demo-lease` from the central API server.
The race resolves within a few heartbeats.

## Step 4 — Observe single-active

Watch the replica counts on both clusters. Exactly one side goes to **2**; the
other stays at **0**:

```bash
watch -n1 '
  echo "east: $(kubectl --context kind-berth-e2e-east -n berth-system get deploy demo-app -o jsonpath='{.spec.replicas}')"
  echo "west: $(kubectl --context kind-berth-e2e-west -n berth-system get deploy demo-app -o jsonpath='{.spec.replicas}')"
'
```

Within ~15 seconds you should see a stable split such as `east: 2 / west: 0`.
That is the whole guarantee: one active workload across two clusters.

Confirm it from the lease's own status. The holder reports `leaseState: held`
and names itself in `currentHolder`; the standby reports `leaseState: waiting`:

```bash
kubectl --context kind-berth-e2e-east -n berth-system \
  get berthlease demo-lease -o jsonpath='{.status.leaseState}{" holder="}{.status.currentHolder}{"\n"}'
kubectl --context kind-berth-e2e-west -n berth-system \
  get berthlease demo-lease -o jsonpath='{.status.leaseState}{" holder="}{.status.currentHolder}{"\n"}'
```

You can also peek at the authoritative record in the coordination cluster — the
central `Lease` object that both operators are contending for:

```bash
kubectl --context kind-berth-e2e-coord -n berth-coordination get leases
```

## Step 5 — Force a failover

Now simulate the holder disappearing. Find the holder from Step 4, then scale
**its operator** to zero so nothing renews the lease. (Scale the operator
Deployment, not a single pod — a deleted pod is rescheduled and resumes renewing
inside the TTL, so the lease never expires.)

Assuming `east` is the current holder:

```bash
kubectl --context kind-berth-e2e-east -n berth-system scale deploy/berth-operator --replicas=0
```

Keep the watch from Step 4 running. The lease expires after its 15-second TTL,
and the standby (`west`) acquires it and scales **its** `demo-app` to 2 — usually
within ~25 seconds of the holder going quiet:

```text
east: 2    # stale — no operator left to scale it down
west: 2    # new holder took over
```

!!! note "Why the old holder stays at 2"
    With its operator scaled to zero, nothing on `east` is left to apply the
    release action, so `demo-app` lingers at 2 until its operator returns. The
    *guarantee* Berth makes is about which cluster **holds the lease**, not about
    instantly tidying an abandoned target. The next step shows the cleanup.

## Step 6 — Rejoin and converge

Bring the original operator back:

```bash
kubectl --context kind-berth-e2e-east -n berth-system scale deploy/berth-operator --replicas=1
```

On its first reconcile, the rejoining operator sees that it no longer holds the
lease (`Acquired=false`) and applies the release action, scaling `east`'s
`demo-app` back to 0. The topology converges to the new steady state:

```text
east: 0    # rejoined, observed it lost, scaled down
west: 2    # still the holder
```

That is a full failover cycle: holder loss → standby takeover → original rejoin
→ convergence.

## Step 7 — Tear it all down

```bash
go -C tools tool task e2e-down
```

This deletes the three `kind` clusters and the harness's temporary files.

## What just happened

- A **lease** is a named, TTL-bounded claim. Only one holder owns it at a time.
- Each **operator** competes for the lease on behalf of its cluster, identified
  by its `--cluster-id`. The holder renews on the heartbeat interval; if it stops
  renewing, the lease expires after the TTL and a standby can win.
- A **`BerthLease`** ties that claim to a workload: `acquireAction` runs when the
  cluster wins, `releaseAction` when it loses. Here both were `scale`, but
  `suspend` (for CronJobs) works the same way.
- Failover time is bounded by **TTL + the standby's reacquire interval**. With
  `ttlSeconds: 15` and `heartbeatIntervalSeconds: 5`, takeover lands in roughly
  25 seconds.

## Next steps

- **Understand the model** — [Concepts](concepts.md) explains holder identity,
  fencing tokens, TTL/heartbeat, lease semantics, and the topology trade-offs.
- **Run it for real** — the
  [project README quickstart](https://github.com/skaphos/berth#quickstart) shows
  the production Helm installs for the API server and operator, and the
  [Configuration reference](reference/configuration.md) covers every flag and
  Helm value.
- **Go deeper on behavior** — [Architecture](architecture.md) covers the lease
  model, holder identity, fencing tokens, storage backends, authorization, and
  failure behavior.
- **Gate a workload you cannot change** — see
  [Workload gating via injection](workload-gating-injection.md).

