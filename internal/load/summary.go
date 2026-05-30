package load

import (
	"sort"
	"sync"
	"time"
)

// Recorder accumulates per-operation latency samples across all worker
// goroutines. It is safe for concurrent use. An optional hook is invoked on
// every observation so the entrypoint can mirror samples into Prometheus
// without coupling this package to a metrics library.
type Recorder struct {
	mu   sync.Mutex
	ops  map[string]*opSamples
	hook func(op string, d time.Duration, err error)
}

type opSamples struct {
	durations []time.Duration
	errors    int
}

// NewRecorder returns an empty Recorder. hook may be nil.
func NewRecorder(hook func(op string, d time.Duration, err error)) *Recorder {
	return &Recorder{ops: make(map[string]*opSamples), hook: hook}
}

// Observe records one operation's latency and whether it errored.
func (r *Recorder) Observe(op string, d time.Duration, err error) {
	r.mu.Lock()
	s, ok := r.ops[op]
	if !ok {
		s = &opSamples{}
		r.ops[op] = s
	}
	s.durations = append(s.durations, d)
	if err != nil {
		s.errors++
	}
	r.mu.Unlock()

	if r.hook != nil {
		r.hook(op, d, err)
	}
}

// OpResult is the latency distribution for a single operation.
type OpResult struct {
	Count  int           `json:"count"`
	Errors int           `json:"errors"`
	Min    time.Duration `json:"-"`
	Max    time.Duration `json:"-"`
	Mean   time.Duration `json:"-"`
	P50    time.Duration `json:"-"`
	P95    time.Duration `json:"-"`
	P99    time.Duration `json:"-"`
	P999   time.Duration `json:"-"`
	MinMS  float64       `json:"minMs"`
	MaxMS  float64       `json:"maxMs"`
	MeanMS float64       `json:"meanMs"`
	P50MS  float64       `json:"p50Ms"`
	P95MS  float64       `json:"p95Ms"`
	P99MS  float64       `json:"p99Ms"`
	P999MS float64       `json:"p999Ms"`
}

// Summary is the full result of a run, keyed by operation label.
type Summary struct {
	Scenario  string              `json:"scenario"`
	Backend   string              `json:"backend,omitempty"`
	Leases    int                 `json:"leases"`
	Pairs     int                 `json:"pairs"`
	TTLMS     float64             `json:"ttlMs"`
	ElapsedMS float64             `json:"elapsedMs"`
	Ops       map[string]OpResult `json:"ops"`
}

// Summarize computes the distribution for every recorded operation. cfg and
// elapsed describe the run that produced the samples.
func (r *Recorder) Summarize(cfg Config, elapsed time.Duration) Summary {
	r.mu.Lock()
	defer r.mu.Unlock()

	ops := make(map[string]OpResult, len(r.ops))
	for op, s := range r.ops {
		ops[op] = resultFor(s)
	}
	return Summary{
		Scenario:  string(cfg.Scenario),
		Backend:   cfg.Backend,
		Leases:    cfg.Leases,
		Pairs:     cfg.Pairs,
		TTLMS:     ms(cfg.TTL),
		ElapsedMS: ms(elapsed),
		Ops:       ops,
	}
}

func resultFor(s *opSamples) OpResult {
	n := len(s.durations)
	res := OpResult{Count: n, Errors: s.errors}
	if n == 0 {
		return res
	}
	sorted := make([]time.Duration, n)
	copy(sorted, s.durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	res.Min = sorted[0]
	res.Max = sorted[n-1]
	res.Mean = total / time.Duration(n)
	res.P50 = percentile(sorted, 0.50)
	res.P95 = percentile(sorted, 0.95)
	res.P99 = percentile(sorted, 0.99)
	res.P999 = percentile(sorted, 0.999)

	res.MinMS = ms(res.Min)
	res.MaxMS = ms(res.Max)
	res.MeanMS = ms(res.Mean)
	res.P50MS = ms(res.P50)
	res.P95MS = ms(res.P95)
	res.P99MS = ms(res.P99)
	res.P999MS = ms(res.P999)
	return res
}

// percentile returns the p-quantile (0..1) of an ascending-sorted slice using
// nearest-rank: the smallest sample whose rank covers the requested fraction.
func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	rank := int(float64(n)*p + 0.9999999) // ceil
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
