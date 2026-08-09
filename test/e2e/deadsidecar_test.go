// SPDX-FileCopyrightText: 2026 Rillan AI LLC
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	deadSidecarNamespace = "dead-sidecar-e2e"
	deadSidecarLease     = "dead-sidecar-singleton"
)

// TestDeadSidecarStopsTheWorkload proves the marker-freshness half of the
// enforcement story (issue #98) in a live cluster: when the sidecar stops
// renewing, the workload is killed *without* the sidecar doing anything.
//
// This is the assertion unit tests structurally cannot make. They can show
// the predicate returns Stale for an old marker; only a cluster shows the
// kubelet acting on it — that the injected probe carries the right bound,
// that the marker really does stop being refreshed, and that the container
// actually dies.
//
// The sidecar is stopped by repointing its image at a tag that cannot be
// pulled. A container image is one of the few mutable fields on a running
// Pod, and it works where `kubectl exec` does not: the helper image is
// distroless, so there is no shell to kill anything with.
//
// Note the failure mode this distinguishes. A *live* sidecar that cannot
// renew self-fences past expiry and removes the marker, so the probe fails
// with "absent" — that is #97's path and it already worked. Here the sidecar
// is gone and cannot remove anything, so the marker is left behind and goes
// stale. Before this change that pod ran on unleased, indefinitely.
func TestDeadSidecarStopsTheWorkload(t *testing.T) {
	ctx := context.Background()
	c := clusters.east

	t.Cleanup(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: deadSidecarNamespace}}
		if err := c.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup namespace %s: %v", deadSidecarNamespace, err)
		}
	})

	if err := ensureNamespace(ctx, c, deadSidecarNamespace); err != nil {
		t.Fatalf("ensure namespace: %v", err)
	}
	if err := copyTokenSecretTo(ctx, c, deadSidecarNamespace); err != nil {
		t.Fatalf("copy token secret: %v", err)
	}

	pod := gatedSingletonPod()
	if err := c.Create(ctx, pod); err != nil {
		t.Fatalf("create gated pod: %v", err)
	}
	key := ctrlclient.ObjectKey{Namespace: deadSidecarNamespace, Name: pod.Name}

	// The workload only becomes Ready once the init "hold" has won the lease,
	// so this also confirms the marker exists and the probe is passing.
	waitFor(t, 3*time.Minute, "gated pod to acquire and become ready", func(ctx context.Context) (bool, string) {
		p := &corev1.Pod{}
		if err := c.Get(ctx, key, p); err != nil {
			return false, err.Error()
		}
		return podReady(*p), fmt.Sprintf("phase=%s ready=%v", p.Status.Phase, podReady(*p))
	})

	before := appRestartCount(ctx, t, c, key)

	// Kill the sidecar by making its image unpullable. The running container
	// is replaced, and the replacement never starts.
	patch := []byte(fmt.Sprintf(
		`{"spec":{"initContainers":[{"name":"berth-sidecar","image":"registry.k8s.io/pause:does-not-exist-%d"}]}}`,
		time.Now().UnixNano()))
	if err := c.Patch(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: pod.Name, Namespace: deadSidecarNamespace,
	}}, ctrlclient.RawPatch(types.StrategicMergePatchType, patch)); err != nil {
		t.Fatalf("stop the sidecar by repointing its image: %v", err)
	}

	// One TTL for the marker to age out, plus probe period and kill latency.
	waitFor(t, 3*time.Minute, "workload to be killed by the stale marker", func(ctx context.Context) (bool, string) {
		p := &corev1.Pod{}
		if err := c.Get(ctx, key, p); err != nil {
			return false, err.Error()
		}
		now := appRestartCountFrom(*p)
		return now > before, fmt.Sprintf("app restartCount %d -> %d (want > %d)", before, now, before)
	})

	// The kill must be explicable. An operator seeing this needs to know it
	// was a dead sidecar, not an ordinary lease handover.
	assertStaleProbeEvent(ctx, t, c, key)
}

// assertStaleProbeEvent looks for the kubelet's probe-failure event carrying
// check's own explanation.
func assertStaleProbeEvent(ctx context.Context, t *testing.T, c ctrlclient.Client, key ctrlclient.ObjectKey) {
	t.Helper()

	events := &corev1.EventList{}
	if err := c.List(ctx, events, ctrlclient.InNamespace(key.Namespace)); err != nil {
		t.Fatalf("list events: %v", err)
	}

	var unhealthy []string
	for _, e := range events.Items {
		if e.InvolvedObject.Name != key.Name || e.Reason != "Unhealthy" {
			continue
		}
		unhealthy = append(unhealthy, e.Message)
		if strings.Contains(e.Message, "stale") {
			return // explained correctly
		}
	}

	if len(unhealthy) == 0 {
		t.Error("no Unhealthy probe events recorded; the kill was not explained at all")
		return
	}
	t.Errorf("probe failure never reported a stale marker, so a dead sidecar is "+
		"indistinguishable from a lease handover. Messages seen: %q", unhealthy)
}

func appRestartCount(ctx context.Context, t *testing.T, c ctrlclient.Client, key ctrlclient.ObjectKey) int32 {
	t.Helper()
	p := &corev1.Pod{}
	if err := c.Get(ctx, key, p); err != nil {
		t.Fatalf("get pod: %v", err)
	}
	return appRestartCountFrom(*p)
}

func appRestartCountFrom(p corev1.Pod) int32 {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Name == "app" {
			return cs.RestartCount
		}
	}
	return 0
}

// copyTokenSecretTo duplicates the harness bearer-token Secret into an
// arbitrary namespace.
func copyTokenSecretTo(ctx context.Context, c ctrlclient.Client, ns string) error {
	src := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: tokenSecret, Namespace: namespace}, src); err != nil {
		return fmt.Errorf("read source secret %s/%s: %w", namespace, tokenSecret, err)
	}
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tokenSecret, Namespace: ns},
		Data:       src.Data,
		Type:       src.Type,
	}
	if err := c.Create(ctx, dst); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func gatedSingletonPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dead-sidecar-demo",
			Namespace: deadSidecarNamespace,
			Labels:    map[string]string{"berth.skaphos.io/inject": "acquire"},
			Annotations: map[string]string{
				"berth.skaphos.io/lease-name":  deadSidecarLease,
				"berth.skaphos.io/mode":        "runtime-singleton",
				"berth.skaphos.io/enforce":     "probe",
				"berth.skaphos.io/ttl-seconds": fmt.Sprintf("%d", ttlSeconds),
			},
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: ptrInt64(1),
			Volumes: []corev1.Volume{{
				Name:         "berth-token",
				VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: tokenSecret}},
			}},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: targetImage,
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "berth-token",
					MountPath: tokenMountDir,
					ReadOnly:  true,
				}},
			}},
		},
	}
}

func ptrInt64(v int64) *int64 { return &v }
