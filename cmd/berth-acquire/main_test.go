package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/skaphos/berth/internal/acquire"
)

func emptyEnv(string) string { return "" }

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRunCheckMarkerPresent(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "healthy")
	if err := os.WriteFile(marker, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"check", marker}, emptyEnv, &bytes.Buffer{}); code != 0 {
		t.Errorf("check on present marker = %d, want 0", code)
	}
}

func TestRunCheckMarkerAbsent(t *testing.T) {
	if code := run([]string{"check", "/no/such/marker"}, emptyEnv, &bytes.Buffer{}); code != 1 {
		t.Errorf("check on absent marker = %d, want 1", code)
	}
}

func TestRunCheckUsesStateDirEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "healthy"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{acquire.EnvStateDir: dir})
	if code := run([]string{"check"}, getenv, &bytes.Buffer{}); code != 0 {
		t.Errorf("check using BERTH_STATE_DIR = %d, want 0", code)
	}
}

func TestRunAcquireInvalidConfigReturns1(t *testing.T) {
	// No lease name / api server in the env → validation fails.
	if code := run([]string{"acquire"}, emptyEnv, &bytes.Buffer{}); code != 1 {
		t.Errorf("acquire with empty config = %d, want 1", code)
	}
}

func TestRunRenewWrongModeReturns1(t *testing.T) {
	getenv := envFrom(map[string]string{
		acquire.EnvLeaseName:    "checkout",
		acquire.EnvAPIServer:    "https://berth:8443",
		acquire.EnvTTLSeconds:   "30",
		acquire.EnvPodNamespace: "prod",
		acquire.EnvMode:         string(acquire.ModeStartupGate),
	})
	if code := run([]string{"renew"}, getenv, &bytes.Buffer{}); code != 1 {
		t.Errorf("renew in startup-gate mode = %d, want 1", code)
	}
}

func TestRunAcquireTimesOutReturns1(t *testing.T) {
	// Valid config pointing at an unreachable server; the acquire-timeout
	// bounds the hold so the command returns rather than blocking.
	getenv := envFrom(map[string]string{
		acquire.EnvLeaseName:    "checkout",
		acquire.EnvAPIServer:    "http://127.0.0.1:1",
		acquire.EnvTTLSeconds:   "30",
		acquire.EnvPodNamespace: "prod",
		acquire.EnvMode:         string(acquire.ModeStartupGate),
	})
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"acquire", "--acquire-timeout=40ms"}, getenv, &bytes.Buffer{})
	}()
	select {
	case code := <-done:
		if code != 1 {
			t.Errorf("acquire against unreachable server = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("acquire did not honor --acquire-timeout")
	}
}

// TestConfigFlagOverridesEnv exercises the override layering directly:
// an explicitly-set flag must win over the env-derived base.
func TestConfigFlagOverridesEnv(t *testing.T) {
	root := &cobra.Command{Use: "x"}
	var f cliFlags
	f.bind(root)
	if err := root.ParseFlags([]string{
		"--lease-name=from-flag",
		"--ttl-seconds=15",
		"--api-server=https://flag:8443",
		"--release-on-shutdown=true",
	}); err != nil {
		t.Fatal(err)
	}

	getenv := envFrom(map[string]string{
		acquire.EnvLeaseName:    "from-env",
		acquire.EnvTTLSeconds:   "30",
		acquire.EnvAPIServer:    "https://env:8443",
		acquire.EnvPodNamespace: "prod",
		acquire.EnvMode:         string(acquire.ModeRuntimeSingleton),
	})
	cfg, err := f.config(root, getenv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.LeaseName != "from-flag" {
		t.Errorf("LeaseName = %q, want flag override", cfg.LeaseName)
	}
	if cfg.TTL != 15*time.Second {
		t.Errorf("TTL = %s, want 15s flag override", cfg.TTL)
	}
	if cfg.APIServer != "https://flag:8443" {
		t.Errorf("APIServer = %q, want flag override", cfg.APIServer)
	}
	if cfg.ReleaseOnShutdown == nil || !*cfg.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown flag override should be true")
	}
}

// TestConfigFromEnvNoOverrides confirms the env base is used when no flags
// are set.
func TestConfigFromEnvNoOverrides(t *testing.T) {
	root := &cobra.Command{Use: "x"}
	var f cliFlags
	f.bind(root)
	if err := root.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}
	getenv := envFrom(map[string]string{
		acquire.EnvLeaseName:    "from-env",
		acquire.EnvTTLSeconds:   "30",
		acquire.EnvAPIServer:    "https://env:8443",
		acquire.EnvPodNamespace: "prod",
	})
	cfg, err := f.config(root, getenv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.LeaseName != "from-env" || cfg.TTL != 30*time.Second {
		t.Errorf("env base not used: %+v", cfg)
	}
}
