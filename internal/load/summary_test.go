package load

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPercentileNearestRank(t *testing.T) {
	t.Parallel()

	sorted := make([]time.Duration, 10)
	for i := range sorted {
		sorted[i] = time.Duration(i+1) * time.Millisecond // 1ms..10ms
	}
	cases := []struct {
		p    float64
		want time.Duration
	}{
		{0.50, 5 * time.Millisecond},
		{0.95, 10 * time.Millisecond},
		{0.99, 10 * time.Millisecond},
		{0.10, 1 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := percentile(sorted, tc.p); got != tc.want {
			t.Fatalf("percentile(%.2f) = %s, want %s", tc.p, got, tc.want)
		}
	}
	if got := percentile(nil, 0.5); got != 0 {
		t.Fatalf("percentile of empty = %s, want 0", got)
	}
}

func TestRecorderSummarize(t *testing.T) {
	t.Parallel()

	rec := NewRecorder(nil)
	rec.Observe(OpAcquire, 1*time.Millisecond, nil)
	rec.Observe(OpAcquire, 3*time.Millisecond, nil)
	rec.Observe(OpAcquire, 2*time.Millisecond, errors.New("boom"))
	rec.Observe(OpRenew, 5*time.Millisecond, nil)

	cfg := validConfig()
	s := rec.Summarize(cfg, 250*time.Millisecond)

	if s.Scenario != string(cfg.Scenario) || s.Leases != cfg.Leases {
		t.Fatalf("summary metadata mismatch: %+v", s)
	}
	acq := s.Ops[OpAcquire]
	if acq.Count != 3 || acq.Errors != 1 {
		t.Fatalf("acquire count/errors = %d/%d, want 3/1", acq.Count, acq.Errors)
	}
	if acq.Min != 1*time.Millisecond || acq.Max != 3*time.Millisecond {
		t.Fatalf("acquire min/max = %s/%s, want 1ms/3ms", acq.Min, acq.Max)
	}
	if acq.MeanMS != 2.0 {
		t.Fatalf("acquire meanMs = %v, want 2.0", acq.MeanMS)
	}
	if renew := s.Ops[OpRenew]; renew.Count != 1 {
		t.Fatalf("renew count = %d, want 1", renew.Count)
	}
}

func TestRecorderHookInvokedConcurrently(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		count int
	)
	rec := NewRecorder(func(string, time.Duration, error) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			rec.Observe(OpRenew, time.Millisecond, nil)
		}()
	}
	wg.Wait()

	if count != n {
		t.Fatalf("hook invoked %d times, want %d", count, n)
	}
	if got := rec.Summarize(validConfig(), time.Second).Ops[OpRenew].Count; got != n {
		t.Fatalf("recorded %d renews, want %d", got, n)
	}
}
