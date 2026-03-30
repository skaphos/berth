package operator

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBerthLeaseReconcilerReconcile(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := berthv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	t.Run("missing object", func(t *testing.T) {
		reconciler := &BerthLeaseReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			Log:    logr.Discard(),
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "missing"},
		})
		if err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
		if result != (ctrl.Result{}) {
			t.Fatalf("result = %+v, want empty result", result)
		}
	})

	t.Run("existing object", func(t *testing.T) {
		lease := &berthv1alpha1.BerthLease{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "lease-a"},
		}

		reconciler := &BerthLeaseReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(lease).Build(),
			Log:    logr.Discard(),
		}

		result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Namespace: "default", Name: "lease-a"},
		})
		if err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
		if result != (ctrl.Result{}) {
			t.Fatalf("result = %+v, want empty result", result)
		}
	})
}
