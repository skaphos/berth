package acquire

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Enforcer stops (and later re-allows) the main container when the lease
// is lost or reacquired. Hold is re-invoked on every renew tick while the
// lease is not held, so implementations must be idempotent and re-entrant
// (a kubelet-restarted container must be re-gated). See ADR-0003.
type Enforcer interface {
	// Hold stops the main container and keeps it stopped.
	Hold(ctx context.Context) error
	// Release lets the main container run again after a reacquire.
	Release(ctx context.Context) error
}

// probeEnforcer drives the injected exec liveness probe by toggling the
// shared health marker. Removing it fails the probe and the kubelet kills
// the container; restoring it lets the restarted container pass.
type probeEnforcer struct {
	state *State
}

func (p probeEnforcer) Hold(context.Context) error    { return p.state.MarkUnhealthy() }
func (p probeEnforcer) Release(context.Context) error { return p.state.MarkHealthy() }

// signalEnforcer stops the main process directly via a shared process
// namespace: SIGTERM first, then SIGKILL once the grace period elapses,
// re-signalling on every Hold so a kubelet-restarted process is re-gated.
// Release does not signal the process; it resets the per-PID escalation
// state so the next Hold starts a fresh SIGTERM rather than jumping
// straight to SIGKILL.
type signalEnforcer struct {
	grace    time.Duration
	now      func() time.Time
	find     func() ([]int, error)
	signal   func(pid int, sig syscall.Signal) error
	log      *slog.Logger
	termedAt map[int]time.Time
}

func newSignalEnforcer(grace time.Duration, target string, log *slog.Logger) *signalEnforcer {
	if target == "" {
		log.Warn("enforce=signal has no signal-target: on lease loss every process in the "+
			"shared PID namespace (except berth's own) is signaled, which can terminate "+
			"co-located sidecars; set the signal-target to scope enforcement to the workload",
			"env", EnvSignalTarget)
	}
	scan := osProcScanner()
	return &signalEnforcer{
		grace:    grace,
		now:      time.Now,
		find:     func() ([]int, error) { return scan.workloadPIDs(target) },
		signal:   func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) },
		log:      log,
		termedAt: map[int]time.Time{},
	}
}

func (s *signalEnforcer) Hold(context.Context) error {
	pids, err := s.find()
	if err != nil {
		return fmt.Errorf("find main process: %w", err)
	}
	now := s.now()
	for _, pid := range pids {
		termedAt, termed := s.termedAt[pid]
		switch {
		case !termed:
			s.termedAt[pid] = now
			if err := s.signal(pid, syscall.SIGTERM); err != nil {
				s.log.Warn("SIGTERM failed", "pid", pid, "error", err)
			} else {
				s.log.Info("sent SIGTERM to main process", "pid", pid)
			}
		case now.Sub(termedAt) >= s.grace:
			if err := s.signal(pid, syscall.SIGKILL); err != nil {
				s.log.Warn("SIGKILL failed", "pid", pid, "error", err)
			} else {
				s.log.Info("sent SIGKILL to main process", "pid", pid)
			}
		}
	}
	return nil
}

func (s *signalEnforcer) Release(context.Context) error {
	// Forget escalation state so a future Hold starts a fresh SIGTERM.
	s.termedAt = map[int]time.Time{}
	return nil
}

// newEnforcer builds the enforcer selected by cfg.Enforce.
func newEnforcer(cfg *Config, state *State, log *slog.Logger) Enforcer {
	if cfg.Enforce == EnforceSignal {
		return newSignalEnforcer(cfg.EnforceGrace, cfg.SignalTarget, log)
	}
	return probeEnforcer{state: state}
}

// procScanner reads the process table. The fields are injectable so the
// selection logic in workloadPIDs is unit-testable without a real /proc.
type procScanner struct {
	self    int    // this process, always excluded
	parent  int    // its parent (the sidecar's shell/entrypoint), excluded
	selfExe string // our own executable; other instances are excluded
	list    func() ([]int, error)
	comm    func(pid int) string // /proc/<pid>/comm; "" if unreadable
	exe     func(pid int) string // readlink /proc/<pid>/exe; "" if unreadable
}

// osProcScanner reads the live /proc filesystem.
func osProcScanner() procScanner {
	selfExe, _ := os.Readlink("/proc/self/exe")
	return procScanner{
		self:    os.Getpid(),
		parent:  os.Getppid(),
		selfExe: selfExe,
		list:    listProcPIDs,
		comm:    readProcComm,
		exe:     readProcExe,
	}
}

// workloadPIDs returns the PIDs to signal on lease loss. It always excludes
// PID 1 (the pause/init process), this process and its parent, and other
// instances of our own binary (the sidecar and the probe check). When target
// is non-empty it returns only processes whose comm or executable basename
// matches it — bounding the blast radius to the gated workload. When target is
// empty it returns every remaining process: a broad heuristic that can reach
// co-located sidecars, which is why signal mode warns when no target is set.
func (sc procScanner) workloadPIDs(target string) ([]int, error) {
	pids, err := sc.list()
	if err != nil {
		return nil, err
	}
	var out []int
	for _, pid := range pids {
		if pid <= 1 || pid == sc.self || pid == sc.parent {
			continue
		}
		if exe := sc.exe(pid); exe != "" && exe == sc.selfExe {
			continue // another instance of our own binary
		}
		if target != "" && !matchesTarget(target, sc.comm(pid), sc.exe(pid)) {
			continue
		}
		out = append(out, pid)
	}
	return out, nil
}

// matchesTarget reports whether a process identified by comm and exe path is
// the configured signal target. It matches the executable basename or comm.
// The kernel truncates comm to 15 bytes, so a longer target is also compared
// against its 15-byte prefix.
func matchesTarget(target, comm, exe string) bool {
	if comm != "" && comm == target {
		return true
	}
	if exe != "" && filepath.Base(exe) == target {
		return true
	}
	const commMax = 15 // TASK_COMM_LEN - 1
	if comm != "" && len(target) > commMax && comm == target[:commMax] {
		return true
	}
	return false
}

// listProcPIDs returns the numeric PID directories under /proc.
func listProcPIDs() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a pid directory
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// readProcComm returns the process command name from /proc/<pid>/comm, or ""
// if it cannot be read (the process may have exited).
func readProcComm(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readProcExe returns the executable path behind /proc/<pid>/exe, or "" if it
// cannot be resolved.
func readProcExe(pid int) string {
	exe, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return ""
	}
	return exe
}
