// Package v1alpha1 defines the Kubernetes custom resource types for the Berth
// lease coordination API.
//
// The primary resource is [BerthLease], which represents a distributed lease
// that coordinates workload access across Kubernetes clusters. Each lease
// tracks a holder identity, TTL-based expiration, heartbeat intervals, and
// optional workload targeting for operator-driven suspend/resume actions.
//
// Lease semantics are selected with the Semantics field. The CRD currently
// accepts "at-most-once" and "at-least-once", but the central API server
// enforces exclusive holder behavior today; "at-least-once" is reserved for a
// future lease-window behavior.
//
// Workload targeting:
//
// A lease may optionally reference a target workload via [TargetRef]. When
// specified, the Berth operator applies [LeaseAction] operations (such as
// suspending or resuming) to the target workload in response to lease state
// transitions.
//
// Registration:
//
// All types in this package are registered with the controller-runtime scheme
// via [SchemeBuilder] and [AddToScheme]. The API group is "berth.skaphos.io".
//
//	utilruntime.Must(v1alpha1.AddToScheme(scheme))
//
// +kubebuilder:object:generate=true
// +groupName=berth.skaphos.io
package v1alpha1
