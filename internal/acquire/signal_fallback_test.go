package acquire

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSignalFallbackGatesDespiteSignalFailure(t *testing.T) {
	for _, fault := range []string{"permission", "scanner", "no-match", "exited"} {
		t.Run(fault, func(t *testing.T) {
			state := NewState(t.TempDir())
			if err := state.MarkHealthy(); err != nil {
				t.Fatal(err)
			}
			e := newEnforcer(&Config{Enforce: EnforceSignal, SignalTarget: "app"}, state, testLogger()).(*signalEnforcer)
			e.find = func() ([]int, error) { return []int{4242}, nil }
			e.signal = func(int, syscall.Signal) error { return syscall.EPERM }
			switch fault {
			case "scanner":
				e.find = func() ([]int, error) { return nil, syscall.EACCES }
			case "no-match":
				e.find = func() ([]int, error) { return nil, nil }
			case "exited":
				e.signal = func(int, syscall.Signal) error { return syscall.ESRCH }
			}
			err := e.Hold(context.Background())
			if (fault == "permission" && !errors.Is(err, syscall.EPERM)) || (fault == "scanner" && !errors.Is(err, syscall.EACCES)) {
				t.Fatalf("lost enforcement error: %v", err)
			}
			if (fault == "no-match" || fault == "exited") && err != nil {
				t.Fatal(err)
			}
			if state.IsFresh(time.Second).OK() {
				t.Fatal("failed signal left the kubelet probe healthy")
			}
			if err := e.Release(context.Background()); err != nil {
				t.Fatal(err)
			}
			stale := time.Now().Add(-time.Hour)
			if err := os.Chtimes(state.HealthyPath(), stale, stale); err != nil {
				t.Fatal(err)
			}
			if err := e.Release(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !state.IsFresh(time.Second).OK() {
				t.Fatal("successful renewal did not refresh the probe")
			}
		})
	}
}

func TestHelperRejectsPositiveWrappedTTL(t *testing.T) {
	c := baseConfig()
	c.TTL = 4294967326 * time.Second
	if err := c.Validate(); err == nil {
		t.Fatal("helper accepts TTL that wraps to 30s in the API")
	}
}
