package operator

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestSetConditionStampsObservedGeneration verifies the refactored helper
// stamps ObservedGeneration on the condition and preserves the
// transition-time-only-on-change semantics.
func TestSetConditionStampsObservedGeneration(t *testing.T) {
	t.Parallel()
	status := &berthv1alpha1.BerthLeaseStatus{}

	setCondition(status, ConditionAcquired, metav1.ConditionTrue, "Held", "held", 5)
	c := findCondition(status.Conditions, ConditionAcquired)
	if c == nil {
		t.Fatal("condition not set")
	}
	if c.ObservedGeneration != 5 {
		t.Fatalf("ObservedGeneration = %d, want 5", c.ObservedGeneration)
	}
	first := c.LastTransitionTime

	// Same status, newer generation: ObservedGeneration advances, transition
	// time does not (status unchanged).
	setCondition(status, ConditionAcquired, metav1.ConditionTrue, "Held", "still held", 6)
	c = findCondition(status.Conditions, ConditionAcquired)
	if c.ObservedGeneration != 6 {
		t.Fatalf("ObservedGeneration = %d, want 6", c.ObservedGeneration)
	}
	if !c.LastTransitionTime.Equal(&first) {
		t.Error("LastTransitionTime changed without a status change")
	}

	// Status flip bumps the transition time.
	setCondition(status, ConditionAcquired, metav1.ConditionFalse, "HeldByOther", "lost", 7)
	c = findCondition(status.Conditions, ConditionAcquired)
	if c.LastTransitionTime.Equal(&first) {
		t.Error("LastTransitionTime should change on a status flip")
	}
}

// TestReconcileHeldStampsObservedGeneration verifies a held reconcile records
// the observed generation at the status level and on each condition.
func TestReconcileHeldStampsObservedGeneration(t *testing.T) {
	t.Parallel()
	scheme := newScheme(t)
	lease := newLease(func(l *berthv1alpha1.BerthLease) { l.Generation = 7 })
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&berthv1alpha1.BerthLease{}).
		WithObjects(lease, newDeployment(3)).
		Build()

	now := time.Now()
	r := &BerthLeaseReconciler{
		Client: c,
		Log:    logr.Discard(),
		LeaseClient: &fakeLeaseClient{
			acquireResult: client.AcquireResult{
				Acquired: true, Holder: "cluster-east", FencingToken: 1,
				AcquiredAt: now, ExpiresAt: now.Add(30 * time.Second),
			},
		},
	}

	if _, err := reconcile(t, r); err != nil {
		t.Fatal(err)
	}

	got := &berthv1alpha1.BerthLease{}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "lease-a"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.ObservedGeneration != 7 {
		t.Fatalf("status.observedGeneration = %d, want 7", got.Status.ObservedGeneration)
	}
	for _, ct := range []string{ConditionAcquired, ConditionHeartbeatHealthy} {
		cond := findCondition(got.Status.Conditions, ct)
		if cond == nil {
			t.Fatalf("missing condition %q", ct)
		}
		if cond.ObservedGeneration != 7 {
			t.Errorf("condition %q ObservedGeneration = %d, want 7", ct, cond.ObservedGeneration)
		}
	}
}
