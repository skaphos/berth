package webhook

import (
	"context"
	"fmt"
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
	APIServer    string
	APIKeyFile   string
	CABundleFile string
	ServerName   string
	ClusterID    string

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
	i.mutate(pod, r)
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
	if r.ttlSeconds <= 0 {
		return fmt.Errorf("%s must be positive", AnnTTLSeconds)
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

	env := i.buildEnv(r)
	stateMount := corev1.VolumeMount{Name: VolumeName, MountPath: i.cfg.StateDir}

	// Blocking init container: the "hold".
	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:            InitContainerName,
		Image:           i.cfg.HelperImage,
		ImagePullPolicy: i.cfg.ImagePullPolicy,
		Args:            []string{"acquire"},
		Env:             env,
		VolumeMounts:    []corev1.VolumeMount{stateMount},
	})

	if r.mode == acquire.ModeRuntimeSingleton {
		always := corev1.ContainerRestartPolicyAlways
		// Native sidecar: an init container with restartPolicy: Always so
		// it runs alongside the main containers and does not block Job
		// completion.
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
			Name:            SidecarContainerName,
			Image:           i.cfg.HelperImage,
			ImagePullPolicy: i.cfg.ImagePullPolicy,
			Args:            []string{"renew"},
			Env:             env,
			RestartPolicy:   &always,
			VolumeMounts:    []corev1.VolumeMount{stateMount},
		})
		i.applyEnforcement(pod, r)
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[AnnInjected] = "true"
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
			if !containerHasMount(c, VolumeName) {
				c.VolumeMounts = append(c.VolumeMounts, roMount)
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

func containerHasMount(c *corev1.Container, name string) bool {
	for _, m := range c.VolumeMounts {
		if m.Name == name {
			return true
		}
	}
	return false
}
