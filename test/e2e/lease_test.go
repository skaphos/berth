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

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
)

const (
	leaseName  = "demo-lease"
	targetName = "demo-app"
	// pause image starts in <2s and never exits — ideal for replica-count
	// assertions where we don't care what the workload does.
	targetImage = "registry.k8s.io/pause:3.9"

	ttlSeconds       = 15
	heartbeatSeconds = 5
	// acquireWindow bounds how long a fresh acquire should take: TTL
	// (worst-case stale lease in store) + 2× heartbeat (one for the
	// loser to give up, one for the winner to settle). Tests fail fast
	// if convergence slips beyond this.
	acquireWindow = time.Duration(ttlSeconds+2*heartbeatSeconds) * time.Second
)

// TestHappyPath_SingleHolder: identical BerthLease applied to both
// runner clusters, exactly one cluster's target Deployment ends up at
// the acquireAction replica count, the other at the releaseAction
// count. Invariant holds across repeated reconciles (no flipping).
func TestHappyPath_SingleHolder(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() { cleanupFixtures(t, ctx) })

	if err := applyTargetDeployment(ctx, clusters.east); err != nil {
		t.Fatalf("apply east target: %v", err)
	}
	if err := applyTargetDeployment(ctx, clusters.west); err != nil {
		t.Fatalf("apply west target: %v", err)
	}

	if err := applyBerthLease(ctx, clusters.east); err != nil {
		t.Fatalf("apply east lease: %v", err)
	}
	if err := applyBerthLease(ctx, clusters.west); err != nil {
		t.Fatalf("apply west lease: %v", err)
	}

	waitFor(t, acquireWindow, "exactly one cluster scaled up", func(ctx context.Context) (bool, string) {
		east, eastErr := getDeploymentReplicas(ctx, clusters.east)
		west, westErr := getDeploymentReplicas(ctx, clusters.west)
		if eastErr != nil || westErr != nil {
			return false, fmt.Sprintf("read replicas: east=%v west=%v", eastErr, westErr)
		}
		if (east == 2 && west == 0) || (east == 0 && west == 2) {
			return true, ""
		}
		return false, fmt.Sprintf("east=%d west=%d (want one=2 other=0)", east, west)
	})

	// Idempotency: hold for 2× heartbeat to make sure the assertion
	// isn't a flap. A flipping pair would shake out here.
	stableFor := 2 * heartbeatSeconds * time.Second
	t.Logf("verifying stable for %s", stableFor)
	stableDeadline := time.Now().Add(stableFor)
	for time.Now().Before(stableDeadline) {
		east, eastErr := getDeploymentReplicas(ctx, clusters.east)
		west, westErr := getDeploymentReplicas(ctx, clusters.west)
		if eastErr != nil || westErr != nil {
			t.Fatalf("read replicas during stable check: east=%v west=%v", eastErr, westErr)
		}
		if !((east == 2 && west == 0) || (east == 0 && west == 2)) {
			t.Fatalf("unstable: east=%d west=%d", east, west)
		}
		time.Sleep(2 * time.Second)
	}
}

// applyTargetDeployment creates the target Deployment for the e2e
// scenarios in the given runner cluster. Idempotent: deletes any prior
// instance so each test starts from a known state.
func applyTargetDeployment(ctx context.Context, c ctrlclient.Client) error {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: targetName, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](0),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": targetName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": targetName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "pause", Image: targetImage},
					},
					TerminationGracePeriodSeconds: ptr.To[int64](1),
				},
			},
		},
	}
	return upsert(ctx, c, dep)
}

// applyBerthLease creates the BerthLease CR. spec.HolderIdentity is a
// placeholder — the operator overrides it with its --cluster-id flag
// (see cmd/operator/main.go).
func applyBerthLease(ctx context.Context, c ctrlclient.Client) error {
	lease := &berthv1alpha1.BerthLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: namespace},
		Spec: berthv1alpha1.BerthLeaseSpec{
			LeaseName:                leaseName,
			HolderIdentity:           "overridden-by-cluster-id",
			TTLSeconds:               ttlSeconds,
			HeartbeatIntervalSeconds: heartbeatSeconds,
			Semantics:                "at-most-once",
			Target: &berthv1alpha1.TargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetName,
			},
			AcquireAction: &berthv1alpha1.LeaseAction{
				Scale: &berthv1alpha1.ScaleAction{Replicas: 2},
			},
			ReleaseAction: &berthv1alpha1.LeaseAction{
				Scale: &berthv1alpha1.ScaleAction{Replicas: 0},
			},
		},
	}
	return upsert(ctx, c, lease)
}

func getDeploymentReplicas(ctx context.Context, c ctrlclient.Client) (int32, error) {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Name: targetName, Namespace: namespace}
	if err := c.Get(ctx, key, dep); err != nil {
		return 0, err
	}
	if dep.Spec.Replicas == nil {
		return 0, nil
	}
	return *dep.Spec.Replicas, nil
}

// upsert deletes any existing object then creates the new one. Simpler
// than apply-semantics three-way merging and good enough for fixtures
// whose only purpose is to seed a test.
func upsert(ctx context.Context, c ctrlclient.Client, obj ctrlclient.Object) error {
	if err := c.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete prior %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	// Object now stale (ResourceVersion set from the failed get on
	// delete). Strip metadata that conflicts with create.
	obj.SetResourceVersion("")
	obj.SetUID("")
	if err := c.Create(ctx, obj); err != nil {
		return fmt.Errorf("create %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	return nil
}

// cleanupFixtures removes the per-test resources from both runner
// clusters so subsequent tests start clean. Errors are logged, not
// fatal — teardown shouldn't mask the real failure.
func cleanupFixtures(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, pair := range []struct {
		name string
		c    ctrlclient.Client
	}{
		{"east", clusters.east},
		{"west", clusters.west},
	} {
		lease := &berthv1alpha1.BerthLease{
			ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: namespace},
		}
		if err := pair.c.Delete(ctx, lease); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup %s lease: %v", pair.name, err)
		}
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: targetName, Namespace: namespace},
		}
		if err := pair.c.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup %s deployment: %v", pair.name, err)
		}
	}
}
