# Quickstart: Validating State-Volume Trust and Marker Freshness

Runnable checks that prove the two defects are closed. Written to be executed
in order — the US1 scenarios must pass before the US2 ones mean anything, since
a freshness rule evaluated by a replaceable binary proves nothing.

See [contracts/admission.md](./contracts/admission.md) and
[contracts/check-cli.md](./contracts/check-cli.md) for the full decision table
and exit codes; this guide does not repeat them.

## Prerequisites

```bash
go -C tools tool task build          # local binaries into bin/
go -C tools tool task test           # baseline: suite green before changes
```

Cluster scenarios need a kind cluster with the operator installed and injection
enabled, as in the existing e2e setup. Unit and admission scenarios need no
cluster.

## 1. The bypass is closed (US1)

**Unit — admission matrix.** The primary evidence for SC-001:

```bash
go test ./internal/webhook/... -run 'TestInject.*StateMount' -v
```

Must cover every row of the admission decision table: writable at another path,
read-only at another path, writable at `StateDir`, injector-owned helper
mounts, both enforce modes, and both admission paths.

**Cluster — the actual exploit.** Apply a gated Deployment whose container adds
a second writable mount of `berth-state`:

```yaml
volumeMounts:
  - { name: berth-state, mountPath: /rw }   # writable — must be refused
```

Expected: the pod is rejected, and the message names the container, the volume,
and `/rw`.

**Cluster — the ephemeral path.** Against an already-running gated pod:

```bash
kubectl debug -it <pod> --image=busybox --target=app -- sh
```

with a writable `berth-state` mount in the generated ephemeral container.
Expected: refused, with a message making clear the pod itself is healthy.

**Cluster — the verifier is protected.** From inside an admitted workload
container, confirm both artifacts resist modification:

```bash
touch /berth/healthy   # expect: read-only file system
cp /bin/true /berth/check
```

This is the check that matters most: `check` is the verifier, and an attacker
who can replace it defeats everything downstream.

## 2. A dead sidecar stops the workload (US2)

**Unit — the freshness boundary.**

```bash
go test ./internal/acquire/... -run 'TestHealthy|TestFreshness' -v
```

Must assert `age == bound` is healthy, `age > bound` is stale, that `Absent`
and `Stale` are distinguishable, and — per FR-009 — that `State.IsHealthy()`
and the `check` subcommand return the same verdict across the table.

**Unit — no false kills.** Simulate a full renew cycle at the default
heartbeat (TTL/3) and at the boundary the config validator permits (heartbeat
just under TTL), asserting the marker never ages past the bound while renewals
succeed. This is SC-003 and FR-007.

**Cluster — the real scenario.** With a healthy gated workload:

```bash
kubectl exec <pod> -c berth-sidecar -- kill 1     # stop renewing
kubectl get pod <pod> -w
```

Expected: the marker ages out and the kubelet kills the main container roughly
one TTL later — with no action from the (dead) sidecar. Confirm the probe
failure event says **stale**, with the observed age, not merely "probe failed".

**Cluster — signal mode.** Repeat against a `enforce: signal` pod whose
container defines no `livenessProbe`; the injected backstop should behave the
same. For a pod whose liveness slot is occupied, confirm the documented
limitation matches observed behavior — that is the FR-010b honesty check, and
it is a real test, not a formality.

## 3. The upgrade is survivable (US3)

**Pre-upgrade inventory.** Run the documented recipe against a cluster that has
**not** been upgraded and confirm it lists exactly the pods scenario 1 would
reject:

```bash
kubectl get pods -A -o json | jq -r '...'   # per upgrade notes (FR-012a)
```

Cross-check by upgrading in a scratch cluster and diffing what actually gets
rejected against what the recipe predicted. A recipe that disagrees with the
webhook is worse than none.

**Rejection counter.**

```bash
kubectl -n <ns> port-forward svc/berth-operator-metrics 8080:8080
curl -s localhost:8080/metrics | grep -i reject
```

Expected: a counter incrementing per rejection, labelled by reason and
admission path, with no pod- or namespace-name labels.

## 4. Gates

```bash
go -C tools tool task test              # full suite
go test -race ./internal/webhook/... ./internal/acquire/...
go -C tools tool task lint              # 0 issues
go -C tools tool task verify-generated  # exit 0
```

Per FR-011 and the constitution's testing constraint, every new regression test
must **fail against the pre-fix code**. Verify by stashing the fix and
re-running, as was done for #97 — a test that passes before the fix proves
nothing.

Chart changes require a `Chart.yaml` version bump (FR-014); `task lint` and the
charts workflow will not catch a missed bump, so check it by hand.
