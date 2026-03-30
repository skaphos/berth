package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewTTLEnforcer(t *testing.T) {
	t.Parallel()

	store := &testStore{}
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

	enforcer := NewTTLEnforcer(&testStore{}, 0)
	err := enforcer.Run(ctx, "default")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want %v", err, context.Canceled)
	}
}

func TestTTLEnforcerRunCallsListOnTicker(t *testing.T) {
	t.Parallel()

	store := &testStore{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	enforcer := NewTTLEnforcer(store, 5*time.Millisecond)
	err := enforcer.Run(ctx, "default")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}
	if store.listCalls == 0 {
		t.Fatal("expected List to be called at least once")
	}
}
