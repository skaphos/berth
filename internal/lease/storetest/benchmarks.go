package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

// RunStoreBenchmarks runs the shared lease-workload benchmark suite against a
// fresh store produced by newStore. It is the throughput counterpart to
// [RunStoreConformance]: because *testing.B satisfies testing.TB, the same
// factory used for conformance works here unchanged.
//
// The three sub-benchmarks isolate the load shapes the sizing model in
// docs/operations/scalability.md predicts, at the raw [lease.Store] /
// [lease.Manager] layer with no HTTP, auth, controller, or network noise:
//
//   - AcquireParallel   — N goroutines doing Get+CAS on one hot key; the cost
//     of compare-and-swap under contention (the cold-start / standby herd).
//   - RenewSteadyState  — many independent holders renewing distinct keys at
//     full speed; the sustained renew ceiling that bounds steady-state QPS.
//   - FailoverFanout     — an expired lease with M standbys racing to reclaim;
//     the acquire-after-expiry path exercised on region-pair failover.
func RunStoreBenchmarks(b *testing.B, newStore func(testing.TB) lease.Store) {
	b.Helper()

	b.Run("AcquireParallel", func(b *testing.B) {
		benchmarkAcquireParallel(b, newStore)
	})
	b.Run("RenewSteadyState", func(b *testing.B) {
		benchmarkRenewSteadyState(b, newStore)
	})
	b.Run("FailoverFanout", func(b *testing.B) {
		benchmarkFailoverFanout(b, newStore)
	})
}

// benchmarkAcquireParallel hammers a single key with parallel Get+CAS cycles,
// bumping the fencing token on every successful write so concurrent writers
// genuinely collide. ErrConflict is the signal being measured — a losing
// writer simply re-reads on its next iteration — so it is not an error.
func benchmarkAcquireParallel(b *testing.B, newStore func(testing.TB) lease.Store) {
	store := newStore(b)
	ctx := context.Background()
	key := lease.Key{Namespace: "bench", Name: "contended"}

	seed := sampleRecord()
	seed.Key = key
	if err := store.Put(ctx, 0, seed); err != nil {
		b.Fatalf("seed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cur, err := store.Get(ctx, key)
			if err != nil {
				b.Errorf("get: %v", err)
				return
			}
			next := *cur
			next.FencingToken = cur.FencingToken + 1
			if err := store.Put(ctx, cur.FencingToken, &next); err != nil &&
				!errors.Is(err, lease.ErrConflict) {
				b.Errorf("put: %v", err)
				return
			}
		}
	})
}

// benchmarkRenewSteadyState pre-acquires a fleet of independent leases and then
// renews them at full speed across all goroutines. Distinct keys mean the
// store's per-key serialization does not artificially cap throughput, so this
// measures the sustained renew ceiling that bounds steady-state holder QPS.
func benchmarkRenewSteadyState(b *testing.B, newStore func(testing.TB) lease.Store) {
	const fleet = 256

	store := newStore(b)
	mgr := lease.NewManager(store)
	ctx := context.Background()

	type held struct {
		key    lease.Key
		holder string
		token  int32
	}
	leases := make([]held, fleet)
	for i := range leases {
		key := lease.Key{Namespace: "bench", Name: fmt.Sprintf("lease-%04d", i)}
		holder := fmt.Sprintf("holder-%04d", i)
		res, err := mgr.Acquire(ctx, key, holder, time.Hour)
		if err != nil {
			b.Fatalf("seed acquire %v: %v", key, err)
		}
		if !res.Acquired {
			b.Fatalf("seed acquire %v: not acquired", key)
		}
		leases[i] = held{key: key, holder: holder, token: res.FencingToken}
	}

	var idx atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h := leases[int(idx.Add(1)-1)%fleet]
			res, err := mgr.Renew(ctx, h.key, h.holder, h.token, time.Hour)
			if err != nil {
				b.Errorf("renew %v: %v", h.key, err)
				return
			}
			if !res.Acquired {
				b.Errorf("renew %v: lease unexpectedly lost", h.key)
				return
			}
		}
	})
}

// benchmarkFailoverFanout times the acquire-after-expiry path: each iteration
// an incumbent holds a lease that has already expired, then standbys race to
// reclaim it and exactly one must win. Only the contended reclaim is timed;
// per-iteration setup and teardown are excluded.
func benchmarkFailoverFanout(b *testing.B, newStore func(testing.TB) lease.Store) {
	const standbys = 8

	store := newStore(b)
	mgr := lease.NewManager(store)
	ctx := context.Background()
	key := lease.Key{Namespace: "bench", Name: "failover"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// An incumbent acquires with a sub-tick TTL and then "dies"; by the
		// time the standbys run the lease is already reclaimable.
		if _, err := mgr.Acquire(ctx, key, "incumbent", time.Nanosecond); err != nil {
			b.Fatalf("seed incumbent: %v", err)
		}

		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			wins  = make([]bool, standbys)
		)
		wg.Add(standbys)
		for s := 0; s < standbys; s++ {
			go func(s int) {
				defer wg.Done()
				<-start
				// Winner takes a long TTL so the rest are cleanly denied.
				res, err := mgr.Acquire(ctx, key, fmt.Sprintf("standby-%d", s), time.Hour)
				if err != nil {
					b.Errorf("standby %d acquire: %v", s, err)
					return
				}
				wins[s] = res.Acquired
			}(s)
		}

		b.StartTimer()
		close(start)
		wg.Wait()
		b.StopTimer()

		won := 0
		for _, w := range wins {
			if w {
				won++
			}
		}
		if won != 1 {
			b.Fatalf("failover winners = %d, want exactly 1", won)
		}

		// Clear the key so the next round starts from a clean create.
		if cur, err := store.Get(ctx, key); err == nil {
			if err := store.Delete(ctx, key, cur.FencingToken); err != nil {
				b.Fatalf("teardown delete: %v", err)
			}
		}
		b.StartTimer()
	}
}
