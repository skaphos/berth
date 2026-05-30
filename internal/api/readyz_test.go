package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// readyManager is a LeaseManager that also reports backend readiness via a
// fixed error, exercising the ReadinessChecker path of NewMux.
type readyManager struct {
	failingManager // unused lease methods; readiness handler never calls them
	readyErr       error
}

func (m readyManager) Ready(context.Context) error { return m.readyErr }

func TestReadyzReports200WhenBackendReachable(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	NewMux(readyManager{readyErr: nil}, nil, nil).ServeHTTP(rec, req)

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
	NewMux(readyManager{readyErr: errors.New("store: dial tcp: connection refused")}, nil, nil).
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
