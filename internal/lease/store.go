package lease

import "context"

// Store defines the persistence interface for lease state. Implementations
// provide the backing storage (Kubernetes CRDs, in-memory, etc.) for the
// lease [Manager] and [TTLEnforcer].
type Store interface {
	// Get retrieves a single lease by namespace and name.
	Get(ctx context.Context, namespace, name string) (*State, error)
	// List returns all leases in the given namespace.
	List(ctx context.Context, namespace string) ([]State, error)
	// Create persists a new lease.
	Create(ctx context.Context, namespace string, lease *State) error
	// Update replaces an existing lease's state.
	Update(ctx context.Context, namespace string, lease *State) error
	// Delete removes a lease by namespace and name.
	Delete(ctx context.Context, namespace, name string) error
}
