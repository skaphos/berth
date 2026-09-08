package operator

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	berthclient "github.com/skaphos/berth/pkg/client"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authnv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func heldLease() *berthv1alpha1.BerthLease {
	return newLease(func(l *berthv1alpha1.BerthLease) {
		l.Status.LeaseState = StateHeld
		l.Status.CurrentHolder = "cluster-east"
		l.Status.FencingToken = 7
		l.Status.ExpiresAt = timePtr(time.Now().Add(time.Minute))
	})
}
func managedPod(name string) *corev1.Pod {
	return &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name, UID: types.UID(name + "-uid"), Annotations: map[string]string{ManagedLease: "lease-a", ManagedUID: "lease-uid"}, Labels: map[string]string{ManagedUID: "lease-uid"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", UID: "rs-uid"}}, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: SchedulingGate}}, Containers: []corev1.Container{{Name: "worker", Image: "example.test/worker"}}}}
}
func managedRS() *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "rs", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(newDeployment(1), appsv1.SchemeGroupVersion.WithKind("Deployment"))}}}
}
func testClient(t *testing.T, objects ...ctrlclient.Object) ctrlclient.WithWatch {
	t.Helper()
	return fake.NewClientBuilder().WithScheme(newScheme(t)).WithStatusSubresource(&berthv1alpha1.BerthLease{}, &corev1.Pod{}).WithObjects(objects...).Build()
}
func testReconciler(c ctrlclient.Client, lc LeaseClient) *BerthLeaseReconciler {
	return &BerthLeaseReconciler{Client: c, LeaseClient: lc, ManagedWorkloads: true, Log: logr.Discard()}
}
func readLease(t *testing.T, c ctrlclient.Client) *berthv1alpha1.BerthLease {
	t.Helper()
	var l berthv1alpha1.BerthLease
	if err := c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "ns", Name: "lease-a"}, &l); err != nil {
		t.Fatal(err)
	}
	return &l
}

func TestFailedStopRetainsUnhealthyOwnership(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		name := "ownership-lost"
		if deleting {
			name = "deletion"
		}
		t.Run(name, func(t *testing.T) {
			l := heldLease()
			if deleting {
				l.DeletionTimestamp = timePtr(time.Now())
			}
			base := testClient(t, l, newDeployment(3))
			stopErr := errors.New("target update denied")
			fail := true
			c := interceptor.NewClient(base, interceptor.Funcs{Patch: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
				if fail && obj.GetObjectKind().GroupVersionKind().Kind == "Deployment" {
					return stopErr
				}
				return c.Patch(ctx, obj, patch, opts...)
			}})
			lc := &fakeLeaseClient{acquireResult: berthclient.AcquireResult{Holder: "cluster-west"}}
			r := testReconciler(c, lc)
			for range 2 {
				if _, err := reconcile(t, r); !errors.Is(err, stopErr) {
					t.Fatalf("cleanup failure was swallowed: %v", err)
				}
				got := readLease(t, base)
				if got.Status.Workload.Phase != PhaseStopping || got.Status.Workload.Holder != "cluster-east" || got.Status.Workload.Token != 7 || len(got.Finalizers) == 0 || len(lc.releaseCalls) != 0 {
					t.Fatalf("failed cleanup discarded ownership: %+v, release calls: %v", got.Status, lc.releaseCalls)
				}
				unhealthy := false
				for _, condition := range got.Status.Conditions {
					if condition.Type == ConditionHeartbeatHealthy && condition.Status == metav1.ConditionFalse && condition.Reason == "Stopping" {
						unhealthy = true
					}
				}
				if !unhealthy {
					t.Fatalf("failed cleanup reported healthy: %+v", got.Status.Conditions)
				}
			}
			fail = false
			if _, err := reconcile(t, r); err != nil {
				t.Fatal(err)
			}
			if len(lc.releaseCalls) != 1 {
				t.Fatalf("cleanup did not recover: %v", lc.releaseCalls)
			}
		})
	}
}

func TestRegistrationFailureCannotUngatePod(t *testing.T) {
	l := heldLease()
	p := managedPod("worker")
	base := testClient(t, l, p, newDeployment(1), managedRS())
	c := interceptor.NewClient(base, interceptor.Funcs{SubResourceUpdate: func(_ context.Context, _ ctrlclient.Client, _ string, _ ctrlclient.Object, _ ...ctrlclient.SubResourceUpdateOption) error {
		return errors.New("status storage unavailable")
	}})
	r := testReconciler(c, &fakeLeaseClient{})
	if err := r.permitPod(context.Background(), readLease(t, c), p); err == nil {
		t.Fatal("registration failure was swallowed")
	}
	var got corev1.Pod
	if err := base.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), &got); err != nil {
		t.Fatal(err)
	}
	if !hasGate(&got) {
		t.Fatal("Pod ungated without a durable UID record")
	}
}

func TestStoppingConflictCannotRegisterOrUngatePod(t *testing.T) {
	l := heldLease()
	p := managedPod("worker")
	c := testClient(t, l, p, newDeployment(1), managedRS())
	r := testReconciler(c, &fakeLeaseClient{})
	stale := readLease(t, c)
	current := readLease(t, c)
	current.Status.Workload.Phase = PhaseStopping
	if err := c.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := r.permitPod(context.Background(), stale, p); err == nil {
		t.Fatal("stale Active state registered after Stopping")
	}
	var got corev1.Pod
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), &got); err != nil {
		t.Fatal(err)
	}
	if !hasGate(&got) {
		t.Fatal("stale registration ungated Pod")
	}
}

func TestDelayedUngateCannotResurrectDrainedPod(t *testing.T) {
	l := heldLease()
	p := managedPod("worker")
	base := testClient(t, l, p, newDeployment(1), managedRS())
	started, proceed := make(chan struct{}), make(chan struct{})
	var once sync.Once
	c := interceptor.NewClient(base, interceptor.Funcs{Update: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, opts ...ctrlclient.UpdateOption) error {
		if pod, ok := obj.(*corev1.Pod); ok && !hasGate(pod) {
			once.Do(func() { close(started) })
			<-proceed
		}
		return c.Update(ctx, obj, opts...)
	}})
	r := testReconciler(c, &fakeLeaseClient{})
	finished := make(chan error, 1)
	go func() { finished <- r.permitPod(context.Background(), readLease(t, c), p) }()
	<-started
	current := readLease(t, base)
	if len(current.Status.Workload.Pods) != 1 {
		t.Fatal("ungate issued before registration")
	}
	current.Status.Workload.Phase = PhaseStopping
	if err := base.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := r.drainPods(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err := <-finished; err == nil {
		t.Fatal("delayed Update unexpectedly recreated deleted Pod")
	}
	var got corev1.Pod
	if err := base.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), &got); err == nil {
		t.Fatal("drained Pod reappeared")
	}
}

func TestSlowTerminationRetainsTokenAndFinalizer(t *testing.T) {
	l := heldLease()
	l.DeletionTimestamp = timePtr(time.Now())
	p := managedPod("worker")
	p.Spec.SchedulingGates = nil
	p.Finalizers = []string{"test/slow-kubelet"}
	p.Status.Phase = corev1.PodRunning
	l.Status.Workload.Pods = []berthv1alpha1.PermittedPod{{Name: p.Name, UID: string(p.UID)}}
	c := testClient(t, l, p, newDeployment(1))
	lc := &fakeLeaseClient{}
	r := testReconciler(c, lc)
	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	current := readLease(t, c)
	if len(lc.releaseCalls) != 0 || current.Status.Workload.Token != 7 || len(current.Finalizers) == 0 || len(current.Status.Workload.Pods) != 1 {
		t.Fatal("cleanup responsibility lost before termination")
	}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), p); err != nil {
		t.Fatal(err)
	}
	p.Finalizers = nil
	if err := c.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if len(lc.releaseCalls) != 1 || lc.releaseCalls[0].token != 7 {
		t.Fatal("drained workload did not release its recorded token")
	}
}

func TestCronJobCleanupStopsJobsAndPods(t *testing.T) {
	l := heldLease()
	l.Spec.Target = &berthv1alpha1.TargetRef{APIVersion: "batch/v1", Kind: "CronJob", Name: "worker"}
	v := false
	l.Spec.AcquireAction = &berthv1alpha1.LeaseAction{Suspend: &v}
	v2 := true
	l.Spec.ReleaseAction = &berthv1alpha1.LeaseAction{Suspend: &v2}
	l.DeletionTimestamp = timePtr(time.Now())
	cron := &batchv1.CronJob{ObjectMeta: newDeployment(0).ObjectMeta, Spec: batchv1.CronJobSpec{Suspend: &v}}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "job", UID: "job-uid", Labels: map[string]string{ManagedUID: "lease-uid"}, Annotations: map[string]string{ManagedUID: "lease-uid", ManagedLease: "lease-a"}, Finalizers: []string{"test/observe-suspend"}}}
	p := managedPod("worker")
	p.Spec.SchedulingGates = nil
	p.Finalizers = []string{"test/slow"}
	l.Status.Workload.Pods = []berthv1alpha1.PermittedPod{{Name: p.Name, UID: string(p.UID)}}
	c := testClient(t, l, cron, job, p)
	lc := &fakeLeaseClient{}
	r := testReconciler(c, lc)
	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(cron), cron); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(job), job); err != nil {
		t.Fatal(err)
	}
	if !*cron.Spec.Suspend || job.Spec.Suspend == nil || !*job.Spec.Suspend || job.DeletionTimestamp == nil || len(lc.releaseCalls) != 0 {
		t.Fatal("CronJob/active Job cleanup did not precede release")
	}
}

func TestOwnershipStatusFailureCannotActivateTarget(t *testing.T) {
	l := newLease(nil)
	base := testClient(t, l, newDeployment(0))
	c := interceptor.NewClient(base, interceptor.Funcs{SubResourceUpdate: func(_ context.Context, _ ctrlclient.Client, _ string, _ ctrlclient.Object, _ ...ctrlclient.SubResourceUpdateOption) error {
		return errors.New("status unavailable")
	}})
	lc := &fakeLeaseClient{acquireResult: berthclient.AcquireResult{Acquired: true, Holder: "cluster-east", FencingToken: 1, ExpiresAt: time.Now().Add(time.Minute)}}
	if _, err := reconcile(t, testReconciler(c, lc)); err == nil {
		t.Fatal("status failure swallowed")
	}
	var dep appsv1.Deployment
	if err := base.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "ns", Name: "worker"}, &dep); err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != 0 {
		t.Fatal("target activated before ownership persisted")
	}
}

func requestFor(t *testing.T, obj, old runtime.Object, operation admissionv1.Operation, user string) admission.Request {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	var previous []byte
	if old != nil {
		previous, err = json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
	}
	return admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Operation: operation, Namespace: "ns", Kind: metav1.GroupVersionKind{Version: "v1", Kind: "Pod"}, Object: runtime.RawExtension{Raw: raw}, OldObject: runtime.RawExtension{Raw: previous}, UserInfo: authnv1.UserInfo{Username: user}}}
}
func TestFinalAdmissionRejectsLateMutationAndPrebinding(t *testing.T) {
	l := heldLease()
	c := testClient(t, l, newDeployment(1), managedRS())
	h := &WorkloadAdmission{Client: c, OperatorUser: "operator"}
	cases := []struct {
		name   string
		change func(*corev1.Pod)
	}{
		{"all Berth metadata and gate stripped", func(p *corev1.Pod) { p.Annotations = nil; p.Labels = nil; p.Spec.SchedulingGates = nil }},
		{"prebound", func(p *corev1.Pod) { p.Spec.NodeName = "worker-node" }},
		{"gate removed", func(p *corev1.Pod) { p.Spec.SchedulingGates = nil }},
		{"lease UID replaced", func(p *corev1.Pod) { p.Annotations[ManagedUID] = "new-lease-uid" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := managedPod("worker")
			tc.change(p)
			if h.Handle(context.Background(), requestFor(t, p, nil, admissionv1.Create, "")).Allowed {
				t.Fatal("unsafe Pod accepted")
			}
		})
	}
	if !h.Handle(context.Background(), requestFor(t, managedPod("worker"), nil, admissionv1.Create, "")).Allowed {
		t.Fatalf("legitimate gated Pod denied: %+v", h.Handle(context.Background(), requestFor(t, managedPod("worker"), nil, admissionv1.Create, "")).Result)
	}
}
func TestUngateRequiresRegisteredUIDAndOperator(t *testing.T) {
	l := heldLease()
	p := managedPod("worker")
	c := testClient(t, l, newDeployment(1), managedRS())
	h := &WorkloadAdmission{Client: c, OperatorUser: "operator"}
	next := p.DeepCopy()
	next.Spec.SchedulingGates = nil
	for _, user := range []string{"other", "operator"} {
		if h.Handle(context.Background(), requestFor(t, next, p, admissionv1.Update, user)).Allowed {
			t.Fatal("unregistered Pod was ungated")
		}
	}
	current := readLease(t, c)
	current.Status.Workload.Pods = []berthv1alpha1.PermittedPod{{Name: p.Name, UID: string(p.UID)}}
	if err := c.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if !h.Handle(context.Background(), requestFor(t, next, p, admissionv1.Update, "operator")).Allowed {
		t.Fatalf("registered operator ungate rejected: %+v", h.Handle(context.Background(), requestFor(t, next, p, admissionv1.Update, "operator")).Result)
	}
	next.UID = "replacement"
	if h.Handle(context.Background(), requestFor(t, next, p, admissionv1.Update, "operator")).Allowed {
		t.Fatal("Pod name reuse inherited old UID permit")
	}
}
func TestLatePodAndJobAfterLeaseDeletionAreRemoved(t *testing.T) {
	p := managedPod("late")
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "late-job", UID: "late-job-uid", Annotations: map[string]string{ManagedLease: "lease-a", ManagedUID: "lease-uid"}}}
	c := testClient(t, p, job)
	r := testReconciler(c, &fakeLeaseClient{})
	if _, err := (&managedPodReconciler{r}).Reconcile(context.Background(), ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(p)}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&managedJobReconciler{r}).Reconcile(context.Background(), ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(job)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), &corev1.Pod{}); err == nil {
		t.Fatal("late Pod retained")
	}
}
func TestMissingParentNeverDowngradesToUnmanaged(t *testing.T) {
	c := testClient(t, heldLease())
	p := managedPod("worker")
	p.Annotations = nil
	p.Labels = nil
	if _, err := expectedBinding(context.Background(), c, p); err == nil {
		t.Fatal("unresolved ReplicaSet treated as unmanaged")
	}
}
func TestManagedTemplatesAllKinds(t *testing.T) {
	b := binding{name: "lease-a", uid: "lease-uid"}
	for _, kind := range []string{"Pod", "Deployment", "StatefulSet", "ReplicaSet", "CronJob", "Job"} {
		t.Run(kind, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": kind, "metadata": map[string]interface{}{"name": "worker", "namespace": "ns"}}}
			if kind != "Pod" {
				if err := unstructured.SetNestedMap(obj.Object, map[string]interface{}{"spec": map[string]interface{}{}}, podTemplatePath(kind)...); err != nil {
					t.Fatal(err)
				}
			}
			if err := stampBinding(obj, b); err != nil {
				t.Fatal(err)
			}
			if err := validateManagedObject(obj, nil, b, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestLeaseOnlyDoesNotRequireAdmission(t *testing.T) {
	l := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Spec.Target = nil
		l.Spec.AcquireAction = nil
		l.Spec.ReleaseAction = nil
		l.Status.Workload = nil
	})
	c := testClient(t, l)
	lc := &fakeLeaseClient{acquireResult: berthclient.AcquireResult{Acquired: true, Holder: "cluster-east", FencingToken: 1, ExpiresAt: time.Now().Add(time.Minute)}}
	r := testReconciler(c, lc)
	r.ManagedWorkloads = false
	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}
	if readLease(t, c).Status.LeaseState != StateHeld {
		t.Fatal("lease-only acquisition failed")
	}
}

func TestStoppingFencesDelayedTargetActivation(t *testing.T) {
	l := heldLease()
	base := testClient(t, l, newDeployment(0))
	started, proceed := make(chan struct{}), make(chan struct{})
	c := interceptor.NewClient(base, interceptor.Funcs{Patch: func(ctx context.Context, c ctrlclient.WithWatch, obj ctrlclient.Object, patch ctrlclient.Patch, opts ...ctrlclient.PatchOption) error {
		if u, ok := obj.(*unstructured.Unstructured); ok {
			n, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
			if n > 0 {
				close(started)
				<-proceed
			}
		}
		return c.Patch(ctx, obj, patch, opts...)
	}})
	result := make(chan error, 1)
	go func() {
		result <- applyActionForUID(context.Background(), c, "ns", l.Spec.Target, l.Spec.AcquireAction, "target-uid")
	}()
	<-started
	r := testReconciler(base, &fakeLeaseClient{})
	current := readLease(t, base)
	current.Status.Workload.Phase = PhaseStopping
	if err := base.Status().Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if err := r.stopTarget(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	if err := <-result; err == nil {
		t.Fatal("delayed activation overwrote the stop fence")
	}
}

type blockingAcquireClient struct {
	fakeLeaseClient
	deadline time.Time
}

func (b *blockingAcquireClient) Acquire(ctx context.Context, _, _, _ string, _ time.Duration) (berthclient.AcquireResult, error) {
	b.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return berthclient.AcquireResult{}, ctx.Err()
}
func TestAcquireRPCBoundedByRemainingOwnership(t *testing.T) {
	l := heldLease()
	l.Status.Workload.Deadline = timePtr(time.Now().Add(100 * time.Millisecond))
	c := testClient(t, l, newDeployment(1))
	b := &blockingAcquireClient{}
	started := time.Now()
	_, _ = reconcile(t, testReconciler(c, b))
	if time.Since(started) > time.Second {
		t.Fatal("acquire waited beyond local cleanup deadline")
	}
	var dep appsv1.Deployment
	if err := c.Get(context.Background(), ctrlclient.ObjectKey{Namespace: "ns", Name: "worker"}, &dep); err != nil {
		t.Fatal(err)
	}
	if *dep.Spec.Replicas != 0 {
		t.Fatal("deadline did not trigger cleanup")
	}
}

func TestRecreatedLeaseCannotAuthorizeOldPod(t *testing.T) {
	l := heldLease()
	l.UID = "replacement-lease"
	p := managedPod("old")
	c := testClient(t, l, p, newDeployment(0), managedRS())
	r := testReconciler(c, &fakeLeaseClient{})
	if _, err := (&managedPodReconciler{r}).Reconcile(context.Background(), ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(p)}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), ctrlclient.ObjectKeyFromObject(p), &corev1.Pod{}); err == nil {
		t.Fatal("old Pod survived lease UID replacement")
	}
}

func TestStoppingAllowsControllerBookkeeping(t *testing.T) {
	l := heldLease()
	l.Status.Workload.Phase = PhaseStopping
	c := testClient(t, l, newDeployment(0))
	h := &WorkloadAdmission{Client: c, OperatorUser: "operator"}
	old := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": map[string]interface{}{"name": "rs", "namespace": "ns", "uid": "rs-uid"}, "spec": map[string]interface{}{"replicas": int64(1), "template": map[string]interface{}{"spec": map[string]interface{}{}}}}}
	if err := stampBinding(old, binding{name: l.Name, uid: string(l.UID)}); err != nil {
		t.Fatal(err)
	}
	next := old.DeepCopy()
	a := next.GetAnnotations()
	a["deployment.kubernetes.io/desired-replicas"] = "0"
	next.SetAnnotations(a)
	req := requestFor(t, next, old, admissionv1.Update, "deployment-controller")
	req.Kind = metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	if res := h.Handle(context.Background(), req); !res.Allowed {
		t.Fatalf("scale-down bookkeeping denied: %+v", res.Result)
	}
	if err := unstructured.SetNestedField(next.Object, int64(2), "spec", "replicas"); err != nil {
		t.Fatal(err)
	}
	req = requestFor(t, next, old, admissionv1.Update, "deployment-controller")
	req.Kind = metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	if h.Handle(context.Background(), req).Allowed {
		t.Fatal("actual activation increase accepted while stopping")
	}
}

func TestBindingRequiresCurrentPodUIDAndRemovedGate(t *testing.T) {
	p := managedPod("worker")
	c := testClient(t, p)
	h := &WorkloadAdmission{Client: c}
	req := admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{Namespace: p.Namespace, Name: p.Name, Operation: admissionv1.Create, SubResource: "binding"}}
	makeBinding := func(uid types.UID) {
		raw, _ := json.Marshal(&corev1.Binding{ObjectMeta: metav1.ObjectMeta{Name: p.Name, UID: uid}})
		req.Object = runtime.RawExtension{Raw: raw}
	}
	makeBinding(p.UID)
	if h.Handle(context.Background(), req).Allowed {
		t.Fatal("gated Pod was bindable")
	}
	p.Spec.SchedulingGates = nil
	if err := c.Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []types.UID{"", "old-uid"} {
		makeBinding(uid)
		if h.Handle(context.Background(), req).Allowed {
			t.Fatal("binding omitted or reused another UID")
		}
	}
	makeBinding(p.UID)
	if !h.Handle(context.Background(), req).Allowed {
		t.Fatal("current ungated UID could not bind")
	}
}

func TestAdmissionRejectsUnregisteredPodAdoption(t *testing.T) {
	l := heldLease()
	c := testClient(t, l, newDeployment(1), managedRS())
	h := &WorkloadAdmission{Client: c, OperatorUser: "operator"}
	old := managedPod("orphan")
	old.Annotations, old.Labels, old.OwnerReferences = nil, nil, nil
	old.Spec.SchedulingGates = nil
	old.Spec.NodeName = "running-node"
	for _, name := range []string{"rs", "missing-parent"} {
		t.Run(name, func(t *testing.T) {
			next := old.DeepCopy()
			next.OwnerReferences = managedPod("orphan").OwnerReferences
			next.OwnerReferences[0].Name = name
			if h.Handle(context.Background(), requestFor(t, next, old, admissionv1.Update, "controller")).Allowed {
				t.Fatal("unregistered running Pod was adopted without admission at birth")
			}
		})
	}
	old.OwnerReferences = managedPod("orphan").OwnerReferences
	next := old.DeepCopy()
	next.Finalizers = []string{"example.test/cleanup"}
	if response := h.Handle(context.Background(), requestFor(t, next, old, admissionv1.Update, "controller")); !response.Allowed {
		t.Fatalf("legacy Pod cleanup metadata denied: %v", response.Result)
	}
}
