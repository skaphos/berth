package operator

import (
	"context"
	"time"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// managedPodReconciler accounts for Pods arriving after the last lease
// reconcile, including inert late creates after the BerthLease was deleted.
type managedPodReconciler struct{ r *BerthLeaseReconciler }

func (p *managedPodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var pod corev1.Pod
	if err := p.r.Get(ctx, req.NamespacedName, &pod); err != nil {
		return ctrl.Result{}, ctrlclient.IgnoreNotFound(err)
	}
	uid := pod.Annotations[ManagedUID]
	if uid == "" {
		return ctrl.Result{}, nil
	}
	var l berthv1alpha1.BerthLease
	err := p.r.Get(ctx, ctrlclient.ObjectKey{Namespace: pod.Namespace, Name: pod.Annotations[ManagedLease]}, &l)
	if apierrors.IsNotFound(err) || err == nil && string(l.UID) != uid {
		return ctrl.Result{}, deleteUID(ctx, p.r.Client, &pod)
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if !l.DeletionTimestamp.IsZero() || l.Status.Workload != nil && l.Status.Workload.Phase == PhaseStopping {
		return ctrl.Result{RequeueAfter: time.Second}, deleteUID(ctx, p.r.Client, &pod)
	}
	if !activeAt(&l, time.Now()) {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: time.Second}, p.r.permitPod(ctx, &l, &pod)
}

type managedJobReconciler struct{ r *BerthLeaseReconciler }

func (j *managedJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var job batchv1.Job
	if err := j.r.Get(ctx, req.NamespacedName, &job); err != nil {
		return ctrl.Result{}, ctrlclient.IgnoreNotFound(err)
	}
	uid := job.Annotations[ManagedUID]
	if uid == "" {
		return ctrl.Result{}, nil
	}
	var l berthv1alpha1.BerthLease
	err := j.r.Get(ctx, ctrlclient.ObjectKey{Namespace: job.Namespace, Name: job.Annotations[ManagedLease]}, &l)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if apierrors.IsNotFound(err) || string(l.UID) != uid || !l.DeletionTimestamp.IsZero() || l.Status.Workload != nil && l.Status.Workload.Phase == PhaseStopping {
		if job.Spec.Suspend == nil || !*job.Spec.Suspend {
			v := true
			job.Spec.Suspend = &v
			if err := j.r.Update(ctx, &job); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: time.Second}, deleteUID(ctx, j.r.Client, &job)
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

func (r *BerthLeaseReconciler) setupManagedControllers(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).Named("managed-pods").For(&corev1.Pod{}).Complete(&managedPodReconciler{r: r}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).Named("managed-jobs").For(&batchv1.Job{}).Complete(&managedJobReconciler{r: r})
}
