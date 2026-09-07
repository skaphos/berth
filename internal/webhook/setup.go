package webhook

import (
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SetupWithManager registers the pod-injection mutating webhook with the
// manager's webhook server. It serves at the controller-runtime default
// paths for core/v1 Pods (/mutate--v1-pod and /validate--v1-pod); the webhook configurations
// (SKA-440) points the API server there and scopes it with object and
// namespace selectors.
func SetupWithManager(mgr ctrl.Manager, cfg InjectorConfig) error {
	injector := NewPodInjector(cfg)
	if err := injector.cfg.Validate(); err != nil {
		return err
	}
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Pod{}).
		WithDefaulter(injector).
		WithValidator(injector).
		Complete()
}
