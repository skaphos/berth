// Package v1alpha1 defines the Kubernetes custom resource types for the Berth
// lease coordination API.
//
// The primary resource is [BerthLease], which represents a distributed lease
// that coordinates workload access across Kubernetes clusters. Each lease
// tracks a holder identity, TTL-based expiration, heartbeat intervals, and
// optional workload targeting for operator-driven suspend/resume actions.
//
// # Lease Semantics
//
// BerthLease supports two acquisition semantics via the Semantics field:
//
//   - "at-most-once": guarantees that at most one holder can acquire the lease
//     at any given time. Suitable for leader election and exclusive resource access.
//   - "at-least-once": allows multiple holders to acquire the lease concurrently.
//     Suitable for availability-oriented coordination where overlap is acceptable.
//
// # Workload Targeting
//
// A lease may optionally reference a target workload via [TargetRef]. When
// specified, the Berth operator applies [LeaseAction] operations (such as
// suspending or resuming) to the target workload in response to lease state
// transitions.
//
// # Registration
//
// All types in this package are registered with the controller-runtime scheme
// via [SchemeBuilder] and [AddToScheme]. The API group is "berth.skaphos.io".
//
//	utilruntime.Must(v1alpha1.AddToScheme(scheme))
//
// +kubebuilder:object:generate=true
// +groupName=berth.skaphos.io
package v1alpha1
