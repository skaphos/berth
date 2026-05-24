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

	t.Run("PutCASRequiresMatchingToken", func(t *testing.T) {
		store := newStore(t)
		first := sampleRecord()
		if err := store.Put(context.Background(), 0, first); err != nil {
			t.Fatal(err)
		}

		stale := *first
		stale.Holder = "cluster-west"
		stale.FencingToken = 2
		if err := store.Put(context.Background(), 99, &stale); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("stale CAS err = %v, want ErrConflict", err)
		}

		if err := store.Put(context.Background(), first.FencingToken, &stale); err != nil {
			t.Fatalf("matching CAS: %v", err)
		}
		got, err := store.Get(context.Background(), stale.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Holder != "cluster-west" || got.FencingToken != 2 {
			t.Fatalf("got holder/token = %s/%d, want cluster-west/2", got.Holder, got.FencingToken)
		}
	})

	t.Run("PutCASOnAbsentReturnsConflict", func(t *testing.T) {
		store := newStore(t)
		if err := store.Put(context.Background(), 1, sampleRecord()); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})

	t.Run("DeleteDistinguishesNotFoundAndConflict", func(t *testing.T) {
		store := newStore(t)
		rec := sampleRecord()
		if err := store.Delete(context.Background(), rec.Key, 1); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("missing delete err = %v, want ErrNotFound", err)
		}
		if err := store.Put(context.Background(), 0, rec); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(context.Background(), rec.Key, 99); !errors.Is(err, lease.ErrConflict) {
			t.Fatalf("stale delete err = %v, want ErrConflict", err)
		}
		if err := store.Delete(context.Background(), rec.Key, rec.FencingToken); err != nil {
			t.Fatalf("matching delete: %v", err)
		}
		if _, err := store.Get(context.Background(), rec.Key); !errors.Is(err, lease.ErrNotFound) {
			t.Fatalf("after delete err = %v, want ErrNotFound", err)
		}
	})

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
		if err := store.Delete(ctx, lease.Key{Namespace: "ns", Name: "a"}, 1); err == nil {
			t.Fatal("Delete must surface context cancellation")
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
		for i := range holders {
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
