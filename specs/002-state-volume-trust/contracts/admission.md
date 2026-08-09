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

## Failure policy dependency

This contract holds **only while the webhook is reachable**. The shipped chart
default is `failurePolicy: Ignore`
(`deploy/helm/berth-operator/values.yaml:157`), so during a webhook outage
every row above degrades to "admit". Operators requiring the guarantee
unconditionally must set `failurePolicy: Fail` and accept that an outage blocks
pod creation in scope. See R4 and issue #103.

## Compatibility

Breaking. Rows 3 and 6 admit today and will not after upgrade, with no opt-out
and no grace period, including on re-admission events such as eviction and
rescheduling. The pre-upgrade inventory recipe (FR-012a) exists so operators
can find affected workloads before installing.
