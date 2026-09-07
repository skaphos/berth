package acquire

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestLeaseDeadlineUsesLocalRequestStart(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"renew", "acquire"} {
		for _, skew := range []time.Duration{-time.Hour, time.Hour} {
			t.Run(operation+"/"+skew.String(), func(t *testing.T) {
				t.Parallel()
				start := time.Now()
				now := start
				respond := func() (acquireResult, error) {
					now = now.Add(5 * time.Second) // Response latency consumes the TTL.
					return acquireResult{Acquired: true, FencingToken: 9, ExpiresAt: now.Add(skew)}, nil
				}
				fc := &fakeClient{
					renewFn:   func(string, int32) (acquireResult, error) { return respond() },
					acquireFn: func(string) (acquireResult, error) { return respond() },
				}
				r, state := newTestRenewer(t, fc)
				r.now = func() time.Time { return now }
				if operation == "renew" {
					r.held = true
					r.expiresAt = start.Add(r.cfg.TTL)
					r.tickHeld(t.Context())
				} else {
					r.tickReacquire(t.Context())
				}
				want := start.Add(r.cfg.TTL)
				if !r.held || !state.IsHealthy() || !r.expiresAt.Equal(want) {
					t.Fatalf("held=%v healthy=%v deadline=%v, want healthy until %v", r.held, state.IsHealthy(), r.expiresAt, want)
				}
				// Exact local expiry must fence even if the server clock is ahead.
				now = want
				fc.renewFn = func(string, int32) (acquireResult, error) {
					return acquireResult{}, errors.New("partition")
				}
				r.tickHeld(t.Context())
				if r.held || state.IsHealthy() {
					t.Fatal("local deadline did not close the gate")
				}
			})
		}
	}
}

func TestRestartGatesBeforeConfirmingHandoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		hc := newHangingClient()
		r, state := newTestRenewer(t, hc)
		for range 3 {
			if err := state.WriteAcquired(r.cfg.Holder(), 11); err != nil {
				t.Fatal(err)
			}
			// The same persisted identity/token is present after every crash.
			r = NewRenewer(r.cfg, hc, state, testLogger())
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- r.Run(ctx) }()
			<-hc.inCall
			synctest.Wait()
			if state.IsHealthy() {
				cancel()
				<-done
				t.Fatal("restart left the gate open while the saved lease could not be confirmed")
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if r.held || !r.expiresAt.IsZero() {
				t.Fatal("restart granted a new lease lifetime without confirmation")
			}
		}
	})
}

func TestHungRenewStopsAtLocalDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, state, _ := newHangingRenewer(t, 10*time.Second)
		r.held = true
		start := time.Now()
		r.expiresAt = start.Add(time.Second)
		if err := state.MarkHealthy(); err != nil {
			t.Fatal(err)
		}
		r.tickHeld(t.Context())
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("hung renewal delayed enforcement for %s, want 1s", elapsed)
		}
		if r.held || state.IsHealthy() {
			t.Fatal("deadline did not fence the hung renewal")
		}
	})
}

func TestRunFencesBetweenHeartbeats(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		confirmed := make(chan time.Time, 1)
		first := true
		fc := &fakeClient{renewFn: func(string, int32) (acquireResult, error) {
			if first {
				first = false
				confirmed <- time.Now()
				return acquired(11, 31*time.Second), nil
			}
			return acquireResult{}, errors.New("partition")
		}}
		r, state := newTestRenewer(t, fc)
		r.cfg.TTL = 31 * time.Second
		if err := state.WriteAcquired(r.cfg.Holder(), 11); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- r.Run(ctx) }()
		<-confirmed
		synctest.Wait()
		time.Sleep(31*time.Second - time.Nanosecond)
		synctest.Wait()
		if !state.IsHealthy() {
			t.Error("transient failures closed the gate before the local deadline")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if state.IsHealthy() {
			t.Error("expiry between heartbeats did not close the gate")
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestDelayedSuccessCannotReopenExpiredLease(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"renew", "acquire"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			now := start
			respond := func() (acquireResult, error) {
				now = now.Add(30 * time.Second)
				return acquired(11, time.Hour), nil
			}
			fc := &fakeClient{
				renewFn:   func(string, int32) (acquireResult, error) { return respond() },
				acquireFn: func(string) (acquireResult, error) { return respond() },
			}
			r, state := newTestRenewer(t, fc)
			r.now = func() time.Time { return now }
			if operation == "renew" {
				r.held = true
				r.expiresAt = start.Add(r.cfg.TTL)
				r.tickHeld(t.Context())
			} else {
				r.tickReacquire(t.Context())
			}
			if r.held || state.IsHealthy() {
				t.Fatal("success received after its local TTL reopened the gate")
			}
		})
	}
}
