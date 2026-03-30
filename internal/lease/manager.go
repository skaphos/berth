package lease

import "time"

// State represents the in-memory state of a single lease.
type State struct {
	// Name is the lease identifier within its namespace.
	Name string
	// Namespace is the Kubernetes namespace containing the lease.
	Namespace string
	// Holder is the identity of the entity holding the lease.
	Holder string
	// TTL is the time-to-live in seconds before the lease expires.
	TTL int32
	// RenewTime is the last time the lease was renewed.
	RenewTime time.Time
}

// Manager orchestrates lease operations using a pluggable [Store] backend.
type Manager struct {
	store Store
}

// NewManager creates a Manager backed by the given [Store].
func NewManager(store Store) *Manager {
	return &Manager{store: store}
}
