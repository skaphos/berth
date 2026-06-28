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

// applyAction reads the target referenced by ref from namespace ns, mutates
// it according to action, and writes it back. Returns nil when action is nil
// (no-op) or when the target is gone — both are treated as success because
// neither prevents the lease lifecycle from progressing.
//
// The write is itself a no-op when the target already matches the action: the
// current spec.replicas/spec.suspend are read and the Update is skipped unless
// at least one differs. This keeps held-state heartbeats from re-writing an
// unchanged target on every reconcile.
func applyAction(ctx context.Context, c client.Client, ns string, ref *berthv1alpha1.TargetRef, action *berthv1alpha1.LeaseAction) error {
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

	mutated := false // action selected a field to manage
	changed := false // obj differs from the live target — an Update is required
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
	if !changed {
		// Target already at the desired state. Skipping the write keeps held-state
		// heartbeats from re-issuing an Update (and the resulting resourceVersion
		// churn and spurious watch events) on every reconcile — material at the
		// 2,000-lease scale target where the operator is client-go QPS-bound.
		return nil
	}

	if err := c.Update(ctx, obj); err != nil {
		return fmt.Errorf("update target %s: %w", key, err)
	}
	return nil
}
