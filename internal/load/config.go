// Package load is the engine behind the Berth API-level load driver
// (test/load). It drives the lease API through pkg/client under a set of named
// scenarios, records client-observed latency, and produces a summary suitable
// for CI assertions. It holds no flag parsing or process wiring — that lives in
// the thin test/load entrypoint — so the scenarios stay unit-testable against
// an in-process server.
package load

import (
	"context"
	"fmt"
	"time"

	"github.com/skaphos/berth/pkg/client"
)

// Scenario names the load shape to run. The shapes mirror the sizing model in
// docs/operations/scalability.md.
type Scenario string

const (
	// ScenarioSteady holds N leases and renews them at heartbeat cadence while
	// a standby contends for each — the steady-state holder + denied-acquire
	// load.
	ScenarioSteady Scenario = "steady"
	// ScenarioColdStart fires all acquires near-simultaneously, modeling every
	// holder racing for its lease inside the first heartbeat window.
	ScenarioColdStart Scenario = "coldstart"
	// ScenarioFailover lets the failover half of the leases expire while the
	// survivor half keeps renewing, and times the standby acquire-after-expiry
	// that reclaims the expired half against that concurrent renew load.
	ScenarioFailover Scenario = "failover"
	// ScenarioChurn renews steadily while a fraction of holders release and a
	// fresh holder re-acquires each heartbeat.
	ScenarioChurn Scenario = "churn"
)

// Operation labels group recorded latencies by the API call that produced
// them. They are the keys in a [Summary].
const (
	OpAcquire = "acquire"
	OpRenew   = "renew"
	OpRelease = "release"
)

// Config is the fully-resolved driver configuration. The thin entrypoint
// builds it from flags; [Config.Validate] enforces invariants.
type Config struct {
	// Namespace is the lease namespace all generated leases live under.
	Namespace string
	// Backend is an informational tag recorded in the summary (e.g. "k8s",
	// "sql"); it does not change driver behavior.
	Backend string

	Scenario Scenario

	// Leases is the number of distinct leases driven.
	Leases int
	// Pairs is the number of region pairs; used only to label leases by region
	// for reporting. Must divide into Leases conceptually but is not enforced.
	Pairs int

	TTL       time.Duration
	Heartbeat time.Duration
	// Duration bounds the sustained scenarios (steady, churn). Ignored by the
	// one-shot scenarios (coldstart, failover).
	Duration time.Duration

	// Concurrency bounds in-flight requests during burst phases (initial
	// acquire, failover reclaim). The sustained scenarios run one goroutine per
	// lease regardless, since the point is to keep every lease active.
	Concurrency int

	// ChurnFraction is the per-heartbeat probability that a held lease is
	// released and re-acquired by a fresh holder. Only used by ScenarioChurn.
	ChurnFraction float64
}

// Validate returns an error if the configuration cannot produce a meaningful
// run.
func (c Config) Validate() error {
	switch c.Scenario {
	case ScenarioSteady, ScenarioColdStart, ScenarioFailover, ScenarioChurn:
	case "":
		return fmt.Errorf("scenario is required (one of %s, %s, %s, %s)",
			ScenarioSteady, ScenarioColdStart, ScenarioFailover, ScenarioChurn)
	default:
		return fmt.Errorf("unknown scenario %q", c.Scenario)
	}
	if c.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if c.Leases <= 0 {
		return fmt.Errorf("leases must be positive, got %d", c.Leases)
	}
	if c.Pairs <= 0 {
		return fmt.Errorf("pairs must be positive, got %d", c.Pairs)
	}
	if c.TTL < time.Second {
		// The API expresses TTL as a whole-second int32 (ttlSeconds), so a
		// sub-second TTL would serialize to 0 and be rejected by the server.
		return fmt.Errorf("ttl must be at least 1s (API granularity is whole seconds), got %s", c.TTL)
	}
	if c.Heartbeat <= 0 {
		return fmt.Errorf("heartbeat must be positive, got %s", c.Heartbeat)
	}
	if c.Heartbeat >= c.TTL {
		return fmt.Errorf("heartbeat (%s) must be shorter than ttl (%s) or leases expire between renewals", c.Heartbeat, c.TTL)
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be positive, got %d", c.Concurrency)
	}
	switch c.Scenario {
	case ScenarioSteady, ScenarioChurn:
		if c.Duration <= 0 {
			return fmt.Errorf("duration must be positive for the %s scenario", c.Scenario)
		}
	}
	if c.Scenario == ScenarioChurn && (c.ChurnFraction <= 0 || c.ChurnFraction > 1) {
		return fmt.Errorf("churn-fraction must be in (0,1] for the churn scenario, got %g", c.ChurnFraction)
	}
	return nil
}

// leaseName is the deterministic name of the i-th lease.
func leaseName(i int) string { return fmt.Sprintf("lease-%06d", i) }

// activeHolder is the identity of the i-th lease's primary holder.
func activeHolder(i int) string { return fmt.Sprintf("active-%06d", i) }

// standbyHolder is the identity contending for the i-th lease from the paired
// region.
func standbyHolder(i int) string { return fmt.Sprintf("standby-%06d", i) }

// acquireResult aliases the client's result type so the scenario runner can
// reference it without importing pkg/client directly.
type acquireResult = client.AcquireResult

// LeaseClient is the subset of [client.Client] the scenarios use. It is an
// interface so tests can substitute a fake and the scenarios can run against an
// in-process server.
type LeaseClient interface {
	Acquire(ctx context.Context, namespace, name, holder string, ttl time.Duration) (client.AcquireResult, error)
	Renew(ctx context.Context, namespace, name, holder string, fencingToken int32, ttl time.Duration) (client.AcquireResult, error)
	Release(ctx context.Context, namespace, name, holder string, fencingToken int32) error
}
