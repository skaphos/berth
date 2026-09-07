package operator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ManagedLease     = "berth.skaphos.io/managed-lease"
	ManagedUID       = "berth.skaphos.io/managed-lease-uid"
	SchedulingGate   = "berth.skaphos.io/lease-held"
	PhaseActive      = "Active"
	PhaseStopping    = "Stopping"
	maxPermittedPods = 1024
	localTimeout     = 5 * time.Second
)

func hasGate(p *corev1.Pod) bool {
	return slices.ContainsFunc(p.Spec.SchedulingGates, func(g corev1.PodSchedulingGate) bool { return g.Name == SchedulingGate })
}

func workloadSpec(spec *berthv1alpha1.BerthLeaseSpec) error {
	if spec.Target == nil {
		if spec.AcquireAction != nil || spec.ReleaseAction != nil {
			return errors.New("actions require a target")
		}
		return nil
	}
	t := spec.Target
	if t.Name == "" {
		return errors.New("target name is required")
	}
	a, b := spec.AcquireAction, spec.ReleaseAction
	if a == nil || b == nil {
		return errors.New("managed targets require acquireAction and releaseAction")
	}
	if t.APIVersion == "batch/v1" && t.Kind == "CronJob" {
		if a.Suspend == nil || *a.Suspend || a.Scale != nil || b.Suspend == nil || !*b.Suspend || b.Scale != nil {
			return errors.New("CronJobs require acquire suspend=false and release suspend=true")
		}
		return nil
	}
	if t.APIVersion != "apps/v1" || (t.Kind != "Deployment" && t.Kind != "StatefulSet" && t.Kind != "ReplicaSet") {
		return errors.New("supported targets are apps/v1 Deployment, StatefulSet, ReplicaSet and batch/v1 CronJob")
	}
	if a.Scale == nil || a.Scale.Replicas < 0 || a.Suspend != nil || b.Scale == nil || b.Scale.Replicas != 0 || b.Suspend != nil {
		return errors.New("scalable targets require scale actions and scale-to-zero release")
	}
	return nil
}

func targetObject(ref *berthv1alpha1.TargetRef) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	if ref != nil {
		obj.SetGroupVersionKind(schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind))
	}
	return obj
}

func (r *BerthLeaseReconciler) verifyTarget(ctx context.Context, l *berthv1alpha1.BerthLease) error {
	if r.OperatorNamespace != "" && l.Namespace == r.OperatorNamespace {
		return errors.New("workloads cannot be managed in the operator namespace")
	}
	if !r.ManagedWorkloads {
		return errors.New("managed workloads require workload admission; lease-only operation is still available")
	}
	w := l.Status.Workload
	if w == nil || w.TargetUID == "" {
		return errors.New("target has no CREATE admission record; drain and recreate the target after its BerthLease")
	}
	obj := targetObject(l.Spec.Target)
	if err := r.Get(ctx, ctrlclient.ObjectKey{Namespace: l.Namespace, Name: l.Spec.Target.Name}, obj); err != nil {
		return err
	}
	if string(obj.GetUID()) != w.TargetUID || obj.GetAnnotations()[ManagedUID] != string(l.UID) || obj.GetAnnotations()[ManagedLease] != l.Name {
		return errors.New("target identity does not match admission record")
	}
	return nil
}

// stopTarget only mutates the admitted incarnation. A replacement is never
// activated by this lease and must not inherit cleanup actions for the old UID.
func (r *BerthLeaseReconciler) stopTarget(ctx context.Context, l *berthv1alpha1.BerthLease) error {
	if l.Spec.Target == nil {
		return nil
	}
	obj := targetObject(l.Spec.Target)
	if err := r.Get(ctx, ctrlclient.ObjectKey{Namespace: l.Namespace, Name: l.Spec.Target.Name}, obj); err != nil {
		return ctrlclient.IgnoreNotFound(err)
	}
	if w := l.Status.Workload; w != nil && w.TargetUID != "" && string(obj.GetUID()) != w.TargetUID {
		return nil
	}
	// Legacy targets are stopped as a migration safeguard but never activated.
	action := &berthv1alpha1.LeaseAction{Scale: &berthv1alpha1.ScaleAction{Replicas: 0}}
	if l.Spec.Target.Kind == "CronJob" && l.Spec.Target.APIVersion == "batch/v1" {
		v := true
		action = &berthv1alpha1.LeaseAction{Suspend: &v}
	}
	return applyFencedAction(ctx, r.Client, l.Namespace, l.Spec.Target, action, string(obj.GetUID()), l.ResourceVersion)
}

func deleteUID(ctx context.Context, c ctrlclient.Client, obj ctrlclient.Object) error {
	uid := obj.GetUID()
	if uid == "" {
		return errors.New("cannot delete a workload without its UID")
	}
	return ctrlclient.IgnoreNotFound(c.Delete(ctx, obj, &ctrlclient.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}))
}

// drainPods must check every recorded UID even when its scheduling gate is
// present: a previously issued gate removal can arrive after this read.
func (r *BerthLeaseReconciler) drainPods(ctx context.Context, l *berthv1alpha1.BerthLease) (bool, error) {
	// A complete namespace LIST gives one consistent set of existing UIDs. Per-UID
	// GETs repeatedly starting at the front can starve cleanup under API throttling.
	var pods corev1.PodList
	if err := r.List(ctx, &pods, ctrlclient.InNamespace(l.Namespace)); err != nil {
		return false, err
	}
	registered := map[string]string{}
	if w := l.Status.Workload; w != nil {
		for _, p := range w.Pods {
			registered[p.Name] = p.UID
		}
	}
	done := true
	for i := range pods.Items {
		p := &pods.Items[i]
		if registered[p.Name] != string(p.UID) && p.Annotations[ManagedUID] != string(l.UID) {
			continue
		}
		terminal := p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed
		if !terminal {
			done = false
		}
		if p.DeletionTimestamp == nil {
			if err := deleteUID(ctx, r.Client, p); err != nil {
				return false, err
			}
		}
	}
	return done, nil
}

func (r *BerthLeaseReconciler) drainJobs(ctx context.Context, l *berthv1alpha1.BerthLease) (bool, error) {
	var jobs batchv1.JobList
	if err := r.List(ctx, &jobs, ctrlclient.InNamespace(l.Namespace), ctrlclient.MatchingLabels{ManagedUID: string(l.UID)}); err != nil {
		return false, err
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if job.DeletionTimestamp != nil {
			continue
		}
		if job.Spec.Suspend == nil || !*job.Spec.Suspend {
			v := true
			job.Spec.Suspend = &v
			if err := r.Update(ctx, job); err != nil {
				return false, err
			}
		}
		if err := deleteUID(ctx, r.Client, job); err != nil {
			return false, err
		}
	}
	return len(jobs.Items) == 0, nil
}

func activeAt(l *berthv1alpha1.BerthLease, now time.Time) bool {
	w := l.Status.Workload
	return l.DeletionTimestamp.IsZero() && w != nil && w.Phase == PhaseActive && w.Deadline != nil && now.Before(w.Deadline.Time) && w.Token > 0
}

func (r *BerthLeaseReconciler) permitPod(ctx context.Context, l *berthv1alpha1.BerthLease, p *corev1.Pod) error {
	if !activeAt(l, time.Now()) || p.DeletionTimestamp != nil || p.UID == "" {
		return nil
	}
	if p.Annotations[ManagedUID] != string(l.UID) || p.Annotations[ManagedLease] != l.Name {
		return errors.New("pod binding mismatch")
	}
	root, err := expectedBinding(ctx, r.Client, p)
	if err != nil {
		return err
	}
	if root.uid != string(l.UID) || root.targetUID != l.Status.Workload.TargetUID {
		return errors.New("pod ancestry differs from admitted target UID")
	}
	w := l.Status.Workload
	found := slices.ContainsFunc(w.Pods, func(v berthv1alpha1.PermittedPod) bool { return v.UID == string(p.UID) && v.Name == p.Name })
	if !found {
		if !hasGate(p) {
			return errors.New("unregistered managed Pod has no scheduling gate")
		}
		if len(w.Pods) >= maxPermittedPods {
			return fmt.Errorf("maximum %d active Pod registrations reached", maxPermittedPods)
		}
		w.Pods = append(w.Pods, berthv1alpha1.PermittedPod{Name: p.Name, UID: string(p.UID)})
		// This compare-and-swap serializes with Stopping and all heartbeat writers.
		if err := r.Status().Update(ctx, l); err != nil {
			return fmt.Errorf("register Pod UID: %w", err)
		}
	}
	if !hasGate(p) {
		return nil
	}
	p.Spec.SchedulingGates = slices.DeleteFunc(p.Spec.SchedulingGates, func(g corev1.PodSchedulingGate) bool { return g.Name == SchedulingGate })
	// Update includes UID and resourceVersion; never create or apply a Pod here.
	if err := r.Update(ctx, p); err != nil {
		return fmt.Errorf("remove registered Pod gate: %w", err)
	}
	return nil
}

func (r *BerthLeaseReconciler) permitPods(ctx context.Context, l *berthv1alpha1.BerthLease) error {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, ctrlclient.InNamespace(l.Namespace), ctrlclient.MatchingLabels{ManagedUID: string(l.UID)}); err != nil {
		return err
	}
	for i := range pods.Items {
		if err := r.permitPod(ctx, l, &pods.Items[i]); err != nil {
			return err
		}
	}
	return nil
}

// pruneCompleted only forgets incarnations that cannot execute again.
func (r *BerthLeaseReconciler) pruneCompleted(ctx context.Context, l *berthv1alpha1.BerthLease) error {
	if l.Status.Workload == nil || len(l.Status.Workload.Pods) == 0 {
		return nil
	}
	var pods corev1.PodList
	if err := r.List(ctx, &pods, ctrlclient.InNamespace(l.Namespace)); err != nil {
		return err
	}
	alive := map[string]bool{}
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			alive[string(p.UID)] = true
		}
	}
	l.Status.Workload.Pods = slices.DeleteFunc(l.Status.Workload.Pods, func(p berthv1alpha1.PermittedPod) bool { return !alive[p.UID] })
	return nil
}
