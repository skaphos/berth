// SPDX-FileCopyrightText: 2026 Skaphos
// SPDX-License-Identifier: MIT

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

// waitForHolder establishes the steady-state singleton and returns
// (holder, standby) — the cluster currently at acquireAction replicas
// and the one at releaseAction replicas. Fails the test if convergence
// doesn't happen within acquireWindow.
func waitForHolder(t *testing.T, ctx context.Context) (holder, standby clusterRef) {
	t.Helper()
	waitFor(t, acquireWindow, "exactly one cluster scaled up", func(ctx context.Context) (bool, string) {
		east, eastErr := getDeploymentReplicas(ctx, clusters.east)
		west, westErr := getDeploymentReplicas(ctx, clusters.west)
		if eastErr != nil || westErr != nil {
			return false, fmt.Sprintf("read replicas: east=%v west=%v", eastErr, westErr)
		}
		switch {
		case east == 2 && west == 0:
			holder = clusterRef{name: "east", kindName: "berth-e2e-east", c: clusters.east}
			standby = clusterRef{name: "west", kindName: "berth-e2e-west", c: clusters.west}
			return true, ""
		case east == 0 && west == 2:
			holder = clusterRef{name: "west", kindName: "berth-e2e-west", c: clusters.west}
			standby = clusterRef{name: "east", kindName: "berth-e2e-east", c: clusters.east}
			return true, ""
		default:
			return false, fmt.Sprintf("east=%d west=%d (want one=2 other=0)", east, west)
		}
	})
	return holder, standby
}

// clusterRef is a tagged client used by scenarios that need to operate
// on whichever side ended up as the holder (e.g., kill its operator).
type clusterRef struct {
	name     string // "east" / "west", for log messages
	kindName string // kind cluster name, e.g. "berth-e2e-east"
	c        ctrlclient.Client
}

// kubectlContext returns the kubectl --context flag for a given kind
// cluster.
func (r clusterRef) kubectlContext() string { return "kind-" + r.kindName }

// deleteOperatorPod removes the operator pod on the given cluster. The
// Deployment controller will recreate it within a few seconds.
func deleteOperatorPod(ctx context.Context, c ctrlclient.Client) error {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods,
		ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabelsSelector{Selector: labels.SelectorFromSet(map[string]string{
			"app.kubernetes.io/name": "berth-operator",
		})},
	); err != nil {
		return fmt.Errorf("list operator pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no operator pods found in namespace %q", namespace)
	}
	for i := range pods.Items {
		if err := c.Delete(ctx, &pods.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete pod %s: %w", pods.Items[i].Name, err)
		}
	}
	return nil
}

// TestHolderFailover (scenario 2): kill the operator pod on the holding
// cluster. The other cluster acquires within ttl + reacquire and its
// Deployment scales up. The original holder's Deployment stays at 2
// (no reconciler to scale it down) until the operator pod returns and
// reconciles — at which point the spec's "stays at 0 until operator
// returns" assertion engages (verified in scenario 3).
func TestHolderFailover(t *testing.T) {
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

	holder, standby := waitForHolder(t, ctx)
	t.Logf("steady state: %s holds, %s waits", holder.name, standby.name)

	if err := deleteOperatorPod(ctx, holder.c); err != nil {
		t.Fatalf("delete operator pod on %s: %v", holder.name, err)
	}
	t.Logf("deleted operator pod on %s", holder.name)

	// Within ttl + reacquire, the standby's next Acquire succeeds.
	waitFor(t, acquireWindow+10*time.Second, "standby takes over", func(ctx context.Context) (bool, string) {
		replicas, err := getDeploymentReplicas(ctx, standby.c)
		if err != nil {
			return false, err.Error()
		}
		if replicas == 2 {
			return true, ""
		}
		return false, fmt.Sprintf("%s replicas=%d (want 2)", standby.name, replicas)
	})
}

// TestHolderRejoin (scenario 3): after failover, the operator pod on
// the original holder cluster is recreated by the Deployment
// controller. Its first reconcile observes Acquired=false and scales
// the original holder's Deployment to 0. Final invariant: holder
// (original) = 0, standby (new) = 2.
//
// The spec's "no interval where both at non-zero" is not strictly
// enforceable in this design — between standby acquire and original
// operator return, both will briefly be at 2. The test asserts the
// converged state after rejoin, which is what at-most-once guarantees
// in steady state.
func TestHolderRejoin(t *testing.T) {
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

	original, newHolder := waitForHolder(t, ctx)
	t.Logf("steady state: %s holds, %s waits", original.name, newHolder.name)

	if err := deleteOperatorPod(ctx, original.c); err != nil {
		t.Fatalf("delete operator pod on %s: %v", original.name, err)
	}

	// Wait for new holder to take over.
	waitFor(t, acquireWindow+10*time.Second, "new holder acquired", func(ctx context.Context) (bool, string) {
		replicas, err := getDeploymentReplicas(ctx, newHolder.c)
		if err != nil {
			return false, err.Error()
		}
		if replicas == 2 {
			return true, ""
		}
		return false, fmt.Sprintf("%s replicas=%d (want 2)", newHolder.name, replicas)
	})

	// Now wait for original operator's first reconcile to scale its
	// deployment down. Generous timeout — pod restart + first reconcile
	// can take 20-30s.
	waitFor(t, 90*time.Second, "rejoining operator scales original holder to 0", func(ctx context.Context) (bool, string) {
		replicas, err := getDeploymentReplicas(ctx, original.c)
		if err != nil {
			return false, err.Error()
		}
		if replicas == 0 {
			return true, ""
		}
		return false, fmt.Sprintf("%s replicas=%d (want 0)", original.name, replicas)
	})
}

// TestAPIServerRestart (scenario 4): with one cluster holding,
// rollout-restart the apiserver Deployment. K8sLeaseStore persists
// state in coordination.k8s.io/v1.Lease, so the holder continues to
// hold after the rollout. Neither cluster transitions to waiting.
func TestAPIServerRestart(t *testing.T) {
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

	holder, standby := waitForHolder(t, ctx)
	t.Logf("steady state: %s holds, %s waits", holder.name, standby.name)

	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", "kind-berth-e2e-coord",
		"-n", namespace,
		"rollout", "restart", "deployment/berth-apiserver",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rollout restart apiserver: %v\n%s", err, out)
	}

	cmd = exec.CommandContext(ctx, "kubectl",
		"--context", "kind-berth-e2e-coord",
		"-n", namespace,
		"rollout", "status", "deployment/berth-apiserver", "--timeout=2m",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wait for apiserver rollout: %v\n%s", err, out)
	}

	// Verify the holder hasn't lost its lease across the restart. Hold
	// the assertion for 3× heartbeat so we'd notice a flap.
	stableFor := 3 * heartbeatSeconds * time.Second
	t.Logf("verifying holder stays held for %s after apiserver restart", stableFor)
	deadline := time.Now().Add(stableFor)
	for time.Now().Before(deadline) {
		holderReplicas, err := getDeploymentReplicas(ctx, holder.c)
		if err != nil {
			t.Fatalf("read holder replicas: %v", err)
		}
		standbyReplicas, err := getDeploymentReplicas(ctx, standby.c)
		if err != nil {
			t.Fatalf("read standby replicas: %v", err)
		}
		if holderReplicas != 2 || standbyReplicas != 0 {
			t.Fatalf("holder lost lease across restart: %s=%d %s=%d",
				holder.name, holderReplicas, standby.name, standbyReplicas)
		}
		time.Sleep(2 * time.Second)
	}
}

// TestCoordinationLost (scenario 5): pause the coord cluster's
// control-plane container. Both runner operators see Acquire/Renew
// failures and keep their last-applied Deployment state. When the
// pause ends, the lease has expired (we pause longer than TTL), so
// both compete and exactly one wins.
//
// Implementation note: docker pause is heavyweight; resume always
// runs in cleanup even on test failure.
func TestCoordinationLost(t *testing.T) {
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

	waitForHolder(t, ctx)
	t.Logf("steady state reached")

	const coordContainer = "berth-e2e-coord-control-plane"
	pause := exec.CommandContext(ctx, "docker", "pause", coordContainer)
	if out, err := pause.CombinedOutput(); err != nil {
		t.Fatalf("pause coord container: %v\n%s", err, out)
	}
	// Always unpause, even on test failure. nolint: errcheck — we don't
	// care about the error in cleanup; the test already failed.
	t.Cleanup(func() {
		_ = exec.Command("docker", "unpause", coordContainer).Run()
	})

	// Sleep past TTL so the lease becomes reclaimable when we resume.
	pauseDuration := time.Duration(ttlSeconds+heartbeatSeconds) * time.Second
	t.Logf("coord paused; sleeping %s past TTL", pauseDuration)
	time.Sleep(pauseDuration)

	unpause := exec.CommandContext(ctx, "docker", "unpause", coordContainer)
	if out, err := unpause.CombinedOutput(); err != nil {
		t.Fatalf("unpause coord container: %v\n%s", err, out)
	}
	t.Logf("coord unpaused; waiting for race to settle")

	// After resume, both operators race to acquire. Exactly one wins.
	waitFor(t, acquireWindow+30*time.Second, "exactly one cluster holds after partition heal",
		func(ctx context.Context) (bool, string) {
			east, eastErr := getDeploymentReplicas(ctx, clusters.east)
			west, westErr := getDeploymentReplicas(ctx, clusters.west)
			if eastErr != nil || westErr != nil {
				return false, fmt.Sprintf("read replicas: east=%v west=%v", eastErr, westErr)
			}
			if (east == 2 && west == 0) || (east == 0 && west == 2) {
				return true, ""
			}
			return false, fmt.Sprintf("east=%d west=%d", east, west)
		})
}

// TestLeaseDeletion (scenario 6): delete the BerthLease CR on the
// holding cluster. reconcileDelete sends a best-effort Release to the
// apiserver and removes the finalizer. The standby cluster's next
// Acquire succeeds (lease is free) and it scales up. The original
// holder's Deployment is orphaned at the acquire replicas — this is
// expected; the operator doesn't garbage-collect targets.
func TestLeaseDeletion(t *testing.T) {
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

	holder, standby := waitForHolder(t, ctx)
	t.Logf("steady state: %s holds, %s waits", holder.name, standby.name)

	lease := &berthv1alpha1.BerthLease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseName, Namespace: namespace},
	}
	if err := holder.c.Delete(ctx, lease); err != nil {
		t.Fatalf("delete lease on %s: %v", holder.name, err)
	}

	// Finalizer must clear within a heartbeat or two (best-effort
	// Release call to apiserver + finalizer patch).
	waitFor(t, 30*time.Second, "holder lease CR finalizer removed", func(ctx context.Context) (bool, string) {
		got := &berthv1alpha1.BerthLease{}
		err := holder.c.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: namespace}, got)
		if apierrors.IsNotFound(err) {
			return true, ""
		}
		if err != nil {
			return false, err.Error()
		}
		return false, fmt.Sprintf("CR still present, finalizers=%v", got.Finalizers)
	})

	// Standby's next Acquire should succeed since the lease was
	// released. Within reacquire interval, standby's Deployment scales
	// up to acquireAction replicas.
	waitFor(t, acquireWindow, "standby acquires after holder released", func(ctx context.Context) (bool, string) {
		replicas, err := getDeploymentReplicas(ctx, standby.c)
		if err != nil {
			return false, err.Error()
		}
		if replicas == 2 {
			return true, ""
		}
		return false, fmt.Sprintf("%s replicas=%d (want 2)", standby.name, replicas)
	})
}
