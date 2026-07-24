package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

// RunStoreConformance runs the shared lease.Store behavior suite against a
// fresh store for each subtest.
func RunStoreConformance(t *testing.T, newStore func(testing.TB) lease.Store) {
	t.Helper()

	t.Run("PingReportsReachable", func(t *testing.T) {
		store := newStore(t)
		if err := store.Ping(context.Background()); err != nil {
			t.Fatalf("ping on a healthy store: %v", err)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		store := newStore(t)
		if _, err := store.Get(context.Background(), lease.Key{Namespace: "x", Name: "y"}); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("PutCreateThenGetRoundTrips", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()

		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatalf("create: %v", err)
		}
		got, err := store.Get(context.Background(), rec.Key)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Key != rec.Key ||
			got.Holder != rec.Holder ||
			got.TTL != rec.TTL ||
			got.FencingToken != rec.FencingToken ||
			!got.AcquiredAt.Equal(rec.AcquiredAt) ||
			!got.RenewedAt.Equal(rec.RenewedAt) {
			t.Fatalf("round-trip mismatch:\n got = %+v\nwant = %+v", got, rec)
		}
		if got.Version != 1 {
			t.Fatalf("created Version = %d, want 1", got.Version)
		}
	})

	t.Run("PutCreateConflictsWhenPresent", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()
		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(context.Background(), 0, rec); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("PutCASRequiresMatchingVersion", func(t *testing.T) {
		store := newStore(t)
		first := sampleRecord()
		if err := store.Put(context.Background(), 0, first); err != nil {
			t.Fatal(err)
		}

		next := *first
		next.Holder = "cluster-west"
		next.FencingToken = 2
		if err := store.Put(context.Background(), 99, &next); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("stale CAS err = %v, want ErrConflict", err)
		}

		if err := store.Put(context.Background(), 1, &next); err != nil {
			t.Fatalf("matching CAS: %v", err)
		}
		got, err := store.Get(context.Background(), next.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Holder != "cluster-west" || got.FencingToken != 2 {
			t.Fatalf("got holder/token = %s/%d, want cluster-west/2", got.Holder, got.FencingToken)
		}
		if got.Version != 2 {
			t.Fatalf("Version after one update = %d, want 2", got.Version)
		}
	})

	t.Run("PutStoresExpectedPlusOneIgnoringCallerVersion", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()
		rec.Version = 999 // must be ignored on create
		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatal(err)
		}
		got, err := store.Get(context.Background(), rec.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 1 {
			t.Fatalf("created Version = %d, want 1 (caller value ignored)", got.Version)
		}

		upd := *got
		upd.Version = 999 // must be ignored on update
		if err := store.Put(context.Background(), got.Version, &upd); err != nil {
			t.Fatal(err)
		}
		got, err = store.Get(context.Background(), rec.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 2 {
			t.Fatalf("updated Version = %d, want 2 (caller value ignored)", got.Version)
		}
	})

	t.Run("PutCASOnAbsentReturnsConflict", func(t *testing.T) {
		store := newStore(t)
		if err := store.Put(context.Background(), 1, sampleRecord()); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	RunStoreSafetyRegressions(t, newStore)

	t.Run("List", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()
		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatal(err)
		}
		rec2 := *rec
		rec2.Key = lease.Key{Namespace: "tenant-b", Name: "egress"}
		if err := store.Put(context.Background(), 0, &rec2); err != nil {
			t.Fatal(err)
		}

		got, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("List len = %d, want 2", len(got))
		}
		for _, r := range got {
			if r.Version != 1 {
				t.Fatalf("listed Version = %d, want 1", r.Version)
			}
		}
	})

	t.Run("ContextCancellationIsHonored", func(t *testing.T) {
		store := newStore(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := store.Get(ctx, lease.Key{Namespace: "ns", Name: "a"}); err == nil {
			t.Fatal("Get must surface context cancellation")
		}
		if _, err := store.List(ctx); err == nil {
			t.Fatal("List must surface context cancellation")
		}
		if err := store.Put(ctx, 0, sampleRecord()); err == nil {
			t.Fatal("Put must surface context cancellation")
		}
		if err := store.Ping(ctx); err == nil {
			t.Fatal("Ping must surface context cancellation")
		}
	})

	t.Run("ConcurrentAcquireExactlyOneWinner", func(t *testing.T) {
		const holders = 16
		mgr := lease.NewManager(newStore(t))
		key := lease.Key{Namespace: "ns", Name: "a"}

		var (
			wg        sync.WaitGroup
			mu        sync.Mutex
			winners   []string
			winnerTok int32
		)
		wg.Add(holders)
		for i := 0; i < holders; i++ {
			holder := fmt.Sprintf("holder-%02d", i)
			go func(holder string) {
				defer wg.Done()
				res, err := mgr.Acquire(context.Background(), key, holder, time.Minute)
				if err != nil {
					t.Errorf("Acquire(%s): %v", holder, err)
					return
				}
				if res.Acquired {
					mu.Lock()
					winners = append(winners, holder)
					winnerTok = res.FencingToken
					mu.Unlock()
				}
			}(holder)
		}
		wg.Wait()

		if len(winners) != 1 {
			t.Fatalf("winners = %d, want 1", len(winners))
		}
		if winnerTok != 1 {
			t.Fatalf("winner token = %d, want 1", winnerTok)
		}
	})
}

// RunStoreSafetyRegressions runs the store-boundary regression suite for
// issues #90 (fencing-token ABA race), #92 (token reuse after release/GC),
// and #93 (GC deleting a reacquired lease). It is split out from
// [RunStoreConformance] so backends whose test double cannot satisfy the
// full contract (the fake Kubernetes clientset ignores context
// cancellation) still run every safety property.
func RunStoreSafetyRegressions(t *testing.T, newStore func(testing.TB) lease.Store) {
	t.Helper()

	// Regression for issue #90: the CAS predicate must change on every
	// write. A renew-shaped write (same holder, same fencing token) based on
	// an observed state must lose once any other write lands — in either
	// landing order, only one of the two racing writes may succeed.
	t.Run("StaleWriteLosesAfterInterferingWrite", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		// Order 1: reclaim lands first, stale renew must conflict.
		orig := sampleRecord()
		orig.Key = lease.Key{Namespace: "tenant-a", Name: "order-one"}
		if err := store.Put(ctx, 0, orig); err != nil {
			t.Fatal(err)
		}
		observed, err := store.Get(ctx, orig.Key)
		if err != nil {
			t.Fatal(err)
		}
		reclaim := *observed
		reclaim.Holder = "cluster-west"
		reclaim.FencingToken = observed.FencingToken + 1
		if err := store.Put(ctx, observed.Version, &reclaim); err != nil {
			t.Fatalf("reclaim: %v", err)
		}
		staleRenew := *observed // same holder, same token — the pre-fix hole
		staleRenew.RenewedAt = observed.RenewedAt.Add(time.Minute)
		if err := store.Put(ctx, observed.Version, &staleRenew); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("stale renew after reclaim: err = %v, want ErrConflict", err)
		}
		got, err := store.Get(ctx, orig.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Holder != "cluster-west" || got.FencingToken != observed.FencingToken+1 {
			t.Fatalf("reclaimer state clobbered: holder/token = %s/%d", got.Holder, got.FencingToken)
		}

		// Order 2: renew lands first, the reclaim prepared from the same
		// observed state must conflict and re-read.
		orig.Key = lease.Key{Namespace: "tenant-a", Name: "order-two"}
		if err := store.Put(ctx, 0, orig); err != nil {
			t.Fatal(err)
		}
		observed, err = store.Get(ctx, orig.Key)
		if err != nil {
			t.Fatal(err)
		}
		renew := *observed
		renew.RenewedAt = observed.RenewedAt.Add(time.Minute)
		if err := store.Put(ctx, observed.Version, &renew); err != nil {
			t.Fatalf("renew: %v", err)
		}
		staleReclaim := *observed
		staleReclaim.Holder = "cluster-west"
		staleReclaim.FencingToken = observed.FencingToken + 1
		if err := store.Put(ctx, observed.Version, &staleReclaim); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("stale reclaim after renew: err = %v, want ErrConflict", err)
		}
	})

	// Regression for issue #92: tombstoning a record must preserve the
	// fencing token as a per-key high-water mark, so tokens issued across
	// release/reacquire cycles strictly increase and never repeat.
	t.Run("TokensNeverRepeatAcrossTombstoneCycles", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		mgr := lease.NewManager(store)
		key := lease.Key{Namespace: "tenant-a", Name: "ingest"}

		var issued []int32
		for range 3 {
			res, err := mgr.Acquire(ctx, key, "cluster-east", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if !res.Acquired {
				t.Fatal("acquire on a free lease must succeed")
			}
			issued = append(issued, res.FencingToken)
			if err := mgr.Release(ctx, key, "cluster-east", res.FencingToken); err != nil {
				t.Fatal(err)
			}
			rec, err := store.Get(ctx, key)
			if err != nil {
				t.Fatalf("released record must remain as a tombstone: %v", err)
			}
			if !rec.Tombstone() || rec.FencingToken != res.FencingToken {
				t.Fatalf("tombstone = %+v, want holder \"\" and token %d", rec, res.FencingToken)
			}
		}
		for i := 1; i < len(issued); i++ {
			if issued[i] <= issued[i-1] {
				t.Fatalf("issued tokens %v not strictly increasing", issued)
			}
		}
	})

	// Regression for issue #93: a garbage-collection write prepared from a
	// scan must be rejected once the key is released and freshly reacquired,
	// because the scanned version can never gate a second successful write.
	t.Run("ScanStaleGCWriteCannotTouchLiveLease", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)
		mgr := lease.NewManager(store)
		key := lease.Key{Namespace: "tenant-a", Name: "ingest"}

		res, err := mgr.Acquire(ctx, key, "cluster-east", time.Minute)
		if err != nil || !res.Acquired {
			t.Fatalf("acquire: %v acquired=%v", err, res.Acquired)
		}

		// GC scans and captures the record state.
		scanned, err := store.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}

		// Between scan and sweep: release, then a fresh reacquire.
		if err := mgr.Release(ctx, key, "cluster-east", res.FencingToken); err != nil {
			t.Fatal(err)
		}
		res2, err := mgr.Acquire(ctx, key, "cluster-west", time.Minute)
		if err != nil || !res2.Acquired {
			t.Fatalf("reacquire: %v acquired=%v", err, res2.Acquired)
		}

		// The sweep's tombstone write with the scanned version must lose.
		tomb := *scanned
		tomb.Holder = ""
		if err := store.Put(ctx, scanned.Version, &tomb); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("scan-stale GC write: err = %v, want ErrConflict", err)
		}
		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Holder != "cluster-west" || got.FencingToken != res2.FencingToken {
			t.Fatalf("live lease disturbed by GC: %+v", got)
		}
	})

	t.Run("TombstoneRoundTrips", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()
		rec.Holder = ""
		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatalf("create tombstone: %v", err)
		}
		got, err := store.Get(context.Background(), rec.Key)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Tombstone() || got.FencingToken != rec.FencingToken {
			t.Fatalf("tombstone round-trip = %+v, want holder \"\" token %d", got, rec.FencingToken)
		}
		list, err := store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || !list[0].Tombstone() {
			t.Fatalf("List must include tombstones, got %+v", list)
		}
	})
}

func sampleRecord() *lease.Record {
	now := time.Date(2026, 5, 24, 10, 30, 0, 123456000, time.UTC)
	return &lease.Record{
		Key:          lease.Key{Namespace: "tenant-a", Name: "ingest"},
		Holder:       "cluster-east",
		TTL:          30 * time.Second,
		AcquiredAt:   now,
		RenewedAt:    now,
		FencingToken: 1,
	}
}
