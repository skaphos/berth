package lease

import "context"

type Store interface {
	Get(ctx context.Context, namespace, name string) (*State, error)
	List(ctx context.Context, namespace string) ([]State, error)
	Create(ctx context.Context, namespace string, lease *State) error
	Update(ctx context.Context, namespace string, lease *State) error
	Delete(ctx context.Context, namespace, name string) error
}
