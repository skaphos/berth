// Package lease implements the core lease coordination logic for Berth.
//
// A lease is represented by [State], which tracks the holder identity,
// namespace, TTL, and last renewal time. The [Manager] orchestrates lease
// operations using a pluggable [Store] backend for persistence.
//
// # Storage
//
// The [Store] interface defines CRUD operations for lease state. Backends
// implement this interface to persist leases in Kubernetes custom resources,
// in-memory stores, or other data stores.
//
// # TTL Enforcement
//
// [TTLEnforcer] runs a background loop that periodically scans for leases
// whose TTL has expired. It uses the configured [Store] to list and evaluate
// lease state on each scan interval.
//
//	enforcer := lease.NewTTLEnforcer(store, 30*time.Second)
//	err := enforcer.Run(ctx, "default")
package lease
