// Package lease implements Berth's lease coordination semantics.
//
// A lease is identified by a [Key] and persisted as a [Record]. Each Record
// tracks the current Holder, the TTL, the time the lease was first acquired,
// the time it was last renewed, and a monotonic FencingToken. The
// FencingToken is bumped on each fresh acquisition (after release or
// expiry) and stable across renewals by the same holder; clients must
// include it on every state-changing call so the server can reject stale
// writes from a holder that has lost the lease.
//
// # Manager
//
// [Manager] exposes the three operations that drive coordination:
//
//   - [Manager.Acquire] — create a lease for a new holder, renew an existing
//     hold, or reclaim an expired one.
//   - [Manager.Renew] — extend the TTL of a held lease, validating the
//     caller's fencing token.
//   - [Manager.Release] — voluntarily relinquish a held lease.
//
// # Storage
//
// [Store] is the persistence interface. Implementations must execute Get,
// Put, and Delete linearizably per Key. [MemStore] provides an in-memory
// implementation suitable for tests and single-process deployments.
//
// # TTL
//
// TTL expiry is enforced lazily on each Acquire/Renew. [TTLEnforcer] runs an
// optional background scan loop for hygiene and metrics; it does not gate
// correctness.
package lease
