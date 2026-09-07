package webhook

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/skaphos/berth/internal/acquire"
)

// ValidateCreate checks the final Pod after all mutating webhooks have run.
// Another injector may append init containers after our preflight check, so
// runtime mode must contain only the two Berth helpers in their startup order.
func (i *PodInjector) ValidateCreate(ctx context.Context, pod *corev1.Pod) (admission.Warnings, error) {
	ns := pod.Namespace
	if req, err := admission.RequestFromContext(ctx); err == nil && req.Namespace != "" {
		ns = req.Namespace
	}
	if i.isControlPlane(ns) || pod.Labels[LabelInject] != InjectValueAcquire {
		return nil, nil
	}
	r, err := i.resolve(pod, ns)
	if err != nil {
		return nil, err
	}
	if r.mode != acquire.ModeRuntimeSingleton {
		return nil, nil
	}
	init := pod.Spec.InitContainers
	if len(init) != 2 || init[0].Name != InitContainerName || init[1].Name != SidecarContainerName ||
		init[0].RestartPolicy != nil || init[1].RestartPolicy == nil || *init[1].RestartPolicy != corev1.ContainerRestartPolicyAlways {
		return nil, fmt.Errorf("cannot admit runtime-singleton: only the injected %s and %s init containers are supported; workload init containers and native sidecars cannot be reliably fenced", InitContainerName, SidecarContainerName)
	}
	return nil, nil
}

// ValidateUpdate permits updates to existing Pods. The chart registers this
// validator for Pod creation only; existing Pods must be drained and recreated
// to adopt the new injection policy. Deletion must remain possible for cleanup.
func (i *PodInjector) ValidateUpdate(_ context.Context, _, _ *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

func (i *PodInjector) ValidateDelete(_ context.Context, _ *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}
