package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies the API group and version for Berth resources.
	GroupVersion = schema.GroupVersion{Group: "berth.skaphos.io", Version: "v1alpha1"}

	// SchemeBuilder registers Berth API types with a runtime.Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the Berth API types to the supplied runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
