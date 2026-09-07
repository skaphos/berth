package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"time"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type binding struct{ name, uid, targetUID string }

func objectKind(obj ctrlclient.Object) schema.GroupVersionKind {
	if _, ok := obj.(*corev1.Pod); ok {
		return corev1.SchemeGroupVersion.WithKind("Pod")
	}
	return obj.GetObjectKind().GroupVersionKind()
}

func supportedController(apiVersion, kind string) bool {
	return apiVersion == "apps/v1" && (kind == "Deployment" || kind == "ReplicaSet" || kind == "StatefulSet") || apiVersion == "batch/v1" && (kind == "Job" || kind == "CronJob")
}

// expectedBinding obtains the expected identity from a live parent/root target,
// independently of submitted Pod/template labels. A missing supported parent
// never downgrades a late controller create to an unmanaged Pod.
func expectedBinding(ctx context.Context, c ctrlclient.Client, obj ctrlclient.Object) (binding, error) {
	return resolveBinding(ctx, c, obj, 0)
}
func resolveBinding(ctx context.Context, c ctrlclient.Client, obj ctrlclient.Object, depth int) (binding, error) {
	if depth > 4 {
		return binding{}, errors.New("workload owner chain exceeds supported depth")
	}
	for _, owner := range obj.GetOwnerReferences() {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		if !supportedController(owner.APIVersion, owner.Kind) {
			break
		}
		parent := &unstructured.Unstructured{}
		parent.SetGroupVersionKind(schema.FromAPIVersionAndKind(owner.APIVersion, owner.Kind))
		if err := c.Get(ctx, ctrlclient.ObjectKey{Namespace: obj.GetNamespace(), Name: owner.Name}, parent); err != nil {
			return binding{}, fmt.Errorf("resolve controller owner: %w", err)
		}
		if parent.GetUID() != owner.UID {
			return binding{}, errors.New("controller owner UID changed")
		}
		return resolveBinding(ctx, c, parent, depth+1)
	}
	gvk := objectKind(obj)
	if supportedController(gvk.GroupVersion().String(), gvk.Kind) && gvk.Kind != "Job" {
		var leases berthv1alpha1.BerthLeaseList
		if err := c.List(ctx, &leases, ctrlclient.InNamespace(obj.GetNamespace())); err != nil {
			return binding{}, err
		}
		found := binding{}
		for _, l := range leases.Items {
			t := l.Spec.Target
			if t != nil && t.Name == obj.GetName() && t.APIVersion == gvk.GroupVersion().String() && t.Kind == gvk.Kind {
				if found.uid != "" {
					return binding{}, errors.New("multiple BerthLeases refer to the same target")
				}
				found = binding{name: l.Name, uid: string(l.UID), targetUID: string(obj.GetUID())}
			}
		}
		if found.uid != "" {
			return found, nil
		}
	}
	if obj.GetAnnotations()[ManagedUID] != "" || obj.GetAnnotations()[ManagedLease] != "" {
		return binding{}, errors.New("managed binding has no matching live target lease")
	}
	return binding{}, nil
}

// WorkloadAdmission is independent of opt-in helper injection. Its registrations
// must use failurePolicy=Fail without caller-controlled selectors.
type WorkloadAdmission struct {
	Client       ctrlclient.Client
	OperatorUser string
	Mutate       bool
}

func SetupWorkloadAdmission(mgr ctrl.Manager, c ctrlclient.Client, operatorUser string) error {
	if operatorUser == "" {
		return errors.New("workload admission requires the operator service-account username")
	}
	mgr.GetWebhookServer().Register("/mutate-managed-workloads", &admission.Webhook{Handler: &WorkloadAdmission{Client: c, OperatorUser: operatorUser, Mutate: true}})
	mgr.GetWebhookServer().Register("/validate-managed-workloads", &admission.Webhook{Handler: &WorkloadAdmission{Client: c, OperatorUser: operatorUser}})
	return nil
}

func (h *WorkloadAdmission) Handle(ctx context.Context, req admission.Request) admission.Response {
	ctx, cancel := context.WithTimeout(ctx, localTimeout)
	defer cancel()
	if req.SubResource == "binding" {
		return h.validateBinding(ctx, req)
	}
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(req.Object.Raw, obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	obj.SetNamespace(req.Namespace)
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: req.Kind.Group, Version: req.Kind.Version, Kind: req.Kind.Kind})
	var old *unstructured.Unstructured
	if req.Operation == admissionv1.Update {
		old = &unstructured.Unstructured{}
		if err := json.Unmarshal(req.OldObject.Raw, old); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		old.SetNamespace(req.Namespace)
		old.SetGroupVersionKind(obj.GroupVersionKind())
	}
	var b binding
	var err error
	if old != nil && old.GetAnnotations()[ManagedUID] != "" {
		// The old admitted object retains identity even when a parent/lease is gone.
		b = binding{name: old.GetAnnotations()[ManagedLease], uid: old.GetAnnotations()[ManagedUID], targetUID: string(old.GetUID())}
	} else {
		b, err = expectedBinding(ctx, h.Client, obj)
	}
	if err != nil && old != nil && old.GetAnnotations()[ManagedUID] == "" && obj.GetAnnotations()[ManagedUID] == "" && reflect.DeepEqual(old.GetOwnerReferences(), obj.GetOwnerReferences()) {
		return admission.Allowed("unmanaged update")
	}
	if err != nil {
		return admission.Denied(err.Error())
	}
	if b.uid == "" {
		return admission.Allowed("")
	}
	if h.Mutate {
		if req.Operation != admissionv1.Create {
			return admission.Allowed("")
		}
		if err := stampBinding(obj, b); err != nil {
			return admission.Denied(err.Error())
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		return admission.PatchResponseFromRaw(req.Object.Raw, raw)
	}
	// An old target cannot acquire a birth record through UPDATE. Permit only
	// deactivation of an unadmitted legacy target so upgrades can drain it.
	if old != nil && old.GetAnnotations()[ManagedUID] == "" {
		// Controller adoption must not turn an already running orphan into an
		// unregistered managed workload. Harmless cleanup keeps its old owner.
		if !reflect.DeepEqual(old.GetOwnerReferences(), obj.GetOwnerReferences()) {
			return admission.Denied("managed controllers cannot adopt unadmitted objects")
		}
		if ((obj.GetKind() != "Pod" && inactiveTarget(obj)) || (obj.GetKind() == "Pod" && reflect.DeepEqual(obj.Object["spec"], old.Object["spec"]))) && obj.GetAnnotations()[ManagedUID] == "" {
			return admission.Allowed("legacy target may only be stopped")
		}
		return admission.Denied("drain and recreate legacy managed targets under admission")
	}
	if err := validateManagedObject(obj, old, b, req.UserInfo.Username == h.OperatorUser); err != nil {
		return admission.Denied(err.Error())
	}
	if obj.GetKind() == "Pod" {
		if old != nil && gateOn(old) && !gateOn(obj) {
			if err := h.validatePermit(ctx, obj, b); err != nil {
				return admission.Denied(err.Error())
			}
		}
		return admission.Allowed("")
	}
	if old != nil && increasesActivity(obj, old) {
		var l berthv1alpha1.BerthLease
		if err := h.Client.Get(ctx, ctrlclient.ObjectKey{Namespace: obj.GetNamespace(), Name: b.name}, &l); err != nil {
			return admission.Denied(err.Error())
		}
		if string(l.UID) != b.uid || !activeAt(&l, time.Now()) {
			return admission.Denied("lease does not authorize workload activation")
		}
	}
	if req.Operation == admissionv1.Create {
		if err := h.recordTarget(ctx, obj, b, req.DryRun != nil && *req.DryRun); err != nil {
			return admission.Denied(err.Error())
		}
	}
	return admission.Allowed("")
}

func bindingMetadata(obj *unstructured.Unstructured, b binding) {
	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	a[ManagedLease] = b.name
	a[ManagedUID] = b.uid
	obj.SetAnnotations(a)
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[ManagedUID] = b.uid
	obj.SetLabels(labels)
}
func podTemplatePath(kind string) []string {
	if kind == "CronJob" {
		return []string{"spec", "jobTemplate", "spec", "template"}
	}
	if kind == "Pod" {
		return nil
	}
	return []string{"spec", "template"}
}
func stampBinding(obj *unstructured.Unstructured, b binding) error {
	bindingMetadata(obj, b)
	path := podTemplatePath(obj.GetKind())
	template := obj
	if len(path) > 0 {
		raw, found, err := unstructured.NestedMap(obj.Object, path...)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("managed workload requires a Pod template")
		}
		template = &unstructured.Unstructured{Object: raw}
		bindingMetadata(template, b)
	}
	gates, _, err := unstructured.NestedSlice(template.Object, "spec", "schedulingGates")
	if err != nil {
		return err
	}
	exists := false
	for _, g := range gates {
		m, ok := g.(map[string]interface{})
		if ok && m["name"] == SchedulingGate {
			exists = true
		}
	}
	if !exists {
		gates = append(gates, map[string]interface{}{"name": SchedulingGate})
	}
	if err := unstructured.SetNestedSlice(template.Object, gates, "spec", "schedulingGates"); err != nil {
		return err
	}
	if len(path) > 0 {
		if err := unstructured.SetNestedMap(obj.Object, template.Object, path...); err != nil {
			return err
		}
	}
	if obj.GetKind() == "CronJob" {
		raw, _, err := unstructured.NestedMap(obj.Object, "spec", "jobTemplate")
		if err != nil {
			return err
		}
		jt := &unstructured.Unstructured{Object: raw}
		bindingMetadata(jt, b)
		if err := unstructured.SetNestedMap(obj.Object, jt.Object, "spec", "jobTemplate"); err != nil {
			return err
		}
	}
	return nil
}
func gateOn(obj *unstructured.Unstructured) bool {
	gates, _, _ := unstructured.NestedSlice(obj.Object, "spec", "schedulingGates")
	for _, g := range gates {
		m, ok := g.(map[string]interface{})
		if ok && m["name"] == SchedulingGate {
			return true
		}
	}
	return false
}
func validateMetadata(obj *unstructured.Unstructured, b binding) error {
	if obj.GetAnnotations()[ManagedLease] != b.name || obj.GetAnnotations()[ManagedUID] != b.uid || obj.GetLabels()[ManagedUID] != b.uid {
		return errors.New("managed workload binding cannot be removed or changed")
	}
	return nil
}
func validateManagedObject(obj, old *unstructured.Unstructured, b binding, operator bool) error {
	if err := validateMetadata(obj, b); err != nil {
		return err
	}
	path := podTemplatePath(obj.GetKind())
	template := obj
	if len(path) > 0 {
		raw, found, err := unstructured.NestedMap(obj.Object, path...)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("missing managed Pod template")
		}
		template = &unstructured.Unstructured{Object: raw}
		if err := validateMetadata(template, b); err != nil {
			return err
		}
	}
	nodeName, _, _ := unstructured.NestedString(template.Object, "spec", "nodeName")
	if nodeName != "" && (obj.GetKind() != "Pod" || old == nil) {
		return errors.New("managed Pods cannot be prebound using nodeName")
	}
	if obj.GetKind() == "Pod" && old != nil {
		if !reflect.DeepEqual(old.GetOwnerReferences(), obj.GetOwnerReferences()) {
			return errors.New("managed Pod owner identity is immutable")
		}
		oldNode, _, _ := unstructured.NestedString(old.Object, "spec", "nodeName")
		if oldNode != nodeName {
			return errors.New("managed Pods must use the binding subresource")
		}
		if gateOn(old) && !gateOn(obj) && !operator {
			return errors.New("only the operator may remove the Berth scheduling gate")
		}
	} else if !gateOn(template) {
		return errors.New("managed Pod must be created with the Berth scheduling gate")
	}
	if obj.GetKind() == "CronJob" {
		raw, _, _ := unstructured.NestedMap(obj.Object, "spec", "jobTemplate")
		if err := validateMetadata(&unstructured.Unstructured{Object: raw}, b); err != nil {
			return err
		}
	}
	return nil
}

// Controllers may update bookkeeping while stopping. Gate only an increase in
// requested execution, not metadata-only writes needed to complete scale-down.
func increasesActivity(obj, old *unstructured.Unstructured) bool {
	if obj.GetKind() == "CronJob" || obj.GetKind() == "Job" {
		before, _, _ := unstructured.NestedBool(old.Object, "spec", "suspend")
		after, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
		return before && !after
	}
	before, _, _ := unstructured.NestedInt64(old.Object, "spec", "replicas")
	after, _, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	return after > before
}

func inactiveTarget(obj *unstructured.Unstructured) bool {
	if obj.GetKind() == "CronJob" || obj.GetKind() == "Job" {
		v, ok, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
		return ok && v
	}
	v, ok, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	return ok && v == 0
}

// recordTarget runs only at final CREATE validation, after the API server fills
// metadata.uid. It has no dry-run side effects. Aborted creates can leave an
// unused UID; a later CREATE may replace it only with no active cleanup epoch.
func (h *WorkloadAdmission) recordTarget(ctx context.Context, obj *unstructured.Unstructured, b binding, dry bool) error {
	var l berthv1alpha1.BerthLease
	if err := h.Client.Get(ctx, ctrlclient.ObjectKey{Namespace: obj.GetNamespace(), Name: b.name}, &l); err != nil {
		return err
	}
	if string(l.UID) != b.uid || !l.DeletionTimestamp.IsZero() {
		return errors.New("target lease is deleted or replaced")
	}
	t := l.Spec.Target
	if t == nil || t.Name != obj.GetName() || t.Kind != obj.GetKind() || t.APIVersion != obj.GetAPIVersion() {
		return nil
	} // descendant Job or ReplicaSet
	if err := workloadSpec(&l.Spec); err != nil {
		return err
	}
	if obj.GetUID() == "" {
		return errors.New("final target CREATE admission requires a Kubernetes UID")
	}
	if w := l.Status.Workload; w != nil && (w.Phase != "" || len(w.Pods) > 0) {
		return errors.New("target cannot be replaced while cleanup responsibility is active")
	}
	existing := targetObject(t)
	if err := h.Client.Get(ctx, ctrlclient.ObjectKeyFromObject(obj), existing); err == nil {
		return errors.New("target already exists; CREATE cannot replace its admission record")
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if dry {
		return nil
	}
	l.Status.Workload = &berthv1alpha1.WorkloadStatus{TargetUID: string(obj.GetUID())}
	return h.Client.Status().Update(ctx, &l)
}
func (h *WorkloadAdmission) validatePermit(ctx context.Context, obj *unstructured.Unstructured, b binding) error {
	var l berthv1alpha1.BerthLease
	if err := h.Client.Get(ctx, ctrlclient.ObjectKey{Namespace: obj.GetNamespace(), Name: b.name}, &l); err != nil {
		return err
	}
	if string(l.UID) != b.uid || !activeAt(&l, time.Now()) {
		return errors.New("lease no longer permits Pod starts")
	}
	if !slices.ContainsFunc(l.Status.Workload.Pods, func(p berthv1alpha1.PermittedPod) bool {
		return p.UID == string(obj.GetUID()) && p.Name == obj.GetName()
	}) {
		return errors.New("pod UID was not durably registered")
	}
	return nil
}
func (h *WorkloadAdmission) validateBinding(ctx context.Context, req admission.Request) admission.Response {
	var pod corev1.Pod
	if err := h.Client.Get(ctx, ctrlclient.ObjectKey{Namespace: req.Namespace, Name: req.Name}, &pod); err != nil {
		return admission.Denied(err.Error())
	}
	if pod.Annotations[ManagedUID] == "" {
		return admission.Allowed("")
	}
	var requested corev1.Binding
	if err := json.Unmarshal(req.Object.Raw, &requested); err != nil {
		return admission.Denied(err.Error())
	}
	if requested.UID == "" || requested.UID != pod.UID {
		return admission.Denied("managed Pod binding must name the observed Pod UID")
	}
	if hasGate(&pod) {
		return admission.Denied("managed Pod is still scheduling gated")
	}
	return admission.Allowed("")
}

var _ admission.Handler = (*WorkloadAdmission)(nil)
