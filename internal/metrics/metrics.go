// Package metrics holds the Prometheus instrumentation for the Berth API
// server: RED metrics on the HTTP lease handlers and latency on the backing
// [lease.Store] calls. Collectors live on a private registry so the /metrics
// endpoint exposes only Berth's series plus the standard process/Go collectors,
// and so tests can construct an isolated instance.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// requestBuckets cover whole-request latency (network + auth + store).
var requestBuckets = prometheus.DefBuckets

// storeBuckets are finer-grained: an in-memory store call is sub-microsecond,
// while a networked SQL or etcd call is milliseconds. 100µs..~3.3s.
var storeBuckets = prometheus.ExponentialBuckets(0.0001, 2, 16)

// Metrics is the registry plus the Berth collectors. It implements the
// recorder interfaces consumed by the api and lease layers.
type Metrics struct {
	registry *prometheus.Registry

	reqDuration   *prometheus.HistogramVec
	reqTotal      *prometheus.CounterVec
	reqInflight   prometheus.Gauge
	leaseOutcomes *prometheus.CounterVec
	storeDuration *prometheus.HistogramVec
}

// New constructs a Metrics with all collectors registered on a fresh registry,
// including the default process and Go runtime collectors.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		reqDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "berth_apiserver_request_duration_seconds",
			Help:    "Latency of API server HTTP requests by route, method, and status.",
			Buckets: requestBuckets,
		}, []string{"route", "method", "status"}),
		reqTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "berth_apiserver_requests_total",
			Help: "Total API server HTTP requests by route, method, and status.",
		}, []string{"route", "method", "status"}),
		reqInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "berth_apiserver_requests_inflight",
			Help: "API server HTTP requests currently being served.",
		}),
		leaseOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "berth_apiserver_lease_outcomes_total",
			Help: "Lease request outcomes by semantic result (acquired, held-by-other, " +
				"renewed, released, conflict, unauthorized, error). Distinguishes signals " +
				"that share an HTTP status — notably held-by-other, which is a 200.",
		}, []string{"outcome"}),
		storeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "berth_lease_store_call_duration_seconds",
			Help:    "Latency of lease store calls by operation, backend, and outcome.",
			Buckets: storeBuckets,
		}, []string{"op", "backend", "outcome"}),
	}
	reg.MustRegister(
		m.reqDuration,
		m.reqTotal,
		m.reqInflight,
		m.leaseOutcomes,
		m.storeDuration,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// ObserveRequest records one served HTTP request. status is the numeric HTTP
// status code; it is formatted as a label so cardinality stays bounded to the
// codes actually returned.
func (m *Metrics) ObserveRequest(route, method string, status int, dur time.Duration) {
	code := fmt.Sprintf("%d", status)
	m.reqDuration.WithLabelValues(route, method, code).Observe(dur.Seconds())
	m.reqTotal.WithLabelValues(route, method, code).Inc()
}

// ObserveOutcome records one lease request's semantic outcome. The set of
// outcome values is small and fixed, so label cardinality stays bounded.
func (m *Metrics) ObserveOutcome(outcome string) {
	m.leaseOutcomes.WithLabelValues(outcome).Inc()
}

// IncInflight increments the in-flight request gauge.
func (m *Metrics) IncInflight() { m.reqInflight.Inc() }

// DecInflight decrements the in-flight request gauge.
func (m *Metrics) DecInflight() { m.reqInflight.Dec() }

// ObserveStoreCall records the latency and outcome of one lease store call.
func (m *Metrics) ObserveStoreCall(op, backend, outcome string, dur time.Duration) {
	m.storeDuration.WithLabelValues(op, backend, outcome).Observe(dur.Seconds())
}

// Handler returns the HTTP handler exposing this instance's metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{Registry: m.registry})
}

// Serve runs a dedicated HTTP server exposing /metrics (and /healthz) on addr
// until ctx is canceled. It is intended to run on a separate, unauthenticated
// port — mirroring the operator's controller-runtime metrics endpoint — so a
// Prometheus scrape never traverses the API server's TLS/auth path. The port
// is expected to be restricted to the monitoring stack via NetworkPolicy.
func (m *Metrics) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- srv.ListenAndServe() }()

	select {
	case err := <-serveErrCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown metrics server: %w", err)
		}
		if err := <-serveErrCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
