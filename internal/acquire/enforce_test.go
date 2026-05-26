package acquire

import (
	"context"
	"syscall"
	"testing"
	"time"
)

func TestProbeEnforcer(t *testing.T) {
	s := NewState(t.TempDir())
	_ = s.MarkHealthy()
	e := probeEnforcer{state: s}

	if err := e.Hold(context.Background()); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	if s.IsHealthy() {
		t.Error("Hold should remove the health marker")
	}
	if err := e.Release(context.Background()); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !s.IsHealthy() {
		t.Error("Release should restore the health marker")
	}
}

func TestSignalEnforcerEscalatesTermThenKill(t *testing.T) {
	var sent []struct {
		pid int
		sig syscall.Signal
	}
	now := time.Now()
	e := newSignalEnforcer(5*time.Second, testLogger())
	e.now = func() time.Time { return now }
	e.find = func() ([]int, error) { return []int{4242}, nil }
	e.signal = func(pid int, sig syscall.Signal) error {
		sent = append(sent, struct {
			pid int
			sig syscall.Signal
		}{pid, sig})
		return nil
	}

	// First Hold: SIGTERM.
	if err := e.Hold(context.Background()); err != nil {
		t.Fatalf("Hold: %v", err)
	}
	// Still within grace: no escalation.
	now = now.Add(2 * time.Second)
	_ = e.Hold(context.Background())
	// Past grace: SIGKILL.
	now = now.Add(4 * time.Second)
	_ = e.Hold(context.Background())

	if len(sent) != 2 {
		t.Fatalf("sent %d signals, want 2 (TERM then KILL); got %+v", len(sent), sent)
	}
	if sent[0].sig != syscall.SIGTERM || sent[1].sig != syscall.SIGKILL {
		t.Errorf("signals = %v, %v; want SIGTERM, SIGKILL", sent[0].sig, sent[1].sig)
	}

	// Release clears escalation so a future Hold starts a fresh SIGTERM.
	_ = e.Release(context.Background())
	sent = nil
	_ = e.Hold(context.Background())
	if len(sent) != 1 || sent[0].sig != syscall.SIGTERM {
		t.Errorf("after Release, want fresh SIGTERM; got %+v", sent)
	}
}
