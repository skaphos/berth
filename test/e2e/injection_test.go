// SPDX-FileCopyrightText: 2026 Skaphos
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// injectNamespace is deliberately NOT berth-system: the chart's
	// MutatingWebhookConfiguration never mutates the release namespace or the
	// configured control-plane namespaces, so opted-in workloads must live
	// elsewhere.
	injectNamespace = "inject-e2e"
	injectApp       = "inject-demo"
	injectLease     = "inject-singleton"
	injectReplicas  = 2
	// tokenSecret is created by the harness in berth-system; the test copies it
	// into injectNamespace and mounts it where operator-values points the
	// injected helper (injection.helper.apiKeyFile=/var/run/berth/api-key).
	tokenSecret   = "berth-api-key"
	tokenMountDir = "/var/run/berth"
)

// TestInjectionGating proves the injection path end to end in one runner
// cluster: an opted-in Deployment (replicas=2, runtime-singleton) is mutated by
// the operator's webhook, and exactly one pod passes the lease gate while the
// rest stay blocked at the injected init "hold".
//
// Two assertions, in order:
//
//  1. Mutation — every pod gains the berth-acquire init container and the
//     berth-sidecar native sidecar. This proves the webhook is served, its
//     serving cert is trusted, the MutatingWebhookConfiguration selectors fire,
//     and the mutation is correct. It does not depend on the helper reaching
//     the API server.
//  2. Gating — exactly one of the replicas becomes Ready; the others remain
//     not-Ready (blocked at the hold because they cannot acquire the lease).
//
// EXPECTED FAILURE until SKA-444: assertion (2) needs the injected helper to
// authenticate to the Berth API server, but the e2e apiserver runs
// auth-mode=static-keys and the webhook does not yet mount the bearer token
// into the helper containers (it only sets BERTH_API_KEY_FILE). So the helper
// gets 401s and no replica ever acquires. This test is the regression guard
// for SKA-444 — once the webhook mounts the token, assertion (2) passes.
func TestInjectionGating(t *testing.T) {
	ctx := context.Background()
	c := clusters.east

	t.Cleanup(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: injectNamespace}}
		if err := c.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup namespace %s: %v", injectNamespace, err)
		}
	})

	if err := ensureNamespace(ctx, c, injectNamespace); err != nil {
		t.Fatalf("ensure namespace: %v", err)
	}
	if err := copyTokenSecret(ctx, c); err != nil {
		t.Fatalf("copy token secret: %v", err)
	}
	if err := applyInjectedDeployment(ctx, c); err != nil {
		t.Fatalf("apply opted-in deployment: %v", err)
	}

	// (1) Mutation: wait for all replicas to be admitted with the injected
	// containers. This is the deterministic proof the webhook fired.
	waitFor(t, 90*time.Second, "all replicas mutated by the injection webhook", func(ctx context.Context) (bool, string) {
		pods, err := listAppPods(ctx, c)
		if err != nil {
			return false, err.Error()
		}
		if len(pods) < injectReplicas {
			return false, fmt.Sprintf("only %d/%d pods scheduled", len(pods), injectReplicas)
		}
		for _, p := range pods {
			if !hasInitContainer(p, "berth-acquire") || !hasInitContainer(p, "berth-sidecar") {
				return false, fmt.Sprintf("pod %s missing injected containers (init=%v)", p.Name, initNames(p))
			}
		}
		return true, ""
	})
	t.Logf("all %d replicas were mutated (berth-acquire + berth-sidecar injected)", injectReplicas)

	// (2) Gating: exactly one replica wins the lease and becomes Ready; the
	// rest stay blocked at the hold. See the EXPECTED FAILURE note above —
	// this step fails until SKA-444 is fixed.
	gateWindow := time.Duration(ttlSeconds+2*heartbeatSeconds)*time.Second + 60*time.Second
	waitFor(t, gateWindow, "exactly one replica passes the lease gate", func(ctx context.Context) (bool, string) {
		pods, err := listAppPods(ctx, c)
		if err != nil {
			return false, err.Error()
		}
		ready := 0
		for _, p := range pods {
			if podReady(p) {
				ready++
			}
		}
		if ready == 1 {
			return true, ""
		}
		return false, fmt.Sprintf("%d/%d replicas Ready (want exactly 1; if this stays 0, see SKA-444)", ready, len(pods))
	})

	// Hold the singleton invariant briefly so a flapping gate would show up.
	stableFor := 2 * heartbeatSeconds * time.Second
	deadline := time.Now().Add(stableFor)
	for time.Now().Before(deadline) {
		pods, err := listAppPods(ctx, c)
		if err != nil {
			t.Fatalf("list pods during stable check: %v", err)
		}
		ready := 0
		for _, p := range pods {
			if podReady(p) {
				ready++
			}
		}
		if ready != 1 {
			t.Fatalf("singleton invariant broken: %d replicas Ready (want 1)", ready)
		}
		time.Sleep(2 * time.Second)
	}
}

func ensureNamespace(ctx context.Context, c ctrlclient.Client, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if err := c.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// copyTokenSecret duplicates the harness-created bearer-token Secret from
// berth-system into injectNamespace so the opted-in workload can mount it at
// the path the injected helper reads.
func copyTokenSecret(ctx context.Context, c ctrlclient.Client) error {
	src := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: tokenSecret, Namespace: namespace}, src); err != nil {
		return fmt.Errorf("read source secret %s/%s: %w", namespace, tokenSecret, err)
	}
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecret, Namespace: injectNamespace},
		Data:       src.Data,
		Type:       src.Type,
	}
	if err := c.Create(ctx, dst); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// applyInjectedDeployment creates the opted-in workload. The opt-in label and
// the behavior annotations live on the pod template (copied onto the Pods the
// Deployment controller creates), which is what the webhook keys off.
func applyInjectedDeployment(ctx context.Context, c ctrlclient.Client) error {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: injectApp, Namespace: injectNamespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](injectReplicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": injectApp}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                     injectApp,
						"berth.skaphos.io/inject": "acquire",
					},
					Annotations: map[string]string{
						"berth.skaphos.io/lease-name":  injectLease,
						"berth.skaphos.io/mode":        "runtime-singleton",
						"berth.skaphos.io/enforce":     "probe",
						"berth.skaphos.io/ttl-seconds": fmt.Sprintf("%d", ttlSeconds),
					},
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](1),
					Volumes: []corev1.Volume{{
						Name:         "berth-token",
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: tokenSecret}},
					}},
					Containers: []corev1.Container{{
						Name:  "app",
						Image: targetImage, // registry.k8s.io/pause — probe uses the injected static check binary, no shell needed
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "berth-token",
							MountPath: tokenMountDir,
							ReadOnly:  true,
						}},
					}},
				},
			},
		},
	}
	if err := c.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete prior deployment: %w", err)
	}
	if err := waitGone(ctx, c, dep); err != nil {
		return err
	}
	dep.SetResourceVersion("")
	dep.SetUID("")
	if err := c.Create(ctx, dep); err != nil {
		return fmt.Errorf("create deployment: %w", err)
	}
	return nil
}

func listAppPods(ctx context.Context, c ctrlclient.Client) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := c.List(ctx, list,
		ctrlclient.InNamespace(injectNamespace),
		ctrlclient.MatchingLabels{"app": injectApp},
	); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func hasInitContainer(p corev1.Pod, name string) bool {
	for _, c := range p.Spec.InitContainers {
		if c.Name == name {
			return true
		}
	}
	return false
}

func initNames(p corev1.Pod) []string {
	names := make([]string, 0, len(p.Spec.InitContainers))
	for _, c := range p.Spec.InitContainers {
		names = append(names, c.Name)
	}
	return names
}

func podReady(p corev1.Pod) bool {
	if p.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
