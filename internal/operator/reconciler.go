package operator

import (
	"context"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BerthLeaseReconciler reconciles BerthLease resources.
type BerthLeaseReconciler struct {
	client.Client
	Log logr.Logger
}

// Reconcile fetches the BerthLease and returns without mutating it.
func (r *BerthLeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var lease berthv1alpha1.BerthLease
	if err := r.Get(ctx, req.NamespacedName, &lease); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.Log.Info("reconciled BerthLease", "namespace", lease.Namespace, "name", lease.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *BerthLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&berthv1alpha1.BerthLease{}).
		Complete(r)
}
