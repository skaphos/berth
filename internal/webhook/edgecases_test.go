package webhook

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/skaphos/berth/internal/acquire"
)

// TestInjectPrependsAheadOfExistingInitContainers verifies the hold and renew
// sidecar run before any workload init containers, so gating covers the whole
// startup and renewal is live during long init sequences.
func TestInjectPrependsAheadOfExistingInitContainers(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.InitContainers = []corev1.Container{{Name: "schema-migrate", Image: "vendor/migrate:1"}}

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	got := make([]string, 0, len(pod.Spec.InitContainers))
	for _, c := range pod.Spec.InitContainers {
		got = append(got, c.Name)
	}
	want := []string{InitContainerName, SidecarContainerName, "schema-migrate"}
	if len(got) != len(want) {
		t.Fatalf("init container order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("init container[%d] = %q, want %q (order %v)", i, got[i], want[i], got)
		}
	}
}

// TestInjectRejectsExistingLivenessProbe ensures probe enforcement does not
// silently clobber a user-defined liveness probe.
func TestInjectRejectsExistingLivenessProbe(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Containers[0].LivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{Command: []string{"/app", "healthcheck"}},
		},
	}

	err := testInjector().Default(context.Background(), pod)
	if err == nil {
		t.Fatal("expected injection to be rejected when a livenessProbe already exists")
	}
	if pod.Annotations[AnnInjected] == "true" {
		t.Error("pod must not be marked injected when preflight rejects it")
	}
}

// TestInjectRejectsNonEmptyDirStateVolume ensures the injector refuses to
// reuse a berth-state volume backed by anything other than an emptyDir.
func TestInjectRejectsNonEmptyDirStateVolume(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Volumes = []corev1.Volume{{
		Name:         VolumeName,
		VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}},
	}}

	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("expected injection to be rejected for a non-emptyDir berth-state volume")
	}
}

// TestInjectAcceptsExistingEmptyDirStateVolume verifies an existing emptyDir
// named berth-state is reused (not duplicated) rather than rejected.
func TestInjectAcceptsExistingEmptyDirStateVolume(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Volumes = []corev1.Volume{{
		Name:         VolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	n := 0
	for _, v := range pod.Spec.Volumes {
		if v.Name == VolumeName {
			n++
		}
	}
	if n != 1 {
		t.Errorf("berth-state volume count = %d, want 1 (reused, not duplicated)", n)
	}
}

// TestInjectRejectsContainerNameCollision ensures a clear error when a pod
// already uses a name the injector reserves, rather than producing an invalid
// PodSpec the API server rejects generically.
func TestInjectRejectsContainerNameCollision(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*corev1.Pod)
	}{
		{"existing init named berth-acquire", func(p *corev1.Pod) {
			p.Spec.InitContainers = []corev1.Container{{Name: InitContainerName, Image: "x"}}
		}},
		{"main container named berth-sidecar", func(p *corev1.Pod) {
			p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: SidecarContainerName, Image: "x"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
			tc.mut(pod)
			if err := testInjector().Default(context.Background(), pod); err == nil {
				t.Fatal("expected injection to be rejected for a reserved-name collision")
			}
		})
	}
}

// TestInjectRejectsForeignMountAtStateDir ensures probe injection refuses a
// pod where a different volume is already mounted at StateDir, which would
// otherwise produce a duplicate-mountPath PodSpec the API server rejects.
func TestInjectRejectsForeignMountAtStateDir(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Volumes = []corev1.Volume{{
		Name:         "cache",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: "cache", MountPath: acquire.DefaultStateDir},
	}

	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("expected rejection when a foreign volume is mounted at the state dir")
	}
}

// TestInjectPropagatesInsecureSkipVerify verifies the operator's TLS-skip
// setting reaches the injected helper via BERTH_INSECURE_SKIP_TLS_VERIFY, and
// is omitted (defaulting to verify) when not set.
func TestInjectPropagatesInsecureSkipVerify(t *testing.T) {
	inj := NewPodInjector(InjectorConfig{
		HelperImage:        "ghcr.io/skaphos/berth-acquire:test",
		DefaultTTLSeconds:  30,
		InsecureSkipVerify: true,
	})
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := inj.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	env := envMap(findContainer(pod.Spec.InitContainers, InitContainerName))
	if env[acquire.EnvInsecure] != "true" {
		t.Errorf("%s = %q, want true", acquire.EnvInsecure, env[acquire.EnvInsecure])
	}

	// Default (verify) must not emit the var at all.
	pod2 := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := testInjector().Default(context.Background(), pod2); err != nil {
		t.Fatalf("Default: %v", err)
	}
	env2 := envMap(findContainer(pod2.Spec.InitContainers, InitContainerName))
	if _, ok := env2[acquire.EnvInsecure]; ok {
		t.Errorf("%s should be absent when InsecureSkipVerify is false", acquire.EnvInsecure)
	}
}

// TestInjectForcesExistingStateMountReadOnly ensures that when a workload
// container already mounts berth-state at StateDir read-write, probe
// enforcement forces it read-only so the workload cannot recreate the health
// marker and bypass lease-loss enforcement.
func TestInjectForcesExistingStateMountReadOnly(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Volumes = []corev1.Volume{{
		Name:         VolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: VolumeName, MountPath: acquire.DefaultStateDir, ReadOnly: false},
	}

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	app := findContainer(pod.Spec.Containers, "app")
	n := 0
	for _, m := range app.VolumeMounts {
		if m.Name == VolumeName && m.MountPath == acquire.DefaultStateDir {
			n++
			if !m.ReadOnly {
				t.Errorf("existing state mount at %s must be forced read-only", acquire.DefaultStateDir)
			}
		}
	}
	if n != 1 {
		t.Errorf("state mount count at %s = %d, want 1 (reused in place, not duplicated)", acquire.DefaultStateDir, n)
	}
}

// TestInjectorConfigValidate covers the startup fail-fast checks so a
// misconfigured operator never admits traffic it would only reject.
func TestInjectorConfigValidate(t *testing.T) {
	base := func() InjectorConfig {
		return InjectorConfig{
			HelperImage:       "ghcr.io/skaphos/berth-acquire:test",
			DefaultMode:       acquire.ModeRuntimeSingleton,
			DefaultEnforce:    acquire.EnforceProbe,
			DefaultTTLSeconds: 30,
			StateDir:          acquire.DefaultStateDir,
		}
	}
	for _, tc := range []struct {
		name    string
		mut     func(*InjectorConfig)
		wantErr bool
	}{
		{"valid", func(*InjectorConfig) {}, false},
		{"missing helper image", func(c *InjectorConfig) { c.HelperImage = "" }, true},
		{"invalid mode", func(c *InjectorConfig) { c.DefaultMode = "bogus" }, true},
		{"invalid enforce", func(c *InjectorConfig) { c.DefaultEnforce = "bogus" }, true},
		{"non-positive ttl", func(c *InjectorConfig) { c.DefaultTTLSeconds = 0 }, true},
		{"relative state-dir", func(c *InjectorConfig) { c.StateDir = "berth" }, true},
		{"empty state-dir", func(c *InjectorConfig) { c.StateDir = "" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mut(&cfg)
			err := cfg.Validate()
			if tc.wantErr != (err != nil) {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestInjectAddsStateMountWhenExistingMountPathDiffers ensures the probe's
// StateDir mount is added even when the container already mounts berth-state
// at a different path, so the probe can find its marker files.
func TestInjectAddsStateMountWhenExistingMountPathDiffers(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	pod.Spec.Volumes = []corev1.Volume{{
		Name:         VolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
		{Name: VolumeName, MountPath: "/somewhere-else"},
	}

	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	app := findContainer(pod.Spec.Containers, "app")
	if !containerHasMountAt(app, VolumeName, acquire.DefaultStateDir) {
		t.Errorf("expected a berth-state mount at %s despite the existing mount at /somewhere-else; got %+v",
			acquire.DefaultStateDir, app.VolumeMounts)
	}
}
