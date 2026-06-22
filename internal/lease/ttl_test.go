package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewTTLEnforcerAppliesDefaults(t *testing.T) {
	t.Parallel()

	store := NewMemStore()
	enforcer := NewTTLEnforcer(store, 0, 0)
	if enforcer.store != store {
		t.Fatal("store was not preserved")
	}
	if enforcer.interval != defaultGCInterval {
		t.Fatalf("interval = %v, want %v", enforcer.interval, defaultGCInterval)
	}
	if enforcer.grace != defaultGCGrace {
		t.Fatalf("grace = %v, want %v", enforcer.grace, defaultGCGrace)
	}
}

func TestTTLEnforcerRunReturnsContextError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	enforcer := NewTTLEnforcer(NewMemStore(), time.Second, time.Minute)
	err := enforcer.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want %v", err, context.Canceled)
	}
}

// seedRecord inserts a fresh record (fencing token 1) so collect has
// something to consider.
func seedRecord(t *testing.T, store Store, key Key, renewedAt time.Time, ttl time.Duration) {
	t.Helper()
	rec := &Record{
		Key:          key,
		Holder:       "holder-1",
		TTL:          ttl,
		AcquiredAt:   renewedAt,
		RenewedAt:    renewedAt,
		FencingToken: 1,
	}
	if err := store.Put(context.Background(), 0, rec); err != nil {
		t.Fatalf("seed Put: %v", err)
	}
}

func TestTTLEnforcerCollectsExpiredBeyondGrace(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "expired"}
	seedRecord(t, store, key, base, time.Minute) // expires at base+1m

	enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
	// Well past expiry+grace (base+6m).
	enforcer.now = func() time.Time { return base.Add(10 * time.Minute) }

	enforcer.collect(context.Background())

	if _, err := store.Get(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after collect = %v, want ErrNotFound (record should be GC'd)", err)
	}
}

func TestTTLEnforcerSkipsExpiredWithinGrace(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "recent"}
	seedRecord(t, store, key, base, time.Minute) // expires at base+1m

	enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
	// Expired (after base+1m) but still inside the grace window (< base+6m).
	enforcer.now = func() time.Time { return base.Add(2 * time.Minute) }

	enforcer.collect(context.Background())

	if _, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("Get after collect = %v, want record retained within grace", err)
	}
}

func TestTTLEnforcerSkipsLiveLease(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "live"}
	seedRecord(t, store, key, base, time.Minute) // expires at base+1m

	enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
	// Before expiry: clearly not eligible.
	enforcer.now = func() time.Time { return base.Add(30 * time.Second) }

	enforcer.collect(context.Background())

	if _, err := store.Get(context.Background(), key); err != nil {
		t.Fatalf("Get after collect = %v, want live lease retained", err)
	}
}

func TestTTLEnforcerRunCollectsOnTicker(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "ticked"}
	seedRecord(t, store, key, base, time.Minute)

	enforcer := NewTTLEnforcer(store, 5*time.Millisecond, time.Minute)
	enforcer.now = func() time.Time { return base.Add(time.Hour) }

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := enforcer.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want context.DeadlineExceeded", err)
	}

	if _, err := store.Get(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Run = %v, want ErrNotFound (ticker should have GC'd)", err)
	}
}
