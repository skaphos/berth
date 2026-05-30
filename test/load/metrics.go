package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// promMetrics is the driver-side (client-observed) request histogram, exposed
// so a Prometheus can scrape the load driver itself while a run is in flight.
// It is intentionally separate from the server-side internal/metrics: this
// measures latency as the client sees it.
type promMetrics struct {
	registry *prometheus.Registry
	duration *prometheus.HistogramVec
}

func newPromMetrics() *promMetrics {
	reg := prometheus.NewRegistry()
	dur := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "berth_load_request_duration_seconds",
		Help:    "Client-observed latency of lease API calls made by the load driver.",
		Buckets: prometheus.DefBuckets,
	}, []string{"op", "result"})
	reg.MustRegister(dur)
	return &promMetrics{registry: reg, duration: dur}
}

// hook matches load.NewRecorder's observation hook signature.
func (p *promMetrics) hook(op string, d time.Duration, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	p.duration.WithLabelValues(op, result).Observe(d.Seconds())
}

// serve exposes /metrics on addr until ctx is canceled.
func (p *promMetrics) serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{Registry: p.registry}))
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown metrics server: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
