// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const annotationBypassNamespace = "annotation-bypass-e2e"

// TestInjectedAnnotationCannotSkipInjection covers #143 against a real API
// server.
//
// The injector used to treat berth.skaphos.io/injected="true" as proof that
// a pod had already been mutated and return early. That annotation is part
// of the pod spec, so a submitter could set it on create and be admitted
// with no init container, no sidecar, no state volume and no probe — while
// still carrying the opt-in label, so anything auditing by label saw a gated
// workload.
//
// Which admission path a request arrived on is now read from the
// AdmissionRequest's subresource instead. A unit test can fabricate that
// value, which is precisely why this test exists: only a live API server
// proves the real request carries what the code expects. An earlier fix on
// this branch was wrong about admission semantics in exactly that way — the
// ephemeralcontainers rule was registered for CREATE when the real operation
// is UPDATE — and unit tests could not have caught it.
func TestInjectedAnnotationCannotSkipInjection(t *testing.T) {
	ctx := context.Background()
	c := clusters.east

	t.Cleanup(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: annotationBypassNamespace}}
		if err := c.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup namespace %s: %v", annotationBypassNamespace, err)
		}
	})

	if err := ensureNamespace(ctx, c, annotationBypassNamespace); err != nil {
		t.Fatalf("ensure namespace: %v", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "claims-injected",
			Namespace: annotationBypassNamespace,
			Labels:    map[string]string{"berth.skaphos.io/inject": "acquire"},
			Annotations: map[string]string{
				"berth.skaphos.io/lease-name": "annotation-bypass",
				"berth.skaphos.io/mode":       "runtime-singleton",
				"berth.skaphos.io/enforce":    "probe",
				// The claim under test.
				"berth.skaphos.io/injected": "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: targetImage}},
		},
	}

	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	t.Cleanup(func() { _ = c.Delete(ctx, pod) })

	fetched := &corev1.Pod{}
	key := ctrlclient.ObjectKey{Namespace: annotationBypassNamespace, Name: pod.Name}
	if err := c.Get(ctx, key, fetched); err != nil {
		t.Fatalf("get admitted pod: %v", err)
	}

	if !hasInitContainer(*fetched, "berth-acquire") {
		t.Error("the pod must still be injected: the annotation is not proof of prior injection")
	}
	if !hasInitContainer(*fetched, "berth-sidecar") {
		t.Error("runtime-singleton must still get its sidecar")
	}

	app := containerByName(fetched.Spec.Containers, "app")
	if app == nil {
		t.Fatal("app container missing")
	}
	if app.LivenessProbe == nil {
		t.Error("probe enforcement must still be wired; an ungated pod is the whole defect")
	}
}
