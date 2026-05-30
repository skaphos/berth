package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/skaphos/berth/internal/auth"
	"github.com/skaphos/berth/internal/lease"
)

func capturingLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

func TestRequestIDSources(t *testing.T) {
	t.Parallel()

	const traceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	cases := []struct {
		name    string
		headers map[string]string
		want    string // "" means "expect a generated 32-hex id"
	}{
		{name: "safe X-Request-Id honored", headers: map[string]string{"X-Request-Id": "abc-123_DEF"}, want: "abc-123_DEF"},
		{name: "traceparent trace-id extracted", headers: map[string]string{"traceparent": traceparent}, want: "4bf92f3577b34da6a3ce929d0e0e4736"},
		{name: "unsafe X-Request-Id rejected", headers: map[string]string{"X-Request-Id": "bad id\nwith newline"}, want: ""},
		{name: "malformed traceparent rejected", headers: map[string]string{"traceparent": "garbage"}, want: ""},
		{name: "none → generated", headers: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			got := requestID(r)
			if tc.want != "" {
				if got != tc.want {
					t.Fatalf("requestID = %q, want %q", got, tc.want)
				}
				return
			}
			if len(got) != 32 || !isHex(got) {
				t.Fatalf("requestID = %q, want a generated 32-hex id", got)
			}
		})
	}
}

func TestLoggingMiddlewareEmitsAccessLineAndHeader(t *testing.T) {
	t.Parallel()

	logger, buf := capturingLogger()
	h := LoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	// Route via a mux so r.Pattern is populated.
	mux := http.NewServeMux()
	mux.Handle("GET /widgets/{id}", h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/widgets/42", nil))

	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("X-Request-Id response header must be set")
	}

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("log line not JSON: %v (%q)", err, buf.String())
	}
	for k, want := range map[string]any{"method": "GET", "path": "/widgets/42", "route": "GET /widgets/{id}", "status": float64(http.StatusTeapot)} {
		if line[k] != want {
			t.Fatalf("log[%q] = %v, want %v", k, line[k], want)
		}
	}
	if line["request_id"] != rec.Header().Get("X-Request-Id") {
		t.Fatalf("log request_id %v != header %q", line["request_id"], rec.Header().Get("X-Request-Id"))
	}
	if _, ok := line["duration"]; !ok {
		t.Fatal("log line must carry a duration")
	}
}

func TestLoggingMiddlewareLogsIdentityNeverToken(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-bearer-token"
	authn := &fakeAuthenticator{identity: &auth.Identity{Holder: "team-a", Tenant: "team-a", Raw: secret}}
	mgr := lease.NewManager(lease.NewMemStore())

	logger, buf := capturingLogger()
	handler := LoggingMiddleware(logger)(NewMux(mgr, authn, nil))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/v1alpha1/namespaces/ns/leases/a/acquire",
		strings.NewReader(`{"holder":"team-a","ttlSeconds":30}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Fatalf("access log leaked the bearer token: %s", out)
	}
	if !strings.Contains(out, `"holder":"team-a"`) || !strings.Contains(out, `"tenant":"team-a"`) {
		t.Fatalf("access log missing identity fields: %s", out)
	}
	if !strings.Contains(out, `"outcome":"acquired"`) {
		t.Fatalf("access log missing acquired outcome: %s", out)
	}
}

func TestLoggingMiddlewareHealthzAtDebug(t *testing.T) {
	t.Parallel()

	// An info-level logger drops the debug-level healthz line entirely.
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := LoggingMiddleware(logger)(NewMux(nil, nil, nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if buf.Len() != 0 {
		t.Fatalf("healthz must log below info, got: %s", buf.String())
	}
	// The correlation header is still emitted even when the line is suppressed.
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("X-Request-Id header expected on healthz")
	}
}

// TestObservabilityChainRecordsOutcomes drives the full Logging→Metrics→mux
// chain and asserts the semantic outcome counter fires for each result class,
// including held-by-other (a 200 that HTTP-status metrics cannot distinguish)
// and unauthorized.
func TestObservabilityChainRecordsOutcomes(t *testing.T) {
	t.Parallel()

	authn := &fakeAuthenticator{identity: &auth.Identity{Holder: "team-a", Tenant: "team-a"}}
	mgr := lease.NewManager(lease.NewMemStore())
	rec := &recordingMetrics{}
	logger, _ := capturingLogger()
	handler := LoggingMiddleware(logger)(MetricsMiddleware(rec)(NewMux(mgr, authn, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	post := func(holder, token string) int {
		t.Helper()
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
			srv.URL+"/v1alpha1/namespaces/ns/leases/a/acquire",
			strings.NewReader(`{"holder":"`+holder+`","ttlSeconds":30}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	post("team-a", "tok")            // first acquire → acquired
	post("team-a/other", "tok")      // same lease, other holder in-tenant → held-by-other
	post("team-a", "")               // missing token → unauthorized
	authn.err = errors.New("denied") // force auth failure
	if got := post("team-a", "tok"); got != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", got)
	}

	got := strings.Join(rec.outcomes, ",")
	for _, want := range []string{outcomeAcquired, outcomeHeldByOther, outcomeUnauthorized} {
		if !strings.Contains(got, want) {
			t.Fatalf("outcomes %q missing %q", got, want)
		}
	}
}
