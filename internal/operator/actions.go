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

	mutated := false
	if action.Suspend != nil {
		if err := unstructured.SetNestedField(obj.Object, *action.Suspend, "spec", "suspend"); err != nil {
			return fmt.Errorf("set spec.suspend: %w", err)
		}
		mutated = true
	}
	if action.Scale != nil {
		if err := unstructured.SetNestedField(obj.Object, int64(action.Scale.Replicas), "spec", "replicas"); err != nil {
			return fmt.Errorf("set spec.replicas: %w", err)
		}
		mutated = true
	}
	if !mutated {
		return errors.New("apply action: action specifies no mutation")
	}

	if err := c.Update(ctx, obj); err != nil {
		return fmt.Errorf("update target %s: %w", key, err)
	}
	return nil
}
