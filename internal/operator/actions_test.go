package operator

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/pkg/client"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func ptr[T any](v T) *T { return &v }

// newCountingClient wraps a fake client and counts Update calls against
// unstructured targets (i.e. applyAction's writes). Lease status writes go
// through Status().Update and finalizer writes use a typed *BerthLease, so
// neither is counted.
func newCountingClient(t *testing.T, scheme *runtime.Scheme, targetUpdates *int, objs ...ctrlclient.Object) ctrlclient.WithWatch {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(objs...).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cl ctrlclient.WithWatch, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
				if _, ok := obj.(*unstructured.Unstructured); ok {
					*targetUpdates++
				}
				return cl.Update(ctx, obj, opts...)
			},
		}).
		Build()
}

func newCronJob(name string, suspend *bool) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Spec:       batchv1.CronJobSpec{Suspend: suspend},
	}
}

func deploymentTarget() *berthv1alpha1.TargetRef {
	return &berthv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "worker"}
}

func cronJobTarget(name string) *berthv1alpha1.TargetRef {
	return &berthv1alpha1.TargetRef{APIVersion: "batch/v1", Kind: "CronJob", Name: name}
}

func TestApplyActionSkipsUpdateWhenScaleAlreadyAtDesired(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	c := newCountingClient(t, scheme, &n, newDeployment(3))
	err := applyAction(context.Background(), c, "ns", deploymentTarget(),
		&berthv1alpha1.LeaseAction{Scale: &berthv1alpha1.ScaleAction{Replicas: 3}})
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if n != 0 {
		t.Fatalf("target Update count = %d, want 0 (already at desired replicas)", n)
	}
}

func TestApplyActionUpdatesWhenScaleDiffers(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	c := newCountingClient(t, scheme, &n, newDeployment(0))
	err := applyAction(context.Background(), c, "ns", deploymentTarget(),
		&berthv1alpha1.LeaseAction{Scale: &berthv1alpha1.ScaleAction{Replicas: 3}})
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if n != 1 {
		t.Fatalf("target Update count = %d, want 1 (replicas differ)", n)
	}
	got := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 3 {
		t.Fatalf("replicas = %v, want 3", got.Spec.Replicas)
	}
}

func TestApplyActionSkipsUpdateWhenAlreadySuspended(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	c := newCountingClient(t, scheme, &n, newCronJob("cron", ptr(true)))
	err := applyAction(context.Background(), c, "ns", cronJobTarget("cron"),
		&berthv1alpha1.LeaseAction{Suspend: ptr(true)})
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if n != 0 {
		t.Fatalf("target Update count = %d, want 0 (already suspended)", n)
	}
}

func TestApplyActionUpdatesWhenSuspendDiffers(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	c := newCountingClient(t, scheme, &n, newCronJob("cron", ptr(false)))
	err := applyAction(context.Background(), c, "ns", cronJobTarget("cron"),
		&berthv1alpha1.LeaseAction{Suspend: ptr(true)})
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if n != 1 {
		t.Fatalf("target Update count = %d, want 1 (suspend differs)", n)
	}
	got := &batchv1.CronJob{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "cron"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Suspend == nil || !*got.Spec.Suspend {
		t.Fatalf("suspend = %v, want true", got.Spec.Suspend)
	}
}

// TestApplyActionSuspendAbsentConvergesToSingleWrite is the core scalability
// assertion: a target whose managed field is absent gets exactly one write to
// set the explicit value, then steady-state heartbeats issue no further writes.
func TestApplyActionSuspendAbsentConvergesToSingleWrite(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	c := newCountingClient(t, scheme, &n, newCronJob("cron", nil))
	act := &berthv1alpha1.LeaseAction{Suspend: ptr(false)}

	if err := applyAction(context.Background(), c, "ns", cronJobTarget("cron"), act); err != nil {
		t.Fatalf("first applyAction: %v", err)
	}
	if n != 1 {
		t.Fatalf("first apply Update count = %d, want 1 (set explicit value)", n)
	}
	n = 0
	if err := applyAction(context.Background(), c, "ns", cronJobTarget("cron"), act); err != nil {
		t.Fatalf("second applyAction: %v", err)
	}
	if n != 0 {
		t.Fatalf("steady-state apply Update count = %d, want 0", n)
	}
}

// TestReconcileHeldSkipsTargetUpdateWhenAlreadyScaled guards the per-heartbeat
// reconcile path: a held lease whose target already matches the acquire action
// must not write the target on every heartbeat.
func TestReconcileHeldSkipsTargetUpdateWhenAlreadyScaled(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	var n int
	lease := newLease(nil) // AcquireAction scales to 3; finalizer already present
	c := newCountingClient(t, scheme, &n, lease, newDeployment(3))

	now := time.Now()
	r := &BerthLeaseReconciler{
		Client: c,
		Log:    logr.Discard(),
		LeaseClient: &fakeLeaseClient{
			acquireResult: client.AcquireResult{
				Acquired:     true,
				Holder:       "cluster-east",
				FencingToken: 1,
				AcquiredAt:   now,
				ExpiresAt:    now.Add(30 * time.Second),
			},
		},
	}

	res, err := reconcile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("held heartbeat issued %d target writes, want 0 (already scaled)", n)
	}
	if res.RequeueAfter != 10*time.Second {
		t.Fatalf("RequeueAfter = %s, want 10s (heartbeat)", res.RequeueAfter)
	}
	got := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.LeaseState != StateHeld {
		t.Fatalf("leaseState = %q, want %q", got.Status.LeaseState, StateHeld)
	}
}
