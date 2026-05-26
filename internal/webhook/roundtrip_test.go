package webhook

import (
	"context"
	"strconv"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/skaphos/berth/internal/acquire"
)

// TestInjectedEnvRoundTripsThroughConfigFromEnv is the contract test for the
// injection seam: the webhook (producer) writes BERTH_*/POD_* env onto the
// injected helper container, and acquire.ConfigFromEnv (consumer) must
// reconstruct an equivalent Config from exactly those names. Because both
// sides now reference acquire.Env*, this guards against the two drifting.
func TestInjectedEnvRoundTripsThroughConfigFromEnv(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName:           "checkout",
		AnnMode:                string(acquire.ModeRuntimeSingleton),
		AnnEnforce:             string(acquire.EnforceProbe),
		AnnTTLSeconds:          "45",
		AnnHeartbeatSeconds:    "10",
		AnnEnforceGraceSeconds: "5",
		AnnHolderIdentity:      "explicit-holder",
		AnnReleaseOnShutdown:   "false",
	})
	yes := true
	pod.OwnerReferences = []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "checkout-7f6c", Controller: &yes},
	}
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	init := findContainer(pod.Spec.InitContainers, InitContainerName)
	if init == nil {
		t.Fatal("expected the berth-acquire init container")
	}

	// The helper's runtime view: the literal env from the injected spec,
	// plus the downward-API values the kubelet resolves from the fieldRefs
	// the webhook injected for POD_NAMESPACE/POD_NAME.
	env := envMap(init)
	get := func(key string) string {
		switch key {
		case acquire.EnvPodNamespace:
			return "prod"
		case acquire.EnvPodName:
			return "checkout-7f6c-abc12"
		default:
			return env[key]
		}
	}

	cfg, err := acquire.ConfigFromEnv(get)
	if err != nil {
		t.Fatalf("ConfigFromEnv: %v", err)
	}

	// String fields must equal exactly what the webhook injected.
	for _, tc := range []struct {
		name, got, want string
	}{
		{"LeaseName", cfg.LeaseName, env[acquire.EnvLeaseName]},
		{"LeaseNamespace", cfg.LeaseNamespace, env[acquire.EnvLeaseNamespace]},
		{"Mode", string(cfg.Mode), env[acquire.EnvMode]},
		{"Enforce", string(cfg.Enforce), env[acquire.EnvEnforce]},
		{"HolderIdentity", cfg.HolderIdentity, env[acquire.EnvHolderIdentity]},
		{"ClusterID", cfg.ClusterID, env[acquire.EnvClusterID]},
		{"WorkloadKind", cfg.WorkloadKind, env[acquire.EnvWorkloadKind]},
		{"WorkloadName", cfg.WorkloadName, env[acquire.EnvWorkloadName]},
		{"StateDir", cfg.StateDir, env[acquire.EnvStateDir]},
		{"APIServer", cfg.APIServer, env[acquire.EnvAPIServer]},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q (round-trip mismatch)", tc.name, tc.got, tc.want)
		}
	}

	// Integer-seconds fields round-trip back to the injected string.
	if got := strconv.Itoa(int(cfg.TTL.Seconds())); got != env[acquire.EnvTTLSeconds] {
		t.Errorf("TTL = %ss, want %ss", got, env[acquire.EnvTTLSeconds])
	}
	if got := strconv.Itoa(int(cfg.HeartbeatInterval.Seconds())); got != env[acquire.EnvHeartbeatSecs] {
		t.Errorf("HeartbeatInterval = %ss, want %ss", got, env[acquire.EnvHeartbeatSecs])
	}
	if got := strconv.Itoa(int(cfg.EnforceGrace.Seconds())); got != env[acquire.EnvEnforceGrace] {
		t.Errorf("EnforceGrace = %ss, want %ss", got, env[acquire.EnvEnforceGrace])
	}

	// Tri-state bool: explicit "false" must arrive as a non-nil false pointer.
	if cfg.ReleaseOnShutdown == nil {
		t.Error("ReleaseOnShutdown = nil, want non-nil false")
	} else if *cfg.ReleaseOnShutdown {
		t.Error("ReleaseOnShutdown = true, want false")
	}

	// Downward-API identity resolves from the injected fieldRefs.
	if cfg.PodNamespace != "prod" || cfg.PodName != "checkout-7f6c-abc12" {
		t.Errorf("pod identity = %q/%q, want prod/checkout-7f6c-abc12", cfg.PodNamespace, cfg.PodName)
	}
}
