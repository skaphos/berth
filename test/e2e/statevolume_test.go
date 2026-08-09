// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const stateVolumeNamespace = "state-volume-e2e"

// TestStateVolumeIsReserved proves in a live cluster that the reserved
// state volume rule (issue #96) is actually served by the webhook, not just
// implemented in the package.
//
// Unit tests in internal/webhook cover the decision table. What they cannot
// prove is that the rule reaches a real API server: that the
// MutatingWebhookConfiguration selectors fire, that the new
// pods/ephemeralcontainers rule is registered, and that the rejection
// surfaces to the submitter. That is what this test is for.
//
// Pods are created directly rather than through a Deployment on purpose.
// An admission rejection of a Deployment's template is reported
// asynchronously on the ReplicaSet, whereas a direct Pod create returns the
// error synchronously — so the assertion is about the message the person
// running kubectl actually sees.
//
// Note on scope: this does not attempt to write to /berth from inside the
// container. The workload image is registry.k8s.io/pause, which has no
// shell, and the harness client has no exec support. Enforcing a read-only
// mount is the kernel's job once the PodSpec says readOnly; what Berth owns
// — and what is asserted here — is that the spec always says so, and that
// the writable shape never admits at all.
func TestStateVolumeIsReserved(t *testing.T) {
	ctx := context.Background()
	c := clusters.east

	t.Cleanup(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: stateVolumeNamespace}}
		if err := c.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup namespace %s: %v", stateVolumeNamespace, err)
		}
	})

	if err := ensureNamespace(ctx, c, stateVolumeNamespace); err != nil {
		t.Fatalf("ensure namespace: %v", err)
	}

	t.Run("writable mount is refused", func(t *testing.T) {
		pod := gatedPod("reserved-writable")
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{Name: "berth-state", MountPath: "/rw"})

		err := c.Create(ctx, pod)
		if err == nil {
			_ = c.Delete(ctx, pod)
			t.Fatal("a writable mount of the state volume must be refused by admission")
		}
		// The message is the whole point of the rejection: it has to tell
		// the owner what to change without reading Berth's source.
		for _, want := range []string{"app", "berth-state", "/rw"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("rejection %q must name %q", err, want)
			}
		}
	})

	t.Run("read-only mount is admitted with the state mount read-only", func(t *testing.T) {
		pod := gatedPod("reserved-readonly")
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{Name: "berth-state", MountPath: "/ro", ReadOnly: true})

		if err := c.Create(ctx, pod); err != nil {
			t.Fatalf("read-only access is not a bypass and must admit: %v", err)
		}
		t.Cleanup(func() { _ = c.Delete(ctx, pod) })

		fetched := &corev1.Pod{}
		key := ctrlclient.ObjectKey{Namespace: stateVolumeNamespace, Name: pod.Name}
		if err := c.Get(ctx, key, fetched); err != nil {
			t.Fatalf("get admitted pod: %v", err)
		}

		app := containerByName(fetched.Spec.Containers, "app")
		if app == nil {
			t.Fatal("admitted pod lost its app container")
		}
		for _, m := range app.VolumeMounts {
			if m.Name == "berth-state" && !m.ReadOnly {
				t.Errorf("every state mount in a workload container must be read-only; %+v is not", m)
			}
		}
	})
}

// gatedPod is a minimal opted-in pod. It deliberately omits the auth token
// mount used by TestInjectionGating: these assertions are about admission,
// which happens long before the helper needs to reach the API server.
func gatedPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: stateVolumeNamespace,
			Labels:    map[string]string{"berth.skaphos.io/inject": "acquire"},
			Annotations: map[string]string{
				"berth.skaphos.io/lease-name": "reserved-volume",
				"berth.skaphos.io/mode":       "runtime-singleton",
				"berth.skaphos.io/enforce":    "probe",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: targetImage}},
		},
	}
}

func containerByName(cs []corev1.Container, name string) *corev1.Container {
	for i := range cs {
		if cs[i].Name == name {
			return &cs[i]
		}
	}
	return nil
}
