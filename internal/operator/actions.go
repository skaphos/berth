package operator

import (
	"context"
	"errors"
	"fmt"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// targetFieldOwner is the field manager recorded in managedFields for every
// write the operator makes to a target workload, so server-side-apply-aware
// tooling attributes spec.replicas, spec.suspend, and the stop fence to Berth
// rather than to a generic manager.
const targetFieldOwner = "berth-operator"

// applyAction reads the target referenced by ref from namespace ns, mutates
// it according to action, and writes it back. Returns nil when action is nil
// (no-op) or when the target is gone — both are treated as success because
// neither prevents the lease lifecycle from progressing.
//
// The write is itself a no-op when the target already matches the action: the
// current spec.replicas/spec.suspend are read and the patch is skipped unless
// at least one differs. This keeps held-state heartbeats from re-writing an
// unchanged target on every reconcile.
func applyAction(ctx context.Context, c client.Client, ns string, ref *berthv1alpha1.TargetRef, action *berthv1alpha1.LeaseAction) error {
	return applyActionForUID(ctx, c, ns, ref, action, "")
}

func applyActionForUID(ctx context.Context, c client.Client, ns string, ref *berthv1alpha1.TargetRef, action *berthv1alpha1.LeaseAction, uid string) error {
	return applyFencedAction(ctx, c, ns, ref, action, uid, "")
}

func applyFencedAction(ctx context.Context, c client.Client, ns string, ref *berthv1alpha1.TargetRef, action *berthv1alpha1.LeaseAction, uid, fence string) error {
	if ref == nil || action == nil {
		return nil
	}
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return fmt.Errorf("parse target apiVersion %q: %w", ref.APIVersion, err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gv.WithKind(ref.Kind))
	key := types.NamespacedName{Namespace: ns, Name: ref.Name}
	if err := c.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get target %s: %w", key, err)
	}

	if uid != "" && string(obj.GetUID()) != uid {
		return errors.New("target UID changed")
	}
	// Snapshot before mutating so the patch is computed against exactly what
	// was read, including its resourceVersion.
	orig := obj.DeepCopy()

	mutated := false // action selected a field to manage
	changed := false // obj differs from the live target — a write is required
	if action.Suspend != nil {
		mutated = true
		cur, found, err := unstructured.NestedBool(obj.Object, "spec", "suspend")
		if err != nil {
			return fmt.Errorf("read spec.suspend on %s: %w", key, err)
		}
		if !found || cur != *action.Suspend {
			if err := unstructured.SetNestedField(obj.Object, *action.Suspend, "spec", "suspend"); err != nil {
				return fmt.Errorf("set spec.suspend: %w", err)
			}
			changed = true
		}
	}
	if action.Scale != nil {
		mutated = true
		desired := int64(action.Scale.Replicas)
		cur, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil {
			return fmt.Errorf("read spec.replicas on %s: %w", key, err)
		}
		if !found || cur != desired {
			if err := unstructured.SetNestedField(obj.Object, desired, "spec", "replicas"); err != nil {
				return fmt.Errorf("set spec.replicas: %w", err)
			}
			changed = true
		}
	}
	if !mutated {
		return errors.New("apply action: action specifies no mutation")
	}
	if fence != "" {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		if annotations["berth.skaphos.io/stop-fence"] != fence {
			annotations["berth.skaphos.io/stop-fence"] = fence
			obj.SetAnnotations(annotations)
			changed = true
		}
	}
	if !changed {
		// Target already at the desired state. Skipping the write keeps held-state
		// heartbeats from re-issuing a patch (and the resulting resourceVersion
		// churn and spurious watch events) on every reconcile — material at the
		// 2,000-lease scale target where the operator is client-go QPS-bound.
		return nil
	}

	// A JSON merge patch carrying only the fields the action manages, with two
	// deliberate properties:
	//
	//   - Optimistic lock. The patch includes the resourceVersion observed by
	//     the Get above, so the API server rejects it with a Conflict if the
	//     target moved in between. This is what makes the stop fence work:
	//     stopTarget always writes the fence annotation (bumping the target's
	//     resourceVersion) even when the target is already stopped, so an
	//     activation write that raced it and is still in flight fails instead
	//     of resurrecting the workload. An unlocked patch, or a write to the
	//     scale subresource, would bypass the fence. The cost is a Conflict
	//     (and a requeue) when an unrelated writer such as an HPA touches the
	//     target at the same instant; that is the intended trade (#105).
	//   - Field owner. The write is attributed to targetFieldOwner in
	//     managedFields rather than to a generic manager.
	//
	// Compared to the full-object Update this replaces, the patch sends only
	// the changed fields, so the operator never rewrites fields it does not
	// own and the write is attributable.
	patch := client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{})
	if err := c.Patch(ctx, obj, patch, client.FieldOwner(targetFieldOwner)); err != nil {
		return fmt.Errorf("patch target %s: %w", key, err)
	}
	return nil
}
