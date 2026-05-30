package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/skaphos/berth/internal/lease"
)

// failingManager is a LeaseManager whose every operation returns a fixed error
// carrying backend-identifying detail, standing in for a store outage.
type failingManager struct{ err error }

func (f failingManager) Acquire(context.Context, lease.Key, string, time.Duration) (lease.AcquireResult, error) {
	return lease.AcquireResult{}, f.err
}

func (f failingManager) Renew(context.Context, lease.Key, string, int32, time.Duration) (lease.AcquireResult, error) {
	return lease.AcquireResult{}, f.err
}

func (f failingManager) Release(context.Context, lease.Key, string, int32) error {
	return f.err
}

// TestInternalErrorsAreGenericButLoggedWithCorrelationID asserts the SKA-448
// contract on every 5xx path: the client gets a generic envelope plus the
// correlation id, the backend detail is withheld from the wire but recorded in
// the server-side log line, and the id in the body matches the X-Request-Id
// header so an operator can join the two.
func TestInternalErrorsAreGenericButLoggedWithCorrelationID(t *testing.T) {
	t.Parallel()

	const secret = "sql store: dial tcp 10.1.2.3:5432: connection refused"
	mgr := failingManager{err: errors.New(secret)}
	logger, buf := capturingLogger()
	srv := httptest.NewServer(LoggingMiddleware(logger)(NewMux(mgr, nil, nil)))
	t.Cleanup(srv.Close)

	cases := []struct{ name, path, body string }{
		{"acquire", "/v1alpha1/namespaces/ns/leases/a/acquire", `{"holder":"h","ttlSeconds":30}`},
		{"renew", "/v1alpha1/namespaces/ns/leases/a/renew", `{"holder":"h","fencingToken":1,"ttlSeconds":30}`},
		{"release", "/v1alpha1/namespaces/ns/leases/a/release", `{"holder":"h","fencingToken":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
				srv.URL+tc.path, strings.NewReader(tc.body))
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", resp.StatusCode)
			}
			if strings.Contains(string(raw), secret) {
				t.Fatalf("response leaked backend detail: %s", raw)
			}

			var env errorResponse
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatalf("body not JSON: %v (%q)", err, raw)
			}
			if env.Error != "internal error" {
				t.Fatalf("error = %q, want %q", env.Error, "internal error")
			}
			if env.RequestID == "" {
				t.Fatal("5xx body must carry a requestId for correlation")
			}
			if hdr := resp.Header.Get("X-Request-Id"); env.RequestID != hdr {
				t.Fatalf("body requestId %q != X-Request-Id header %q", env.RequestID, hdr)
			}

			// The detail must survive server-side, at error level, joined to the
			// client by the same correlation id.
			var line map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
				t.Fatalf("log line not JSON: %v (%q)", err, buf.String())
			}
			if line["level"] != "ERROR" {
				t.Fatalf("log level = %v, want ERROR", line["level"])
			}
			if line["error"] != secret {
				t.Fatalf("log error = %v, want the detailed backend error", line["error"])
			}
			if line["request_id"] != env.RequestID {
				t.Fatalf("log request_id %v != body requestId %q", line["request_id"], env.RequestID)
			}
		})
	}
}

// TestReleaseConflictStaysGenericNot500 guards the regression boundary: a
// conflict is a safe, deliberate 409 message and must not be reclassified as a
// 5xx by the error-redaction change.
func TestReleaseConflictStaysGenericNot500(t *testing.T) {
	t.Parallel()

	mgr := failingManager{err: lease.ErrConflict}
	srv := httptest.NewServer(NewMux(mgr, nil, nil))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/v1alpha1/namespaces/ns/leases/a/release",
		strings.NewReader(`{"holder":"h","fencingToken":1}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// TestInternalErrorCarriesIDWithoutObservabilityMiddleware pins the 5xx
// contract for the bare mux: NewMux can be used directly (tests, package
// clients) with no LoggingMiddleware to mint or carry a correlation id. The
// generic body must still include a non-empty requestId — and must still not
// leak the backend detail — by synthesizing the id in writeInternalError.
func TestInternalErrorCarriesIDWithoutObservabilityMiddleware(t *testing.T) {
	t.Parallel()

	const secret = "k8s lease store: get ns/a: connection refused"
	srv := httptest.NewServer(NewMux(failingManager{err: errors.New(secret)}, nil, nil))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/v1alpha1/namespaces/ns/leases/a/acquire",
		strings.NewReader(`{"holder":"h","ttlSeconds":30}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("response leaked backend detail without middleware: %s", raw)
	}
	var env errorResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("body not JSON: %v (%q)", err, raw)
	}
	if env.RequestID == "" {
		t.Fatal("5xx body must carry a requestId even when no observability middleware is installed")
	}
}
