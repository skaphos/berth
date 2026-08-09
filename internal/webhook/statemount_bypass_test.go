package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/skaphos/berth/internal/acquire"
)

// Bypasses found in review of the first cut of the reserved-state-volume
// rule. Each one admitted a pod that could still write the health marker or
// the check binary, so each gets a regression test rather than only a fix.

// The StateDir exemption assumed applyEnforcement would repair the mount,
// but that only ran for main containers under enforce:probe. An author's
// initContainer could therefore keep write access and tamper with the marker
// and the verifier before the app container ever started.
func TestWritableStateDirMountOnInitContainerIsRepaired(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:         "setup",
		Image:        "vendor/setup:1",
		VolumeMounts: []corev1.VolumeMount{{Name: VolumeName, MountPath: acquire.DefaultStateDir}},
	})

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	setup := findContainer(pod.Spec.InitContainers, "setup")
	if setup == nil {
		t.Fatal("author initContainer disappeared")
	}
	for _, m := range setup.VolumeMounts {
		if m.Name == VolumeName && !m.ReadOnly {
			t.Error("an author initContainer must not keep write access to the state volume")
		}
	}
}

// Same exemption, other half: enforce:signal ran no repair at all, so a
// writable StateDir mount survived untouched.
func TestWritableStateDirMountIsRepairedInSignalMode(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName:    "checkout",
		AnnEnforce:      string(acquire.EnforceSignal),
		AnnSignalTarget: "app",
	})
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: VolumeName, MountPath: acquire.DefaultStateDir},
	}

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	app := findContainer(pod.Spec.Containers, "app")
	for _, m := range app.VolumeMounts {
		if m.Name == VolumeName && !m.ReadOnly {
			t.Error("signal mode must not leave a writable state mount")
		}
	}
}

// The exemption for injector-owned containers was applied by name on every
// request. On create no injected container exists yet, so an author could
// claim the exemption just by naming their container berth-sidecar — and
// startup-gate does not reserve that name in preflight, so nothing else
// caught it.
func TestAuthorCannotClaimInjectorExemptionByName(t *testing.T) {
	pod := optInPod("prod", map[string]string{
		AnnLeaseName: "checkout",
		AnnMode:      string(acquire.ModeStartupGate),
	})
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:         SidecarContainerName, // impersonating an injected helper
		Image:        "vendor/evil:1",
		VolumeMounts: []corev1.VolumeMount{{Name: VolumeName, MountPath: "/rw"}},
	})

	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("naming a container after an injected helper must not exempt it from the mount rule")
	}
}

// The genuine exemption must still hold once the helpers really exist,
// otherwise the ephemeral-container path would reject every already-injected
// pod on account of the helpers' own writable mounts.
func TestInjectedHelpersKeepTheirExemptionOnAnInjectedPod(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("initial inject: %v", err)
	}

	// Re-admission of the injected pod (the shape the ephemeral path sees).
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("an already-injected pod must still admit: %v", err)
	}
}
