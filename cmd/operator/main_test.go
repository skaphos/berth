package main

import (
	"flag"
	"io"
	"reflect"
	"testing"

	"github.com/skaphos/berth/internal/acquire"
	"github.com/skaphos/berth/internal/webhook"
)

// newTestFlagSet returns an isolated FlagSet that returns parse errors instead
// of exiting the process, so parseConfig can be exercised without touching
// flag.CommandLine or os.Exit.
func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("operator-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func TestParseConfigRequiresAPIServer(t *testing.T) {
	_, err := parseConfig(newTestFlagSet(), nil)
	if err == nil {
		t.Fatal("parseConfig without --berth-api-server: want error, got nil")
	}
}

func TestParseConfigRejectsMutuallyExclusiveKeys(t *testing.T) {
	_, err := parseConfig(newTestFlagSet(), []string{
		"--berth-api-server=https://berth.example.com:8443",
		"--berth-api-key=secret",
		"--berth-api-key-file=/run/token",
	})
	if err == nil {
		t.Fatal("parseConfig with both --berth-api-key and --berth-api-key-file: want error, got nil")
	}
}

func TestParseConfigRejectsUnknownFlag(t *testing.T) {
	_, err := parseConfig(newTestFlagSet(), []string{
		"--berth-api-server=https://berth.example.com:8443",
		"--not-a-flag=1",
	})
	if err == nil {
		t.Fatal("parseConfig with unknown flag: want error, got nil")
	}
}

// TestParseConfigMinimalDefaults checks that a minimal valid invocation applies
// the documented defaults, including the InjectorConfig defaults that back the
// injection-* flags.
func TestParseConfigMinimalDefaults(t *testing.T) {
	cfg, err := parseConfig(newTestFlagSet(), []string{
		"--berth-api-server=https://berth.example.com:8443",
	})
	if err != nil {
		t.Fatalf("parseConfig: unexpected error: %v", err)
	}

	if cfg.metricsAddr != ":8080" {
		t.Errorf("metricsAddr = %q, want :8080", cfg.metricsAddr)
	}
	if cfg.probeAddr != ":8081" {
		t.Errorf("probeAddr = %q, want :8081", cfg.probeAddr)
	}
	if cfg.apiServerURL != "https://berth.example.com:8443" {
		t.Errorf("apiServerURL = %q", cfg.apiServerURL)
	}
	if cfg.enableWebhook {
		t.Error("enableWebhook = true, want false by default")
	}

	ic := cfg.injectorConfig
	if ic.APIServer != cfg.apiServerURL {
		t.Errorf("injectorConfig.APIServer = %q, want %q", ic.APIServer, cfg.apiServerURL)
	}
	if ic.APIKeySecretKey != "token" {
		t.Errorf("injectorConfig.APIKeySecretKey = %q, want token", ic.APIKeySecretKey)
	}
	if ic.CABundleKey != "ca.crt" {
		t.Errorf("injectorConfig.CABundleKey = %q, want ca.crt", ic.CABundleKey)
	}
	if ic.StateDir != acquire.DefaultStateDir {
		t.Errorf("injectorConfig.StateDir = %q, want %q", ic.StateDir, acquire.DefaultStateDir)
	}
	if ic.DefaultTTLSeconds != 30 {
		t.Errorf("injectorConfig.DefaultTTLSeconds = %d, want 30", ic.DefaultTTLSeconds)
	}
	if ic.DefaultMode != acquire.ModeRuntimeSingleton {
		t.Errorf("injectorConfig.DefaultMode = %q, want %q", ic.DefaultMode, acquire.ModeRuntimeSingleton)
	}
	if ic.DefaultEnforce != acquire.EnforceProbe {
		t.Errorf("injectorConfig.DefaultEnforce = %q, want %q", ic.DefaultEnforce, acquire.EnforceProbe)
	}
	if !reflect.DeepEqual(ic.ControlPlaneNamespaces, []string{"berth-system"}) {
		t.Errorf("injectorConfig.ControlPlaneNamespaces = %v, want [berth-system]", ic.ControlPlaneNamespaces)
	}
}

// TestParseConfigInjectorMapping pins every injection-* flag to a distinct
// value and asserts the assembled InjectorConfig maps each one to the right
// field (and that CSV namespaces are split/trimmed).
func TestParseConfigInjectorMapping(t *testing.T) {
	cfg, err := parseConfig(newTestFlagSet(), []string{
		"--berth-api-server=https://berth.example.com:8443",
		"--cluster-id=east-1",
		"--berth-server-name=berth.example.com",
		"--berth-insecure-skip-tls-verify=true",
		"--enable-injection-webhook=true",
		"--injection-helper-image=ghcr.io/skaphos/berth-acquire:test",
		"--injection-control-plane-namespaces= berth-system , kube-system ,, extra ",
		"--injection-helper-api-key-file=/var/run/berth/token",
		"--injection-helper-api-key-secret=berth-token",
		"--injection-helper-api-key-secret-key=apikey",
		"--injection-helper-ca-bundle-file=/var/run/berth/ca.crt",
		"--injection-helper-ca-bundle-configmap=berth-ca",
		"--injection-helper-ca-bundle-key=bundle.pem",
		"--injection-state-dir=/state",
		"--injection-default-mode=startup-gate",
		"--injection-default-enforce=signal",
		"--injection-default-ttl-seconds=45",
	})
	if err != nil {
		t.Fatalf("parseConfig: unexpected error: %v", err)
	}

	if !cfg.enableWebhook {
		t.Fatal("enableWebhook = false, want true")
	}

	want := webhook.InjectorConfig{
		HelperImage:            "ghcr.io/skaphos/berth-acquire:test",
		APIServer:              "https://berth.example.com:8443",
		APIKeyFile:             "/var/run/berth/token",
		APIKeySecretName:       "berth-token",
		APIKeySecretKey:        "apikey",
		CABundleFile:           "/var/run/berth/ca.crt",
		CABundleConfigMapName:  "berth-ca",
		CABundleKey:            "bundle.pem",
		ServerName:             "berth.example.com",
		ClusterID:              "east-1",
		InsecureSkipVerify:     true,
		ControlPlaneNamespaces: []string{"berth-system", "kube-system", "extra"},
		DefaultTTLSeconds:      45,
		DefaultMode:            acquire.Mode("startup-gate"),
		DefaultEnforce:         acquire.Enforce("signal"),
		StateDir:               "/state",
	}
	if !reflect.DeepEqual(cfg.injectorConfig, want) {
		t.Errorf("injectorConfig mismatch:\n got %+v\nwant %+v", cfg.injectorConfig, want)
	}

	// Top-level fields run() consumes directly must mirror the flags too.
	if cfg.clusterID != "east-1" {
		t.Errorf("clusterID = %q, want east-1", cfg.clusterID)
	}
	if cfg.serverName != "berth.example.com" {
		t.Errorf("serverName = %q, want berth.example.com", cfg.serverName)
	}
	if !cfg.insecureSkipVerify {
		t.Error("insecureSkipVerify = false, want true")
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b ,, c ", []string{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := splitCSV(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
