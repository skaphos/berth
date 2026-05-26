package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/skaphos/berth/internal/acquire"
)

func testInjector() *PodInjector {
	return NewPodInjector(InjectorConfig{
		HelperImage:            "ghcr.io/skaphos/berth-acquire:test",
		APIServer:              "https://berth.example:8443",
		ClusterID:              "east",
		ControlPlaneNamespaces: []string{"berth-system"},
		DefaultTTLSeconds:      30,
	})
}

func optInPod(ns string, ann map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ns,
			Labels:      map[string]string{LabelInject: InjectValueAcquire},
			Annotations: ann,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "vendor/app:1"}},
		},
	}
}

func findContainer(cs []corev1.Container, name string) *corev1.Container {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}

func envMap(c *corev1.Container) map[string]string {
	m := map[string]string{}
	for _, e := range c.Env {
		m[e.Name] = e.Value
	}
	return m
}

func TestInjectRuntimeSingletonProbe(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	if !hasVolume(pod, VolumeName) {
		t.Error("expected the shared state volume")
	}
	init := findContainer(pod.Spec.InitContainers, InitContainerName)
	side := findContainer(pod.Spec.InitContainers, SidecarContainerName)
	if init == nil || side == nil {
		t.Fatalf("expected init + sidecar; got init=%v side=%v", init != nil, side != nil)
	}
	if side.RestartPolicy == nil || *side.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Error("sidecar must be a native sidecar (restartPolicy: Always)")
	}
	if init.Args[0] != "acquire" || side.Args[0] != "renew" {
		t.Errorf("args = %v / %v, want acquire / renew", init.Args, side.Args)
	}

	app := findContainer(pod.Spec.Containers, "app")
	if app.LivenessProbe == nil || app.LivenessProbe.Exec == nil {
		t.Fatal("probe mode should inject an exec liveness probe on the main container")
	}
	wantCmd := []string{"/berth/check", "check", "/berth/healthy"}
	got := app.LivenessProbe.Exec.Command
	if len(got) != 3 || got[0] != wantCmd[0] || got[1] != wantCmd[1] || got[2] != wantCmd[2] {
		t.Errorf("probe command = %v, want %v", got, wantCmd)
	}
	if !containerHasMountAt(app, VolumeName, acquire.DefaultStateDir) {
		t.Error("main container should mount the state volume for the probe")
	}

	env := envMap(init)
	if env[acquire.EnvLeaseName] != "checkout" || env[acquire.EnvMode] != string(acquire.ModeRuntimeSingleton) || env[acquire.EnvEnforce] != string(acquire.EnforceProbe) {
		t.Errorf("env defaults wrong: %v", env)
	}
	if env[acquire.EnvTTLSeconds] != "30" || env[acquire.EnvClusterID] != "east" || env[acquire.EnvAPIServer] == "" {
		t.Errorf("env missing chart defaults: %v", env)
	}
	// Downward-API env carries pod identity via fieldRef, not a literal.
	if findEnvSource(init, acquire.EnvPodName) == nil {
		t.Error("expected POD_NAME fieldRef env")
	}
	if pod.Annotations[AnnInjected] != "true" {
		t.Error("expected the injected marker annotation")
	}
}

func TestInjectStartupGateNoSidecarNoProbe(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName: "nightly",
		AnnMode:      string(acquire.ModeStartupGate),
	})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	if findContainer(pod.Spec.InitContainers, InitContainerName) == nil {
		t.Error("startup-gate still injects the init hold")
	}
	if findContainer(pod.Spec.InitContainers, SidecarContainerName) != nil {
		t.Error("startup-gate must not inject a sidecar")
	}
	if app := findContainer(pod.Spec.Containers, "app"); app.LivenessProbe != nil {
		t.Error("startup-gate must not inject a liveness probe")
	}
}

func TestInjectSignalSharesProcessNamespace(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName: "checkout",
		AnnEnforce:   string(acquire.EnforceSignal),
	})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	if pod.Spec.ShareProcessNamespace == nil || !*pod.Spec.ShareProcessNamespace {
		t.Error("signal mode must set shareProcessNamespace=true")
	}
	if app := findContainer(pod.Spec.Containers, "app"); app.LivenessProbe != nil {
		t.Error("signal mode must not inject a probe")
	}
}

func TestInjectSignalRejectsExplicitFalseShareProcessNamespace(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName: "checkout",
		AnnEnforce:   string(acquire.EnforceSignal),
	})
	no := false
	pod.Spec.ShareProcessNamespace = &no

	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("expected rejection when shareProcessNamespace is explicitly false in signal mode")
	}
}

func TestInjectIdempotent(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	inj := testInjector()
	if err := inj.Default(context.Background(), pod); err != nil {
		t.Fatalf("first Default: %v", err)
	}
	initCount := len(pod.Spec.InitContainers)
	volCount := len(pod.Spec.Volumes)

	if err := inj.Default(context.Background(), pod); err != nil {
		t.Fatalf("second Default: %v", err)
	}
	if len(pod.Spec.InitContainers) != initCount {
		t.Errorf("re-injection added init containers: %d -> %d", initCount, len(pod.Spec.InitContainers))
	}
	if len(pod.Spec.Volumes) != volCount {
		t.Errorf("re-injection added volumes: %d -> %d", volCount, len(pod.Spec.Volumes))
	}
}

func TestInjectSkipsWithoutOptInLabel(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Labels = nil // remove opt-in

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	if len(pod.Spec.InitContainers) != 0 {
		t.Error("pods without the opt-in label must not be mutated")
	}
}

func TestInjectSkipsControlPlaneNamespace(t *testing.T) {
	pod := optInPod("berth-system", map[string]string{AnnLeaseName: "checkout"})

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	if len(pod.Spec.InitContainers) != 0 {
		t.Error("control-plane pods must never be mutated")
	}
}

func TestInjectInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		ann  map[string]string
	}{
		{"missing lease name", map[string]string{}},
		{"bad mode", map[string]string{AnnLeaseName: "x", AnnMode: "weird"}},
		{"bad enforce", map[string]string{AnnLeaseName: "x", AnnEnforce: "nuke"}},
		{"non-integer ttl", map[string]string{AnnLeaseName: "x", AnnTTLSeconds: "soon"}},
		{"negative ttl", map[string]string{AnnLeaseName: "x", AnnTTLSeconds: "-5"}},
		{"heartbeat >= ttl", map[string]string{AnnLeaseName: "x", AnnTTLSeconds: "10", AnnHeartbeatSeconds: "10"}},
		{"bad release-on-shutdown", map[string]string{AnnLeaseName: "x", AnnReleaseOnShutdown: "maybe"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := optInPod("prod", tt.ann)
			if err := testInjector().Default(context.Background(), pod); err == nil {
				t.Fatalf("expected validation error for %s", tt.name)
			}
		})
	}
}

func TestInjectHolderIdentityAndWorkloadEnv(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName:         "checkout",
		AnnHolderIdentity:    "explicit-holder",
		AnnReleaseOnShutdown: "false",
	})
	yes := true
	pod.OwnerReferences = []metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "checkout-7f6c", Controller: &yes},
	}
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	env := envMap(findContainer(pod.Spec.InitContainers, InitContainerName))
	if env[acquire.EnvHolderIdentity] != "explicit-holder" {
		t.Errorf("holder env = %q, want explicit-holder", env[acquire.EnvHolderIdentity])
	}
	if env[acquire.EnvReleaseOnDown] != "false" {
		t.Errorf("release-on-shutdown env = %q, want false", env[acquire.EnvReleaseOnDown])
	}
	if env[acquire.EnvWorkloadKind] != "ReplicaSet" || env[acquire.EnvWorkloadName] != "checkout-7f6c" {
		t.Errorf("workload env = %q/%q, want ReplicaSet/checkout-7f6c", env[acquire.EnvWorkloadKind], env[acquire.EnvWorkloadName])
	}
}

func findEnvSource(c *corev1.Container, name string) *corev1.EnvVarSource {
	for _, e := range c.Env {
		if e.Name == name {
			return e.ValueFrom
		}
	}
	return nil
}
