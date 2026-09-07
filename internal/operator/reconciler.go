package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	"github.com/skaphos/berth/pkg/client"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// FinalizerName is the finalizer applied to BerthLease objects so the
// reconciler can stop managed Pods and release ownership before garbage collection.
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
const defaultRequeueOnFailure = time.Second

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

	// ManagedWorkloads requires the separate fail-closed admission installation.
	ManagedWorkloads  bool
	OperatorNamespace string
}

// holderFor returns the holder identity the reconciler should use for a
// given lease, applying the ClusterIdentity override when configured.
func (r *BerthLeaseReconciler) holderFor(lease *berthv1alpha1.BerthLease) string {
	if r.ClusterIdentity != "" {
		return r.ClusterIdentity
	}
	return lease.Spec.HolderIdentity
}

// Reconcile bounds both central calls and local cleanup operations. The client
// supplied by main bypasses the informer cache for security-sensitive reads.
func (r *BerthLeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	var l berthv1alpha1.BerthLease
	if err := r.Get(ctx, req.NamespacedName, &l); err != nil {
		return ctrl.Result{}, ctrlclient.IgnoreNotFound(err)
	}
	if !controllerutil.ContainsFinalizer(&l, FinalizerName) {
		if !l.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, nil
		}
		controllerutil.AddFinalizer(&l, FinalizerName)
		return ctrl.Result{}, r.Update(ctx, &l)
	}
	l.Status.ObservedGeneration = l.Generation
	w := l.Status.Workload
	invalid := validateSpec(&l.Spec)
	if !l.DeletionTimestamp.IsZero() || (w != nil && (w.Phase == PhaseStopping || (w.Phase == PhaseActive && (!activeAt(&l, time.Now()) || w.Holder != r.holderFor(&l) || invalid != nil)))) {
		return r.stopAndRelease(ctx, &l)
	}
	if invalid != nil {
		// Legacy/invalid active configurations must still stop before reporting an
		// invalid spec. Schema immutability prevents new identity transitions.
		if l.Spec.Target != nil {
			return r.stopAndRelease(ctx, &l)
		}
		setCondition(&l.Status, ConditionAcquired, metav1.ConditionFalse, "InvalidSpec", invalid.Error(), l.Generation)
		return ctrl.Result{}, r.Status().Update(ctx, &l)
	}
	if l.Spec.Target != nil {
		if err := r.verifyTarget(ctx, &l); err != nil {
			if w != nil && w.Phase == PhaseActive {
				return r.stopAndRelease(ctx, &l)
			}
			// Stop old targets during migration, but never activate them without the
			// admission record. The upgrade procedure also requires draining old Pods.
			stopErr := r.stopTarget(ctx, &l)
			setCondition(&l.Status, ConditionAcquired, metav1.ConditionFalse, "AdmissionRequired", err.Error(), l.Generation)
			return ctrl.Result{RequeueAfter: time.Second}, errors.Join(stopErr, r.Status().Update(ctx, &l))
		}
	}
	started := time.Now()
	ttl := time.Duration(l.Spec.TTLSeconds) * time.Second
	heartbeat := time.Duration(l.Spec.HeartbeatIntervalSeconds) * time.Second
	budget := min(heartbeat, localTimeout/2)
	if activeAt(&l, started) {
		budget = min(budget, time.Until(l.Status.Workload.Deadline.Time))
	}
	rpcCtx, rpcCancel := context.WithTimeout(ctx, budget)
	res, err := r.LeaseClient.Acquire(rpcCtx, l.Namespace, l.Spec.LeaseName, r.holderFor(&l), ttl)
	rpcCancel()
	if err != nil {
		if l.Status.Workload != nil && l.Status.Workload.Phase == PhaseActive && !activeAt(&l, time.Now()) {
			return r.stopAndRelease(ctx, &l)
		}
		if l.Spec.Target != nil && l.Status.Workload == nil && l.Status.ExpiresAt != nil && !time.Now().Before(l.Status.ExpiresAt.Time) {
			return r.stopAndRelease(ctx, &l)
		}
		setCondition(&l.Status, ConditionHeartbeatHealthy, metav1.ConditionFalse, "AcquireFailed", err.Error(), l.Generation)
		next := min(heartbeat, time.Second)
		if activeAt(&l, time.Now()) {
			next = min(next, time.Until(l.Status.Workload.Deadline.Time))
		}
		return ctrl.Result{RequeueAfter: next}, r.Status().Update(ctx, &l)
	}
	if !res.Acquired {
		if l.Spec.Target != nil {
			return r.stopAndRelease(ctx, &l)
		}
		l.Status.LeaseState = StateWaiting
		l.Status.FencingToken = 0
		l.Status.CurrentHolder = res.Holder
		l.Status.ExpiresAt = timePtr(res.ExpiresAt)
		l.Status.AcquiredAt = nil
		setCondition(&l.Status, ConditionAcquired, metav1.ConditionFalse, "HeldByOther", fmt.Sprintf("lease held by %q", res.Holder), l.Generation)
		setCondition(&l.Status, ConditionHeartbeatHealthy, metav1.ConditionTrue, "Standby", "monitoring for reacquire", l.Generation)
		return ctrl.Result{RequeueAfter: reacquireInterval(heartbeat, ttl)}, r.Status().Update(ctx, &l)
	}
	if res.FencingToken <= 0 || res.Holder != r.holderFor(&l) || !res.ExpiresAt.After(time.Now()) {
		return ctrl.Result{}, errors.New("invalid central ownership response")
	}
	// Record ownership before any activation. Round down to match metav1.Time
	// serialization, and never infer a later deadline from a delayed response.
	deadline := minTime(started.Add(ttl), res.ExpiresAt).Truncate(time.Second)
	if !time.Now().Before(deadline) {
		return r.stopAndRelease(ctx, &l)
	}
	if l.Spec.Target != nil {
		w = l.Status.Workload
		if w.Phase == PhaseActive && w.Token != res.FencingToken {
			return r.stopAndRelease(ctx, &l)
		}
		w.Phase = PhaseActive
		w.LeaseName = l.Spec.LeaseName
		w.Holder = res.Holder
		w.Token = res.FencingToken
		w.Deadline = timePtr(deadline)
		if err := r.pruneCompleted(ctx, &l); err != nil {
			return ctrl.Result{}, err
		}
	}
	l.Status.LeaseState = StateHeld
	l.Status.CurrentHolder = res.Holder
	l.Status.FencingToken = res.FencingToken
	l.Status.AcquiredAt = timePtr(res.AcquiredAt)
	l.Status.ExpiresAt = timePtr(res.ExpiresAt)
	l.Status.LastHeartbeat = timePtr(time.Now())
	setCondition(&l.Status, ConditionAcquired, metav1.ConditionTrue, "Held", fmt.Sprintf("lease held with fencing token %d", res.FencingToken), l.Generation)
	setCondition(&l.Status, ConditionHeartbeatHealthy, metav1.ConditionTrue, "Heartbeating", "lease renewed", l.Generation)
	if err := r.Status().Update(ctx, &l); err != nil {
		return ctrl.Result{}, fmt.Errorf("persist ownership: %w", err)
	}
	if l.Spec.Target != nil {
		if err := applyActionForUID(ctx, r.Client, l.Namespace, l.Spec.Target, l.Spec.AcquireAction, l.Status.Workload.TargetUID); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.permitPods(ctx, &l); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: min(heartbeat, time.Until(deadline))}, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// stopAndRelease first closes admission to new permits, then drains the UIDs
// already permitted. A failed write/termination never clears cleanup state.
func (r *BerthLeaseReconciler) stopAndRelease(ctx context.Context, l *berthv1alpha1.BerthLease) (ctrl.Result, error) {
	w := l.Status.Workload
	if l.Spec.Target != nil {
		if w == nil {
			w = &berthv1alpha1.WorkloadStatus{LeaseName: l.Spec.LeaseName, Holder: l.Status.CurrentHolder, Token: l.Status.FencingToken}
			l.Status.Workload = w
		}
		if w.Phase != PhaseStopping {
			w.Phase = PhaseStopping
			setCondition(&l.Status, ConditionHeartbeatHealthy, metav1.ConditionFalse, "Stopping", "workload activation is closed during cleanup", l.Generation)
			setCondition(&l.Status, ConditionAcquired, metav1.ConditionFalse, "Stopping", "waiting for managed Pods to terminate", l.Generation)
			if err := r.Status().Update(ctx, l); err != nil {
				return ctrl.Result{}, err
			}
		}
		if err := r.stopTarget(ctx, l); err != nil {
			return ctrl.Result{}, err
		}
		jobsDone, err := r.drainJobs(ctx, l)
		if err != nil {
			return ctrl.Result{}, err
		}
		done, err := r.drainPods(ctx, l)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !done || !jobsDone {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}
	holder, token, name := l.Status.CurrentHolder, l.Status.FencingToken, l.Spec.LeaseName
	if w != nil {
		holder, token, name = w.Holder, w.Token, w.LeaseName
	}
	if holder != "" && token > 0 {
		rpcCtx, cancel := context.WithTimeout(ctx, localTimeout/2)
		err := r.LeaseClient.Release(rpcCtx, l.Namespace, name, holder, token)
		cancel()
		if err != nil && !errors.Is(err, client.ErrConflict) {
			return ctrl.Result{RequeueAfter: time.Second}, err
		}
	}
	if !l.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(l, FinalizerName)
		return ctrl.Result{}, r.Update(ctx, l)
	}
	if w != nil {
		w.Phase = ""
		w.Pods = nil
		w.Token = 0
		w.Holder = ""
		w.LeaseName = ""
		w.Deadline = nil
	}
	l.Status.LeaseState = StateWaiting
	l.Status.FencingToken = 0
	l.Status.CurrentHolder = ""
	l.Status.ExpiresAt = nil
	l.Status.AcquiredAt = nil
	setCondition(&l.Status, ConditionAcquired, metav1.ConditionFalse, "Stopped", "managed Pods stopped", l.Generation)
	return ctrl.Result{RequeueAfter: time.Second}, r.Status().Update(ctx, l)
}

// SetupWithManager registers the reconciler with the controller-runtime
// manager.
func (r *BerthLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.LeaseClient == nil {
		return errors.New("BerthLeaseReconciler.LeaseClient is required")
	}
	if r.ManagedWorkloads {
		if err := r.setupManagedControllers(mgr); err != nil {
			return err
		}
	}
	// Workload management additionally watches Pods and Jobs so late creates
	// remain accounted for after lease deletion. All authorization reads use
	// the direct API client supplied by main.
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
	return workloadSpec(spec)
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

// setCondition upserts a status condition using the apimachinery helper, which
// preserves LastTransitionTime unless the status changes and stamps the
// per-condition ObservedGeneration so clients get a freshness signal.
func setCondition(status *berthv1alpha1.BerthLeaseStatus, condType string, condStatus metav1.ConditionStatus, reason, message string, observedGeneration int64) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: observedGeneration,
	})
}

func timePtr(t time.Time) *metav1.Time {
	if t.IsZero() {
		return nil
	}
	out := metav1.NewTime(t)
	return &out
}
