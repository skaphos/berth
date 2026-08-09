package webhook

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
)

func TestRejectionCounterRecordsReasonAndPath(t *testing.T) {
	rejectionsTotal.Reset()

	// A refused pod creation.
	pod := mountedPod(corev1.VolumeMount{Name: VolumeName, MountPath: "/rw"})
	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("expected the writable mount to be rejected")
	}

	// A refused ephemeral container on an already-injected pod.
	running := optInPod("prod", map[string]string{AnnLeaseName: "checkout"})
	if err := testInjector().Default(context.Background(), running); err != nil {
		t.Fatalf("initial inject: %v", err)
	}
	running.Spec.EphemeralContainers = append(running.Spec.EphemeralContainers, corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:         "debugger",
			Image:        "busybox",
			VolumeMounts: []corev1.VolumeMount{{Name: VolumeName, MountPath: "/rw"}},
		},
	})
	if err := testInjector().Default(ephemeralCtx("prod"), running); err == nil {
		t.Fatal("expected the ephemeral writable mount to be rejected")
	}

	got := testutil.ToFloat64(rejectionsTotal.WithLabelValues(
		string(ReasonWritableStateMount), admissionPathPods))
	if got != 1 {
		t.Errorf("pods rejections = %v, want 1", got)
	}

	got = testutil.ToFloat64(rejectionsTotal.WithLabelValues(
		string(ReasonWritableStateMountEphemeral), admissionPathEphemeralContainers))
	if got != 1 {
		t.Errorf("ephemeralcontainers rejections = %v, want 1", got)
	}
}

// Cardinality is a correctness property here, not a style preference: a
// rejected pod is usually recreated by its controller in a hot loop, so a
// pod- or namespace-scoped label would mint a new series per attempt and
// eventually take out the metrics endpoint.
func TestRejectionCounterHasBoundedLabels(t *testing.T) {
	rejectionsTotal.Reset()

	pod := mountedPod(corev1.VolumeMount{Name: VolumeName, MountPath: "/rw"})
	if err := testInjector().Default(context.Background(), pod); err == nil {
		t.Fatal("expected rejection")
	}

	var buf strings.Builder
	if err := testutil.CollectAndCompare(rejectionsTotal, strings.NewReader(
		`# HELP berth_webhook_admission_rejections_total Admissions rejected by the Berth injection webhook, by reason and admission path.
# TYPE berth_webhook_admission_rejections_total counter
berth_webhook_admission_rejections_total{path="pods",reason="writable_state_mount"} 1
`)); err != nil {
		t.Errorf("unexpected series: %v%s", err, buf.String())
	}
}
