package load

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/api"
	"github.com/skaphos/berth/internal/lease"
	"github.com/skaphos/berth/pkg/client"
)

// newInProcessClient stands up the real API mux backed by an in-memory store
// (no auth) on an httptest server and returns a client pointed at it. This
// exercises the driver end-to-end — pkg/client → HTTP → handlers → store —
// with zero external infrastructure.
func newInProcessClient(t *testing.T) LeaseClient {
	t.Helper()
	mux := api.NewMux(lease.NewManager(lease.NewMemStore()), nil, nil)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return client.New(ts.URL)
}

func assertOp(t *testing.T, s Summary, op string, wantCount int) {
	t.Helper()
	r, ok := s.Ops[op]
	if !ok {
		t.Fatalf("summary missing op %q; ops=%v", op, keys(s.Ops))
	}
	if r.Errors != 0 {
		t.Fatalf("op %q had %d errors, want 0", op, r.Errors)
	}
	if wantCount >= 0 && r.Count != wantCount {
		t.Fatalf("op %q count = %d, want %d", op, r.Count, wantCount)
	}
	if wantCount < 0 && r.Count == 0 {
		t.Fatalf("op %q count = 0, want > 0", op)
	}
}

func keys(m map[string]OpResult) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRunSteadyInProcess(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Namespace: "berth-load", Scenario: ScenarioSteady,
		Leases: 4, Pairs: 2, TTL: 2 * time.Second, Heartbeat: 200 * time.Millisecond,
		Duration: 600 * time.Millisecond, Concurrency: 4,
	}
	s, err := Run(context.Background(), newInProcessClient(t), cfg, NewRecorder(nil))
	if err != nil {
		t.Fatal(err)
	}
	assertOp(t, s, OpRenew, -1)   // many renews, no errors
	assertOp(t, s, OpAcquire, -1) // initial + standby contention, no errors
}

func TestRunColdStartInProcess(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Namespace: "berth-load", Scenario: ScenarioColdStart,
		Leases: 6, Pairs: 2, TTL: 2 * time.Second, Heartbeat: 200 * time.Millisecond,
		Concurrency: 6,
	}
	s, err := Run(context.Background(), newInProcessClient(t), cfg, NewRecorder(nil))
	if err != nil {
		t.Fatal(err)
	}
	assertOp(t, s, OpAcquire, 6) // exactly one acquire per lease
}

func TestRunFailoverInProcess(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Namespace: "berth-load", Scenario: ScenarioFailover,
		Leases: 4, Pairs: 2, TTL: time.Second, Heartbeat: 100 * time.Millisecond,
		Concurrency: 4,
	}
	s, err := Run(context.Background(), newInProcessClient(t), cfg, NewRecorder(nil))
	if err != nil {
		t.Fatal(err)
	}
	// 4 initial acquires + 2 reclaims of the even-index failover half. The
	// odd-index survivor half renews instead of re-acquiring, so it adds no
	// acquires.
	assertOp(t, s, OpAcquire, 6)
	// The survivor half (odd indices) renews at heartbeat cadence throughout the
	// expiry window and reclaim, so the backend carries steady load while the
	// even half is reclaimed.
	assertOp(t, s, OpRenew, -1)
}

func TestRunChurnInProcess(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Namespace: "berth-load", Scenario: ScenarioChurn,
		Leases: 4, Pairs: 2, TTL: 2 * time.Second, Heartbeat: 100 * time.Millisecond,
		Duration: 400 * time.Millisecond, Concurrency: 4, ChurnFraction: 1.0,
	}
	s, err := Run(context.Background(), newInProcessClient(t), cfg, NewRecorder(nil))
	if err != nil {
		t.Fatal(err)
	}
	assertOp(t, s, OpRelease, -1) // fraction 1.0 → every heartbeat releases
	assertOp(t, s, OpAcquire, -1) // initial + re-acquires
}
