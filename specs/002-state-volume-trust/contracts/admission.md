# Contract: Admission

What the gating webhook accepts and rejects after this change. This is a
user-facing contract: it decides whether someone's pod starts.

## Scope

Applies only to pods **subject to injection** (opted in by label/annotation and
resolved to `runtime-singleton`). Pods Berth does not gate are unaffected,
including pods that coincidentally declare a volume named `berth-state`.

## Registered rules

| Path | Operations | Resources | Status |
|---|---|---|---|
| Pod creation | `CREATE` | `pods` | Exists today |
| Ephemeral containers | `CREATE` | `pods/ephemeralcontainers` | **New** (FR-001a) |

`UPDATE` on `pods` is deliberately not registered: `volumes` and
`volumeMounts` are immutable after creation, so it would add webhook load on
every status write without closing any path (R3).

## Decision table

| # | Pod shape | Outcome |
|---|---|---|
| 1 | No author-declared mount of the state volume | **Admit** — injected as today |
| 2 | Author-declared mount, `readOnly: true`, any path | **Admit** — read access is not a bypass |
| 3 | Author-declared mount, writable, at a path other than the injected one | **Reject** — `WritableStateMount` |
| 4 | Author-declared mount, writable, at the injected `StateDir` | **Admit, forced read-only** — preserves today's behavior (`inject.go:489-500`) |
| 5 | A *different* volume mounted at `StateDir` | **Reject** — pre-existing rule, unchanged (`inject.go:253-257`) |
| 6 | Ephemeral container with a writable state-volume mount | **Reject** — `WritableStateMountEphemeral` |
| 7 | Ephemeral container with a read-only state-volume mount | **Admit** — debugging stays possible |
| 8 | Injector-owned helper mounts (writable) | **Admit** — the helpers must write |

Row 4 is worth stating explicitly: the existing behavior *silently repairs* a
writable mount at the exact injected path rather than rejecting it. That stays,
because it is the shape a well-meaning author most often writes and the repair
is unambiguous. Rows 3 and 6 cannot be repaired the same way — silently
flipping a mount the author put somewhere else to read-only would break a
workload that genuinely expected to write there, without telling them.

## Rejection message

Must name the offending **container**, **volume**, and **mount path**, state
why it is refused, and give the resolutions (FR-002). No annotation or
configuration permits the rejected shape — the message must not imply an escape
hatch exists.

Shape, not literal text:

```
cannot inject: container "app" mounts the Berth state volume "berth-state"
writably at "/rw". The state volume is reserved: a writable mount lets the
workload forge the health marker and replace the probe's check binary,
defeating at-most-once enforcement. Either mount it readOnly: true, or use a
separate volume for workload data.
```

For the ephemeral path, the message must additionally make clear the running
pod is healthy and the *debug request* was refused — an operator reading it
should not conclude their workload is failing.

## Failure policy

This contract holds **unconditionally**, including while the webhook is
unreachable. The shipped chart default becomes `failurePolicy: Fail`
(FR-001b), so an outage cannot admit a pod the table above would reject.

The trade is explicit: while the webhook is unreachable, **pod creation is
blocked** for pods matching the selector. The operator is a hard dependency for
pod creation in gated namespaces. This is the intended posture for a control
whose job is at-most-once — a rule that lapses during an outage is not a rule —
but it is a genuine availability cost and operators must be told, not
surprised.

Inspection is unaffected: lease inventory, holders, and status stay readable
throughout an outage, so the system degrades toward refusing writes rather than
toward blindness.

## Compatibility

Breaking, in two separate ways. Both need calling out in upgrade notes.

1. **Rejected pod shapes.** Rows 3 and 6 admit today and will not after
   upgrade, with no opt-out and no grace period, including on re-admission
   events such as eviction and rescheduling. The pre-upgrade inventory recipe
   (FR-012a) exists so operators can find affected workloads before installing.
2. **Failure posture.** Installations currently defaulting to
   `failurePolicy: Ignore` inherit `Fail` on upgrade. A webhook outage that
   previously degraded silently will now block pod creation. This is a behavior
   change for the *cluster*, not only for the workloads in row 1 — it deserves
   its own line in the upgrade notes rather than a footnote in a values table.
