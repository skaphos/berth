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
	"math"
	"net/url"
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
	// SignalTarget bounds enforce=signal to the workload process. It matches a
	// process by comm or executable basename (e.g. "nginx"). When empty the
	// enforcer falls back to a broad heuristic that signals every process in the
	// shared PID namespace except a small exclusion set — which can take down
	// co-located sidecars — so it warns loudly. Set it to scope the blast radius
	// to the gated workload. Ignored unless Enforce is EnforceSignal.
	SignalTarget string
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
	PodUID         string
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
	if c.Mode == ModeRuntimeSingleton && c.Enforce == EnforceSignal && c.SignalTarget == "" {
		return fmt.Errorf("signal target is required when enforce=%s in mode=%s: set %s to a process "+
			"comm or executable basename (e.g. \"nginx\"); an empty target signals every process in the "+
			"shared PID namespace, which can terminate co-located sidecars", EnforceSignal, ModeRuntimeSingleton, EnvSignalTarget)
	}
	if c.Mode == ModeRuntimeSingleton && c.HolderIdentity == "" {
		if c.PodUID == "" {
			return errors.New("pod UID is required for the runtime-singleton holder (set --pod-uid or POD_UID from metadata.uid)")
		}
		if strings.ContainsAny(c.PodUID, ":/ \t\r\n") || strings.TrimSpace(c.PodUID) != c.PodUID {
			return errors.New("pod UID must be a non-whitespace identity component without ':' or '/'")
		}
		// The lease API limits decoded holders to 253 bytes. Check before the
		// Acquire retry loop so adding the UID cannot leave an oversized holder
		// retrying forever. Preserve the full UID and tenant root.
		if len(c.Holder()) > 253 {
			return errors.New("runtime holder exceeds the API limit of 253 bytes; shorten cluster, workload or pod names")
		}
	}
	if c.TTL <= 0 || c.TTL > time.Duration(math.MaxInt32)*time.Second {
		return errors.New("ttl must be positive and at most 2147483647 seconds (the API limit)")
	}
	if c.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	// Strict inequality is not enough. At heartbeat = ttl-1s the margin to
	// server-side expiry is one second, so any renewal slower than that leaves
	// the server treating the lease as expired: a transient hiccup becomes
	// definitive loss, and enforcement or handover fires. Half the TTL leaves
	// room for a full missed renewal, which is what docs/concepts.md means by
	// a heartbeat "comfortably shorter than the TTL".
	if c.HeartbeatInterval > c.TTL/2 {
		return fmt.Errorf("heartbeat interval (%s) must be at most half the ttl (%s); "+
			"a heartbeat nearer the ttl leaves no margin to retry a slow renewal", c.HeartbeatInterval, c.TTL)
	}
	if c.EnforceGrace < 0 {
		return errors.New("enforce grace must not be negative")
	}
	if c.APIServer == "" {
		return errors.New("api server URL is required")
	}
	// The bearer token — an OIDC JWT or a static API key — is attached to
	// every request unconditionally, and nothing downstream re-checks the
	// scheme. A plaintext URL therefore puts the credential on the wire from
	// every injected pod, so refuse it here rather than at first use.
	if err := ValidateAPIServerURL(c.APIServer); err != nil {
		return err
	}
	if c.APIKey != "" && c.APIKeyFile != "" {
		return errors.New("api key and api key file are mutually exclusive")
	}
	return nil
}

// ValidateAPIServerURL checks that raw is a usable Berth API server URL over
// TLS. It is exported so the injection webhook can reject a bad
// --berth-api-server at operator startup, rather than admitting pods whose
// helper would fail its own validation one layer later.
//
// https is required because the client attaches the bearer token to every
// request without inspecting the scheme (pkg/client), so an http:// endpoint
// transmits the credential in cleartext from every pod that was injected with
// it. There is deliberately no escape hatch: a plaintext lease endpoint has no
// safe use once a credential is attached to it.
func ValidateAPIServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("api server URL %q is not a valid URL: %w", raw, err)
	}
	if u.Scheme != "https" {
		scheme := u.Scheme
		if scheme == "" {
			scheme = "(none)"
		}
		return fmt.Errorf("api server URL %q must use https, got scheme %s; "+
			"the bearer token is attached to every request, so a plaintext endpoint would send it in cleartext", raw, scheme)
	}
	// Hostname(), not Host: url.Parse reads "https://:8443" as Host ":8443"
	// with an empty hostname, so a Host check alone accepts a port-only URL
	// that no request can ever reach.
	if u.Hostname() == "" {
		return fmt.Errorf("api server URL %q must include a host", raw)
	}
	return nil
}

// Holder returns the holder identity for Acquire / Renew / Release. An
// explicit HolderIdentity wins; otherwise the default is mode-specific
// (see the design doc "Holder Identity Defaulting").
//
// Both modes share one tenant-owning root: the cluster id when configured,
// otherwise the pod namespace. Runtime-singleton then folds in the Pod UID so
// different Pod incarnations never share a holder by accident. Startup-gate
// stays workload-level (no pod name or UID when the owning workload is known)
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

	add(c.ClusterID)
	add(c.PodNamespace)
	add(c.WorkloadKind)
	add(c.WorkloadName)

	switch c.Mode {
	case ModeStartupGate:
		// Fall back to pod name if the workload identity is unknown, so we
		// still produce a non-empty holder.
		if c.WorkloadKind == "" && c.WorkloadName == "" {
			add(c.PodName)
		}
	default: // runtime-singleton
		if c.PodName != "" {
			parts = append(parts, "pod", c.PodName)
		}
		parts = append(parts, "uid", c.PodUID)
	}

	// The first segment is the tenant-owning root, separated from the rest of
	// the hierarchy with "/" so the derived holder is recognized as *owned* by a
	// tenant equal to that root — the tenant-ownership boundary is exactly
	// "<tenant>/" (see internal/tenant.DefaultAuthorizer.AuthorizeHolder). The
	// root is the same in both modes: the cluster id when set, falling back to
	// the pod namespace. Rooting startup-gate at the namespace while
	// runtime-singleton rooted at the cluster id (the earlier behavior, #158)
	// meant a cluster-scoped credential could authorize one mode but not the
	// other. A ":"-joined root (an even earlier format) is owned by no tenant,
	// so an authenticated backend rejects every injected acquire with 403. The
	// remaining segments stay ":"-joined; in runtime-singleton mode the whole
	// string is still a unique per-pod identity.
	if len(parts) <= 1 {
		return strings.Join(parts, ":")
	}
	return parts[0] + "/" + strings.Join(parts[1:], ":")
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
