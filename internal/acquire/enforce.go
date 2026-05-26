package acquire

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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

func newSignalEnforcer(grace time.Duration, log *slog.Logger) *signalEnforcer {
	return &signalEnforcer{
		grace:    grace,
		now:      time.Now,
		find:     findMainPIDs,
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
		return newSignalEnforcer(cfg.EnforceGrace, log)
	}
	return probeEnforcer{state: state}
}

// findMainPIDs returns candidate main-container PIDs visible in the
// shared process namespace: everything except PID 1 (the pause/init
// process), this process and its parent, and processes running our own
// binary (the sidecar and the probe check). It is a heuristic; the design
// reserves signal mode for cases where probe injection is not viable and
// recommends probe as the default.
func findMainPIDs() ([]int, error) {
	self := os.Getpid()
	parent := os.Getppid()
	selfExe, _ := os.Readlink("/proc/self/exe")

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
		if pid <= 1 || pid == self || pid == parent {
			continue
		}
		if exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe")); err == nil && exe == selfExe {
			continue // another instance of our own binary
		}
		pids = append(pids, pid)
	}
	return pids, nil
}
