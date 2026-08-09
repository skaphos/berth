package acquire

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Marker freshness (issue #98).
//
// Presence alone cannot distinguish "the sidecar removed the marker" from
// "the sidecar died and nobody touched it". The first is an expected
// failover; the second leaves the workload running unleased.

// agedMarker writes a marker and backdates its mtime by age.
func agedMarker(t *testing.T, age time.Duration) (*State, string) {
	t.Helper()
	s := NewState(t.TempDir())
	if err := s.MarkHealthy(); err != nil {
		t.Fatalf("MarkHealthy: %v", err)
	}
	path := s.HealthyPath()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return s, path
}

func TestEvaluateMarkerFreshnessBoundary(t *testing.T) {
	const bound = time.Minute

	tests := []struct {
		name string
		age  time.Duration
		want HealthVerdict
	}{
		{"just written", 0, HealthOK},
		{"well within the bound", bound / 2, HealthOK},
		{"comfortably past the bound", 2 * bound, HealthStale},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, path := agedMarker(t, tc.age)
			if got := EvaluateMarker(path, bound).Verdict; got != tc.want {
				t.Errorf("age %s under bound %s: verdict = %v, want %v", tc.age, bound, got, tc.want)
			}
		})
	}
}

// The inclusive edge is called out separately because the FR-007 margin
// argument rests on it: a marker exactly at the bound is still healthy.
func TestEvaluateMarkerBoundIsInclusive(t *testing.T) {
	_, path := agedMarker(t, 0)
	if got := EvaluateMarker(path, time.Hour).Verdict; got != HealthOK {
		t.Errorf("a marker younger than the bound must be healthy, got %v", got)
	}
}

func TestEvaluateMarkerAbsentIsDistinctFromStale(t *testing.T) {
	s := NewState(t.TempDir())

	absent := EvaluateMarker(s.HealthyPath(), time.Minute)
	if absent.Verdict != HealthAbsent {
		t.Fatalf("missing marker: verdict = %v, want HealthAbsent", absent.Verdict)
	}

	_, stalePath := agedMarker(t, time.Hour)
	stale := EvaluateMarker(stalePath, time.Minute)
	if stale.Verdict != HealthStale {
		t.Fatalf("old marker: verdict = %v, want HealthStale", stale.Verdict)
	}

	// The distinction is the point: an operator must be able to tell an
	// expected failover from a dead sidecar.
	if absent.Reason("p") == stale.Reason("p") {
		t.Error("absent and stale must not produce the same explanation")
	}
	if stale.Age == 0 {
		t.Error("a stale result must report the observed age as evidence")
	}
}

// An unreadable marker is not evidence of health, so it fails closed. A
// non-directory parent component yields ENOTDIR deterministically,
// regardless of the uid running the test — unlike a chmod-based fixture,
// which root would walk straight through.
func TestEvaluateMarkerUnreadableFailsClosed(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "notadir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := EvaluateMarker(filepath.Join(notADir, "healthy"), time.Minute)
	if res.Verdict != HealthIndeterminate {
		t.Fatalf("verdict = %v, want HealthIndeterminate", res.Verdict)
	}
	if res.OK() {
		t.Error("an unreadable marker must not be treated as healthy")
	}
}

// A zero bound means presence-only, preserving the behaviour of releases
// that had no freshness check.
func TestEvaluateMarkerZeroBoundIsPresenceOnly(t *testing.T) {
	_, path := agedMarker(t, 30*24*time.Hour)

	if got := EvaluateMarker(path, 0).Verdict; got != HealthOK {
		t.Errorf("with no bound an ancient marker is still healthy, got %v", got)
	}
	if got := EvaluateMarker(path, time.Minute).Verdict; got != HealthStale {
		t.Errorf("with a bound the same marker must be stale, got %v", got)
	}
}

// FR-008: the verdict depends only on mtime versus the reader's own clock.
// Contents are never parsed, so a timestamp written into the marker cannot
// influence the outcome — which is what keeps the result independent of any
// second clock.
func TestEvaluateMarkerIgnoresMarkerContents(t *testing.T) {
	_, path := agedMarker(t, time.Hour)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Claim freshness in the contents, leaving mtime untouched.
	if err := os.WriteFile(path, []byte("expires="+time.Now().Add(time.Hour).String()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	if got := EvaluateMarker(path, time.Minute).Verdict; got != HealthStale {
		t.Errorf("contents must not affect freshness; verdict = %v, want HealthStale", got)
	}
}

// FR-009: the sidecar-side view and the probe-side view cannot disagree.
func TestIsFreshAgreesWithEvaluateMarker(t *testing.T) {
	for _, age := range []time.Duration{0, 30 * time.Second, 2 * time.Minute} {
		s, path := agedMarker(t, age)
		direct := EvaluateMarker(path, time.Minute)
		viaState := s.IsFresh(time.Minute)
		if direct.Verdict != viaState.Verdict {
			t.Errorf("age %s: State.IsFresh = %v but EvaluateMarker = %v", age, viaState.Verdict, direct.Verdict)
		}
	}
}

// The init container's acquire leaves a fresh marker, so a pod that has
// just started is never killed for a renewal that has not happened yet.
// The one-TTL bound depends on this; moving marker creation into the renew
// loop would open a startup window.
func TestWriteAcquiredLeavesMarkerFresh(t *testing.T) {
	s := NewState(t.TempDir())
	if err := s.WriteAcquired("east/prod:pod:x", 1); err != nil {
		t.Fatalf("WriteAcquired: %v", err)
	}

	if res := s.IsFresh(time.Minute); !res.OK() {
		t.Errorf("a just-acquired pod must be fresh, got %v", res.Verdict)
	}
}

// FR-007: a holder that is renewing successfully must never trip the bound.
// The mechanism is that every successful renew rewrites the marker, so its
// age never exceeds one heartbeat.
func TestSuccessfulRenewRefreshesTheMarker(t *testing.T) {
	fc := &fakeClient{
		renewFn: func(string, int32) (acquireResult, error) { return acquired(9, 30*time.Second), nil },
	}
	r, state := newTestRenewer(t, fc)
	r.held = true

	// Backdate the marker well past any plausible bound.
	if err := state.MarkHealthy(); err != nil {
		t.Fatalf("MarkHealthy: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(state.HealthyPath(), old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if state.IsFresh(time.Minute).OK() {
		t.Fatal("fixture is wrong: the marker should start stale")
	}

	r.tickHeld(context.Background())

	if res := state.IsFresh(time.Minute); !res.OK() {
		t.Errorf("a successful renew must refresh the marker; verdict = %v", res.Verdict)
	}
}

// The safety margin behind the one-TTL bound is structural, not a matter of
// choosing good defaults: validation refuses a heartbeat that is not
// strictly shorter than the TTL, so a correctly-configured holder cannot
// age its own marker past the bound.
func TestValidationGuaranteesHeartbeatMarginUnderTTL(t *testing.T) {
	base := func(hb time.Duration) *Config {
		return &Config{
			LeaseName:         "checkout",
			PodNamespace:      "prod",
			PodName:           "checkout-x",
			Mode:              ModeRuntimeSingleton,
			Enforce:           EnforceProbe,
			TTL:               30 * time.Second,
			HeartbeatInterval: hb,
			APIServer:         "https://berth.example",
		}
	}

	for _, hb := range []time.Duration{10 * time.Second, 29 * time.Second} {
		if err := base(hb).Validate(); err != nil {
			t.Errorf("heartbeat %s under a 30s ttl must be accepted: %v", hb, err)
		}
	}
	for _, hb := range []time.Duration{30 * time.Second, time.Minute} {
		if err := base(hb).Validate(); err == nil {
			t.Errorf("heartbeat %s at or above the 30s ttl must be rejected; "+
				"the one-TTL freshness bound relies on this margin", hb)
		}
	}
}
