package operator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/pkg/client"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := berthv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeLeaseClient struct {
	mu sync.Mutex

	acquireCalls []acquireCall
	releaseCalls []releaseCall

	acquireResult client.AcquireResult
	acquireErr    error
	releaseErr    error
}

type acquireCall struct {
	namespace, name, holder string
	ttl                     time.Duration
}

type releaseCall struct {
	namespace, name, holder string
	token                   int32
}

func (f *fakeLeaseClient) Acquire(_ context.Context, ns, name, holder string, ttl time.Duration) (client.AcquireResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquireCalls = append(f.acquireCalls, acquireCall{ns, name, holder, ttl})
	if f.acquireErr != nil {
		return client.AcquireResult{}, f.acquireErr
	}
	return f.acquireResult, nil
}

func (f *fakeLeaseClient) Renew(_ context.Context, _, _, _ string, _ int32, _ time.Duration) (client.AcquireResult, error) {
	return client.AcquireResult{}, errors.New("Renew not used by reconciler")
}

func (f *fakeLeaseClient) Release(_ context.Context, ns, name, holder string, token int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls = append(f.releaseCalls, releaseCall{ns, name, holder, token})
	return f.releaseErr
}

func newLease(modifier func(*berthv1alpha1.BerthLease)) *berthv1alpha1.BerthLease {
	replicas := int32(3)
	zero := int32(0)
	l := &berthv1alpha1.BerthLease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "ns",
			Name:       "lease-a",
			Finalizers: []string{FinalizerName},
		},
		Spec: berthv1alpha1.BerthLeaseSpec{
			LeaseName:                "shared",
			HolderIdentity:           "cluster-east",
			TTLSeconds:               30,
			HeartbeatIntervalSeconds: 10,
			Semantics:                "at-most-once",
			Target:                   &berthv1alpha1.TargetRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "worker"},
			AcquireAction:            &berthv1alpha1.LeaseAction{Scale: &berthv1alpha1.ScaleAction{Replicas: replicas}},
			ReleaseAction:            &berthv1alpha1.LeaseAction{Scale: &berthv1alpha1.ScaleAction{Replicas: zero}},
		},
	}
	if modifier != nil {
		modifier(l)
	}
	return l
}

func newDeployment(replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "worker"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
}

func reconcile(t *testing.T, r *BerthLeaseReconciler) (ctrl.Result, error) {
	t.Helper()
	return r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "lease-a"}})
}

func TestReconcileMissingObject(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	r := &BerthLeaseReconciler{
		Client:      fake.NewClientBuilder().WithScheme(scheme).Build(),
		Log:         logr.Discard(),
		LeaseClient: &fakeLeaseClient{},
	}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "ns", Name: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if res != (ctrl.Result{}) {
		t.Fatalf("result = %+v, want empty", res)
	}
}

func TestReconcileAddsFinalizerOnFirstObservation(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) { l.Finalizers = nil })
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&berthv1alpha1.BerthLease{}).WithObjects(lease).Build()
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: &fakeLeaseClient{}}

	res, err := reconcile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Requeue {
		t.Fatal("expected immediate requeue after adding finalizer")
	}
	got := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, got); err != nil {
		t.Fatal(err)
	}
	if len(got.Finalizers) != 1 || got.Finalizers[0] != FinalizerName {
		t.Fatalf("finalizers = %v, want [%s]", got.Finalizers, FinalizerName)
	}
}

func TestReconcileHeldScalesDeploymentUpAndWritesStatus(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil)
	dep := newDeployment(0)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	now := time.Now()
	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     true,
			Holder:       "cluster-east",
			FencingToken: 1,
			AcquiredAt:   now,
			ExpiresAt:    now.Add(30 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	res, err := reconcile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != 10*time.Second {
		t.Fatalf("RequeueAfter = %v, want 10s (heartbeat)", res.RequeueAfter)
	}

	got := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 3 {
		t.Fatalf("replicas = %v, want 3", got.Spec.Replicas)
	}

	updated := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.LeaseState != StateHeld {
		t.Fatalf("LeaseState = %q, want %q", updated.Status.LeaseState, StateHeld)
	}
	if updated.Status.FencingToken != 1 {
		t.Fatalf("FencingToken = %d, want 1", updated.Status.FencingToken)
	}
	if updated.Status.CurrentHolder != "cluster-east" {
		t.Fatalf("CurrentHolder = %q, want cluster-east", updated.Status.CurrentHolder)
	}
	if got := findCondition(updated.Status.Conditions, ConditionAcquired); got == nil || got.Status != metav1.ConditionTrue {
		t.Fatalf("Acquired condition = %v, want True", got)
	}
}

func TestReconcileNotHeldScalesDeploymentToZero(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil)
	dep := newDeployment(3)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     false,
			Holder:       "cluster-west",
			FencingToken: 5,
			ExpiresAt:    time.Now().Add(20 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	got := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 0 {
		t.Fatalf("replicas = %v, want 0", got.Spec.Replicas)
	}

	updated := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.LeaseState != StateWaiting {
		t.Fatalf("LeaseState = %q, want %q", updated.Status.LeaseState, StateWaiting)
	}
	if updated.Status.CurrentHolder != "cluster-west" {
		t.Fatalf("CurrentHolder = %q, want cluster-west", updated.Status.CurrentHolder)
	}
}

func TestReconcileDeletionReleasesAndRemovesFinalizer(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	deletedAt := metav1.Now()
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.DeletionTimestamp = &deletedAt
		l.Status.LeaseState = StateHeld
		l.Status.CurrentHolder = "cluster-east"
		l.Status.FencingToken = 7
	})
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease).
		Build()

	leaseClient := &fakeLeaseClient{}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	if len(leaseClient.releaseCalls) != 1 {
		t.Fatalf("release calls = %d, want 1", len(leaseClient.releaseCalls))
	}
	got := leaseClient.releaseCalls[0]
	if got.holder != "cluster-east" || got.token != 7 || got.namespace != "ns" || got.name != "shared" {
		t.Fatalf("release call = %+v, want ns/shared cluster-east token=7", got)
	}

	// Lease is now gone (finalizer removal allowed GC in the fake client).
	updated := &berthv1alpha1.BerthLease{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, updated)
	if err == nil {
		t.Fatalf("expected lease to be gone, still present with finalizers=%v", updated.Finalizers)
	}
}

func TestReconcileDeletionWithNoTokenSkipsRelease(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	deletedAt := metav1.Now()
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.DeletionTimestamp = &deletedAt
		// no status — never reconciled while held
	})
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease).
		Build()

	leaseClient := &fakeLeaseClient{}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if len(leaseClient.releaseCalls) != 0 {
		t.Fatalf("release calls = %d, want 0", len(leaseClient.releaseCalls))
	}
}

func TestReconcileInvalidSpecDoesNotCallAcquire(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Spec.HeartbeatIntervalSeconds = 30 // == TTL, invalid
	})
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease).
		Build()

	leaseClient := &fakeLeaseClient{}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if len(leaseClient.acquireCalls) != 0 {
		t.Fatalf("acquire calls = %d, want 0", len(leaseClient.acquireCalls))
	}
}

func TestReconcileTargetGoneIsNotAnError(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil) // declares Deployment ns/worker but it doesn't exist
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     true,
			Holder:       "cluster-east",
			FencingToken: 1,
			ExpiresAt:    time.Now().Add(30 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatalf("missing target should not error, got %v", err)
	}
}

func TestReconcileAcquireErrorRequeues(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease).
		Build()

	leaseClient := &fakeLeaseClient{acquireErr: errors.New("connection refused")}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	res, err := reconcile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != defaultRequeueOnFailure {
		t.Fatalf("RequeueAfter = %v, want %v", res.RequeueAfter, defaultRequeueOnFailure)
	}
}

// TestReconcileAcquireErrorDoesNotMutateDeployment guards against a class of
// failover bug where an unreachable API server would cause the operator to
// mistakenly scale a Deployment based on stale status. When Acquire fails,
// the operator must leave the workload alone and retry.
func TestReconcileAcquireErrorDoesNotMutateDeployment(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil)
	dep := newDeployment(3)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{acquireErr: errors.New("connection refused")}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	got := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 3 {
		t.Fatalf("replicas = %v, want 3 (Deployment must not be mutated when API server is unreachable)", got.Spec.Replicas)
	}
}

// TestReconcileLostLeaseScalesDownAndClearsToken simulates the failover
// scenario where this cluster previously held the lease, lost connectivity
// long enough for the other cluster to reclaim, and is now reconciling
// again. The reconciler must:
//
//   - Apply releaseAction (scale Deployment to 0).
//   - Update status to reflect the new holder.
//   - Clear the stale fencing token so the deletion path won't issue a
//     Release with a token we no longer hold.
func TestReconcileLostLeaseScalesDownAndClearsToken(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Status.LeaseState = StateHeld
		l.Status.CurrentHolder = "cluster-east"
		l.Status.FencingToken = 1
	})
	dep := newDeployment(3) // was running because we held the lease
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     false,
			Holder:       "cluster-west",
			FencingToken: 2,
			ExpiresAt:    time.Now().Add(20 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{
		Client:          c,
		Log:             logr.Discard(),
		LeaseClient:     leaseClient,
		ClusterIdentity: "cluster-east",
	}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	gotDep := &appsv1.Deployment{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, gotDep); err != nil {
		t.Fatal(err)
	}
	if gotDep.Spec.Replicas == nil || *gotDep.Spec.Replicas != 0 {
		t.Fatalf("replicas = %v, want 0 after losing the lease", gotDep.Spec.Replicas)
	}

	updated := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.LeaseState != StateWaiting {
		t.Fatalf("LeaseState = %q, want %q", updated.Status.LeaseState, StateWaiting)
	}
	if updated.Status.CurrentHolder != "cluster-west" {
		t.Fatalf("CurrentHolder = %q, want cluster-west", updated.Status.CurrentHolder)
	}
	if updated.Status.FencingToken != 0 {
		t.Fatalf("FencingToken = %d, want 0 (must be cleared after losing lease)", updated.Status.FencingToken)
	}
}

func TestReacquireIntervalCaps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		heartbeat time.Duration
		ttl       time.Duration
		want      time.Duration
	}{
		{name: "heartbeat below ttl/3", heartbeat: 5 * time.Second, ttl: 30 * time.Second, want: 5 * time.Second},
		{name: "heartbeat equal to ttl/3", heartbeat: 10 * time.Second, ttl: 30 * time.Second, want: 10 * time.Second},
		{name: "heartbeat above ttl/3", heartbeat: 20 * time.Second, ttl: 30 * time.Second, want: 10 * time.Second},
		{name: "heartbeat unset falls back to ttl/3", heartbeat: 0, ttl: 30 * time.Second, want: 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reacquireInterval(tc.heartbeat, tc.ttl); got != tc.want {
				t.Fatalf("reacquireInterval(%v, %v) = %v, want %v", tc.heartbeat, tc.ttl, got, tc.want)
			}
		})
	}
}

// TestReconcileNotHeldRequeueIsBounded asserts the standby cadence so that
// the cross-cluster failover RTO stays bounded by ttl. With ttl=30s and
// heartbeat=10s, the standby tries to reacquire every 10s — so the worst
// case is ~ttl + heartbeat (i.e., the full TTL elapses, then up to one
// reacquire interval before this cluster wins).
func TestReconcileNotHeldRequeueIsBounded(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(nil) // ttl=30s, heartbeat=10s
	dep := newDeployment(0)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired: false, Holder: "cluster-west", FencingToken: 2,
			ExpiresAt: time.Now().Add(30 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	res, err := reconcile(t, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter <= 0 || res.RequeueAfter > 10*time.Second {
		t.Fatalf("RequeueAfter = %v, want > 0 and ≤ 10s (heartbeat-bounded)", res.RequeueAfter)
	}
}

func TestReconcileClusterIdentityOverridesSpecHolder(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Spec.HolderIdentity = "spec-default"
	})
	dep := newDeployment(0)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     true,
			Holder:       "cluster-east",
			FencingToken: 1,
			ExpiresAt:    time.Now().Add(30 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{
		Client:          c,
		Log:             logr.Discard(),
		LeaseClient:     leaseClient,
		ClusterIdentity: "cluster-east",
	}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if len(leaseClient.acquireCalls) != 1 {
		t.Fatalf("acquire calls = %d, want 1", len(leaseClient.acquireCalls))
	}
	if got := leaseClient.acquireCalls[0].holder; got != "cluster-east" {
		t.Fatalf("acquire holder = %q, want %q (ClusterIdentity must override spec.HolderIdentity)", got, "cluster-east")
	}
}

func TestReconcileFallsBackToSpecHolderWhenClusterIdentityUnset(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Spec.HolderIdentity = "from-spec"
	})
	dep := newDeployment(0)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, dep).
		Build()

	leaseClient := &fakeLeaseClient{
		acquireResult: client.AcquireResult{
			Acquired:     true,
			Holder:       "from-spec",
			FencingToken: 1,
			ExpiresAt:    time.Now().Add(30 * time.Second),
		},
	}
	r := &BerthLeaseReconciler{Client: c, Log: logr.Discard(), LeaseClient: leaseClient}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if got := leaseClient.acquireCalls[0].holder; got != "from-spec" {
		t.Fatalf("acquire holder = %q, want %q (should use spec.HolderIdentity)", got, "from-spec")
	}
}

func TestSetupWithManagerRequiresLeaseClient(t *testing.T) {
	t.Parallel()
	r := &BerthLeaseReconciler{Log: logr.Discard()}
	if err := r.SetupWithManager(nil); err == nil {
		t.Fatal("expected error when LeaseClient is nil")
	}
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// Compile-time assertion that the production client satisfies the interface
// the reconciler expects, so we don't drift.
var _ LeaseClient = (*client.Client)(nil)
