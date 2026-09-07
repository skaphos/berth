package acquire

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Run in an isolated Linux container as root with SETUID/SETGID but no KILL
// capability: BERTH_TEST_CROSS_UID=1 <test-binary> -test.run=TestSignalCrossUID.
// The helper child uses the shipped image's nonroot UID. No host PIDs are used.
func TestSignalCrossUID(t *testing.T) {
	switch os.Getenv("BERTH_SIGNAL_TEST_ROLE") {
	case "target":
		fmt.Println("ready")
		for {
			time.Sleep(time.Hour)
		}
	case "helper":
		if os.Geteuid() != 65532 {
			t.Fatal("helper did not drop to the shipped nonroot UID")
		}
		pid, err := strconv.Atoi(os.Getenv("BERTH_SIGNAL_TEST_PID"))
		if err != nil {
			t.Fatal(err)
		}
		state := NewState(os.Getenv("BERTH_SIGNAL_TEST_STATE"))
		if err := state.MarkHealthy(); err != nil {
			t.Fatal(err)
		}
		e := newEnforcer(&Config{Enforce: EnforceSignal, SignalTarget: "target"}, state, testLogger()).(*signalEnforcer)
		e.find = func() ([]int, error) { return []int{pid}, nil }
		// Exercise the real Linux permission check for both TERM and KILL.
		for attempt := 0; attempt < 2; attempt++ {
			if err := e.Hold(context.Background()); !errors.Is(err, syscall.EPERM) {
				t.Fatalf("want real cross-UID EPERM, got %v", err)
			}
			if state.IsFresh(time.Second).OK() {
				t.Fatal("permission failure left the mandatory probe healthy")
			}
		}
		if err := e.Release(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !state.IsFresh(time.Second).OK() {
			t.Fatal("renewal did not restore freshness")
		}
		return
	}
	if os.Getenv("BERTH_TEST_CROSS_UID") != "1" {
		t.Skip("opt-in isolated Linux cross-UID test")
	}
	if os.Geteuid() != 0 {
		t.Fatal("cross-UID harness requires container root")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	target := exec.Command(exe, "-test.run=^TestSignalCrossUID$")
	target.Env = append(os.Environ(), "BERTH_SIGNAL_TEST_ROLE=target")
	stdout, err := target.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Process.Kill(); _ = target.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("target readiness %q: %v", line, err)
	}
	dir, err := os.MkdirTemp("", "berth-cross-uid-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	helper := exec.CommandContext(t.Context(), exe, "-test.run=^TestSignalCrossUID$")
	helper.Env = append(os.Environ(), "BERTH_SIGNAL_TEST_ROLE=helper", "BERTH_SIGNAL_TEST_PID="+strconv.Itoa(target.Process.Pid), "BERTH_SIGNAL_TEST_STATE="+dir)
	helper.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65532, Gid: 65532}}
	if out, err := helper.CombinedOutput(); err != nil {
		t.Fatalf("nonroot helper: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	// Failed signals did not kill the root workload; the failing health probe
	// is the necessary fallback. This harness does not simulate a kubelet.
	if err := target.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("target unexpectedly exited: %v", err)
	}
}
