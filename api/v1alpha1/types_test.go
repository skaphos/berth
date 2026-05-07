package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToSchemeRegistersBerthLeaseTypes(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	kinds, _, err := scheme.ObjectKinds(&BerthLease{})
	if err != nil {
		t.Fatalf("ObjectKinds() error = %v", err)
	}
	if len(kinds) != 1 {
		t.Fatalf("kinds length = %d, want 1", len(kinds))
	}
	if kinds[0].Group != GroupVersion.Group || kinds[0].Version != GroupVersion.Version || kinds[0].Kind != "BerthLease" {
		t.Fatalf("kind = %+v, want %s/%s BerthLease", kinds[0], GroupVersion.Group, GroupVersion.Version)
	}
}

func TestBerthLeaseDeepCopyCopiesNestedState(t *testing.T) {
	t.Parallel()

	suspend := true
	acquiredAt := metav1.NewTime(time.Now().UTC())
	expiresAt := metav1.NewTime(time.Now().Add(time.Minute).UTC())
	heartbeat := metav1.NewTime(time.Now().Add(2 * time.Minute).UTC())

	lease := &BerthLease{
		TypeMeta: metav1.TypeMeta{APIVersion: GroupVersion.String(), Kind: "BerthLease"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example",
			Namespace: "default",
			Labels:    map[string]string{"app": "berth"},
		},
		Spec: BerthLeaseSpec{
			LeaseName:                "lease-a",
			HolderIdentity:           "holder-a",
			TTLSeconds:               30,
			HeartbeatIntervalSeconds: 10,
			Semantics:                "at-most-once",
			Target:                   &TargetRef{APIVersion: "batch/v1", Kind: "CronJob", Name: "job-a"},
			AcquireAction:            &LeaseAction{Suspend: &suspend},
			ReleaseAction:            &LeaseAction{Suspend: &suspend},
		},
		Status: BerthLeaseStatus{
			LeaseState:    "acquired",
			CurrentHolder: "holder-a",
			Tenant:        "tenant-a",
			AcquiredAt:    &acquiredAt,
			ExpiresAt:     &expiresAt,
			LastHeartbeat: &heartbeat,
			Conditions: []metav1.Condition{{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
			}},
		},
	}

	copy := lease.DeepCopy()
	if copy == nil {
		t.Fatal("DeepCopy() returned nil")
	}
	if copy == lease {
		t.Fatal("DeepCopy() returned the same pointer")
	}

	copy.Labels["app"] = "changed"
	copy.Spec.Target.Name = "job-b"
	copy.Status.Conditions[0].Type = "Changed"
	*copy.Spec.AcquireAction.Suspend = false

	if lease.Labels["app"] != "berth" {
		t.Fatal("labels were not deeply copied")
	}
	if lease.Spec.Target.Name != "job-a" {
		t.Fatal("target was not deeply copied")
	}
	if lease.Status.Conditions[0].Type != "Ready" {
		t.Fatal("conditions were not deeply copied")
	}
	if !*lease.Spec.AcquireAction.Suspend {
		t.Fatal("lease action was not deeply copied")
	}

	object := lease.DeepCopyObject()
	if _, ok := object.(*BerthLease); !ok {
		t.Fatalf("DeepCopyObject() type = %T, want *BerthLease", object)
	}
}

func TestBerthLeaseListDeepCopyCopiesItems(t *testing.T) {
	t.Parallel()

	list := &BerthLeaseList{
		Items: []BerthLease{{
			ObjectMeta: metav1.ObjectMeta{Name: "lease-a"},
		}},
	}

	copy := list.DeepCopy()
	if copy == nil {
		t.Fatal("DeepCopy() returned nil")
	}
	copy.Items[0].Name = "lease-b"
	if list.Items[0].Name != "lease-a" {
		t.Fatal("items were not deeply copied")
	}

	object := list.DeepCopyObject()
	if _, ok := object.(*BerthLeaseList); !ok {
		t.Fatalf("DeepCopyObject() type = %T, want *BerthLeaseList", object)
	}
}

func TestAdditionalDeepCopyHelpers(t *testing.T) {
	t.Parallel()

	boolValue := true
	spec := &BerthLeaseSpec{
		LeaseName:      "lease-a",
		HolderIdentity: "holder-a",
		Semantics:      "at-most-once",
		Target:         &TargetRef{APIVersion: "batch/v1", Kind: "CronJob", Name: "job-a"},
		AcquireAction:  &LeaseAction{Suspend: &boolValue},
		ReleaseAction:  &LeaseAction{Suspend: &boolValue},
	}

	specCopy := spec.DeepCopy()
	if specCopy == nil {
		t.Fatal("spec DeepCopy() returned nil")
	}
	specCopy.Target.Name = "job-b"
	if spec.Target.Name != "job-a" {
		t.Fatal("spec target was not deeply copied")
	}

	timestamp := metav1.NewTime(time.Now().UTC())
	status := &BerthLeaseStatus{
		AcquiredAt:    &timestamp,
		ExpiresAt:     &timestamp,
		LastHeartbeat: &timestamp,
		Conditions: []metav1.Condition{{
			Type: "Ready",
		}},
	}

	statusCopy := status.DeepCopy()
	if statusCopy == nil {
		t.Fatal("status DeepCopy() returned nil")
	}
	statusCopy.Conditions[0].Type = "Changed"
	if status.Conditions[0].Type != "Ready" {
		t.Fatal("status conditions were not deeply copied")
	}

	action := &LeaseAction{Suspend: &boolValue}
	actionCopy := action.DeepCopy()
	if actionCopy == nil {
		t.Fatal("action DeepCopy() returned nil")
	}
	*actionCopy.Suspend = false
	if !*action.Suspend {
		t.Fatal("lease action suspend was not deeply copied")
	}

	scaleAction := &LeaseAction{Scale: &ScaleAction{Replicas: 3}}
	scaleCopy := scaleAction.DeepCopy()
	if scaleCopy == nil {
		t.Fatal("scale action DeepCopy() returned nil")
	}
	scaleCopy.Scale.Replicas = 0
	if scaleAction.Scale.Replicas != 3 {
		t.Fatal("lease action scale was not deeply copied")
	}
	if (*ScaleAction)(nil).DeepCopy() != nil {
		t.Fatal("nil scale action DeepCopy() should return nil")
	}

	target := &TargetRef{APIVersion: "batch/v1", Kind: "CronJob", Name: "job-a"}
	targetCopy := target.DeepCopy()
	if targetCopy == nil {
		t.Fatal("target DeepCopy() returned nil")
	}
	targetCopy.Name = "job-b"
	if target.Name != "job-a" {
		t.Fatal("target ref was not copied")
	}

	if (*BerthLeaseSpec)(nil).DeepCopy() != nil {
		t.Fatal("nil spec DeepCopy() should return nil")
	}
	if (*BerthLeaseStatus)(nil).DeepCopy() != nil {
		t.Fatal("nil status DeepCopy() should return nil")
	}
	if (*LeaseAction)(nil).DeepCopy() != nil {
		t.Fatal("nil action DeepCopy() should return nil")
	}
	if (*TargetRef)(nil).DeepCopy() != nil {
		t.Fatal("nil target DeepCopy() should return nil")
	}
}
