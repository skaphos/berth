package acquire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

func newTestRenewer(t *testing.T, lc LeaseClient) (*Renewer, *State) {
	t.Helper()
	cfg := &Config{
		LeaseName:    "checkout",
		PodNamespace: "prod",
		PodName:      "checkout-x",
		Mode:         ModeRuntimeSingleton,
		Enforce:      EnforceProbe,
		TTL:          30 * time.Second,
		APIServer:    "https://berth.example",
	}
	cfg.ApplyDefaults()
	state := NewState(t.TempDir())
	return NewRenewer(cfg, lc, state, testLogger()), state
}

// newHangingRenewer builds a Renewer whose lease calls never return, with
// a short heartbeat so the per-tick bound is observable in test time.
func newHangingRenewer(t *testing.T, heartbeat time.Duration) (*Renewer, *State, *hangingClient) {
	t.Helper()
	hc := newHangingClient()
	r, state := newTestRenewer(t, hc)
	r.cfg.HeartbeatInterval = heartbeat
	return r, state, hc
}

// runBounded runs fn and reports whether it returned before limit. It
// leaks the goroutine on timeout rather than blocking the suite forever —
// which is the pre-fix behavior this guards against.
func runBounded(t *testing.T, limit time.Duration, fn func()) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(limit):
		return false
	}
}

func TestTickHeldBoundsHungRenewAndEnforcesPastExpiry(t *testing.T) {
	const heartbeat = 50 * time.Millisecond
	r, state, hc := newHangingRenewer(t, heartbeat)
	r.held = true
	// The lease already expired, so once the hung call returns the
	// past-expiry branch must gate the container.
	r.expiresAt = time.Now().Add(-time.Second)
	if err := state.MarkHealthy(); err != nil {
		t.Fatalf("MarkHealthy: %v", err)
	}

	// Background(), never canceled: pre-fix, Renew parks here forever.
	if !runBounded(t, 5*time.Second, func() { r.tickHeld(context.Background()) }) {
		t.Fatal("tickHeld did not return: a hung renew wedged the loop (issue #97)")
	}

	if hc.callCount() != 1 {
		t.Errorf("renew calls = %d, want 1", hc.callCount())
	}
	if r.held {
		t.Error("a renew hung past lease expiry must mark the lease lost")
	}
	if state.IsHealthy() {
		t.Error("a renew hung past lease expiry must gate the main container")
	}
}

func TestTickHeldHungRenewWithinTTLKeepsContainerRunning(t *testing.T) {
	const heartbeat = 50 * time.Millisecond
	r, state, _ := newHangingRenewer(t, heartbeat)
	r.held = true
	// Still comfortably inside the lease: a timed-out renew is transient,
	// so the container keeps running until the expiry actually passes.
	r.expiresAt = time.Now().Add(time.Minute)
	if err := state.MarkHealthy(); err != nil {
		t.Fatalf("MarkHealthy: %v", err)
	}

	if !runBounded(t, 5*time.Second, func() { r.tickHeld(context.Background()) }) {
		t.Fatal("tickHeld did not return within the heartbeat bound")
	}

	if !r.held {
		t.Error("a timed-out renew inside the TTL must not drop the lease")
	}
	if !state.IsHealthy() {
		t.Error("a timed-out renew inside the TTL must not gate the container")
	}
}

func TestTickReacquireBoundsHungAcquire(t *testing.T) {
	const heartbeat = 50 * time.Millisecond
	r, _, hc := newHangingRenewer(t, heartbeat)
	r.held = false

	if !runBounded(t, 5*time.Second, func() { r.tickReacquire(context.Background()) }) {
		t.Fatal("tickReacquire did not return: a hung acquire wedged the loop")
	}

	if hc.callCount() != 1 {
		t.Errorf("acquire calls = %d, want 1", hc.callCount())
	}
	if r.held {
		t.Error("a hung acquire must not be read as a won lease")
	}
}

// The loop must keep ticking through hung calls — one stall cannot stop
// the sidecar from ever re-evaluating enforcement.
func TestRunKeepsTickingThroughHungCalls(t *testing.T) {
	const heartbeat = 30 * time.Millisecond
	r, _, hc := newHangingRenewer(t, heartbeat)
	r.cfg.ReleaseOnShutdown = new(bool) // false: shutdown must not block on a hung Release

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Run(ctx)
	}()

	// Wait for three separate calls to start; pre-fix the first one never
	// returns, so the second tick never issues a call.
	for i := 0; i < 3; i++ {
		select {
		case <-hc.inCall:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d lease calls started; the loop stopped ticking", hc.callCount())
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestTickHeldRenewSuccessKeepsHealthy(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquired(9, 30*time.Second), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true
	r.token = 8

	r.tickHeld(context.Background())

	if !r.held {
		t.Error("should remain held on successful renew")
	}
	if r.token != 9 {
		t.Errorf("token = %d, want updated to 9", r.token)
	}
	if !state.IsHealthy() {
		t.Error("renew success should keep the marker healthy")
	}
}

func TestTickHeldConflictEnforces(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquireResult{}, client.ErrConflict },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true
	_ = state.MarkHealthy()

	r.tickHeld(context.Background())

	if r.held {
		t.Error("conflict should mark lease lost")
	}
	if state.IsHealthy() {
		t.Error("conflict should gate the main container (remove marker)")
	}
}

func TestTickHeldHeldByOtherEnforces(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return heldByOther("rival"), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true
	_ = state.MarkHealthy()

	r.tickHeld(context.Background())

	if r.held || state.IsHealthy() {
		t.Error("a renew reporting another holder should gate the container")
	}
}

func TestTickHeldTransientWithinTTLKeepsRunning(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquireResult{}, errors.New("network blip") },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true
	_ = state.MarkHealthy()
	now := time.Now()
	r.now = func() time.Time { return now }
	r.expiresAt = now.Add(10 * time.Second) // still within TTL

	r.tickHeld(context.Background())

	if !r.held {
		t.Error("a transient renew error within TTL must not give up the lease")
	}
	if !state.IsHealthy() {
		t.Error("a transient renew error within TTL must not gate the container")
	}
}

// TestTickHeldTransientPastExpiryEnforces is the SKA-436 failover-after-
// expiry guarantee: once we can no longer confirm the lease past its known
// expiry, we must stop the main container rather than risk split-brain.
func TestTickHeldTransientPastExpiryEnforces(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquireResult{}, errors.New("api unreachable") },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true
	_ = state.MarkHealthy()
	now := time.Now()
	r.now = func() time.Time { return now }
	r.expiresAt = now.Add(-time.Second) // already expired

	r.tickHeld(context.Background())

	if r.held {
		t.Error("past expiry with failing renew must mark the lease lost")
	}
	if state.IsHealthy() {
		t.Error("past expiry with failing renew must gate the main container")
	}
}

func TestTickReacquireRestoresContainer(t *testing.T) {
	fc := &fakeClient{
		acquireFn: func(string) (acquireResult, error) { return acquired(13, 30*time.Second), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.held = false
	_ = state.MarkUnhealthy()

	r.tickReacquire(context.Background())

	if !r.held {
		t.Error("successful reacquire should set held")
	}
	if r.token != 13 {
		t.Errorf("token = %d, want 13", r.token)
	}
	if !state.IsHealthy() {
		t.Error("reacquire should restore the health marker")
	}
	if tok, err := state.ReadToken(); err != nil || tok != 13 {
		t.Errorf("reacquire should persist token; got %d (%v)", tok, err)
	}
}

func TestTickReacquireStillHeldByOtherStaysGated(t *testing.T) {
	fc := &fakeClient{
		acquireFn: func(string) (acquireResult, error) { return heldByOther("rival"), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.held = false
	_ = state.MarkUnhealthy()

	r.tickReacquire(context.Background())

	if r.held {
		t.Error("should stay not-held while another holder owns the lease")
	}
	if state.IsHealthy() {
		t.Error("should stay gated while waiting to reacquire")
	}
}

func TestShutdownReleasesWhenConfigured(t *testing.T) {
	fc := &fakeClient{}
	r, _ := newTestRenewer(t, fc)
	r.held = true
	r.token = 5

	r.shutdown()

	if fc.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", fc.releaseCalls)
	}
	if fc.lastRelease.token != 5 {
		t.Errorf("released token = %d, want 5", fc.lastRelease.token)
	}
}

func TestShutdownSkipsReleaseWhenDisabled(t *testing.T) {
	fc := &fakeClient{}
	r, _ := newTestRenewer(t, fc)
	no := false
	r.cfg.ReleaseOnShutdown = &no
	r.held = true

	r.shutdown()

	if fc.releaseCalls != 0 {
		t.Errorf("release calls = %d, want 0 when release-on-shutdown is false", fc.releaseCalls)
	}
}

func TestShutdownSkipsReleaseWhenNotHeld(t *testing.T) {
	fc := &fakeClient{}
	r, _ := newTestRenewer(t, fc)
	r.held = false

	r.shutdown()

	if fc.releaseCalls != 0 {
		t.Errorf("release calls = %d, want 0 when not held", fc.releaseCalls)
	}
}

func TestLoadHandoffFromState(t *testing.T) {
	fc := &fakeClient{}
	r, state := newTestRenewer(t, fc)
	if err := state.WriteAcquired("east:prod:pod:x", 11); err != nil {
		t.Fatal(err)
	}

	r.loadHandoff()

	if !r.held || r.holder != "east:prod:pod:x" || r.token != 11 {
		t.Errorf("loadHandoff = {held:%v holder:%q token:%d}, want the persisted handoff", r.held, r.holder, r.token)
	}
}

func TestLoadHandoffFallbackWhenStateMissing(t *testing.T) {
	fc := &fakeClient{}
	r, _ := newTestRenewer(t, fc)

	r.loadHandoff()

	if r.held {
		t.Error("missing handoff state should start not-held")
	}
	if r.holder != r.cfg.Holder() {
		t.Errorf("fallback holder = %q, want derived default %q", r.holder, r.cfg.Holder())
	}
}

// TestRunRenewsAndReleasesOnCancel drives the full loop with a fast
// heartbeat: it should renew at least once and release on context cancel.
func TestRunRenewsAndReleasesOnCancel(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquired(3, 30*time.Second), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.cfg.HeartbeatInterval = 2 * time.Millisecond
	if err := state.WriteAcquired(r.cfg.Holder(), 1); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Let a few heartbeats elapse, then shut down.
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on graceful shutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	if fc.renewCalls == 0 {
		t.Error("expected at least one renew during Run")
	}
	if fc.releaseCalls != 1 {
		t.Errorf("release calls = %d, want 1 on graceful shutdown", fc.releaseCalls)
	}
}
