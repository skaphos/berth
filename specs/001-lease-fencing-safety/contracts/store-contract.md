# Contract: `internal/lease.Store`

The store contract of record lives in `internal/lease/store.go` doc comments;
this file is the design-time statement the implementation must match.

## Interface (after this feature)

```go
type Store interface {
    Ping(ctx context.Context) error
    Get(ctx context.Context, key Key) (*Record, error)
    List(ctx context.Context) ([]Record, error)

    // Put atomically writes record. If expectedVersion is 0 the write
    // succeeds only when no record exists for record.Key and stores
    // Version 1; otherwise it succeeds only when the current record's
    // Version equals expectedVersion, and stores expectedVersion+1.
    // Returns ErrConflict otherwise. The caller's record.Version input is
    // ignored; the store owns the field.
    Put(ctx context.Context, expectedVersion int64, record *Record) error

    // Delete is REMOVED. Records are never hard-deleted by Berth; release
    // and GC write tombstones (Holder == "", FencingToken preserved).
}
```

## Semantics every backend must provide (conformance-tested)

1. **Linearizable per-key** Get/Put (unchanged baseline).
2. **Version discipline**: create → 1; successful conditional write →
   `expected + 1`; `Get`/`List` return the stored version.
3. **Uniqueness of success**: for any observed version v of a key, at most
   one `Put(expected=v)` can ever succeed — regardless of payload, holder,
   token, or timing. (The #90 regression: a same-token renew-shaped write and
   a reclaim write racing from the same observed state → exactly one wins,
   in both landing orders.)
4. **No reuse**: a version value, once superseded, never gates a successful
   write again for that key (no hard-delete path exists to reset it). Same
   for fencing tokens: the stored token never decreases across any sequence
   of writes (#92/#93 regressions).
5. **Tombstone round-trip**: a record with `Holder == ""` is stored, listed,
   and read back like any record.
6. **Errors**: `ErrNotFound` from Get on absent keys; `ErrConflict` on any
   predicate miss (create-on-existing, stale version); context cancellation
   surfaces.

## Manager behavior on top of the contract

| Operation | Read state | Write |
|---|---|---|
| Acquire, absent | — | `Put(0, {holder, token 1})` |
| Acquire, tombstone or expired | version v, high-water token t | `Put(v, {holder, token t+1})`; refuse if `t == math.MaxInt32` |
| Acquire/Renew, held by caller, unexpired | version v, token t | `Put(v, {token t, new RenewedAt})` — token stable |
| Renew, lost (holder/token mismatch, expired, absent, tombstone) | — | none; `Acquired=false` with current state |
| Release, held by caller (token match) | version v, token t | `Put(v, {Holder: "", token t})` — tombstone; `ErrConflict` loss races resolve to nil (idempotent) |
| GC sweep, expired > grace, not tombstone | scanned version v | `Put(v, tombstone)`; `ErrConflict`/late races skipped |
