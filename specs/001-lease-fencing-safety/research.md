# Phase 0 Research: Lease Fencing and Isolation Safety

All decisions below were made against the current code (`internal/lease/*`,
`internal/lease/sqlstore/*`, `internal/api/leases.go` as of `0312bb5`) and the
resolved maintainer decisions recorded in the spec (monotonic tokens in code;
key validation at the API boundary).

## D1 — CAS predicate: store-maintained per-record `Version`

**Decision**: Add `Version int64` to `lease.Record`, maintained by the store:
every successful `Put` stores `expected+1` (create stores 1). The conditional
write predicate becomes the version, not the fencing token:
`Put(ctx, expectedVersion int64, rec *Record)` with `expectedVersion == 0`
meaning "create, only if absent". The fencing token stays a pure domain value
(holder-transition counter) and is no longer overloaded as the concurrency
predicate.

**Per backend**:
- *mem*: compare `cur.Version == expected`, store `rec` with
  `Version = expected + 1` under the existing mutex.
- *SQL*: new `version bigint NOT NULL` column;
  `UPDATE ... SET ..., version = version + 1 WHERE namespace=? AND name=? AND
  version=?`; insert writes version 1. Rows-affected == 0 → `ErrConflict`
  (existing pattern).
- *k8s*: persist the version in a `berth.skaphos.io/version` annotation and
  keep the existing read-check-update flow; the `Update` carries the
  `resourceVersion` from the read, so the kube-apiserver's optimistic
  concurrency still closes the read→write race window (this backend was
  already ABA-safe via resourceVersion; the annotation aligns it with the
  uniform contract so one conformance suite exercises all three).

**Rationale**: #90's root cause is a predicate that does not change on renew.
A version bumped on *every* write makes "two conditional writes from the same
observed state both succeed" impossible by construction, in every backend,
with one uniform, conformance-testable semantic. int64 cannot realistically
exhaust.

**Alternatives considered**:
- *Opaque string revision (k8s resourceVersion pass-through)*: fewer moving
  parts for k8s but pushes backend-specific semantics into the shared
  contract and makes the SQL/mem implementations stringly-typed. Rejected.
- *Bump the fencing token on renew*: repairs the predicate but breaks the
  documented "token stable across renewals" contract and conflates two
  concepts the spec deliberately separates. Rejected.

## D2 — Tombstones instead of deletion; `Store.Delete` removed

**Decision**: Release and GC never delete a record. They write it back as a
**tombstone**: `Holder = ""`, `FencingToken` preserved (the key's high-water
mark), version bumped by the store as any write. `Manager.Acquire` treats a
tombstone as available and issues `FencingToken + 1` (ceiling-checked), CAS'd
on the tombstone's version. `Store.Delete` is removed from the interface and
all backends — with no hard-delete code path, token reuse (#92) and
GC-deletes-live-lease (#93) become structurally impossible rather than merely
guarded.

`Holder == ""` is unambiguous as the tombstone marker: every manager
operation requires a non-empty holder, so no live record can have one.

**Tombstone shape per backend**: mem/SQL — same row with `holder = ''`;
k8s — Lease object kept, `spec.holderIdentity` empty, `leaseTransitions`
preserved.

**Retention**: indefinite, per the spec assumption — one O(1) record per
distinct key ever used. Out-of-band deletion (SQL `DELETE`, `kubectl delete
lease`) forfeits monotonicity for that key; documented, not prevented.

**Rationale**: the spec (FR-003/FR-004) requires per-key state that survives
release and GC. Keeping it *in the record itself* means no second table/object
type, no dual-read on the hot path, and the version counter (D1) also never
resets — which is what makes the GC predicate never-reused (D3).

**Alternatives considered**:
- *Separate high-water table/object + real deletes*: keeps List small but
  adds a second write per release (non-atomic across two objects in k8s),
  a join on acquire, and reopens the version-reset question. Rejected.
- *Doc weakening instead of code*: rejected by maintainer decision (spec
  Input).
- *Keeping `Store.Delete` for future admin purge*: YAGNI, and its existence
  invites exactly the misuse being fixed. Re-adding a purge is a separate,
  documented decision (spec Assumptions).

## D3 — GC becomes a tombstoning sweep with a never-reused predicate

**Decision**: `TTLEnforcer.collect` skips tombstones (`Holder == ""`) and
converts records expired beyond the grace window into tombstones via
`Put(expectedVersion = scanned version)`. A record re-written after the scan
has a higher version → `ErrConflict` → skipped, exactly as the file's comment
already promises. Multi-replica sweeps stay coordination-free and idempotent.

**Rationale**: closes #93 at the root: the predicate (version) is never
reused across the key's lifetime because records are never deleted (D2).

**Alternative considered**: keep Delete but CAS on (token, holder,
renewedAt): narrows but does not eliminate reuse; more fragile predicate.
Rejected.

## D4 — Token monotonicity semantics

**Decision**: unchanged issuance rule (bump only on holder transition,
stable across renews), but the high-water mark now survives release and GC
(D2), so every issued token is strictly greater than every prior token for
that key for the lifetime of the store's data. `math.MaxInt32` ceiling and
refuse-to-wrap behavior are preserved verbatim; the docs gain the note that
the ceiling is now cumulative across the key's lifetime (still ~2.1e9
transitions). The int32 width is kept — it matches
`coordination.k8s.io/v1` `leaseTransitions` (see `Record.FencingToken` doc).

## D5 — Key validation at the API boundary

**Decision**: new `lease.ValidateKey(key) error` in `internal/lease`:
- `Namespace`: RFC 1123 DNS label (lowercase alphanumerics and `-`, ≤ 63
  chars, no dots) — matches Kubernetes namespace naming, so the k8s backend's
  `<namespace>.<name>` encoding becomes injective (the first dot is
  unambiguously the separator).
- `Name`: RFC 1123 DNS subdomain (dots allowed), and
  `len(namespace) + 1 + len(name) ≤ 253` so the encoded Lease object name is
  always valid.
- Implemented with `k8s.io/apimachinery/pkg/util/validation` (already a
  dependency).

Enforced in the three HTTP handlers (`handleAcquire/Renew/Release`) after
authorization (403 ordering unchanged; no pre-auth oracle), returning 400
naming the field and allowed format. `Manager` also calls `ValidateKey` so
non-HTTP callers (tests, future CLI) get the same guarantee — defense in
depth at one shared definition.

**Rationale**: maintainer decision (spec Input). No storage migration; keys
that were previously *dangerous* become *rejected*.

**Alternatives considered**: collision-free (hashed/length-prefixed) object
naming — no API restriction but orphans every existing Lease object and
makes object names unreadable. Rejected by maintainer decision.

## D6 — SQL schema migration (upgrade path, FR-011)

**Decision**: extend `migrate()` to run additive, idempotent statements after
`CREATE TABLE IF NOT EXISTS`: add `version bigint NOT NULL DEFAULT 1`.
- Postgres: `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`.
- MySQL / SQLite: no `IF NOT EXISTS` for columns — probe
  (`information_schema.columns` / `PRAGMA table_info`) or tolerate the
  duplicate-column error; either way idempotent.
- `migrate=off` deployments: document the statement in the SQL backend docs;
  the store fails cleanly (unknown column) rather than corrupting.

Legacy rows surface as `Version = 1` (the default) and join the new CAS on
their first post-upgrade write. Legacy k8s objects lack the version
annotation → read as `Version = 1`; resourceVersion preconditions keep even
the first post-upgrade racing writes safe (D1).

## D7 — Test strategy (FR-009)

**Decision**: extend `internal/lease/storetest` (runs against mem, k8s fake,
and all three SQL dialects) with:
1. **#90 regression**: read record at version v; land an interfering write;
   the stale `Put(expected=v)` must return `ErrConflict` — asserted for both
   orders of the two writes, and specifically for a *same-token renew-shaped*
   write, which is the exact pre-fix hole.
2. **#92 regression**: acquire → release → reacquire cycles (and expired →
   GC-tombstone → reacquire): issued tokens strictly increase, never repeat.
3. **#93 regression**: List-captured version → release + fresh reacquire →
   GC's tombstone write with the stale version must conflict and leave the
   live lease intact.
4. Manager-level interleaving tests using a stalling `Store` wrapper to
   reproduce the issue #90 trace verbatim (renew passes checks, stalls,
   reclaim lands, stale write must lose) — this is the store-boundary
   regression test issue #90 asks for.
5. Property-style check that `ValidateKey`-accepted keys map injectively to
   k8s object names.

Race detector on (repo CI default); benchmarks in `storetest/benchmarks.go`
and `membench_test.go` updated for the new signatures.

## D8 — Documentation impact (FR-010)

- `docs/concepts.md`: fencing section — monotonic across the key's lifetime
  including release/expiry/GC; tombstone behavior; out-of-band deletion
  caveat; cumulative int32 ceiling.
- `docs/architecture.md`: GC is a tombstoning sweep; correctness argument
  (never-reused version predicate) replaces the token-CAS claim.
- `docs/reference/api.md`: key format rules and the new 400 responses;
  fencing token guarantee as delivered.
- `internal/lease/store.go` contract comments rewritten (they are the
  in-repo contract of record).
- No CRD/API-type changes → no generated-artifact churn expected; no Helm
  chart file changes expected (re-check at implement time; bump chart if any
  chart file is touched).

## D9 — Compatibility statement

- HTTP success responses unchanged; only new 400s for invalid keys and the
  pre-existing conflict/`Acquired=false` semantics on lost leases.
- `pkg/client` and the operator need no changes (keys originate from k8s
  namespace/CR names, which already satisfy the validation rules).
- `internal/lease.Store` is repo-internal; its shape change (Version, no
  Delete) is contained to the lease packages and their tests.
