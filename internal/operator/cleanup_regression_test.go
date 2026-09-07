package operator

import (
	"errors"
	"testing"
	"time"

	"context"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestExpiredLeaseStopsTargetOnAPIError(t *testing.T) {
	l := newLease(func(l *berthv1alpha1.BerthLease) {
		l.Status.LeaseState = StateHeld
		l.Status.CurrentHolder = "cluster-east"
		l.Status.FencingToken = 1
		l.Status.ExpiresAt = &metav1.Time{Time: time.Now().Add(-time.Minute)}
	})
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithStatusSubresource(&berthv1alpha1.BerthLease{}).WithObjects(l, newDeployment(3)).Build()
	r := &BerthLeaseReconciler{ManagedWorkloads: true, Client: c, Log: logr.Discard(), LeaseClient: &fakeLeaseClient{acquireErr: errors.New("central API unavailable")}}
	for range 3 {
		_, _ = reconcile(t, r)
	}
	var target appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, &target); err != nil {
		t.Fatal(err)
	}
	if *target.Spec.Replicas != 0 {
		t.Fatalf("expired holder still requests %d running replicas", *target.Spec.Replicas)
	}
}

func TestDeletionStopsTargetBeforeRelease(t *testing.T) {
	l := newLease(func(l *berthv1alpha1.BerthLease) {
		l.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		l.Status.LeaseState = StateHeld
		l.Status.CurrentHolder = "cluster-east"
		l.Status.FencingToken = 1
	})
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithStatusSubresource(&berthv1alpha1.BerthLease{}).WithObjects(l, newDeployment(3)).Build()
	lc := &fakeLeaseClient{}
	r := &BerthLeaseReconciler{ManagedWorkloads: true, Client: c, Log: logr.Discard(), LeaseClient: lc}
	_, _ = reconcile(t, r)
	var target appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "worker"}, &target); err != nil {
		t.Fatal(err)
	}
	if len(lc.releaseCalls) > 0 && *target.Spec.Replicas != 0 {
		t.Fatal("central ownership released while the target still requests running replicas")
	}
}
