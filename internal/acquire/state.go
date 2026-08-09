package acquire

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// State manages the files the helper shares between its init container,
// its sidecar, and the injected liveness probe via an emptyDir volume.
//
//	<dir>/holder   holder identity the init container acquired with
//	<dir>/token    fencing token from the successful Acquire
//	<dir>/healthy  presence-based health marker (probe enforcement)
//	<dir>/check    a copy of this binary so the probe can exec it without
//	               needing a shell in the (possibly distroless) main image
type State struct {
	dir string
}

// NewState returns a State rooted at dir.
func NewState(dir string) *State { return &State{dir: dir} }

func (s *State) holderPath() string  { return filepath.Join(s.dir, "holder") }
func (s *State) tokenPath() string   { return filepath.Join(s.dir, "token") }
func (s *State) healthyPath() string { return filepath.Join(s.dir, "healthy") }

// CheckPath is the path of the copied check binary the probe execs.
func (s *State) CheckPath() string { return filepath.Join(s.dir, "check") }

// HealthyPath is exported so the probe command can be pointed at it.
func (s *State) HealthyPath() string { return s.healthyPath() }

// WriteAcquired records a successful Acquire: it persists the holder and
// fencing token for the sidecar to renew with, then marks the pod healthy.
func (s *State) WriteAcquired(holder string, token int32) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := writeFileAtomic(s.holderPath(), []byte(holder), 0o644); err != nil {
		return fmt.Errorf("write holder: %w", err)
	}
	if err := writeFileAtomic(s.tokenPath(), []byte(strconv.FormatInt(int64(token), 10)), 0o644); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	return s.MarkHealthy()
}

// ReadHolder returns the holder identity the init container persisted.
func (s *State) ReadHolder() (string, error) {
	b, err := os.ReadFile(s.holderPath())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ReadToken returns the fencing token the init container persisted.
func (s *State) ReadToken() (int32, error) {
	b, err := os.ReadFile(s.tokenPath())
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse token %q: %w", strings.TrimSpace(string(b)), err)
	}
	return int32(n), nil
}

// MarkHealthy creates the health marker; the injected liveness probe
// passes while it exists.
func (s *State) MarkHealthy() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := writeFileAtomic(s.healthyPath(), []byte("ok\n"), 0o644); err != nil {
		return fmt.Errorf("write health marker: %w", err)
	}
	return nil
}

// MarkUnhealthy removes the health marker so the kubelet kills the main
// container. Absence of the marker is the unhealthy signal, so a missing
// file is treated as success.
func (s *State) MarkUnhealthy() error {
	if err := os.Remove(s.healthyPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove health marker: %w", err)
	}
	return nil
}

// HealthVerdict is why the health marker did or did not pass. Absent and
// Stale are deliberately distinct: they are different operational stories.
// Absent means the sidecar removed the marker, so the lease was lost — an
// expected failover. Stale means nobody removed it and nobody refreshed it,
// so the sidecar is dead or wedged — an incident.
type HealthVerdict int

const (
	// HealthOK: the marker exists and is within the freshness bound.
	HealthOK HealthVerdict = iota
	// HealthAbsent: the marker does not exist.
	HealthAbsent
	// HealthStale: the marker exists but has not been refreshed in time.
	HealthStale
	// HealthIndeterminate: the marker exists but could not be examined.
	// Treated as unhealthy — an unreadable marker is not evidence of health.
	HealthIndeterminate
)

// HealthResult is the verdict plus the evidence behind it, so a failing
// probe can report *why* rather than only that it failed.
type HealthResult struct {
	Verdict HealthVerdict
	// Age of the marker, set when it could be stat'ed.
	Age time.Duration
	// MaxAge applied; zero means the freshness check was not requested.
	MaxAge time.Duration
	// Err is the underlying error for HealthIndeterminate.
	Err error
}

// OK reports whether the marker should be treated as healthy.
func (r HealthResult) OK() bool { return r.Verdict == HealthOK }

// Reason is a short human-readable explanation suitable for probe stderr,
// which the kubelet surfaces on the pod's probe-failure event.
func (r HealthResult) Reason(path string) string {
	switch r.Verdict {
	case HealthOK:
		return ""
	case HealthAbsent:
		return fmt.Sprintf("health marker absent: %s", path)
	case HealthStale:
		return fmt.Sprintf("health marker stale: %s not refreshed for %s (limit %s); "+
			"the lease sidecar is not renewing", path, r.Age.Round(time.Second), r.MaxAge)
	default:
		return fmt.Sprintf("health marker indeterminate: %s: %v", path, r.Err)
	}
}

// EvaluateMarker is the single freshness predicate. Both the sidecar's
// [State.IsHealthy] and the probe's check subcommand call it, so the two
// can never disagree about what "healthy" means.
//
// A maxAge of zero or less requests presence-only evaluation, preserving
// the behavior of releases that had no freshness bound.
//
// Freshness compares the marker's modification time against the caller's
// own clock. Both the writing sidecar and the reading probe observe one
// node kernel clock through the shared volume, so the result never depends
// on two container clocks agreeing.
func EvaluateMarker(path string, maxAge time.Duration) HealthResult {
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return HealthResult{Verdict: HealthAbsent, MaxAge: maxAge}
	case err != nil:
		return HealthResult{Verdict: HealthIndeterminate, MaxAge: maxAge, Err: err}
	}

	if maxAge <= 0 {
		return HealthResult{Verdict: HealthOK}
	}

	age := time.Since(fi.ModTime())
	if age > maxAge {
		return HealthResult{Verdict: HealthStale, Age: age, MaxAge: maxAge}
	}
	return HealthResult{Verdict: HealthOK, Age: age, MaxAge: maxAge}
}

// IsHealthy reports whether the health marker is present. It is the
// sidecar-side view and deliberately shares [EvaluateMarker] with the
// probe so the two cannot drift apart.
func (s *State) IsHealthy() bool {
	return EvaluateMarker(s.healthyPath(), 0).OK()
}

// IsFresh reports whether the health marker is present and was refreshed
// within maxAge, using the same predicate the probe applies.
func (s *State) IsFresh(maxAge time.Duration) HealthResult {
	return EvaluateMarker(s.healthyPath(), maxAge)
}

// InstallCheckBinary copies the currently-running executable to
// <dir>/check so the injected probe can exec it in the main container,
// which may be distroless and lack a shell.
func (s *State) InstallCheckBinary() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	return copyFile(self, s.CheckPath(), 0o755)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // src is our own executable path
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	// Use a unique temp name in the destination dir to avoid clobber/races
	// with a concurrent writer.
	out, err := os.CreateTemp(filepath.Dir(dst), ".check-")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", filepath.Dir(dst), err)
	}
	tmp := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	// Chmod explicitly: os.CreateTemp makes 0600 and any create mode is
	// subject to the umask, either of which would drop the execute bits the
	// liveness probe needs to exec /berth/check.
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// writeFileAtomic writes via a sibling temp file + rename so a reader
// never observes a partial write.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
