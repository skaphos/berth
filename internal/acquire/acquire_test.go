package acquire

import (
	"context"
	"testing"
	"time"
)

func holdTestConfig() *Config {
	c := &Config{
		LeaseName:    "checkout",
		PodNamespace: "prod",
		PodName:      "checkout-x",
		Mode:         ModeRuntimeSingleton,
		TTL:          30 * time.Second,
		// Tiny heartbeat so the retry between Acquire attempts is fast.
		HeartbeatInterval: time.Millisecond,
		APIServer:         "https://berth.example",
	}
	c.ApplyDefaults()
	return c
}

func TestHoldRetriesUntilAcquired(t *testing.T) {
	cfg := holdTestConfig()
	state := NewState(t.TempDir())

	calls := 0
	fc := &fakeClient{
		acquireFn: func(string) (acquireResult, error) {
			calls++
			if calls < 3 {
				return heldByOther("someone-else"), nil
			}
			return acquired(42, cfg.TTL), nil
		},
	}

	res, err := Hold(context.Background(), cfg, fc, state, testLogger())
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if res.FencingToken != 42 {
		t.Errorf("token = %d, want 42", res.FencingToken)
	}
	if calls != 3 {
		t.Errorf("acquire attempts = %d, want 3", calls)
	}
	// State and check binary must be persisted on success.
	if h, err := state.ReadHolder(); err != nil || h != cfg.Holder() {
		t.Errorf("persisted holder = %q (%v), want %q", h, err, cfg.Holder())
	}
	if tok, err := state.ReadToken(); err != nil || tok != 42 {
		t.Errorf("persisted token = %d (%v), want 42", tok, err)
	}
	if !state.IsHealthy() {
		t.Error("expected healthy marker after Hold")
	}
}

func TestHoldStopsOnContextCancel(t *testing.T) {
	cfg := holdTestConfig()
	state := NewState(t.TempDir())
	fc := &fakeClient{
		acquireFn: func(string) (acquireResult, error) { return heldByOther("other"), nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	if _, err := Hold(ctx, cfg, fc, state, testLogger()); err == nil {
		t.Fatal("expected context error when never able to acquire")
	}
}
