# Feature Specification: Lease Fencing and Isolation Safety

**Feature Branch**: `001-lease-fencing-safety`

**Created**: 2026-07-24

**Status**: Draft

**Input**: User description: "Fix the lease-store safety defects tracked in
GitHub issues #90 (fencing-token ABA race grants two concurrent holders on the
failover hot path), #92 (fencing token is not monotonic: resets to 1 after
every release and GC sweep), #93 (TTL garbage collector can delete a live,
just-reacquired lease), and #94 (k8s Lease-name encoding collides two distinct
leases into one object). All four are milestone v0.3.1. Decisions taken with
the maintainer: for #92, make tokens truly monotonic (code matches the
documented downstream-fencing contract) rather than weakening the docs; for
#94, validate lease keys at the API boundary rather than re-encoding stored
object names."

## Context

Berth's product guarantee (`docs/concepts.md`) is that a lease has **at most
one holder at any moment**, and that every lease carries a **monotonically
increasing fencing token** that clients can propagate to downstream systems to
reject stale writers. Four verified defects currently break or undermine that
guarantee:

- **#90 (critical)**: the store's conditional-write predicate is the fencing
  token, but a renewal writes the token back unchanged — so a stale renewal
  and a concurrent reclaim can both succeed against the same prior state,
  yielding two holders that both believe they own the lease, exactly during a
  degraded-holder failover.
- **#92 (high)**: release and garbage collection delete the record, and the
  next acquisition restarts the token at its initial value — tokens repeat
  across lease lifetimes, so the documented downstream-fencing contract does
  not hold and a downstream cannot distinguish a stale writer's token from a
  legitimate new holder's.
- **#93 (high)**: the garbage collector's delete predicate uses the token
  captured at scan time; because tokens repeat (see #92), the predicate can
  match a *different, live* lease created after the scan, and GC deletes it.
- **#94 (high)**: lease keys are encoded into backing-store object names by
  joining namespace and name with a separator that is legal inside the
  segments themselves, so two distinct keys — potentially owned by different
  tenants — can address the same stored object (cross-tenant theft/deletion).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Failover never yields two holders (Priority: P1) — #90

An operator runs an active/standby workload across two clusters coordinated by
one Berth lease. The active holder degrades (process pause, slow network) mid
renewal exactly as the lease expires; the standby reclaims. The system must
guarantee that at most one of the two is told it holds the lease — the stale
renewal must lose, no matter how the writes interleave.

**Why this priority**: this is the core product guarantee, on the exact code
path (failover under degradation) the product exists to make safe. Severity
critical, release blocker.

**Independent Test**: at the store boundary, drive the confirmed interleaving
(renew reads state, stalls; reclaim commits; stale renew's write lands) against
every store backend and assert exactly one winner.

**Acceptance Scenarios**:

1. **Given** holder A's lease has just expired and A has a renewal in flight
   that already passed its pre-write checks, **When** standby B reclaims the
   lease before A's write lands, **Then** A's write is rejected, A is told it
   no longer holds the lease, and B is the sole holder.
2. **Given** the same interleaving with the order of the two landing writes
   reversed, **When** B's reclaim write lands after A's renewal write,
   **Then** exactly one of the two operations succeeds and the other is
   rejected — never both.
3. **Given** any concurrent mix of acquire/renew/release against one key,
   **When** operations interleave arbitrarily (including stalls between read
   and write), **Then** at no instant do two callers both hold unexpired,
   store-confirmed leases on that key.

---

### User Story 2 - Fencing tokens never repeat for a key (Priority: P2) — #92, #93

A user follows the documented guidance: they carry the fencing token to a
downstream system that rejects writes tagged with a token lower than the
highest it has seen. Leases on the key are acquired, released, expire, and are
garbage-collected over time. The downstream comparison must stay correct: a
newly issued token is always strictly greater than every token ever issued for
that key, so stale writers are rejected and legitimate new holders are not.

**Why this priority**: the documented downstream-fencing contract is the
feature's second half; without it, Berth's own state is safe but users'
systems are not, and the docs overclaim (constitution IX/X violation). Fixing
token reuse also removes the root cause that lets GC delete a live lease.

**Independent Test**: exercise release → reacquire and expiry → GC →
reacquire cycles on one key across every backend and assert the sequence of
issued tokens is strictly increasing with no repeats.

**Acceptance Scenarios**:

1. **Given** a lease whose holder held token N, **When** the holder releases
   and any party later reacquires the key, **Then** the new token is strictly
   greater than N.
2. **Given** a lease record that expired and was garbage-collected, **When**
   the key is later reacquired, **Then** the new token is strictly greater
   than every token issued before the collection.
3. **Given** the garbage collector scanned an expired record, **When** the
   key is released and freshly reacquired between the scan and the GC's
   removal attempt, **Then** the removal attempt is rejected and the live
   lease is untouched (#93).
4. **Given** concurrent GC sweeps from multiple replicas plus a concurrent
   reacquisition, **When** they race on the same key, **Then** at most the
   truly-expired state is removed and the live lease survives.

---

### User Story 3 - Distinct keys can never share a stored object (Priority: P3) — #94

Tenant *acme* holds a lease. Tenant *evil*, authenticated but unrelated,
crafts a key whose namespace/name split differs from acme's but whose encoded
storage identity would be the same. The system must make this impossible: the
API rejects keys that could be ambiguous, and every accepted key maps to a
storage identity no other accepted key can produce.

**Why this priority**: cross-tenant interference is a security defect, but it
requires a deliberately crafted key and only affects one backend's encoding;
the fix is a validation rule at the API boundary.

**Independent Test**: request leases with dotted/invalid namespace segments
and assert rejection with a clear error; property-check that no two accepted
keys encode to the same storage identity.

**Acceptance Scenarios**:

1. **Given** any lease endpoint, **When** a request's namespace segment
   contains a dot or otherwise violates the namespace format, **Then** the
   request is rejected with a client-error status and a message naming the
   offending field and the allowed format.
2. **Given** two distinct accepted keys, **When** each is stored in any
   backend, **Then** they occupy distinct stored objects (acme's lease can
   never be read, renewed, stolen, or deleted through evil's key).
3. **Given** an existing valid lease, **When** its holder renews or releases
   it after the validation ships, **Then** the operation succeeds unchanged.

---

### Edge Cases

- Token ceiling: the fencing token has a fixed maximum and refuses to wrap.
  Monotonic persistence across lifetimes consumes the range faster than
  before (every release/reacquire cycle advances it permanently). The ceiling
  behavior (refuse, clear error) must be preserved and documented.
- Storage growth: preserving per-key token history across deletion means
  release/GC can no longer erase all trace of a key. Retained state must be
  bounded (one small tombstone/high-water record per distinct key ever used)
  and the GC must still reclaim the space expired *live* records consumed.
- Upgrade: existing deployments have records written without the new
  conditional-write state. Reads and conditional writes against pre-upgrade
  records must not fail or silently reset safety state.
- Pre-existing keys with dotted namespaces: newly rejected at the API; any
  such stored records (believed nonexistent) become unreachable rather than
  colliding. Their expiry/GC handling must not wedge the sweep.
- Concurrent sweeps from multiple API-server replicas remain safe and
  idempotent under the new predicates.
- A renewal racing a release of the same holder/token must still resolve to
  exactly one outcome (renewed, or released and gone).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every store's conditional write MUST be predicated on state
  that changes on every successful write to the record, such that two
  concurrent conditional writes based on the same observed state can never
  both succeed. (#90)
- **FR-002**: A renewal whose lease was reclaimed (or otherwise re-written)
  between its read and its write MUST fail, and the caller MUST be told it no
  longer holds the lease.
- **FR-003**: For any key, every newly issued fencing token MUST be strictly
  greater than every token previously issued for that key — across renewals,
  releases, expiries, and garbage collection. Token values are never reused
  for the lifetime of the backing store's data. (#92)
- **FR-004**: Voluntary release and TTL garbage collection MUST NOT discard
  the state that enforces FR-003 for the key. Whatever state is retained per
  key is O(1) in size.
- **FR-005**: The TTL garbage collector's removal MUST be predicated on state
  that is never reused across record lifetimes, so a removal prepared from a
  scan can never affect a record written after that scan. (#93)
- **FR-006**: The lease API MUST reject any lease request whose namespace
  segment is not a valid lowercase DNS label (alphanumerics and hyphens, no
  dots), and whose name segment is not a valid DNS subdomain, before the
  request reaches lease logic; the error names the field and allowed format.
  (#94)
- **FR-007**: For all accepted keys, the mapping from key to backing-store
  object identity MUST be injective in every backend.
- **FR-008**: The fencing-token ceiling behavior is preserved: at the maximum
  value, further holder transitions on the key are refused with a clear
  error, never wrapped or reused.
- **FR-009**: The store conformance suite MUST cover the concurrent
  interleavings behind #90 and #93 and the monotonicity property behind #92,
  and every backend (in-memory, Kubernetes, SQL) MUST pass it. Each fixed
  defect gets a regression test that fails on the pre-fix behavior.
- **FR-010**: User-facing documentation (concepts, architecture, API
  reference) MUST describe exactly the delivered guarantees — the
  monotonicity contract as now actually provided, the validation rules for
  keys, and the token-ceiling consequence of permanent monotonicity.
- **FR-011**: Pre-upgrade records (written without the new safety state)
  MUST remain readable, renewable, releasable, and collectable after upgrade,
  with safety guarantees applying from their first post-upgrade write.

### Key Entities

- **Lease record**: the persisted state of one lease — key, holder, TTL,
  acquisition/renewal times, fencing token. Gains whatever per-write state
  FR-001 requires (changes on every write).
- **Fencing token**: per-key, strictly increasing across all holder
  transitions for the life of the store's data; the value users propagate
  downstream.
- **Retained key state (tombstone / high-water mark)**: the O(1) per-key
  state that survives release and GC so FR-003 holds; invisible to normal
  API responses.
- **Lease key**: `(namespace, name)` pair; namespace is a DNS label, name a
  DNS subdomain; injectively mapped to storage identity in every backend.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across all store backends, adversarial interleaving tests
  (including the exact #90 and #93 traces) show at most one confirmed holder
  per key at any instant — zero double-grant outcomes over the full suite,
  race detector enabled.
- **SC-002**: Over any tested sequence of acquire/renew/release/expiry/GC on
  a key, the issued token sequence is strictly increasing with zero repeats.
- **SC-003**: Requests with invalid namespace/name segments are rejected with
  a client error and never reach a store; no two accepted keys map to the
  same stored object in any backend.
- **SC-004**: The full test suite (including the new conformance cases)
  passes race-enabled on all backends; the new regression tests fail when
  run against the pre-fix store logic.
- **SC-005**: Published docs contain no guarantee stronger than what the
  tests demonstrate; the downstream-fencing guidance is again accurate as
  written.

## Assumptions

- No existing deployment uses dotted namespace segments; rejecting them
  needs no data migration. (The k8s backend's own naming makes such keys
  hazardous today, which is the defect.)
- Retaining one O(1) tombstone/high-water record per distinct key ever used
  is an acceptable storage cost; no retention/purge window is required in
  this release. A future purge mechanism would be a separate, documented
  decision because it re-opens the reuse question.
- The store contract (`internal/lease.Store`) is internal to the repository
  and may change shape; the HTTP API surface changes only by adding request
  validation (new client errors), not by changing success responses.
- The Kubernetes backend's optimistic-concurrency primitive
  (resourceVersion) already provides a per-write-changing predicate for
  updates; the defect there is the mem/SQL predicate and the shared
  contract's semantics. Alignment of all three behind one contract is design
  work for the plan phase.
- Issues #90, #92, #93 are one coherent unit of work (same files, same root
  cause family); #94 is independently implementable and testable. All four
  target milestone v0.3.1.
