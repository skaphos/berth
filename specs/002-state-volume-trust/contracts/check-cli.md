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

The last row looked like a trap, and was investigated rather than assumed
(task T003). Two findings:

1. **An old `check` would indeed fail.** The root command sets
   `SilenceUsage` and `SilenceErrors` (`cmd/berth-acquire/main.go:46-47`),
   which suppress *printing* but do not change parsing — Cobra still returns
   an error for an unknown flag, and the process exits non-zero.
2. **That combination is nevertheless unreachable through normal upgrade.**
   The probe command and the helper image both come from one
   `InjectorConfig` (`internal/webhook/inject.go:22` and `:397`) and are
   applied in a single admission pass. A pod therefore always receives a
   `check` binary and a probe command originating from the same operator
   configuration. Existing pods keep their old pair untouched; new pods get
   the new pair.

So there is **no release-ordering requirement**. The only way to reach the
failing row is to pin `injection.helper.image` to a tag older than the
operator binary, which is a misconfiguration rather than an upgrade path.
Worth a sentence in the configuration reference; not a sequencing constraint.

## Agreement with `State.IsHealthy()`

Both call one shared predicate (R5) and must return the same verdict for the
same marker and bound. FR-009 forbids divergence; a test asserts agreement
across a table of ages including the exact boundary (`age == bound` is
healthy).
