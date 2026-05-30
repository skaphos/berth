package acquire

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
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
	e := newSignalEnforcer(5*time.Second, "", testLogger())
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

// fakeScanner builds a procScanner backed by in-memory tables so the PID
// selection can be exercised without a real /proc.
func fakeScanner(self, parent int, selfExe string, pids []int, comm, exe map[int]string) procScanner {
	return procScanner{
		self:    self,
		parent:  parent,
		selfExe: selfExe,
		list:    func() ([]int, error) { return pids, nil },
		comm:    func(pid int) string { return comm[pid] },
		exe:     func(pid int) string { return exe[pid] },
	}
}

// TestWorkloadPIDsTargetScopesEnforcementToWorkload is the core SKA-449
// guarantee: in a shared PID namespace populated with the workload plus two
// unrelated sidecars, a signal-target selects only the workload PID, so lease
// loss never reaches the co-located sidecars.
func TestWorkloadPIDsTargetScopesEnforcementToWorkload(t *testing.T) {
	const selfExe = "/berth/berth-acquire"
	pids := []int{1, 7, 8, 42, 100, 101, 102}
	comm := map[int]string{100: "nginx", 101: "envoy", 102: "fluent-bit"}
	exe := map[int]string{
		42:  selfExe, // a second instance of our own binary (the probe check)
		100: "/usr/sbin/nginx",
		101: "/usr/local/bin/envoy",
		102: "/opt/fluent-bit/bin/fluent-bit",
	}
	sc := fakeScanner(7 /*self*/, 8 /*parent*/, selfExe, pids, comm, exe)

	got, err := sc.workloadPIDs("nginx")
	if err != nil {
		t.Fatalf("workloadPIDs: %v", err)
	}
	if len(got) != 1 || got[0] != 100 {
		t.Fatalf("workloadPIDs(nginx) = %v, want [100] (workload only, sidecars spared)", got)
	}
}

// TestWorkloadPIDsNoTargetReturnsEveryNonExcluded documents the unscoped
// fallback: without a target every process survives selection except PID 1,
// self, parent, and other instances of our own binary.
func TestWorkloadPIDsNoTargetReturnsEveryNonExcluded(t *testing.T) {
	const selfExe = "/berth/berth-acquire"
	pids := []int{1, 7, 8, 42, 100, 101, 102}
	exe := map[int]string{42: selfExe, 100: "/usr/sbin/nginx", 101: "/usr/local/bin/envoy", 102: "/bin/app"}
	sc := fakeScanner(7, 8, selfExe, pids, map[int]string{}, exe)

	got, err := sc.workloadPIDs("")
	if err != nil {
		t.Fatalf("workloadPIDs: %v", err)
	}
	want := map[int]bool{100: true, 101: true, 102: true}
	if len(got) != len(want) {
		t.Fatalf("workloadPIDs(\"\") = %v, want the three non-excluded pids %v", got, want)
	}
	for _, pid := range got {
		if !want[pid] {
			t.Fatalf("workloadPIDs(\"\") returned excluded pid %d: %v", pid, got)
		}
	}
}

func TestMatchesTarget(t *testing.T) {
	cases := []struct {
		name         string
		target, comm string
		exe          string
		want         bool
	}{
		{"comm match", "nginx", "nginx", "/usr/sbin/nginx", true},
		{"exe basename match when comm differs", "nginx", "nginx: master", "/usr/sbin/nginx", true},
		{"long target matched against 15-byte comm prefix", "my-very-long-process-name", "my-very-long-pr", "", true},
		{"no match", "nginx", "envoy", "/usr/local/bin/envoy", false},
		{"empty comm and exe never match", "nginx", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesTarget(tc.target, tc.comm, tc.exe); got != tc.want {
				t.Fatalf("matchesTarget(%q,%q,%q) = %v, want %v", tc.target, tc.comm, tc.exe, got, tc.want)
			}
		})
	}
}

// TestSignalEnforcerWarnsWithoutTarget asserts the operability guardrail: an
// unscoped signal enforcer logs a warning naming the env knob, so an operator
// is told about the multi-sidecar blast radius. A scoped one stays quiet.
func TestSignalEnforcerWarnsWithoutTarget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   string
		wantWarn bool
	}{
		{"no target warns", "", true},
		{"target stays quiet", "nginx", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			log := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
			newSignalEnforcer(time.Second, tc.target, log)
			warned := strings.Contains(buf.String(), EnvSignalTarget)
			if warned != tc.wantWarn {
				t.Fatalf("warned = %v, want %v (log: %q)", warned, tc.wantWarn, buf.String())
			}
		})
	}
}
