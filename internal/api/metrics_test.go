package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type recordingMetrics struct {
	route       string
	method      string
	status      int
	dur         time.Duration
	calls       int
	outcomes    []string
	inflightNow int
	inflightMax int
}

func (r *recordingMetrics) ObserveRequest(route, method string, status int, dur time.Duration) {
	r.route = route
	r.method = method
	r.status = status
	r.dur = dur
	r.calls++
}

func (r *recordingMetrics) ObserveOutcome(outcome string) {
	r.outcomes = append(r.outcomes, outcome)
}

func (r *recordingMetrics) IncInflight() {
	r.inflightNow++
	if r.inflightNow > r.inflightMax {
		r.inflightMax = r.inflightNow
	}
}

func (r *recordingMetrics) DecInflight() { r.inflightNow-- }

func TestMetricsMiddlewareRecordsMatchedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := &recordingMetrics{}
	h := MetricsMiddleware(rec)(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/widgets/42", nil))

	if rec.route != "GET /widgets/{id}" {
		t.Fatalf("route = %q, want the templated pattern", rec.route)
	}
	if rec.method != http.MethodGet {
		t.Fatalf("method = %q, want GET", rec.method)
	}
	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusTeapot)
	}
	if rec.inflightNow != 0 || rec.inflightMax != 1 {
		t.Fatalf("inflight now=%d max=%d, want now=0 max=1", rec.inflightNow, rec.inflightMax)
	}
}

func TestMetricsMiddlewareLabelsUnmatchedRoute(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux() // nothing registered → 404, empty Pattern
	rec := &recordingMetrics{}
	h := MetricsMiddleware(rec)(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.route != "unmatched" {
		t.Fatalf("route = %q, want unmatched", rec.route)
	}
	if rec.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.status)
	}
}

func TestStatusRecorderDefaultsTo200OnBodyOnlyWrite(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hi")) // no explicit WriteHeader
	})
	rec := &recordingMetrics{}
	h := MetricsMiddleware(rec)(mux)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))

	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a body-only write", rec.status)
	}
}
