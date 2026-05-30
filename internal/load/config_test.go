package load

import (
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Namespace:     "berth-load",
		Scenario:      ScenarioSteady,
		Leases:        10,
		Pairs:         2,
		TTL:           30 * time.Second,
		Heartbeat:     10 * time.Second,
		Duration:      time.Minute,
		Concurrency:   8,
		ChurnFraction: 0.1,
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid steady", func(*Config) {}, false},
		{"valid coldstart", func(c *Config) { c.Scenario = ScenarioColdStart; c.Duration = 0 }, false},
		{"valid failover no duration", func(c *Config) { c.Scenario = ScenarioFailover; c.Duration = 0 }, false},
		{"empty scenario", func(c *Config) { c.Scenario = "" }, true},
		{"unknown scenario", func(c *Config) { c.Scenario = "bogus" }, true},
		{"empty namespace", func(c *Config) { c.Namespace = "" }, true},
		{"zero leases", func(c *Config) { c.Leases = 0 }, true},
		{"zero pairs", func(c *Config) { c.Pairs = 0 }, true},
		{"zero ttl", func(c *Config) { c.TTL = 0 }, true},
		{"subsecond ttl", func(c *Config) { c.TTL = 500 * time.Millisecond; c.Heartbeat = 100 * time.Millisecond }, true},
		{"zero heartbeat", func(c *Config) { c.Heartbeat = 0 }, true},
		{"heartbeat >= ttl", func(c *Config) { c.Heartbeat = c.TTL }, true},
		{"zero concurrency", func(c *Config) { c.Concurrency = 0 }, true},
		{"steady without duration", func(c *Config) { c.Duration = 0 }, true},
		{"churn without duration", func(c *Config) { c.Scenario = ScenarioChurn; c.Duration = 0 }, true},
		{"churn fraction zero", func(c *Config) { c.Scenario = ScenarioChurn; c.ChurnFraction = 0 }, true},
		{"churn fraction over one", func(c *Config) { c.Scenario = ScenarioChurn; c.ChurnFraction = 1.5 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLeaseNamingIsDistinctAndStable(t *testing.T) {
	t.Parallel()

	if leaseName(0) == leaseName(1) {
		t.Fatal("lease names must be distinct per index")
	}
	if got := leaseName(7); got != "lease-000007" {
		t.Fatalf("leaseName(7) = %q, want stable lease-000007", got)
	}
	if activeHolder(3) == standbyHolder(3) {
		t.Fatal("active and standby holders must differ for the same lease")
	}
}
