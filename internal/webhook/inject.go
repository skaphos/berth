package webhook

import (
	"context"
	"fmt"
	"path"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/skaphos/berth/internal/acquire"
)

// InjectorConfig holds the chart/operator-level defaults the webhook
// applies to opted-in pods. Per-pod annotations override these where the
// contract allows it. Values are populated from operator flags (set by
// Helm in SKA-440).
type InjectorConfig struct {
	// HelperImage is the berth-acquire image injected as the init
	// container and (for runtime-singleton) the sidecar. Required.
	HelperImage     string
	ImagePullPolicy corev1.PullPolicy

	// API client config handed to the injected helper via env.
	APIServer          string
	APIKeyFile         string
	CABundleFile       string
	ServerName         string
	ClusterID          string
	InsecureSkipVerify bool

	// Sources that back APIKeyFile / CABundleFile inside the workload pod.
	// The webhook mounts these into the injected helper containers so those
	// file paths actually resolve (SKA-444); without them the helper would
	// reference paths that do not exist. The referenced Secret/ConfigMap must
	// exist in each opted-in workload's own namespace.
	APIKeySecretName      string
	APIKeySecretKey       string
	CABundleConfigMapName string
	CABundleKey           string

	// ControlPlaneNamespaces are never mutated (the Berth control plane
	// must not inject itself).
	ControlPlaneNamespaces []string

	// Behavior defaults applied when a pod omits the annotation.
	DefaultTTLSeconds int
	DefaultMode       acquire.Mode
	DefaultEnforce    acquire.Enforce

	// StateDir is the shared emptyDir mount path.
	StateDir string
}

// Validate checks the operator-supplied defaults so a misconfigured webhook
// fails fast at startup rather than admitting traffic and then rejecting
// every opted-in pod that relies on a default. Call it after withDefaults.
func (c *InjectorConfig) Validate() error {
	if c.HelperImage == "" {
		return fmt.Errorf("injection webhook: helper image is required")
	}
	switch c.DefaultMode {
	case acquire.ModeStartupGate, acquire.ModeRuntimeSingleton:
	default:
		return fmt.Errorf("injection webhook: invalid default mode %q", c.DefaultMode)
	}
	switch c.DefaultEnforce {
	case acquire.EnforceProbe, acquire.EnforceSignal:
	default:
		return fmt.Errorf("injection webhook: invalid default enforce %q", c.DefaultEnforce)
	}
	if c.DefaultTTLSeconds <= 0 {
		return fmt.Errorf("injection webhook: default ttl-seconds must be positive, got %d", c.DefaultTTLSeconds)
	}
	// StateDir becomes a Pod volumeMount.mountPath and the base for the probe
	// marker paths (<StateDir>/healthy, <StateDir>/check). Kubernetes requires
	// an absolute mountPath, so a relative or empty value would produce invalid
	// PodSpecs (or broken enforcement) for every opted-in pod. Fail fast here
	// instead. Defaulting runs before Validate, so this only trips on an
	// explicit bad --injection-state-dir.
	if !path.IsAbs(c.StateDir) {
		return fmt.Errorf("injection webhook: state-dir must be an absolute path, got %q", c.StateDir)
	}
	// An auth file path is only useful if the webhook can mount a source at it;
	// otherwise the helper points at a path that does not exist (SKA-444).
	// Require the file and its source together (or neither, for an
	// auth-mode=none / system-trust API server).
	if (c.APIKeyFile == "") != (c.APIKeySecretName == "") {
		return fmt.Errorf("injection webhook: api-key file and secret must be set together (file=%q secret=%q)", c.APIKeyFile, c.APIKeySecretName)
	}
	if (c.CABundleFile == "") != (c.CABundleConfigMapName == "") {
		return fmt.Errorf("injection webhook: ca-bundle file and configmap must be set together (file=%q configmap=%q)", c.CABundleFile, c.CABundleConfigMapName)
	}
	// Each auth file is split into a VolumeMount.mountPath (its parent dir) and
	// a KeyToPath.Path (its basename) at mount-construction time. Validate that
	// the split is well-formed so the mounted file actually lands at the env-var
	// value the helper reads.
	if c.APIKeyFile != "" {
		if err := validateMountableFile("api-key file", c.APIKeyFile); err != nil {
			return err
		}
	}
	if c.CABundleFile != "" {
		if err := validateMountableFile("ca-bundle file", c.CABundleFile); err != nil {
			return err
		}
	}
	// Auth volumes mount at the file's parent directory; a collision with the
	// state dir (or with each other) would produce an invalid PodSpec. Compare
	// normalized paths (path.Dir already cleans its result) so a trailing slash
	// cannot slip a collision past the guard.
	stateDir := path.Clean(c.StateDir)
	if c.APIKeyFile != "" && path.Dir(c.APIKeyFile) == stateDir {
		return fmt.Errorf("injection webhook: api-key file directory %q must differ from state-dir %q", path.Dir(c.APIKeyFile), stateDir)
	}
	if c.CABundleFile != "" && path.Dir(c.CABundleFile) == stateDir {
		return fmt.Errorf("injection webhook: ca-bundle file directory %q must differ from state-dir %q", path.Dir(c.CABundleFile), stateDir)
	}
	if c.APIKeyFile != "" && c.CABundleFile != "" && path.Dir(c.APIKeyFile) == path.Dir(c.CABundleFile) {
		return fmt.Errorf("injection webhook: api-key and ca-bundle files must be in different directories (both %q)", path.Dir(c.APIKeyFile))
	}
	return nil
}

// validateMountableFile checks that p can be split into a volumeMount.mountPath
// (path.Dir) and a Secret/ConfigMap KeyToPath.Path (path.Base) such that the
// file mounts back exactly at p. It must be absolute, already clean (a trailing
// slash, ".", or ".." would make path.Dir/path.Base diverge from p, so the
// helper would read a different path than the env var names), and have a
// non-root parent (mounting at "/" has no real directory and would clobber the
// container root).
func validateMountableFile(field, p string) error {
	if !path.IsAbs(p) {
		return fmt.Errorf("injection webhook: %s must be an absolute path, got %q", field, p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("injection webhook: %s must be a clean path with no trailing slash, %q, or %q segments, got %q", field, ".", "..", p)
	}
	if path.Dir(p) == "/" {
		return fmt.Errorf("injection webhook: %s must have a non-root parent directory, got %q", field, p)
	}
	return nil
}

func (c *InjectorConfig) withDefaults() *InjectorConfig {
	if c.ImagePullPolicy == "" {
		c.ImagePullPolicy = corev1.PullIfNotPresent
	}
	if c.DefaultMode == "" {
		c.DefaultMode = acquire.ModeRuntimeSingleton
	}
	if c.DefaultEnforce == "" {
		c.DefaultEnforce = acquire.EnforceProbe
	}
	if c.StateDir == "" {
		c.StateDir = acquire.DefaultStateDir
	}
	if c.APIKeySecretName != "" && c.APIKeySecretKey == "" {
		c.APIKeySecretKey = "token"
	}
	if c.CABundleConfigMapName != "" && c.CABundleKey == "" {
		c.CABundleKey = "ca.crt"
	}
	return c
}

// PodInjector is the CustomDefaulter that mutates opted-in pods.
type PodInjector struct {
	cfg *InjectorConfig
}

// NewPodInjector returns a PodInjector with config defaults applied.
func NewPodInjector(cfg InjectorConfig) *PodInjector {
	return &PodInjector{cfg: cfg.withDefaults()}
}

// Default implements the typed admission.Defaulter for Pods.
// controller-runtime diffs the pod before and after this call and emits
// the JSON patch.
func (i *PodInjector) Default(ctx context.Context, pod *corev1.Pod) error {
	// On create the object's namespace is often empty; the authoritative
	// value is on the admission request.
	ns := pod.Namespace
	if req, err := admission.RequestFromContext(ctx); err == nil && req.Namespace != "" {
		ns = req.Namespace
	}

	if i.isControlPlane(ns) {
		return nil
	}
	if pod.Labels[LabelInject] != InjectValueAcquire {
		return nil
	}
	if pod.Annotations[AnnInjected] == "true" {
		return nil // already injected; idempotent no-op
	}

	r, err := i.resolve(pod, ns)
	if err != nil {
		return err
	}
	if err := i.preflight(pod, r); err != nil {
		return err
	}
	i.mutate(pod, r)
	return nil
}

// preflight rejects pods the injector cannot safely mutate, so the failure
// surfaces as a clear admission error rather than a silently-broken pod or a
// generic API-server validation rejection. It guards against: a collision
// with an injected container name; a pre-existing berth-state volume that is
// not the emptyDir the helper relies on; and (in probe mode) a main container
// that already defines a livenessProbe we would otherwise clobber.
func (i *PodInjector) preflight(pod *corev1.Pod, r resolved) error {
	reserved := []string{InitContainerName}
	if r.mode == acquire.ModeRuntimeSingleton {
		reserved = append(reserved, SidecarContainerName)
	}
	for _, name := range reserved {
		if hasContainerNamed(pod.Spec.InitContainers, name) || hasContainerNamed(pod.Spec.Containers, name) {
			return fmt.Errorf("cannot inject: pod already has a container named %q", name)
		}
	}

	for _, v := range pod.Spec.Volumes {
		if v.Name == VolumeName && v.EmptyDir == nil {
			return fmt.Errorf("cannot inject: pod already has a volume named %q that is not an emptyDir", VolumeName)
		}
	}

	// The auth volume names are reserved. A pre-existing volume with one of
	// these names would be silently reused by mutate (hasVolume matches by
	// name), pointing the helper at the wrong token/CA, so reject it up front
	// when we would inject that source.
	if i.cfg.APIKeySecretName != "" && hasVolume(pod, AuthTokenVolume) {
		return fmt.Errorf("cannot inject: pod already has a volume named %q (reserved for the injected auth token)", AuthTokenVolume)
	}
	if i.cfg.CABundleConfigMapName != "" && hasVolume(pod, AuthCABundleVolume) {
		return fmt.Errorf("cannot inject: pod already has a volume named %q (reserved for the injected CA bundle)", AuthCABundleVolume)
	}

	if r.mode == acquire.ModeRuntimeSingleton && r.enforce == acquire.EnforceProbe {
		for idx := range pod.Spec.Containers {
			c := &pod.Spec.Containers[idx]
			if c.LivenessProbe != nil {
				return fmt.Errorf("cannot inject probe enforcement: container %q already defines a livenessProbe; set %s=%s to enforce by signal instead", c.Name, AnnEnforce, acquire.EnforceSignal)
			}
			// We add a read-only state mount at StateDir for the probe; a
			// different volume already mounted there would make the PodSpec
			// invalid (duplicate mountPath), so reject it up front.
			for _, m := range c.VolumeMounts {
				if m.MountPath == i.cfg.StateDir && m.Name != VolumeName {
					return fmt.Errorf("cannot inject probe enforcement: container %q already mounts volume %q at the state dir %s", c.Name, m.Name, i.cfg.StateDir)
				}
			}
		}
	}
	return nil
}

func (i *PodInjector) isControlPlane(ns string) bool {
	for _, cp := range i.cfg.ControlPlaneNamespaces {
		if ns == cp {
			return true
		}
	}
	return false
}

// resolved is the validated, defaulted per-pod configuration.
type resolved struct {
	leaseName        string
	leaseNamespace   string
	mode             acquire.Mode
	enforce          acquire.Enforce
	signalTarget     string
	ttlSeconds       int
	heartbeatSeconds int    // 0 → let the helper derive ttl/3
	enforceGrace     int    // 0 → helper default
	releaseOnDown    string // "", "true", "false"
	holderIdentity   string
	workloadKind     string
	workloadName     string
}

// resolve parses annotations, applies operator defaults, and validates.
// Validation errors are returned to the API server, which applies the
// webhook's configured failure policy (Fail vs Ignore).
func (i *PodInjector) resolve(pod *corev1.Pod, ns string) (resolved, error) {
	ann := pod.Annotations
	r := resolved{
		leaseName:      ann[AnnLeaseName],
		leaseNamespace: orDefault(ann[AnnLeaseNamespace], ns),
		mode:           acquire.Mode(orDefault(ann[AnnMode], string(i.cfg.DefaultMode))),
		enforce:        acquire.Enforce(orDefault(ann[AnnEnforce], string(i.cfg.DefaultEnforce))),
		signalTarget:   ann[AnnSignalTarget],
		ttlSeconds:     i.cfg.DefaultTTLSeconds,
		releaseOnDown:  ann[AnnReleaseOnShutdown],
		holderIdentity: ann[AnnHolderIdentity],
	}
	if owner := controllerOwner(pod); owner != nil {
		r.workloadKind = owner.Kind
		r.workloadName = owner.Name
	}

	var err error
	if r.ttlSeconds, err = intAnn(ann, AnnTTLSeconds, r.ttlSeconds); err != nil {
		return r, err
	}
	if r.heartbeatSeconds, err = intAnn(ann, AnnHeartbeatSeconds, 0); err != nil {
		return r, err
	}
	if r.enforceGrace, err = intAnn(ann, AnnEnforceGraceSeconds, 0); err != nil {
		return r, err
	}

	if err := r.validate(); err != nil {
		return r, err
	}

	// signal mode requires a shared process namespace; refuse to silently
	// override an explicit opt-out (design "Validation & Error Behavior").
	if r.mode == acquire.ModeRuntimeSingleton && r.enforce == acquire.EnforceSignal &&
		pod.Spec.ShareProcessNamespace != nil && !*pod.Spec.ShareProcessNamespace {
		return r, fmt.Errorf("%s=%s requires shareProcessNamespace, but the pod sets it to false", AnnEnforce, acquire.EnforceSignal)
	}
	return r, nil
}

func (r resolved) validate() error {
	if r.leaseName == "" {
		return fmt.Errorf("%s is required when %s=%s", AnnLeaseName, LabelInject, InjectValueAcquire)
	}
	switch r.mode {
	case acquire.ModeStartupGate, acquire.ModeRuntimeSingleton:
	default:
		return fmt.Errorf("invalid %s %q", AnnMode, r.mode)
	}
	switch r.enforce {
	case acquire.EnforceProbe, acquire.EnforceSignal:
	default:
		return fmt.Errorf("invalid %s %q", AnnEnforce, r.enforce)
	}
	if r.mode == acquire.ModeRuntimeSingleton && r.enforce == acquire.EnforceSignal && r.signalTarget == "" {
		return fmt.Errorf("%s is required when %s=%s and %s=%s: an empty target signals every process in the shared "+
			"PID namespace, which can terminate co-located sidecars; set it to the workload's process name "+
			"(comm or executable basename)", AnnSignalTarget, AnnMode, acquire.ModeRuntimeSingleton, AnnEnforce, acquire.EnforceSignal)
	}
	if r.ttlSeconds <= 0 {
		return fmt.Errorf("%s must be positive", AnnTTLSeconds)
	}
	if r.heartbeatSeconds < 0 {
		return fmt.Errorf("%s must not be negative", AnnHeartbeatSeconds)
	}
	if r.heartbeatSeconds > 0 && r.heartbeatSeconds >= r.ttlSeconds {
		return fmt.Errorf("%s (%d) must be less than %s (%d)", AnnHeartbeatSeconds, r.heartbeatSeconds, AnnTTLSeconds, r.ttlSeconds)
	}
	if r.enforceGrace < 0 {
		return fmt.Errorf("%s must not be negative", AnnEnforceGraceSeconds)
	}
	switch r.releaseOnDown {
	case "", "true", "false":
	default:
		return fmt.Errorf("invalid %s %q (want true or false)", AnnReleaseOnShutdown, r.releaseOnDown)
	}
	return nil
}

// mutate applies the injection. It assumes resolve() validated r and that
// the pod is not already injected.
func (i *PodInjector) mutate(pod *corev1.Pod, r resolved) {
	if !hasVolume(pod, VolumeName) {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         VolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}

	// Mount the bearer-token / CA sources the helper's BERTH_*_FILE env vars
	// point at (SKA-444), alongside the shared state volume.
	authVols, authMounts := i.authVolumeMounts()
	for _, v := range authVols {
		if !hasVolume(pod, v.Name) {
			pod.Spec.Volumes = append(pod.Spec.Volumes, v)
		}
	}

	env := i.buildEnv(r)
	stateMount := corev1.VolumeMount{Name: VolumeName, MountPath: i.cfg.StateDir}
	helperMounts := append([]corev1.VolumeMount{stateMount}, authMounts...)

	// Blocking init container: the "hold".
	injected := []corev1.Container{{
		Name:            InitContainerName,
		Image:           i.cfg.HelperImage,
		ImagePullPolicy: i.cfg.ImagePullPolicy,
		Args:            []string{"acquire"},
		Env:             env,
		VolumeMounts:    helperMounts,
	}}

	if r.mode == acquire.ModeRuntimeSingleton {
		always := corev1.ContainerRestartPolicyAlways
		// Native sidecar: an init container with restartPolicy: Always so
		// it runs alongside the main containers and does not block Job
		// completion.
		injected = append(injected, corev1.Container{
			Name:            SidecarContainerName,
			Image:           i.cfg.HelperImage,
			ImagePullPolicy: i.cfg.ImagePullPolicy,
			Args:            []string{"renew"},
			Env:             env,
			RestartPolicy:   &always,
			VolumeMounts:    helperMounts,
		})
	}

	// Prepend so the hold (and, for runtime-singleton, the renew sidecar)
	// run ahead of any workload init containers: gating must cover the
	// entire pod startup, and renewal must be live during long init
	// sequences so the lease TTL cannot lapse before the app starts.
	pod.Spec.InitContainers = append(injected, pod.Spec.InitContainers...)

	if r.mode == acquire.ModeRuntimeSingleton {
		i.applyEnforcement(pod, r)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[AnnInjected] = "true"
}

// authVolumeMounts returns the volumes and matching mounts that place the
// bearer token and CA bundle (referenced by BERTH_API_KEY_FILE /
// BERTH_CA_BUNDLE_FILE) into the injected helper containers. Each source key
// is projected to the basename of its configured file path and mounted at that
// path's parent directory, so the file lands exactly where the env var points.
// Returns nil when no source is configured (auth-mode=none + system-trust /
// insecure TLS, where no files are needed).
func (i *PodInjector) authVolumeMounts() ([]corev1.Volume, []corev1.VolumeMount) {
	var vols []corev1.Volume
	var mounts []corev1.VolumeMount
	if i.cfg.APIKeySecretName != "" {
		vols = append(vols, corev1.Volume{
			Name: AuthTokenVolume,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: i.cfg.APIKeySecretName,
				Items:      []corev1.KeyToPath{{Key: i.cfg.APIKeySecretKey, Path: path.Base(i.cfg.APIKeyFile)}},
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: AuthTokenVolume, MountPath: path.Dir(i.cfg.APIKeyFile), ReadOnly: true})
	}
	if i.cfg.CABundleConfigMapName != "" {
		vols = append(vols, corev1.Volume{
			Name: AuthCABundleVolume,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: i.cfg.CABundleConfigMapName},
				Items:                []corev1.KeyToPath{{Key: i.cfg.CABundleKey, Path: path.Base(i.cfg.CABundleFile)}},
			}},
		})
		mounts = append(mounts, corev1.VolumeMount{Name: AuthCABundleVolume, MountPath: path.Dir(i.cfg.CABundleFile), ReadOnly: true})
	}
	return vols, mounts
}

// applyEnforcement wires the runtime-singleton kill mechanism.
func (i *PodInjector) applyEnforcement(pod *corev1.Pod, r resolved) {
	switch r.enforce {
	case acquire.EnforceSignal:
		// shareProcessNamespace lets the sidecar signal the main process.
		t := true
		pod.Spec.ShareProcessNamespace = &t
	default: // probe
		marker := i.cfg.StateDir + "/healthy"
		check := i.cfg.StateDir + "/check"
		roMount := corev1.VolumeMount{Name: VolumeName, MountPath: i.cfg.StateDir, ReadOnly: true}
		for idx := range pod.Spec.Containers {
			c := &pod.Spec.Containers[idx]
			c.LivenessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{Command: []string{check, "check", marker}},
				},
				PeriodSeconds:    2,
				FailureThreshold: 1,
			}
			if !containerHasMountAt(c, VolumeName, i.cfg.StateDir) {
				c.VolumeMounts = append(c.VolumeMounts, roMount)
			} else {
				// Ensure the existing state mount is read-only so the workload
				// cannot recreate the health marker and bypass enforcement.
				for mi := range c.VolumeMounts {
					m := &c.VolumeMounts[mi]
					if m.Name == VolumeName && m.MountPath == i.cfg.StateDir {
						m.ReadOnly = true
					}
				}
			}
		}
	}
}

// buildEnv assembles the BERTH_*/POD_* environment the helper reads.
func (i *PodInjector) buildEnv(r resolved) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: acquire.EnvLeaseName, Value: r.leaseName},
		{Name: acquire.EnvLeaseNamespace, Value: r.leaseNamespace},
		{Name: acquire.EnvMode, Value: string(r.mode)},
		{Name: acquire.EnvEnforce, Value: string(r.enforce)},
		{Name: acquire.EnvTTLSeconds, Value: strconv.Itoa(r.ttlSeconds)},
		{Name: acquire.EnvStateDir, Value: i.cfg.StateDir},
		{Name: acquire.EnvPodNamespace, ValueFrom: fieldRef("metadata.namespace")},
		{Name: acquire.EnvPodName, ValueFrom: fieldRef("metadata.name")},
	}
	env = appendIf(env, r.signalTarget != "" && r.enforce == acquire.EnforceSignal, acquire.EnvSignalTarget, r.signalTarget)
	env = appendIf(env, r.heartbeatSeconds > 0, acquire.EnvHeartbeatSecs, strconv.Itoa(r.heartbeatSeconds))
	env = appendIf(env, r.enforceGrace > 0, acquire.EnvEnforceGrace, strconv.Itoa(r.enforceGrace))
	env = appendIf(env, r.releaseOnDown != "", acquire.EnvReleaseOnDown, r.releaseOnDown)
	env = appendIf(env, r.holderIdentity != "", acquire.EnvHolderIdentity, r.holderIdentity)
	env = appendIf(env, r.workloadKind != "", acquire.EnvWorkloadKind, r.workloadKind)
	env = appendIf(env, r.workloadName != "", acquire.EnvWorkloadName, r.workloadName)
	env = appendIf(env, i.cfg.ClusterID != "", acquire.EnvClusterID, i.cfg.ClusterID)
	env = appendIf(env, i.cfg.APIServer != "", acquire.EnvAPIServer, i.cfg.APIServer)
	env = appendIf(env, i.cfg.APIKeyFile != "", acquire.EnvAPIKeyFile, i.cfg.APIKeyFile)
	env = appendIf(env, i.cfg.CABundleFile != "", acquire.EnvCABundleFile, i.cfg.CABundleFile)
	env = appendIf(env, i.cfg.ServerName != "", acquire.EnvServerName, i.cfg.ServerName)
	env = appendIf(env, i.cfg.InsecureSkipVerify, acquire.EnvInsecure, "true")
	return env
}

func fieldRef(path string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: path}}
}

func appendIf(env []corev1.EnvVar, cond bool, name, value string) []corev1.EnvVar {
	if !cond {
		return env
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// intAnn parses an integer annotation, returning def when absent.
func intAnn(ann map[string]string, key string, def int) (int, error) {
	v, ok := ann[key]
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("invalid %s %q: must be an integer", key, v)
	}
	return n, nil
}

func controllerOwner(pod *corev1.Pod) *metaOwner {
	for _, o := range pod.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			return &metaOwner{Kind: o.Kind, Name: o.Name}
		}
	}
	return nil
}

type metaOwner struct{ Kind, Name string }

func hasVolume(pod *corev1.Pod, name string) bool {
	for _, v := range pod.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func hasContainerNamed(cs []corev1.Container, name string) bool {
	for i := range cs {
		if cs[i].Name == name {
			return true
		}
	}
	return false
}

// containerHasMountAt reports whether c already mounts volume name at path.
// Matching the mount path (not just the volume name) matters because the
// probe command reads marker files under StateDir: a berth-state mount at a
// different path would leave the probe without the files it needs, so we must
// still add the StateDir mount in that case.
func containerHasMountAt(c *corev1.Container, name, path string) bool {
	for _, m := range c.VolumeMounts {
		if m.Name == name && m.MountPath == path {
			return true
		}
	}
	return false
}
