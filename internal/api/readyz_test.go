package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// readyManager is a LeaseManager that also reports backend readiness via a
// fixed error, exercising the ReadinessChecker path of NewMux. It counts how
// many times the backend was actually probed so cache behavior is observable.
type readyManager struct {
	failingManager // unused lease methods; readiness handler never calls them
	readyErr       error
	probes         atomic.Int64
}

func (m *readyManager) Ready(context.Context) error {
	m.probes.Add(1)
	return m.readyErr
}

func TestReadyzReports200WhenBackendReachable(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	NewMux(&readyManager{readyErr: nil}, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "ok\n" {
		t.Fatalf("body = %q, want %q", body, "ok\n")
	}
}

func TestReadyzReports503WhenBackendUnreachable(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	NewMux(&readyManager{readyErr: errors.New("store: dial tcp: connection refused")}, nil, nil).
		ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	// The generic body must not leak the underlying store error.
	if body := rec.Body.String(); body != "not ready\n" {
		t.Fatalf("body = %q, want %q", body, "not ready\n")
	}
}

// TestReadyzReports200WithoutChecker covers the dev/no-store configuration:
// a manager that does not implement ReadinessChecker leaves nothing to gate,
// so readiness is unconditionally 200 (distinct from the liveness route only
// in that a real backend can fail it).
func TestReadyzReports200WithoutChecker(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	// failingManager satisfies LeaseManager but not ReadinessChecker.
	NewMux(failingManager{err: errors.New("unused")}, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestReadyzServedWithNilManager confirms the route exists even in the
// no-manager configuration used by the default server and dev setups.
func TestReadyzServedWithNilManager(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	NewMux(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestReadyzCollapsesRequestStormIntoOneProbe is the core anti-amplification
// guarantee: a burst of unauthenticated /readyz requests within the cache TTL
// must hit the backend at most once, so the route cannot be weaponized into a
// backend-query amplifier.
func TestReadyzCollapsesRequestStormIntoOneProbe(t *testing.T) {
	t.Parallel()

	mgr := &readyManager{}
	mux := NewMux(mgr, nil, nil)
	for range 50 {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	}
	if n := mgr.probes.Load(); n != 1 {
		t.Fatalf("backend probed %d times for a 50-request storm, want 1", n)
	}
}

// TestReadinessGateCachesWithinTTL exercises the gate directly with an
// injected clock: results are cached within the TTL and re-probed after it.
func TestReadinessGateCachesWithinTTL(t *testing.T) {
	t.Parallel()

	mgr := &readyManager{}
	now := time.Unix(0, 0)
	gate := &readinessGate{
		checker: mgr,
		ttl:     time.Second,
		timeout: time.Second,
		now:     func() time.Time { return now },
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	// First call probes; the next two within the TTL are served from cache.
	for range 3 {
		if err := gate.ready(); err != nil {
			t.Fatalf("ready: %v", err)
		}
	}
	if n := mgr.probes.Load(); n != 1 {
		t.Fatalf("probes within TTL = %d, want 1", n)
	}

	// Advancing past the TTL forces a fresh probe.
	now = now.Add(2 * time.Second)
	if err := gate.ready(); err != nil {
		t.Fatalf("ready after TTL: %v", err)
	}
	if n := mgr.probes.Load(); n != 2 {
		t.Fatalf("probes after TTL expiry = %d, want 2", n)
	}
}

// TestReadinessGateNilCheckerAlwaysReady covers the dev/no-store gate: a nil
// checker never touches a backend and is always ready.
func TestReadinessGateNilCheckerAlwaysReady(t *testing.T) {
	t.Parallel()

	gate := &readinessGate{ttl: time.Second, timeout: time.Second, now: time.Now}
	if err := gate.ready(); err != nil {
		t.Fatalf("nil-checker gate should be ready, got %v", err)
	}
}
