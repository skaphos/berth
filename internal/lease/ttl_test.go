package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewTTLEnforcerPreservesFields(t *testing.T) {
	t.Parallel()

	store := NewMemStore()
	enforcer := NewTTLEnforcer(store, time.Second)
	if enforcer.store != store {
		t.Fatal("store was not preserved")
	}
	if enforcer.scanInterval != time.Second {
		t.Fatalf("scanInterval = %v, want %v", enforcer.scanInterval, time.Second)
	}
}

func TestTTLEnforcerRunReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enforcer := NewTTLEnforcer(NewMemStore(), 0)
	err := enforcer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want %v", err, context.Canceled)
	}
}

func TestTTLEnforcerRunCallsListOnTicker(t *testing.T) {
	t.Parallel()

	store := &countingStore{Store: NewMemStore()}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	enforcer := NewTTLEnforcer(store, 5*time.Millisecond)
	err := enforcer.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}
	if store.listCalls == 0 {
		t.Fatal("expected List to be called at least once")
	}
}

type countingStore struct {
	Store
	listCalls int
}

func (s *countingStore) List(ctx context.Context) ([]Record, error) {
	s.listCalls++
	return s.Store.List(ctx)
}
