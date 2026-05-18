package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// FinalizerName is the finalizer applied to BerthLease objects so the
// reconciler can release the lease before the resource is garbage-collected.
const FinalizerName = "berth.skaphos.io/lease-release"

// LeaseState values written to BerthLease.status.leaseState.
const (
	StateHeld     = "held"
	StateWaiting  = "waiting"
	StateReleased = "released"
)

// Condition types written to BerthLease.status.conditions.
const (
	ConditionAcquired         = "Acquired"
	ConditionHeartbeatHealthy = "HeartbeatHealthy"
)

// defaultRequeueOnFailure is used when a transient error occurs; the
// controller manager applies its own backoff on top of this.
const defaultRequeueOnFailure = 10 * time.Second

// BerthLeaseReconciler reconciles BerthLease resources by holding (or
// renewing) a lease against the central API server and applying the
// configured workload actions in response to lease state.
type BerthLeaseReconciler struct {
	ctrlclient.Client

	// Log is the reconciler's logger.
	Log logr.Logger

	// LeaseClient is the central API server client. Required.
	LeaseClient LeaseClient

	// ClusterIdentity, when non-empty, is used as the holder identity for
	// every Acquire / Release call, overriding spec.HolderIdentity on the
	// BerthLease. Set this to a cluster-distinct value (typically via the
	// operator's --cluster-id flag) to enable the cross-cluster singleton
	// pattern: the same BerthLease applied to multiple clusters competes
	// for the lease rather than co-renewing under a shared identity.
	//
	// When empty, the reconciler falls back to spec.HolderIdentity. That
	// path supports the original use case where an external client manages
	// its own holder identity directly against the Berth API server.
	ClusterIdentity string
}

// holderFor returns the holder identity the reconciler should use for a
// given lease, applying the ClusterIdentity override when configured.
func (r *BerthLeaseReconciler) holderFor(lease *berthv1alpha1.BerthLease) string {
	if r.ClusterIdentity != "" {
		return r.ClusterIdentity
	}
	return lease.Spec.HolderIdentity
}

// Reconcile implements [reconcile.Reconciler].
func (r *BerthLeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("berthlease", req.NamespacedName)

	var lease berthv1alpha1.BerthLease
	if err := r.Get(ctx, req.NamespacedName, &lease); err != nil {
		return ctrl.Result{}, ctrlclient.IgnoreNotFound(err)
	}

	if !lease.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, log, &lease)
	}

	if !controllerutil.ContainsFinalizer(&lease, FinalizerName) {
		controllerutil.AddFinalizer(&lease, FinalizerName)
		if err := r.Update(ctx, &lease); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		// The Update bumps resourceVersion and the watch delivers it as a
		// fresh reconcile, so an explicit Requeue is unnecessary.
		return ctrl.Result{}, nil
	}

	if err := validateSpec(&lease.Spec); err != nil {
		log.Error(err, "invalid BerthLease spec")
		setCondition(&lease.Status, ConditionAcquired, metav1.ConditionFalse, "InvalidSpec", err.Error())
		_ = r.Status().Update(ctx, &lease)
		return ctrl.Result{}, nil
	}

	holder := r.holderFor(&lease)
	ttl := time.Duration(lease.Spec.TTLSeconds) * time.Second
	heartbeat := time.Duration(lease.Spec.HeartbeatIntervalSeconds) * time.Second

	res, err := r.LeaseClient.Acquire(ctx, lease.Namespace, lease.Spec.LeaseName, holder, ttl)
	if err != nil {
		log.Error(err, "lease acquire failed")
		setCondition(&lease.Status, ConditionHeartbeatHealthy, metav1.ConditionFalse, "AcquireFailed", err.Error())
		_ = r.Status().Update(ctx, &lease)
		return ctrl.Result{RequeueAfter: defaultRequeueOnFailure}, nil
	}

	if res.Acquired {
		return r.reconcileHeld(ctx, log, &lease, res, heartbeat)
	}
	return r.reconcileNotHeld(ctx, log, &lease, res, heartbeat, ttl)
}

func (r *BerthLeaseReconciler) reconcileHeld(ctx context.Context, log logr.Logger, lease *berthv1alpha1.BerthLease, res client.AcquireResult, heartbeat time.Duration) (ctrl.Result, error) {
	if err := applyAction(ctx, r.Client, lease.Namespace, lease.Spec.Target, lease.Spec.AcquireAction); err != nil {
		log.Error(err, "apply acquireAction")
		setCondition(&lease.Status, ConditionHeartbeatHealthy, metav1.ConditionFalse, "ApplyAcquireActionFailed", err.Error())
		_ = r.Status().Update(ctx, lease)
		return ctrl.Result{RequeueAfter: defaultRequeueOnFailure}, nil
	}

	now := metav1.NewTime(time.Now())
	lease.Status.LeaseState = StateHeld
	lease.Status.CurrentHolder = res.Holder
	lease.Status.FencingToken = res.FencingToken
	lease.Status.AcquiredAt = timePtr(res.AcquiredAt)
	lease.Status.ExpiresAt = timePtr(res.ExpiresAt)
	lease.Status.LastHeartbeat = &now
	setCondition(&lease.Status, ConditionAcquired, metav1.ConditionTrue, "Held", fmt.Sprintf("lease held with fencing token %d", res.FencingToken))
	setCondition(&lease.Status, ConditionHeartbeatHealthy, metav1.ConditionTrue, "Heartbeating", "lease renewed")

	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{RequeueAfter: heartbeat}, nil
}

func (r *BerthLeaseReconciler) reconcileNotHeld(ctx context.Context, log logr.Logger, lease *berthv1alpha1.BerthLease, res client.AcquireResult, heartbeat, ttl time.Duration) (ctrl.Result, error) {
	if err := applyAction(ctx, r.Client, lease.Namespace, lease.Spec.Target, lease.Spec.ReleaseAction); err != nil {
		log.Error(err, "apply releaseAction")
		// Don't fail the reconcile — record the condition and try again.
	}

	lease.Status.LeaseState = StateWaiting
	lease.Status.CurrentHolder = res.Holder
	// We do not hold a fencing token in this state. Clearing it prevents the
	// deletion path from sending a stale token in a Release call later.
	lease.Status.FencingToken = 0
	lease.Status.AcquiredAt = nil
	lease.Status.ExpiresAt = timePtr(res.ExpiresAt)
	setCondition(&lease.Status, ConditionAcquired, metav1.ConditionFalse, "HeldByOther", fmt.Sprintf("lease held by %q", res.Holder))
	setCondition(&lease.Status, ConditionHeartbeatHealthy, metav1.ConditionTrue, "Standby", "monitoring for reacquire")

	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{RequeueAfter: reacquireInterval(heartbeat, ttl)}, nil
}

func (r *BerthLeaseReconciler) reconcileDelete(ctx context.Context, log logr.Logger, lease *berthv1alpha1.BerthLease) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(lease, FinalizerName) {
		return ctrl.Result{}, nil
	}
	if lease.Status.LeaseState == StateHeld && lease.Status.CurrentHolder != "" && lease.Status.FencingToken > 0 {
		// Best-effort release. If this fails the lease will simply expire on
		// its own TTL; we still proceed to remove the finalizer.
		err := r.LeaseClient.Release(ctx, lease.Namespace, lease.Spec.LeaseName, lease.Status.CurrentHolder, lease.Status.FencingToken)
		if err != nil && !errors.Is(err, client.ErrConflict) {
			log.Error(err, "best-effort release on delete failed")
		}
	}
	controllerutil.RemoveFinalizer(lease, FinalizerName)
	if err := r.Update(ctx, lease); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the reconciler with the controller-runtime
// manager.
func (r *BerthLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.LeaseClient == nil {
		return errors.New("BerthLeaseReconciler.LeaseClient is required")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&berthv1alpha1.BerthLease{}).
		Complete(r)
}

// validateSpec checks the small set of invariants the reconciler depends on.
// CRD validation handles the rest.
func validateSpec(spec *berthv1alpha1.BerthLeaseSpec) error {
	if spec.LeaseName == "" {
		return errors.New("spec.leaseName is required")
	}
	if spec.HolderIdentity == "" {
		return errors.New("spec.holderIdentity is required")
	}
	if spec.TTLSeconds <= 0 {
		return errors.New("spec.ttlSeconds must be positive")
	}
	if spec.HeartbeatIntervalSeconds <= 0 {
		return errors.New("spec.heartbeatIntervalSeconds must be positive")
	}
	if spec.HeartbeatIntervalSeconds >= spec.TTLSeconds {
		return errors.New("spec.heartbeatIntervalSeconds must be less than spec.ttlSeconds")
	}
	return nil
}

// reacquireInterval picks a polling cadence for standby reconciles. We want
// to attempt reacquire well before the current holder's TTL elapses so that
// failover RTO is bounded; ttl/3 is a common rule of thumb.
func reacquireInterval(heartbeat, ttl time.Duration) time.Duration {
	if heartbeat > 0 && heartbeat < ttl/3 {
		return heartbeat
	}
	return ttl / 3
}

func setCondition(status *berthv1alpha1.BerthLeaseStatus, condType string, condStatus metav1.ConditionStatus, reason, message string) {
	now := metav1.NewTime(time.Now())
	for i := range status.Conditions {
		if status.Conditions[i].Type == condType {
			c := &status.Conditions[i]
			if c.Status != condStatus {
				c.LastTransitionTime = now
			}
			c.Status = condStatus
			c.Reason = reason
			c.Message = message
			return
		}
	}
	status.Conditions = append(status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	})
}

func timePtr(t time.Time) *metav1.Time {
	if t.IsZero() {
		return nil
	}
	out := metav1.NewTime(t)
	return &out
}
