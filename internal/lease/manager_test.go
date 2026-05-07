package lease

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestManager(t *testing.T, now time.Time) (*Manager, *MemStore, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: now}
	store := NewMemStore()
	mgr := NewManager(store).WithClock(clock.Now)
	return mgr, store, clock
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func TestNewManagerPreservesStore(t *testing.T) {
	t.Parallel()

	store := NewMemStore()
	mgr := NewManager(store)
	if mgr.store != store {
		t.Fatal("store was not preserved")
	}
	if mgr.now == nil {
		t.Fatal("clock was not initialized")
	}
}

func TestAcquireFreshLease(t *testing.T) {
	t.Parallel()

	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	res, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !res.Acquired {
		t.Fatal("expected Acquired=true on fresh lease")
	}
	if res.Holder != "holder-1" {
		t.Fatalf("Holder = %q, want %q", res.Holder, "holder-1")
	}
	if res.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", res.FencingToken)
	}
	if want := clock.now.Add(30 * time.Second); !res.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
}

func TestAcquireSecondHolderIsRejectedWhileLive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if !first.Acquired {
		t.Fatal("first Acquire should succeed")
	}

	clock.advance(5 * time.Second)
	second, err := mgr.Acquire(ctx, key, "holder-2", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if second.Acquired {
		t.Fatal("second Acquire should be rejected while first is live")
	}
	if second.Holder != "holder-1" {
		t.Fatalf("Holder = %q, want %q", second.Holder, "holder-1")
	}
	if second.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", second.FencingToken)
	}
}

func TestAcquireSameHolderIsRenewalNoTokenBump(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	clock.advance(10 * time.Second)
	second, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if !second.Acquired {
		t.Fatal("same holder should renew successfully")
	}
	if second.FencingToken != first.FencingToken {
		t.Fatalf("FencingToken bumped on renewal: %d → %d", first.FencingToken, second.FencingToken)
	}
	if !second.AcquiredAt.Equal(first.AcquiredAt) {
		t.Fatalf("AcquiredAt drifted: %v → %v", first.AcquiredAt, second.AcquiredAt)
	}
	if want := clock.now.Add(30 * time.Second); !second.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (renewed)", second.ExpiresAt, want)
	}
}

func TestAcquireReclaimsAfterExpiryAndBumpsToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, err := mgr.Acquire(ctx, key, "holder-1", 30*time.Second)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	clock.advance(31 * time.Second) // past TTL
	second, err := mgr.Acquire(ctx, key, "holder-2", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if !second.Acquired {
		t.Fatal("expected reclaim after expiry")
	}
	if second.Holder != "holder-2" {
		t.Fatalf("Holder = %q, want holder-2", second.Holder)
	}
	if second.FencingToken != first.FencingToken+1 {
		t.Fatalf("FencingToken = %d, want %d", second.FencingToken, first.FencingToken+1)
	}
}

func TestRenewExtendsTTL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	acq, err := mgr.Acquire(ctx, key, "h", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	clock.advance(20 * time.Second)
	res, err := mgr.Renew(ctx, key, "h", acq.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !res.Acquired {
		t.Fatal("Renew should succeed for live holder")
	}
	if want := clock.now.Add(30 * time.Second); !res.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", res.ExpiresAt, want)
	}
}

func TestRenewRejectsStaleToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, _ := mgr.Acquire(ctx, key, "holder-1", 5*time.Second)
	clock.advance(10 * time.Second) // expire
	mgr.Acquire(ctx, key, "holder-2", 30*time.Second)

	res, err := mgr.Renew(ctx, key, "holder-1", first.FencingToken, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if res.Acquired {
		t.Fatal("Renew with stale token must not succeed")
	}
}

func TestRenewOnAbsentLeaseReturnsNotAcquired(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	res, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 1, 5*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if res.Acquired {
		t.Fatal("Renew on absent lease must not succeed")
	}
}

func TestReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, _ := newTestManager(t, time.Now())
	key := Key{Namespace: "ns", Name: "a"}

	acq, _ := mgr.Acquire(ctx, key, "h", 30*time.Second)
	if err := mgr.Release(ctx, key, "h", acq.FencingToken); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := mgr.Release(ctx, key, "h", acq.FencingToken); err != nil {
		t.Fatalf("second Release should be no-op: %v", err)
	}
}

func TestReleaseWithStaleTokenReturnsConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mgr, _, clock := newTestManager(t, time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC))
	key := Key{Namespace: "ns", Name: "a"}

	first, _ := mgr.Acquire(ctx, key, "holder-1", 5*time.Second)
	clock.advance(10 * time.Second)
	mgr.Acquire(ctx, key, "holder-2", 30*time.Second)

	err := mgr.Release(ctx, key, "holder-1", first.FencingToken)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestAcquireRejectsEmptyHolderAndZeroTTL(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if _, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 30*time.Second); err == nil {
		t.Fatal("Acquire with empty holder must return error")
	}
	if _, err := mgr.Acquire(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 0); err == nil {
		t.Fatal("Acquire with zero TTL must return error")
	}
}

func TestRenewValidatesArgs(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 1, 30*time.Second); err == nil {
		t.Fatal("Renew with empty holder must return error")
	}
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 0, 30*time.Second); err == nil {
		t.Fatal("Renew with zero token must return error")
	}
	if _, err := mgr.Renew(context.Background(), Key{Namespace: "ns", Name: "a"}, "h", 1, 0); err == nil {
		t.Fatal("Renew with zero TTL must return error")
	}
}

func TestReleaseValidatesArgs(t *testing.T) {
	t.Parallel()

	mgr, _, _ := newTestManager(t, time.Now())
	if err := mgr.Release(context.Background(), Key{Namespace: "ns", Name: "a"}, "", 1); err == nil {
		t.Fatal("Release with empty holder must return error")
	}
}
