# Quickstart Validation: Lease Fencing and Isolation Safety

Prerequisites: repo toolchain only (`go -C tools tool task ...`); Docker if
running the SQL integration tests against real Postgres/MySQL.

## 1. Full suite (race-enabled) — the primary gate

```sh
go -C tools tool task test
```

Expected: all packages pass, including the extended
`internal/lease/storetest` conformance run by the mem, k8s-fake, and SQLite
backends. The three new regression families must be present and green:

- `#90` version-CAS interleaving (stale renew-shaped write loses in both
  landing orders),
- `#92` token monotonicity across release/GC/reacquire cycles,
- `#93` GC tombstone write with a scan-stale version is rejected.

Sanity that they are real regression tests: reverting the
`internal/lease` changes (leaving the tests) must fail exactly these cases.

## 2. SQL backends against real engines

```sh
go test ./internal/lease/sqlstore/ -tags=integration -race
```

Expected: conformance + migration idempotency pass on Postgres and MySQL,
including upgrade-in-place: a database created from the pre-change schema
gains the `version` column via `migrate=auto` and legacy rows behave as
`Version = 1` (see [research.md](research.md) D6).

## 3. Key validation end-to-end

Run an API server (`auth-mode=none` is fine) against any backend:

```sh
bin/apiserver --store=mem &
curl -s -X POST localhost:8080/v1alpha1/namespaces/a.b/leases/c/acquire \
  -d '{"holder":"h1","ttlSeconds":30}'         # → 400, names "namespace"
curl -s -X POST localhost:8080/v1alpha1/namespaces/a/leases/b.c/acquire \
  -d '{"holder":"h1","ttlSeconds":30}'         # → 200 (dots legal in name)
```

Expected: the dotted-namespace request never reaches the store; the second
request succeeds; `("a","b.c")` and `("a.b","c")` can no longer share a
backing object because the latter is unrepresentable.

## 4. Monotonicity smoke test over the API

```sh
# acquire → note fencingToken (t) → release → acquire again → token must be t+1
```

Expected: the second acquisition's `fencingToken` is strictly greater than
the first's, even with a GC sweep or apiserver restart (durable backends)
in between.

## 5. Docs and drift gates

```sh
go -C tools tool task verify-generated
go -C tools tool task lint
```

Expected: no generated-artifact drift (no API types changed); lint clean.
Manually verify `docs/concepts.md`, `docs/architecture.md`, and
`docs/reference/api.md` state exactly the delivered guarantees
([contracts/http-api.md](contracts/http-api.md)).
