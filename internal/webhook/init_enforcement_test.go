package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/skaphos/berth/internal/acquire"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestRuntimeRejectsWorkloadInitContainers(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	onFailure := corev1.ContainerRestartPolicyOnFailure
	for _, mode := range []string{"", string(acquire.ModeRuntimeSingleton)} {
		for _, enforce := range []string{"", string(acquire.EnforceProbe), string(acquire.EnforceSignal)} {
			for _, restart := range []*corev1.ContainerRestartPolicy{nil, &always, &onFailure} {
				pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnMode: mode, AnnEnforce: enforce, AnnSignalTarget: "app", AnnInjected: "true"})
				pod.Spec.InitContainers = []corev1.Container{{Name: "workload-init", Image: "example/init:1", RestartPolicy: restart}}
				err := testInjector().Default(context.Background(), pod)
				if err == nil || !strings.Contains(err.Error(), "workload-init") {
					t.Fatalf("workload init admitted: mode=%q enforce=%q restart=%v err=%v", mode, enforce, restart, err)
				}
				if findContainer(pod.Spec.InitContainers, InitContainerName) != nil {
					t.Fatal("rejected Pod was injected")
				}
			}
		}
	}
}

func TestFinalAdmissionRejectsLateInitInjection(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	inj := testInjector()
	handler := admission.WithValidator(scheme, inj)
	always := corev1.ContainerRestartPolicyAlways
	for _, mode := range []acquire.Mode{acquire.ModeRuntimeSingleton, acquire.ModeStartupGate} {
		for _, native := range []bool{false, true} {
			pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnMode: string(mode)})
			if err := inj.Default(context.Background(), pod); err != nil {
				t.Fatal(err)
			}
			check := func(want bool) {
				t.Helper()
				raw, err := json.Marshal(pod)
				if err != nil {
					t.Fatal(err)
				}
				res := handler.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Create, Namespace: "prod", Object: runtime.RawExtension{Raw: raw},
				}})
				if res.Allowed != want {
					t.Fatalf("mode=%s native=%v admitted=%v want=%v: %+v", mode, native, res.Allowed, want, res.Result)
				}
			}
			check(true) // The trusted injected helpers pass final validation.
			late := corev1.Container{Name: "late-init", Image: "example/init:1"}
			if native {
				late.RestartPolicy = &always
			}
			pod.Spec.InitContainers = append(pod.Spec.InitContainers, late)
			check(mode == acquire.ModeStartupGate)
		}
	}
}

func TestStartupGateAllowsWorkloadInitShapes(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	for _, policy := range []*corev1.ContainerRestartPolicy{nil, &always} {
		pod := optInPod("prod", map[string]string{AnnLeaseName: "test", AnnMode: string(acquire.ModeStartupGate)})
		pod.Spec.InitContainers = []corev1.Container{{Name: "workload", Image: "example/init:1", RestartPolicy: policy}}
		if err := testInjector().Default(context.Background(), pod); err != nil {
			t.Fatal(err)
		}
		if len(pod.Spec.InitContainers) != 2 || pod.Spec.InitContainers[1].Name != "workload" {
			t.Fatal("startup-gate init ordering changed")
		}
	}
}

func TestFinalAdmissionSkipsUnmanagedAndControlPlanePods(t *testing.T) {
	inj := testInjector()
	pod := optInPod("prod", map[string]string{AnnLeaseName: "test"})
	delete(pod.Labels, LabelInject)
	if _, err := inj.ValidateCreate(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	pod.Labels[LabelInject] = InjectValueAcquire
	ctx := admission.NewContextWithRequest(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Namespace: "berth-system"}})
	if _, err := inj.ValidateCreate(ctx, pod); err != nil {
		t.Fatal("control plane blocked", err)
	}
	// Old unsupported Pods must still be updateable and deletable during migration.
	if _, err := inj.ValidateUpdate(context.Background(), pod, pod); err != nil {
		t.Fatal(err)
	}
	if _, err := inj.ValidateDelete(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
}
