package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion identifies the API group and version for Berth resources.
	GroupVersion = schema.GroupVersion{Group: "berth.skaphos.io", Version: "v1alpha1"}

	// SchemeBuilder registers Berth API types with a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the Berth API types to the supplied runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &BerthLease{}, &BerthLeaseList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
