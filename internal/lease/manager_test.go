package lease

import (
	"context"
	"testing"
)

type testStore struct {
	listCalls int
	listFn    func(context.Context, string) ([]State, error)
}

func (s *testStore) Get(ctx context.Context, namespace, name string) (*State, error) {
	_ = ctx
	_ = namespace
	_ = name
	return nil, nil
}

func (s *testStore) List(ctx context.Context, namespace string) ([]State, error) {
	s.listCalls++
	if s.listFn != nil {
		return s.listFn(ctx, namespace)
	}
	return nil, nil
}

func (s *testStore) Create(ctx context.Context, namespace string, lease *State) error {
	_ = ctx
	_ = namespace
	_ = lease
	return nil
}

func (s *testStore) Update(ctx context.Context, namespace string, lease *State) error {
	_ = ctx
	_ = namespace
	_ = lease
	return nil
}

func (s *testStore) Delete(ctx context.Context, namespace, name string) error {
	_ = ctx
	_ = namespace
	_ = name
	return nil
}

func TestNewManager(t *testing.T) {
	t.Parallel()

	store := &testStore{}
	manager := NewManager(store)

	if manager.store != store {
		t.Fatal("store was not preserved")
	}
}
