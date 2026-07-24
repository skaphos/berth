# Data Model: Lease Fencing and Isolation Safety

## Key

`(Namespace, Name)` — validated by `lease.ValidateKey` before any lease
logic:

| Field | Rule | Why |
|---|---|---|
| Namespace | RFC 1123 DNS label: lowercase alphanumerics and `-`, 1–63 chars, **no dots** | Matches Kubernetes namespace naming; makes the k8s `<ns>.<name>` object-name encoding injective (first dot = separator) |
| Name | RFC 1123 DNS subdomain: lowercase alphanumerics, `-`, `.`, 1–253 chars | Dots stay legal in names; still a valid k8s object-name segment |
| Combined | `len(Namespace) + 1 + len(Name) ≤ 253` | Encoded k8s Lease object name must be a valid DNS subdomain |

Violation → HTTP 400 naming the field and allowed format; the store never
sees an invalid key.

## Record

| Field | Type | Semantics |
|---|---|---|
| Key | Key | Identity; immutable |
| Holder | string | Holder identity. **`""` marks a tombstone** (no live record can have it: every manager operation requires a non-empty holder) |
| TTL | duration | Expiry window after RenewedAt; meaningless on tombstones |
| AcquiredAt | time | First acquisition by current holder; preserved across renews |
| RenewedAt | time | Last renew/acquire |
| FencingToken | int32 | Holder-transition counter; **per-key high-water mark, never reused** (survives release/expiry/GC via the tombstone). Ceiling `math.MaxInt32`: refuse, never wrap |
| Version | int64 | **New.** Store-maintained write counter and sole CAS predicate: create stores 1, every successful Put stores `expected + 1`. Never reused for a key (records are never hard-deleted) |

## Record states and transitions

```text
                    Acquire (absent)            create: token=1, version=1
   ┌─────────┐  ──────────────────────────►  ┌──────────┐
   │ absent  │                               │   held   │◄─┐
   └─────────┘                               └────┬─────┘  │ Renew (same holder,
                                                  │        │  token match, CAS version):
              Acquire (tombstone or expired):     │        │  token stable, version+1
              token = high-water + 1,             │        │
              CAS on observed version             ▼        │
   ┌───────────┐   Release / GC sweep       ┌──────────┐   │
   │ tombstone │ ◄────────────────────────  │ expired  │   │
   │ holder="" │   (write-back, holder="",  └──────────┘   │
   │ token=HW  │    token preserved,             ▲         │
   └─────┬─────┘    version+1)                   │ TTL elapses (lazy)
         │                                       │
         └── Acquire ────────────────────────────┘
```

- **held → held (renew)**: token unchanged; version bumps → a reclaim racing
  the renew always invalidates exactly one of the two (closes #90).
- **held/expired → tombstone (release or GC)**: token preserved as the
  high-water mark; version bumps. GC's CAS uses the version captured at scan
  — any post-scan write has a higher version, so the sweep can never touch a
  live lease (closes #93).
- **tombstone → held (acquire)**: token = high-water + 1 → strictly greater
  than every token ever issued for the key (closes #92).
- **No transition deletes a record.** Out-of-band deletion (SQL `DELETE`,
  `kubectl delete lease`) forfeits per-key monotonicity; documented caveat.

## Per-backend representation

| Concern | mem | SQL (pg/mysql/sqlite) | k8s |
|---|---|---|---|
| Record | map entry | `berth_leases` row | Lease object |
| Version | struct field | `version bigint NOT NULL DEFAULT 1` column; `SET version = version + 1 WHERE version = ?` | `berth.skaphos.io/version` annotation; update additionally guarded by `metadata.resourceVersion` |
| Tombstone | entry, `Holder ""` | row, `holder = ''` | object kept, `spec.holderIdentity` empty, `leaseTransitions` preserved |
| Token | struct field | `fencing_token integer` | `spec.leaseTransitions` |
| Legacy record (pre-upgrade) | n/a (ephemeral) | column default → Version 1 | missing annotation → read as Version 1; resourceVersion still guards first racing writes |
| Key → object identity | map key | primary key `(namespace, name)` | object name `<ns>.<name>` — injective given key validation |
