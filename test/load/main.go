// Command berth-load is the API-level load driver for Berth (scalability
// Phase 3). It drives the lease API through pkg/client under a named scenario
// and prints a JSON latency summary; optionally it serves its own /metrics so a
// Prometheus can scrape the driver during a run. It is a test/dev tool — not a
// shipped binary — run with `go run ./test/load`.
//
// Example:
//
//	go run ./test/load \
//	  --target=https://berth.example.com:8443 --ca-file=ca.pem \
//	  --api-key-file=/etc/berth/token --scenario=steady \
//	  --leases=2000 --pairs=8 --ttl=30s --heartbeat=10s --duration=5m
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/skaphos/berth/internal/load"
	"github.com/skaphos/berth/pkg/client"
)

func main() { os.Exit(run()) }

func run() int {
	var (
		target         string
		metricsAddr    string
		apiKey         string
		apiKeyFile     string
		caFile         string
		insecure       bool
		requestTimeout time.Duration
		cfg            load.Config
		scenario       string
	)

	flag.StringVar(&target, "target", "", "API server base URL, e.g. https://berth.example.com:8443 (required)")
	flag.StringVar(&cfg.Namespace, "namespace", "berth-load", "lease namespace for generated leases")
	flag.StringVar(&cfg.Backend, "store-backend", "", "informational backend tag recorded in the summary (k8s|sql); does not change behavior")
	flag.StringVar(&scenario, "scenario", "", "load scenario: steady, coldstart, failover, or churn (required)")
	flag.IntVar(&cfg.Leases, "leases", 2000, "number of distinct leases to drive")
	flag.IntVar(&cfg.Pairs, "pairs", 8, "number of region pairs (labeling only)")
	flag.DurationVar(&cfg.TTL, "ttl", 30*time.Second, "lease TTL")
	flag.DurationVar(&cfg.Heartbeat, "heartbeat", 10*time.Second, "renew/contend cadence; must be shorter than ttl")
	flag.DurationVar(&cfg.Duration, "duration", 5*time.Minute, "run length for sustained scenarios (steady, churn)")
	flag.IntVar(&cfg.Concurrency, "concurrency", 256, "max in-flight requests during burst phases")
	flag.Float64Var(&cfg.ChurnFraction, "churn-fraction", 0.1, "per-heartbeat restart probability for the churn scenario")
	flag.StringVar(&metricsAddr, "metrics-addr", "", "expose the driver's own /metrics on this address (empty disables)")
	flag.StringVar(&apiKey, "api-key", "", "static bearer token (mutually exclusive with --api-key-file)")
	flag.StringVar(&apiKeyFile, "api-key-file", "", "file containing a bearer token, re-read each request (mutually exclusive with --api-key)")
	flag.StringVar(&caFile, "ca-file", "", "PEM CA bundle for API server TLS verification")
	flag.BoolVar(&insecure, "insecure-skip-tls-verify", false, "skip API server TLS verification (development only)")
	flag.DurationVar(&requestTimeout, "request-timeout", 10*time.Second, "per-request timeout")
	flag.Parse()

	cfg.Scenario = load.Scenario(scenario)

	if target == "" {
		slog.Error("--target is required")
		return 2
	}
	if apiKey != "" && apiKeyFile != "" {
		slog.Error("--api-key and --api-key-file are mutually exclusive")
		return 2
	}
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		return 2
	}

	cli, err := buildClient(target, apiKey, apiKeyFile, caFile, insecure, requestTimeout)
	if err != nil {
		slog.Error("build client", "error", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var hook func(string, time.Duration, error)
	if metricsAddr != "" {
		pm := newPromMetrics()
		hook = pm.hook
		go func() {
			slog.Info("driver metrics endpoint listening", "addr", metricsAddr, "path", "/metrics")
			if err := pm.serve(ctx, metricsAddr); err != nil && !isServerClosed(err) {
				slog.Error("driver metrics server exited", "error", err)
			}
		}()
	}

	slog.Info("starting load run",
		"scenario", cfg.Scenario, "leases", cfg.Leases, "ttl", cfg.TTL,
		"heartbeat", cfg.Heartbeat, "target", target)

	rec := load.NewRecorder(hook)
	summary, err := load.Run(ctx, cli, cfg, rec)
	if err != nil {
		slog.Error("run failed", "error", err)
		return 1
	}

	if err := json.NewEncoder(os.Stdout).Encode(summary); err != nil {
		slog.Error("encode summary", "error", err)
		return 1
	}
	return 0
}

func isServerClosed(err error) bool {
	return err != nil && err.Error() == http.ErrServerClosed.Error()
}

// buildClient assembles a pkg/client Client with the requested TLS and auth.
func buildClient(target, apiKey, apiKeyFile, caFile string, insecure bool, timeout time.Duration) (*client.Client, error) {
	httpClient := &http.Client{Timeout: timeout}
	opts := []client.Option{client.WithHTTPClient(httpClient)}

	if strings.HasPrefix(target, "https://") {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec // gated behind an explicit dev-only flag
		if caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("read --ca-file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("--ca-file %q contained no PEM certificates", caFile)
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, client.WithTLSConfig(tlsCfg))
	}

	switch {
	case apiKey != "":
		opts = append(opts, client.WithAPIKey(apiKey))
	case apiKeyFile != "":
		opts = append(opts, client.WithAPIKeyFunc(tokenFileGetter(apiKeyFile)))
	}

	return client.New(target, opts...), nil
}

// tokenFileGetter returns a getter that reads and trims the token file on each
// call, so an externally-rotated token is picked up without a restart.
func tokenFileGetter(path string) func() string {
	return func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("read api key file", "path", path, "error", err)
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}
