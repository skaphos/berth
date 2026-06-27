package acquire

import (
	"testing"
	"time"
)

func baseConfig() *Config {
	return &Config{
		LeaseName:    "checkout",
		PodNamespace: "prod",
		PodName:      "checkout-7f6c-j4n8x",
		TTL:          30 * time.Second,
		APIServer:    "https://berth.example:8443",
	}
}

func TestApplyDefaults(t *testing.T) {
	c := baseConfig()
	c.ApplyDefaults()

	if c.StateDir != DefaultStateDir {
		t.Errorf("StateDir = %q, want %q", c.StateDir, DefaultStateDir)
	}
	if c.Mode != ModeRuntimeSingleton {
		t.Errorf("Mode = %q, want %q", c.Mode, ModeRuntimeSingleton)
	}
	if c.Enforce != EnforceProbe {
		t.Errorf("Enforce = %q, want %q", c.Enforce, EnforceProbe)
	}
	if c.LeaseNamespace != "prod" {
		t.Errorf("LeaseNamespace = %q, want pod namespace prod", c.LeaseNamespace)
	}
	if c.HeartbeatInterval != 10*time.Second {
		t.Errorf("HeartbeatInterval = %s, want ttl/3 = 10s", c.HeartbeatInterval)
	}
	if c.ReleaseOnShutdown == nil || !*c.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown should default true for runtime-singleton")
	}
}

func TestApplyDefaultsStartupGateReleaseFalse(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.ApplyDefaults()
	if c.ReleaseOnShutdown == nil || *c.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown should default false for startup-gate")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid", func(*Config) {}, false},
		{"no lease name", func(c *Config) { c.LeaseName = "" }, true},
		{"no namespace", func(c *Config) { c.PodNamespace = ""; c.LeaseNamespace = "" }, true},
		{"bad mode", func(c *Config) { c.Mode = "weird" }, true},
		{"bad enforce", func(c *Config) { c.Enforce = "nuke" }, true},
		{"zero ttl", func(c *Config) { c.TTL = 0 }, true},
		{"heartbeat >= ttl", func(c *Config) { c.HeartbeatInterval = 30 * time.Second }, true},
		{"negative grace", func(c *Config) { c.EnforceGrace = -time.Second }, true},
		{"no api server", func(c *Config) { c.APIServer = "" }, true},
		{"key and key file", func(c *Config) { c.APIKey = "k"; c.APIKeyFile = "/f" }, true},
		{"signal without target", func(c *Config) { c.Enforce = EnforceSignal }, true},
		{"signal with target", func(c *Config) { c.Enforce = EnforceSignal; c.SignalTarget = "nginx" }, false},
		{"startup-gate signal without target ok", func(c *Config) { c.Mode = ModeStartupGate; c.Enforce = EnforceSignal }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseConfig()
			tt.mutate(c)
			err := c.Validate()
			if tt.wantErr != (err != nil) {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestHolderRuntimeSingletonIncludesPodName(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeRuntimeSingleton
	c.ClusterID = "east"
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	got := c.Holder()
	want := "east:prod:deployment:checkout:pod:checkout-7f6c-j4n8x"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderRuntimeSingletonWithoutClusterStillUnique(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeRuntimeSingleton
	c.ApplyDefaults()
	// No cluster id / workload info, but the pod name must still be present
	// so replicas never share a holder.
	got := c.Holder()
	want := "prod:pod:checkout-7f6c-j4n8x"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderStartupGatePrefersWorkload(t *testing.T) {
	c := baseConfig()
	c.Mode = ModeStartupGate
	c.WorkloadKind = "deployment"
	c.WorkloadName = "checkout"
	c.ApplyDefaults()

	got := c.Holder()
	want := "prod:deployment:checkout"
	if got != want {
		t.Errorf("Holder() = %q, want %q", got, want)
	}
}

func TestHolderExplicitOverride(t *testing.T) {
	c := baseConfig()
	c.HolderIdentity = "my-custom-holder"
	c.ApplyDefaults()
	if got := c.Holder(); got != "my-custom-holder" {
		t.Errorf("Holder() = %q, want explicit override", got)
	}
}

func TestNewClientBuilds(t *testing.T) {
	c := baseConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	lc, err := c.NewClient()
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if lc == nil {
		t.Fatal("NewClient returned nil client")
	}
}

func TestNewClientWithAPIKeyFileMissing(t *testing.T) {
	c := baseConfig()
	c.APIKeyFile = "/nonexistent/token"
	if _, err := c.NewClient(); err == nil {
		t.Error("expected error for missing api key file")
	}
}
