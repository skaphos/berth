package acquire

import (
	"fmt"
	"strconv"
	"time"
)

// Environment variable names the injected helper reads. The mutating
// webhook (internal/webhook) sets these on the injected containers, and
// the berth-acquire binary loads them via ConfigFromEnv. This is the
// single source of truth for the env side of the injection contract.
const (
	EnvLeaseName      = "BERTH_LEASE_NAME"
	EnvLeaseNamespace = "BERTH_LEASE_NAMESPACE"
	EnvMode           = "BERTH_MODE"
	EnvEnforce        = "BERTH_ENFORCE"
	EnvTTLSeconds     = "BERTH_TTL_SECONDS"
	EnvHeartbeatSecs  = "BERTH_HEARTBEAT_SECONDS"
	EnvEnforceGrace   = "BERTH_ENFORCE_GRACE_SECONDS"
	EnvReleaseOnDown  = "BERTH_RELEASE_ON_SHUTDOWN"
	EnvHolderIdentity = "BERTH_HOLDER_IDENTITY"
	EnvClusterID      = "BERTH_CLUSTER_ID"
	EnvWorkloadKind   = "BERTH_WORKLOAD_KIND"
	EnvWorkloadName   = "BERTH_WORKLOAD_NAME"
	EnvStateDir       = "BERTH_STATE_DIR"
	EnvAPIServer      = "BERTH_API_SERVER"
	EnvAPIKey         = "BERTH_API_KEY"
	EnvAPIKeyFile     = "BERTH_API_KEY_FILE"
	EnvCABundleFile   = "BERTH_CA_BUNDLE_FILE"
	EnvServerName     = "BERTH_SERVER_NAME"
	EnvInsecure       = "BERTH_INSECURE_SKIP_TLS_VERIFY"

	// Downward-API env used for holder-identity defaulting.
	EnvPodNamespace = "POD_NAMESPACE"
	EnvPodName      = "POD_NAME"
)

// ConfigFromEnv builds a Config from the BERTH_*/POD_* environment using
// the supplied getter (os.Getenv in production, a stub in tests). It does
// not apply defaults or validate — callers layer flag overrides on top
// and then call Validate. It returns an error only for malformed values
// (non-integer seconds, an unparseable boolean) so a typo surfaces at
// startup rather than silently falling back to a default.
func ConfigFromEnv(get func(string) string) (*Config, error) {
	cfg := &Config{
		LeaseName:      get(EnvLeaseName),
		LeaseNamespace: get(EnvLeaseNamespace),
		Mode:           Mode(get(EnvMode)),
		Enforce:        Enforce(get(EnvEnforce)),
		HolderIdentity: get(EnvHolderIdentity),
		ClusterID:      get(EnvClusterID),
		PodNamespace:   get(EnvPodNamespace),
		PodName:        get(EnvPodName),
		WorkloadKind:   get(EnvWorkloadKind),
		WorkloadName:   get(EnvWorkloadName),
		StateDir:       get(EnvStateDir),
		APIServer:      get(EnvAPIServer),
		APIKey:         get(EnvAPIKey),
		APIKeyFile:     get(EnvAPIKeyFile),
		CABundleFile:   get(EnvCABundleFile),
		ServerName:     get(EnvServerName),
	}

	ttl, err := secondsEnv(get, EnvTTLSeconds)
	if err != nil {
		return nil, err
	}
	cfg.TTL = ttl

	hb, err := secondsEnv(get, EnvHeartbeatSecs)
	if err != nil {
		return nil, err
	}
	cfg.HeartbeatInterval = hb

	grace, err := secondsEnv(get, EnvEnforceGrace)
	if err != nil {
		return nil, err
	}
	cfg.EnforceGrace = grace

	if v := get(EnvReleaseOnDown); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("%s=%q: %w", EnvReleaseOnDown, v, err)
		}
		cfg.ReleaseOnShutdown = &b
	}
	if v := get(EnvInsecure); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("%s=%q: %w", EnvInsecure, v, err)
		}
		cfg.InsecureSkipVerify = b
	}

	return cfg, nil
}

// secondsEnv parses an integer-seconds env var into a Duration. An absent
// or empty value yields 0 (let defaulting decide).
func secondsEnv(get func(string) string, key string) (time.Duration, error) {
	v := get(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: must be an integer number of seconds", key, v)
	}
	return time.Duration(n) * time.Second, nil
}
