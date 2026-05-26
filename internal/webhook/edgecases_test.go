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

// TestInjectMountsAuthSourcesIntoHelpers verifies the webhook mounts the
// bearer-token Secret and CA-bundle ConfigMap into both injected helper
// containers at the paths the BERTH_*_FILE env vars point at (SKA-444), so the
// helper can actually read them.
func TestInjectMountsAuthSourcesIntoHelpers(t *testing.T) {
	inj := NewPodInjector(InjectorConfig{
		HelperImage:           "ghcr.io/skaphos/berth-acquire:test",
		DefaultTTLSeconds:     30,
		APIKeyFile:            "/var/run/berth/token",
		APIKeySecretName:      "berth-token",
		APIKeySecretKey:       "token",
		CABundleFile:          "/etc/berth/ca/ca.crt",
		CABundleConfigMapName: "berth-ca",
		CABundleKey:           "ca.crt",
	})
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := inj.Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}

	// Both helper containers mount token and CA at the file's parent dir.
	for _, name := range []string{InitContainerName, SidecarContainerName} {
		c := findContainer(pod.Spec.InitContainers, name)
		if c == nil {
			t.Fatalf("missing injected container %q", name)
		}
		if !containerHasMountAt(c, AuthTokenVolume, "/var/run/berth") {
			t.Errorf("%s: token not mounted at /var/run/berth; mounts=%+v", name, c.VolumeMounts)
		}
		if !containerHasMountAt(c, AuthCABundleVolume, "/etc/berth/ca") {
			t.Errorf("%s: CA not mounted at /etc/berth/ca; mounts=%+v", name, c.VolumeMounts)
		}
	}

	// The pod carries the source volumes, projecting each key to the file base
	// so it lands exactly at the configured path.
	tok := findVolume(pod, AuthTokenVolume)
	if tok == nil || tok.Secret == nil {
		t.Fatalf("token secret volume missing: %+v", tok)
	}
	if tok.Secret.SecretName != "berth-token" || len(tok.Secret.Items) != 1 ||
		tok.Secret.Items[0].Key != "token" || tok.Secret.Items[0].Path != "token" {
		t.Errorf("token volume = %+v, want secret berth-token item token->token", tok.Secret)
	}
	ca := findVolume(pod, AuthCABundleVolume)
	if ca == nil || ca.ConfigMap == nil {
		t.Fatalf("CA configmap volume missing: %+v", ca)
	}
	if ca.ConfigMap.Name != "berth-ca" || len(ca.ConfigMap.Items) != 1 ||
		ca.ConfigMap.Items[0].Key != "ca.crt" || ca.ConfigMap.Items[0].Path != "ca.crt" {
		t.Errorf("CA volume = %+v, want configmap berth-ca item ca.crt->ca.crt", ca.ConfigMap)
	}

	// The helper still reads them through the file-path env vars.
	env := envMap(findContainer(pod.Spec.InitContainers, InitContainerName))
	if env[acquire.EnvAPIKeyFile] != "/var/run/berth/token" {
		t.Errorf("%s = %q, want /var/run/berth/token", acquire.EnvAPIKeyFile, env[acquire.EnvAPIKeyFile])
	}
	if env[acquire.EnvCABundleFile] != "/etc/berth/ca/ca.crt" {
		t.Errorf("%s = %q, want /etc/berth/ca/ca.crt", acquire.EnvCABundleFile, env[acquire.EnvCABundleFile])
	}
}

// TestInjectNoAuthMountsWhenUnset confirms the no-auth path (auth-mode=none +
// system-trust/insecure TLS) injects only the state volume — no token/CA mounts.
func TestInjectNoAuthMountsWhenUnset(t *testing.T) {
	pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := testInjector().Default(context.Background(), pod); err != nil {
		t.Fatalf("Default: %v", err)
	}
	if findVolume(pod, AuthTokenVolume) != nil || findVolume(pod, AuthCABundleVolume) != nil {
		t.Error("no auth source configured, but an auth volume was injected")
	}
	init := findContainer(pod.Spec.InitContainers, InitContainerName)
	if len(init.VolumeMounts) != 1 || init.VolumeMounts[0].Name != VolumeName {
		t.Errorf("init container mounts = %+v, want only %s", init.VolumeMounts, VolumeName)
	}
}

// TestInjectRejectsReservedAuthVolumeName ensures a pre-existing volume using
// a reserved auth name is rejected rather than silently reused — which would
// point the helper at the wrong token/CA.
func TestInjectRejectsReservedAuthVolumeName(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    InjectorConfig
		volume string
	}{
		{"token", InjectorConfig{HelperImage: "x", DefaultTTLSeconds: 30, APIKeyFile: "/var/run/berth/token", APIKeySecretName: "berth-token"}, AuthTokenVolume},
		{"ca", InjectorConfig{HelperImage: "x", DefaultTTLSeconds: 30, CABundleFile: "/etc/berth/ca/ca.crt", CABundleConfigMapName: "berth-ca"}, AuthCABundleVolume},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
			pod.Spec.Volumes = []corev1.Volume{{
				Name:         tc.volume,
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			}}
			if err := NewPodInjector(tc.cfg).Default(context.Background(), pod); err == nil {
				t.Fatalf("expected rejection for pre-existing reserved volume %q", tc.volume)
			}
		})
	}
}

func findVolume(pod *corev1.Pod, name string) *corev1.Volume {
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == name {
			return &pod.Spec.Volumes[i]
		}
	}
	return nil
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
		{"api-key file without secret", func(c *InjectorConfig) { c.APIKeyFile = "/var/run/berth/token" }, true},
		{"api-key secret without file", func(c *InjectorConfig) { c.APIKeySecretName = "berth-token" }, true},
		{"ca file without configmap", func(c *InjectorConfig) { c.CABundleFile = "/etc/berth/ca/ca.crt" }, true},
		{"ca configmap without file", func(c *InjectorConfig) { c.CABundleConfigMapName = "berth-ca" }, true},
		{"api-key dir collides with state-dir", func(c *InjectorConfig) {
			c.APIKeyFile = c.StateDir + "/token"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"api-key and ca share a dir", func(c *InjectorConfig) {
			c.APIKeyFile = "/etc/berth/auth/token"
			c.APIKeySecretName = "berth-token"
			c.CABundleFile = "/etc/berth/auth/ca.crt"
			c.CABundleConfigMapName = "berth-ca"
		}, true},
		{"relative api-key file", func(c *InjectorConfig) {
			c.APIKeyFile = "var/run/berth/token"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"trailing-slash state-dir still collides", func(c *InjectorConfig) {
			c.StateDir = "/berth/"
			c.APIKeyFile = "/berth/token"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"api-key file mounts at root", func(c *InjectorConfig) {
			c.APIKeyFile = "/token"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"api-key file trailing slash", func(c *InjectorConfig) {
			c.APIKeyFile = "/var/run/berth/token/"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"api-key file with dotdot", func(c *InjectorConfig) {
			c.APIKeyFile = "/var/run/../berth/token"
			c.APIKeySecretName = "berth-token"
		}, true},
		{"valid auth pair", func(c *InjectorConfig) {
			c.APIKeyFile = "/var/run/berth/token"
			c.APIKeySecretName = "berth-token"
		}, false},
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
