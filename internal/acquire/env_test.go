package acquire

import (
	"testing"
	"time"
)

// getter returns a getenv-style function backed by a map.
func getter(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigFromEnvFull(t *testing.T) {
	cfg, err := ConfigFromEnv(getter(map[string]string{
		EnvLeaseName:      "checkout",
		EnvLeaseNamespace: "prod",
		EnvMode:           string(ModeRuntimeSingleton),
		EnvEnforce:        string(EnforceSignal),
		EnvTTLSeconds:     "30",
		EnvHeartbeatSecs:  "7",
		EnvEnforceGrace:   "5",
		EnvReleaseOnDown:  "false",
		EnvHolderIdentity: "h",
		EnvClusterID:      "east",
		EnvWorkloadKind:   "ReplicaSet",
		EnvWorkloadName:   "checkout-abc",
		EnvStateDir:       "/berth",
		EnvAPIServer:      "https://berth:8443",
		EnvAPIKeyFile:     "/var/run/token",
		EnvCABundleFile:   "/etc/ca.pem",
		EnvServerName:     "berth.svc",
		EnvInsecure:       "true",
		EnvPodNamespace:   "prod",
		EnvPodName:        "checkout-abc-xyz",
	}))
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}

	if cfg.LeaseName != "checkout" || cfg.LeaseNamespace != "prod" {
		t.Errorf("lease = %q/%q", cfg.LeaseName, cfg.LeaseNamespace)
	}
	if cfg.Mode != ModeRuntimeSingleton || cfg.Enforce != EnforceSignal {
		t.Errorf("mode/enforce = %q/%q", cfg.Mode, cfg.Enforce)
	}
	if cfg.TTL != 30*time.Second || cfg.HeartbeatInterval != 7*time.Second || cfg.EnforceGrace != 5*time.Second {
		t.Errorf("durations = %s/%s/%s", cfg.TTL, cfg.HeartbeatInterval, cfg.EnforceGrace)
	}
	if cfg.ReleaseOnShutdown == nil || *cfg.ReleaseOnShutdown {
		t.Error("release-on-shutdown should parse to false")
	}
	if cfg.ClusterID != "east" || cfg.PodName != "checkout-abc-xyz" || cfg.APIKeyFile != "/var/run/token" {
		t.Errorf("misc fields wrong: %+v", cfg)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("insecure should parse to true")
	}
}

func TestConfigFromEnvEmpty(t *testing.T) {
	cfg, err := ConfigFromEnv(getter(nil))
	if err != nil {
		t.Fatalf("empty env should not error: %v", err)
	}
	if cfg.TTL != 0 || cfg.HeartbeatInterval != 0 || cfg.ReleaseOnShutdown != nil {
		t.Errorf("empty env should leave zero values for defaulting: %+v", cfg)
	}
}

func TestConfigFromEnvReleaseTrue(t *testing.T) {
	cfg, err := ConfigFromEnv(getter(map[string]string{EnvReleaseOnDown: "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReleaseOnShutdown == nil || !*cfg.ReleaseOnShutdown {
		t.Error("release-on-shutdown=true should parse to a non-nil true")
	}
}

func TestConfigFromEnvInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad ttl", map[string]string{EnvTTLSeconds: "soon"}},
		{"bad heartbeat", map[string]string{EnvHeartbeatSecs: "x"}},
		{"bad grace", map[string]string{EnvEnforceGrace: "y"}},
		{"bad release", map[string]string{EnvReleaseOnDown: "maybe"}},
		{"bad insecure", map[string]string{EnvInsecure: "sorta"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ConfigFromEnv(getter(tt.env)); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}
