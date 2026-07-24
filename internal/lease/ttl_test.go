package lease

import (
	"context"
	"errors"
	"sync"
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

// mustGet fetches key or fails the test.
func mustGet(t *testing.T, store Store, key Key) *Record {
	t.Helper()
	rec, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get %v: %v", key, err)
	}
	return rec
}

func TestTTLEnforcerTombstonesExpiredBeyondGrace(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "expired"}
	seedRecord(t, store, key, base, time.Minute) // expires at base+1m

	enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
	// Well past expiry+grace (base+6m).
	enforcer.now = func() time.Time { return base.Add(10 * time.Minute) }

	enforcer.collect(context.Background())

	got := mustGet(t, store, key)
	if !got.Tombstone() {
		t.Fatalf("record after collect = %+v, want tombstone (holder cleared)", got)
	}
	if got.FencingToken != 1 {
		t.Fatalf("tombstone token = %d, want the high-water mark 1 preserved", got.FencingToken)
	}
}

func TestTTLEnforcerSkipsTombstones(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "tombstoned"}
	tomb := &Record{Key: key, Holder: "", RenewedAt: base, FencingToken: 7}
	if err := store.Put(context.Background(), 0, tomb); err != nil {
		t.Fatal(err)
	}

	enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
	enforcer.now = func() time.Time { return base.Add(24 * time.Hour) }

	enforcer.collect(context.Background())

	got := mustGet(t, store, key)
	if got.Version != 1 || got.FencingToken != 7 {
		t.Fatalf("tombstone was rewritten by the sweep: %+v", got)
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

	if got := mustGet(t, store, key); got.Tombstone() {
		t.Fatal("record inside the grace window must not be tombstoned")
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

	if got := mustGet(t, store, key); got.Tombstone() {
		t.Fatal("live lease must not be tombstoned")
	}
}

// TestTTLEnforcerScanStaleWriteSkipsReacquiredLease is the issue #93
// regression at the enforcer level: a record scanned as collectable but
// released and freshly reacquired before the sweep's write must be left
// alone — the version CAS from the scan can no longer match.
func TestTTLEnforcerScanStaleWriteSkipsReacquiredLease(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	mgr := NewManager(store).WithClock(func() time.Time { return base.Add(10 * time.Minute) })
	key := Key{Namespace: "ns", Name: "reacquired"}
	seedRecord(t, store, key, base, time.Minute) // expired long ago

	// Interpose on List so that after the enforcer scans, but before it
	// writes, the lease is released and reacquired fresh.
	interposed := &listInterposingStore{Store: store}
	interposed.afterList = func() {
		if err := mgr.Release(context.Background(), key, "holder-1", 1); err != nil {
			t.Errorf("release: %v", err)
		}
		res, err := mgr.Acquire(context.Background(), key, "holder-2", time.Hour)
		if err != nil || !res.Acquired {
			t.Errorf("reacquire: %v acquired=%v", err, res.Acquired)
		}
	}

	enforcer := NewTTLEnforcer(interposed, time.Second, 5*time.Minute)
	enforcer.now = func() time.Time { return base.Add(10 * time.Minute) }

	enforcer.collect(context.Background())

	got := mustGet(t, store, key)
	if got.Tombstone() || got.Holder != "holder-2" {
		t.Fatalf("sweep disturbed the reacquired lease: %+v", got)
	}
	if got.FencingToken != 2 {
		t.Fatalf("reacquired token = %d, want 2", got.FencingToken)
	}
}

// TestTTLEnforcerConcurrentSweepsAreIdempotent verifies the multi-replica
// property: several enforcers sweeping the same store leave exactly one
// tombstone write behind.
func TestTTLEnforcerConcurrentSweepsAreIdempotent(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemStore()
	key := Key{Namespace: "ns", Name: "contended"}
	seedRecord(t, store, key, base, time.Minute)

	const replicas = 8
	var wg sync.WaitGroup
	wg.Add(replicas)
	for range replicas {
		go func() {
			defer wg.Done()
			enforcer := NewTTLEnforcer(store, time.Second, 5*time.Minute)
			enforcer.now = func() time.Time { return base.Add(10 * time.Minute) }
			enforcer.collect(context.Background())
		}()
	}
	wg.Wait()

	got := mustGet(t, store, key)
	if !got.Tombstone() || got.FencingToken != 1 {
		t.Fatalf("record after concurrent sweeps = %+v, want tombstone with token 1", got)
	}
	if got.Version != 2 {
		t.Fatalf("Version = %d, want 2 (exactly one sweep write)", got.Version)
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

	if got := mustGet(t, store, key); !got.Tombstone() {
		t.Fatalf("record after Run = %+v, want tombstone (ticker should have swept)", got)
	}
}

// listInterposingStore runs afterList once, immediately after a successful
// List, to open a deterministic window between a GC scan and its writes.
type listInterposingStore struct {
	Store
	afterList func()
	once      sync.Once
}

func (s *listInterposingStore) List(ctx context.Context) ([]Record, error) {
	recs, err := s.Store.List(ctx)
	if err == nil && s.afterList != nil {
		s.once.Do(s.afterList)
	}
	return recs, err
}
