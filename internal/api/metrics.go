package api

import (
	"net/http"
	"time"
)

// RequestMetrics is the subset of metrics recording the HTTP layer needs.
// Defined here as an interface so the api package stays free of a direct
// Prometheus dependency; internal/metrics provides the implementation.
type RequestMetrics interface {
	// ObserveRequest records one served request. route is the matched mux
	// pattern (templated, so cardinality is bounded), not the raw path.
	ObserveRequest(route, method string, status int, dur time.Duration)
	// ObserveOutcome records one lease request's semantic outcome (acquired,
	// held-by-other, conflict, unauthorized, …) — the signal HTTP status hides.
	ObserveOutcome(outcome string)
	IncInflight()
	DecInflight()
}

// MetricsMiddleware records RED metrics for every request passing through it.
// It should wrap the mux outside auth/handlers so the recorded status reflects
// the final response, including auth rejections. The route label is taken from
// [http.Request.Pattern], which the mux populates during routing, so it is read
// after the inner handler returns.
func MetricsMiddleware(m RequestMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.IncInflight()
			defer m.DecInflight()

			r, rc := ensureRequestContext(r)
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)

			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			m.ObserveRequest(route, r.Method, rec.status, time.Since(start))
			if rc.outcome != "" {
				m.ObserveOutcome(rc.outcome)
			}
		})
	}
}

// statusRecorder captures the response status code while delegating writes to
// the wrapped ResponseWriter. A handler that writes a body without calling
// WriteHeader implicitly returns 200, which is the zero-configured default.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}
