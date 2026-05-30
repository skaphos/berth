package load

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// Run executes cfg's scenario against cli, recording every call into rec, and
// returns the computed [Summary]. It validates cfg first. The run stops early
// if ctx is canceled; partial samples are still summarized.
func Run(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) (Summary, error) {
	if err := cfg.Validate(); err != nil {
		return Summary{}, fmt.Errorf("invalid config: %w", err)
	}

	start := time.Now()
	switch cfg.Scenario {
	case ScenarioSteady:
		runSteady(ctx, cli, cfg, rec)
	case ScenarioColdStart:
		runColdStart(ctx, cli, cfg, rec)
	case ScenarioFailover:
		runFailover(ctx, cli, cfg, rec)
	case ScenarioChurn:
		runChurn(ctx, cli, cfg, rec)
	default:
		return Summary{}, fmt.Errorf("unknown scenario %q", cfg.Scenario)
	}
	return rec.Summarize(cfg, time.Since(start)), nil
}

// runSteady acquires every lease, then holds them: each lease's holder renews
// at heartbeat cadence while its paired standby contends (and is denied) on the
// same cadence, until Duration elapses.
func runSteady(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) {
	tokens := initialAcquire(ctx, cli, cfg, rec)

	deadlineCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	forEachLease(deadlineCtx, cfg.Leases, func(c context.Context, i int) {
		token := tokens[i]
		for c.Err() == nil {
			res, err := recRenew(c, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), token, cfg.TTL)
			if err == nil && res.Acquired {
				token = res.FencingToken
			}
			// Standby contends and is expected to be denied; the latency of the
			// denied acquire is itself part of the steady-state load.
			_, _ = recAcquire(c, rec, cli, cfg.Namespace, leaseName(i), standbyHolder(i), cfg.TTL)
			sleepCtx(c, cfg.Heartbeat)
		}
	})
}

// runColdStart fires every acquire as close to simultaneously as the scheduler
// allows, modeling a fleet booting into an empty coordination plane. There is
// deliberately no concurrency cap — simultaneity is the scenario.
func runColdStart(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(cfg.Leases)
	for i := 0; i < cfg.Leases; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, _ = recAcquire(ctx, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), cfg.TTL)
		}(i)
	}
	close(start)
	wg.Wait()
}

// runFailover populates every lease, then models a region-pair failover: the
// failover half (even indices) is left to expire by not renewing for one TTL,
// while the survivor half (odd indices) keeps renewing at heartbeat cadence
// throughout the expiry window and the reclaim. It times the standby acquire
// that reclaims each expired lease against that concurrent survivor traffic, so
// the reclaim latency reflects a backend under steady load rather than one idle
// except for the reclaim burst.
func runFailover(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) {
	// renewCtx governs only the survivor renew *loop* and its inter-renew sleep.
	// The acquire/renew calls themselves run against ctx, so the teardown
	// stopRenew() below tears the loops down without cancelling an in-flight
	// request and recording it as a spurious context-canceled error.
	renewCtx, stopRenew := context.WithCancel(ctx)
	defer stopRenew()

	// Survivor half (odd indices): each lease is acquired and then renewed
	// continuously from the moment *it* is acquired — not after the whole
	// population phase — so no survivor sits idle long enough to expire even when
	// population is slow on a throttled backend. A shared semaphore bounds the
	// acquire burst (the renew loop holds no slot), matching the bounded burst of
	// the failover half below. These hold steady write load on the backend
	// through the expiry window and the reclaim.
	acquireSlots := make(chan struct{}, cfg.Concurrency)
	var survivors sync.WaitGroup
	for i := 1; i < cfg.Leases; i += 2 {
		survivors.Add(1)
		go func(i int) {
			defer survivors.Done()
			select {
			case <-renewCtx.Done():
				return
			case acquireSlots <- struct{}{}:
			}
			res, err := recAcquire(ctx, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), cfg.TTL)
			<-acquireSlots
			var token int32
			if err == nil && res.Acquired {
				token = res.FencingToken
			}
			for renewCtx.Err() == nil {
				res, err := recRenew(ctx, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), token, cfg.TTL)
				if err == nil && res.Acquired {
					token = res.FencingToken
				}
				sleepCtx(renewCtx, cfg.Heartbeat)
			}
		}(i)
	}

	// Failover half (even indices): acquire under the bounded burst, then leave
	// to expire by not renewing.
	forEachLeaseBounded(ctx, cfg.Leases, cfg.Concurrency, func(c context.Context, i int) {
		if i%2 != 0 {
			return // the odd half is held by the survivor renewers above
		}
		_, _ = recAcquire(c, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), cfg.TTL)
	})

	// Wait out the TTL plus a margin so the un-renewed even half is reclaimable.
	// The odd half stays held throughout because its renewers never pause.
	margin := cfg.TTL / 10
	if margin < 100*time.Millisecond {
		margin = 100 * time.Millisecond
	}
	sleepCtx(ctx, cfg.TTL+margin)

	// Reclaim the failover half against the survivors' concurrent renew load.
	forEachLeaseBounded(ctx, cfg.Leases, cfg.Concurrency, func(c context.Context, i int) {
		if i%2 != 0 {
			return // only the failover half is reclaimed
		}
		_, _ = recAcquire(c, rec, cli, cfg.Namespace, leaseName(i), standbyHolder(i), cfg.TTL)
	})

	// Reclaim done: stop the survivor renewers and let in-flight calls drain.
	stopRenew()
	survivors.Wait()
}

// runChurn holds every lease and renews at cadence, but each heartbeat a
// ChurnFraction of holders release and a fresh holder re-acquires — modeling
// rolling restarts under sustained load.
func runChurn(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) {
	tokens := initialAcquire(ctx, cli, cfg, rec)

	deadlineCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	forEachLease(deadlineCtx, cfg.Leases, func(c context.Context, i int) {
		token := tokens[i]
		holder := activeHolder(i)
		gen := 0
		for c.Err() == nil {
			if rand.Float64() < cfg.ChurnFraction {
				recRelease(c, rec, cli, cfg.Namespace, leaseName(i), holder, token)
				gen++
				holder = fmt.Sprintf("%s-r%d", activeHolder(i), gen)
				res, err := recAcquire(c, rec, cli, cfg.Namespace, leaseName(i), holder, cfg.TTL)
				if err == nil && res.Acquired {
					token = res.FencingToken
				}
			} else {
				res, err := recRenew(c, rec, cli, cfg.Namespace, leaseName(i), holder, token, cfg.TTL)
				if err == nil && res.Acquired {
					token = res.FencingToken
				}
			}
			sleepCtx(c, cfg.Heartbeat)
		}
	})
}

// initialAcquire acquires every lease with its active holder under a bounded
// worker pool and returns the per-lease fencing tokens (0 where the acquire
// did not succeed).
func initialAcquire(ctx context.Context, cli LeaseClient, cfg Config, rec *Recorder) []int32 {
	tokens := make([]int32, cfg.Leases)
	forEachLeaseBounded(ctx, cfg.Leases, cfg.Concurrency, func(c context.Context, i int) {
		res, err := recAcquire(c, rec, cli, cfg.Namespace, leaseName(i), activeHolder(i), cfg.TTL)
		if err == nil && res.Acquired {
			tokens[i] = res.FencingToken
		}
	})
	return tokens
}

// --- timing wrappers ---

func recAcquire(ctx context.Context, rec *Recorder, cli LeaseClient, ns, name, holder string, ttl time.Duration) (acquireResult, error) {
	start := time.Now()
	res, err := cli.Acquire(ctx, ns, name, holder, ttl)
	rec.Observe(OpAcquire, time.Since(start), err)
	return res, err
}

func recRenew(ctx context.Context, rec *Recorder, cli LeaseClient, ns, name, holder string, token int32, ttl time.Duration) (acquireResult, error) {
	start := time.Now()
	res, err := cli.Renew(ctx, ns, name, holder, token, ttl)
	rec.Observe(OpRenew, time.Since(start), err)
	return res, err
}

func recRelease(ctx context.Context, rec *Recorder, cli LeaseClient, ns, name, holder string, token int32) {
	start := time.Now()
	err := cli.Release(ctx, ns, name, holder, token)
	rec.Observe(OpRelease, time.Since(start), err)
}

// --- fan-out helpers ---

// forEachLease runs fn for every lease index concurrently, one goroutine each,
// and waits for them all. Used by sustained scenarios where every lease must be
// driven in parallel.
func forEachLease(ctx context.Context, leases int, fn func(ctx context.Context, i int)) {
	var wg sync.WaitGroup
	wg.Add(leases)
	for i := 0; i < leases; i++ {
		go func(i int) {
			defer wg.Done()
			fn(ctx, i)
		}(i)
	}
	wg.Wait()
}

// forEachLeaseBounded runs fn for every lease index with at most concurrency in
// flight at once. Used by burst phases (initial acquire, failover reclaim)
// where unbounded fan-out would itself be the bottleneck.
func forEachLeaseBounded(ctx context.Context, leases, concurrency int, fn func(ctx context.Context, i int)) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
loop:
	for i := 0; i < leases; i++ {
		// Acquire a slot, but stay responsive to cancellation while every slot
		// is occupied — otherwise a slow/stuck client would block scheduling
		// past ctx cancellation. In-flight goroutines drain at wg.Wait below.
		select {
		case <-ctx.Done():
			break loop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, i)
		}(i)
	}
	wg.Wait()
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
