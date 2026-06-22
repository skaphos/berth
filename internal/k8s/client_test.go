package k8s

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewClientsetInvalidKubeconfig(t *testing.T) {
	t.Parallel()

	clientset, err := NewClientset("/path/that/does/not/exist", ClientConfig{})
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path")
	}
	if clientset != nil {
		t.Fatal("expected nil clientset on error")
	}
}

// writeKubeconfig writes a minimal but valid kubeconfig that BuildConfigFromFlags
// parses without contacting any API server.
func writeKubeconfig(t *testing.T) string {
	t.Helper()
	const kubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: test-token
`
	path := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	if err := os.WriteFile(path, []byte(kubeconfig), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestBuildConfigAppliesRaisedDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := buildConfig(writeKubeconfig(t), ClientConfig{})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.QPS != DefaultQPS {
		t.Fatalf("QPS = %v, want default %v", cfg.QPS, DefaultQPS)
	}
	if cfg.Burst != DefaultBurst {
		t.Fatalf("Burst = %d, want default %d", cfg.Burst, DefaultBurst)
	}
}

func TestBuildConfigHonorsExplicitOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := buildConfig(writeKubeconfig(t), ClientConfig{QPS: 500, Burst: 1000})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.QPS != 500 {
		t.Fatalf("QPS = %v, want 500", cfg.QPS)
	}
	if cfg.Burst != 1000 {
		t.Fatalf("Burst = %d, want 1000", cfg.Burst)
	}
}

func TestBuildConfigNonPositiveFallsBackPerField(t *testing.T) {
	t.Parallel()

	// QPS set, Burst left at zero: QPS is honored, Burst falls back to default.
	cfg, err := buildConfig(writeKubeconfig(t), ClientConfig{QPS: 42})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.QPS != 42 {
		t.Fatalf("QPS = %v, want 42", cfg.QPS)
	}
	if cfg.Burst != DefaultBurst {
		t.Fatalf("Burst = %d, want default %d", cfg.Burst, DefaultBurst)
	}
}

func TestBuildConfigInvalidKubeconfig(t *testing.T) {
	t.Parallel()

	cfg, err := buildConfig("/path/that/does/not/exist", ClientConfig{})
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path")
	}
	if cfg != nil {
		t.Fatal("expected nil config on error")
	}
}

func TestNewClientsetBuildsWithValidKubeconfig(t *testing.T) {
	t.Parallel()

	clientset, err := NewClientset(writeKubeconfig(t), ClientConfig{QPS: 10, Burst: 20})
	if err != nil {
		t.Fatalf("NewClientset: %v", err)
	}
	if clientset == nil {
		t.Fatal("expected a non-nil clientset")
	}
}
