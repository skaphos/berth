// Package acquire implements the berth-acquire helper: a small binary
// injected into workload pods (via the SKA-439 mutating webhook) that
// gates pod startup on a Berth lease and, in runtime-singleton mode,
// keeps the lease renewed and enforces at-most-once by stopping the main
// container when the lease is lost.
//
// It talks to the Berth API lease RPCs directly (Acquire / Renew /
// Release) and deliberately does not depend on the BerthLease CRD — see
// docs/adr/0001-pod-level-gating-for-injected-singletons.md.
package acquire

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/skaphos/berth/internal/clientauth"
	"github.com/skaphos/berth/pkg/client"
)

// Mode selects the injected helper behavior. See the design doc
// "Injected Modes" section.
type Mode string

const (
	// ModeStartupGate blocks pod start until an initial Acquire succeeds,
	// with no runtime lease guarantee after the init container exits.
	ModeStartupGate Mode = "startup-gate"
	// ModeRuntimeSingleton blocks start, then keeps a sidecar renewing the
	// lease and stops the main container if the lease is lost.
	ModeRuntimeSingleton Mode = "runtime-singleton"
)

// Enforce selects how the runtime-singleton sidecar stops the main
// container on lease loss. See ADR-0003.
type Enforce string

const (
	// EnforceProbe removes a shared health marker so an injected exec
	// liveness probe fails and the kubelet kills the container.
	EnforceProbe Enforce = "probe"
	// EnforceSignal sends SIGTERM then SIGKILL to the main process via a
	// shared process namespace.
	EnforceSignal Enforce = "signal"
)

// DefaultStateDir is the shared volume mount where the helper writes the
// fencing token, holder identity, health marker, and check binary.
const DefaultStateDir = "/berth"

// Config is the fully-resolved helper configuration. The cmd layer
// gathers values from flags / env / downward API and hands a Config to
// this package; defaulting and validation live here so both the binary
// and its tests share one source of truth.
type Config struct {
	// Lease target.
	LeaseName      string
	LeaseNamespace string

	// Behavior.
	Mode              Mode
	Enforce           Enforce
	TTL               time.Duration
	HeartbeatInterval time.Duration
	EnforceGrace      time.Duration
	// ReleaseOnShutdown, when nil, defaults per mode (true for
	// runtime-singleton, false for startup-gate). A non-nil value is an
	// explicit override.
	ReleaseOnShutdown *bool

	// Identity. HolderIdentity, when set, completely replaces the derived
	// default. The remaining fields feed the mode-specific default.
	HolderIdentity string
	ClusterID      string
	PodNamespace   string
	PodName        string
	WorkloadKind   string
	WorkloadName   string

	// Shared state volume.
	StateDir string

	// API client / auth.
	APIServer          string
	APIKey             string
	APIKeyFile         string
	CABundleFile       string
	ServerName         string
	InsecureSkipVerify bool
}

// ApplyDefaults fills mode-specific and derived defaults for any unset
// field. It is idempotent and is called by Validate.
func (c *Config) ApplyDefaults() {
	if c.StateDir == "" {
		c.StateDir = DefaultStateDir
	}
	if c.Mode == "" {
		c.Mode = ModeRuntimeSingleton
	}
	if c.Enforce == "" {
		c.Enforce = EnforceProbe
	}
	if c.LeaseNamespace == "" {
		c.LeaseNamespace = c.PodNamespace
	}
	if c.HeartbeatInterval <= 0 && c.TTL > 0 {
		// ttl/3 mirrors the operator's reacquire cadence so failover RTO
		// matches the operator-as-holder path (see reconciler.go).
		c.HeartbeatInterval = c.TTL / 3
	}
	if c.ReleaseOnShutdown == nil {
		release := c.Mode == ModeRuntimeSingleton
		c.ReleaseOnShutdown = &release
	}
}

// Validate applies defaults and then checks the invariants the helper
// depends on. The webhook (SKA-439) enforces most of these at admission;
// re-checking here keeps the binary safe when run directly.
func (c *Config) Validate() error {
	c.ApplyDefaults()

	if c.LeaseName == "" {
		return errors.New("lease name is required")
	}
	if c.LeaseNamespace == "" {
		return errors.New("lease namespace is required (set --lease-namespace or POD_NAMESPACE)")
	}
	switch c.Mode {
	case ModeStartupGate, ModeRuntimeSingleton:
	default:
		return fmt.Errorf("invalid mode %q (want %q or %q)", c.Mode, ModeStartupGate, ModeRuntimeSingleton)
	}
	switch c.Enforce {
	case EnforceProbe, EnforceSignal:
	default:
		return fmt.Errorf("invalid enforce %q (want %q or %q)", c.Enforce, EnforceProbe, EnforceSignal)
	}
	if c.TTL <= 0 {
		return errors.New("ttl must be positive")
	}
	if c.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	if c.HeartbeatInterval >= c.TTL {
		return fmt.Errorf("heartbeat interval (%s) must be less than ttl (%s)", c.HeartbeatInterval, c.TTL)
	}
	if c.EnforceGrace < 0 {
		return errors.New("enforce grace must not be negative")
	}
	if c.APIServer == "" {
		return errors.New("api server URL is required")
	}
	if c.APIKey != "" && c.APIKeyFile != "" {
		return errors.New("api key and api key file are mutually exclusive")
	}
	return nil
}

// Holder returns the holder identity for Acquire / Renew / Release. An
// explicit HolderIdentity wins; otherwise the default is mode-specific
// (see the design doc "Holder Identity Defaulting").
//
// Runtime-singleton always folds in the pod name so replicas never share
// a holder by accident. Startup-gate prefers a workload-level identity
// because it only proves startup admission.
func (c *Config) Holder() string {
	if c.HolderIdentity != "" {
		return c.HolderIdentity
	}

	var parts []string
	add := func(s string) {
		if s != "" {
			parts = append(parts, s)
		}
	}

	switch c.Mode {
	case ModeStartupGate:
		add(c.PodNamespace)
		add(c.WorkloadKind)
		add(c.WorkloadName)
		// Fall back to pod name if the workload identity is unknown, so we
		// still produce a non-empty holder.
		if len(parts) <= 1 {
			add(c.PodName)
		}
	default: // runtime-singleton
		add(c.ClusterID)
		add(c.PodNamespace)
		add(c.WorkloadKind)
		add(c.WorkloadName)
		if c.PodName != "" {
			parts = append(parts, "pod", c.PodName)
		}
	}

	return strings.Join(parts, ":")
}

// NewClient builds a Berth API client from the configured endpoint and
// auth, reusing the shared clientauth helpers. When APIKeyFile is set the
// returned cleanup-free client reads a refreshing token on every request.
func (c *Config) NewClient() (*client.Client, error) {
	opts := []client.Option{}
	switch {
	case c.APIKeyFile != "":
		ts, err := clientauth.NewFileTokenSource(c.APIKeyFile, time.Second)
		if err != nil {
			return nil, fmt.Errorf("load api key file: %w", err)
		}
		opts = append(opts, client.WithAPIKeyFunc(ts.Get))
	case c.APIKey != "":
		opts = append(opts, client.WithAPIKey(c.APIKey))
	}

	tlsCfg, err := clientauth.LoadTLSConfig(c.CABundleFile, c.ServerName, c.InsecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("load TLS config: %w", err)
	}
	opts = append(opts, client.WithTLSConfig(tlsCfg))

	return client.New(c.APIServer, opts...), nil
}
