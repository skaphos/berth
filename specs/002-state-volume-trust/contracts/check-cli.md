# Contract: `berth-acquire check`

The liveness probe's entrypoint. A user-facing contract in an unusual sense:
its consumer is the kubelet, and its output is what an operator sees in a
probe-failure event.

## Invocation

The webhook templates the command into the injected probe. Today:

```text
/berth/check check /berth/healthy
```

After this change:

```text
/berth/check check /berth/healthy --max-age 30s
```

`--max-age` carries the freshness bound — one lease TTL — resolved by the
webhook, which already knows the TTL when it builds the pod (R1).

## Execution constraints

Non-negotiable, because this runs inside the workload's container:

- No configuration loading, no env dependency, no client construction.
- No network I/O.
- No filesystem dependency beyond the marker path it is given.
- Must run in a distroless image with no shell.

These are why the bound arrives as an argument rather than through config.

## Exit codes and reasons

| Condition | Exit | stderr |
|---|---|---|
| Marker present, age ≤ `--max-age` | 0 | — |
| Marker absent | 1 | Names the path; states the marker is absent |
| Marker present, age > `--max-age` | 1 | States it is **stale**, with observed age and the bound |
| Marker unreadable | 1 | States the verdict is indeterminate, with the error |

Distinguishing *absent* from *stale* is required by FR-011a, not cosmetic. They
are two different operational stories:

- **absent** — the sidecar removed it. The lease was lost. Expected failover.
- **stale** — nobody removed it and nobody refreshed it. The sidecar is dead or
  wedged. An incident.

`check`'s stderr surfaces in the kubelet's probe-failure event, so this is the
signal an operator actually sees.

## Backward and forward compatibility

`--max-age` is optional. Omitted, `check` preserves today's presence-only
behavior.

This matters because the helper image and the probe command are versioned
independently in a running cluster: a pod admitted by an old webhook may run a
new `check`, and a pod admitted by a new webhook may briefly run an old
`check` mid-rollout. Both combinations must degrade predictably:

| Probe spec | `check` binary | Behavior |
|---|---|---|
| No `--max-age` | Old | Presence-only (today) |
| No `--max-age` | New | Presence-only |
| `--max-age` | New | Freshness enforced |
| `--max-age` | Old | Unknown flag — **must not** crash-loop the workload |

The last row is the trap. An old `check` receiving an unknown flag must not
exit non-zero on argument parsing, or the rollout itself kills healthy
workloads. Implementation must confirm the current Cobra configuration's
behavior on unknown flags and, if it errors, treat sequencing (helper image
before webhook config) as a release-ordering requirement rather than an
assumption.

## Agreement with `State.IsHealthy()`

Both call one shared predicate (R5) and must return the same verdict for the
same marker and bound. FR-009 forbids divergence; a test asserts agreement
across a table of ages including the exact boundary (`age == bound` is
healthy).
