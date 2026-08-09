package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The check subcommand is the liveness probe's entrypoint. Its consumer is
// the kubelet, and its output is what an operator sees on the pod's
// probe-failure event — so both the exit code and the wording are contract.

// markerAged writes a marker backdated by age and returns its path.
func markerAged(t *testing.T, age time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "healthy")
	if err := os.WriteFile(path, []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckFreshMarkerPasses(t *testing.T) {
	path := markerAged(t, time.Second)

	var out bytes.Buffer
	if code := run([]string{"check", path, "--max-age", "1m"}, emptyEnv, &out); code != 0 {
		t.Errorf("a fresh marker must pass: exit = %d, output %q", code, out.String())
	}
}

func TestCheckStaleMarkerFailsWithReason(t *testing.T) {
	path := markerAged(t, time.Hour)

	var out bytes.Buffer
	code := run([]string{"check", path, "--max-age", "1m"}, emptyEnv, &out)
	if code != 1 {
		t.Fatalf("a stale marker must fail: exit = %d", code)
	}

	got := out.String()
	if !strings.Contains(got, "stale") {
		t.Errorf("output %q must say the marker is stale, not merely that the probe failed", got)
	}
	// The observed age and the bound are the evidence an operator needs to
	// tell a dead sidecar from an expected failover.
	if !strings.Contains(got, "1m0s") {
		t.Errorf("output %q must report the bound it exceeded", got)
	}
}

func TestCheckAbsentMarkerFailsWithDistinctReason(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	code := run([]string{"check", filepath.Join(dir, "healthy"), "--max-age", "1m"}, emptyEnv, &out)
	if code != 1 {
		t.Fatalf("an absent marker must fail: exit = %d", code)
	}

	got := out.String()
	if !strings.Contains(got, "absent") {
		t.Errorf("output %q must say the marker is absent", got)
	}
	if strings.Contains(got, "stale") {
		t.Errorf("absent must not be reported as stale; got %q", got)
	}
}

// The compatibility row from the check-CLI contract: without --max-age the
// command keeps the presence-only behaviour of earlier releases, so a pod
// admitted before the freshness change is unaffected.
func TestCheckWithoutMaxAgeIsPresenceOnly(t *testing.T) {
	path := markerAged(t, 30*24*time.Hour)

	var out bytes.Buffer
	if code := run([]string{"check", path}, emptyEnv, &out); code != 0 {
		t.Errorf("without --max-age an old marker must still pass: exit = %d, output %q", code, out.String())
	}
}

// FR-010: check runs inside the workload's container, which may be
// distroless. It must not need configuration, environment, or a client —
// an empty environment and a bare marker path are the whole contract.
func TestCheckNeedsNoEnvironmentOrConfig(t *testing.T) {
	path := markerAged(t, time.Second)

	var out bytes.Buffer
	if code := run([]string{"check", path, "--max-age", "5m"}, emptyEnv, &out); code != 0 {
		t.Fatalf("check must work with no environment at all: exit = %d, output %q", code, out.String())
	}

	// Nothing that would require BERTH_* config should be demanded even when
	// the marker is missing; the failure must be about the marker.
	out.Reset()
	code := run([]string{"check", filepath.Join(t.TempDir(), "healthy"), "--max-age", "5m"}, emptyEnv, &out)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "marker") {
		t.Errorf("failure %q must be about the marker, not missing configuration", out.String())
	}
}
