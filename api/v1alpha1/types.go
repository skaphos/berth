package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BerthLeaseSpec declares the desired state for a Berth lease.
type BerthLeaseSpec struct {
	// LeaseName is the unique identifier for this lease within its namespace.
	LeaseName string `json:"leaseName"`

	// HolderIdentity identifies the entity requesting or holding the lease.
	HolderIdentity string `json:"holderIdentity"`

	// TTLSeconds is the time-to-live for the lease in seconds. The lease
	// expires if not renewed within this duration.
	TTLSeconds int32 `json:"ttlSeconds"`

	// HeartbeatIntervalSeconds is the interval at which the holder must
	// renew the lease to prevent TTL expiration.
	HeartbeatIntervalSeconds int32 `json:"heartbeatIntervalSeconds"`

	// Semantics controls the lease acquisition mode: "at-most-once"
	// guarantees exclusive access, "at-least-once" permits concurrent holders.
	// +kubebuilder:validation:Enum=at-most-once;at-least-once
	Semantics string `json:"semantics"`

	// Target is an optional reference to a workload the operator manages
	// in response to lease state transitions.
	Target *TargetRef `json:"target,omitempty"`

	// AcquireAction defines the action applied to the target workload when
	// the lease is acquired.
	AcquireAction *LeaseAction `json:"acquireAction,omitempty"`

	// ReleaseAction defines the action applied to the target workload when
	// the lease is released or expires.
	ReleaseAction *LeaseAction `json:"releaseAction,omitempty"`
}

// TargetRef identifies a workload target managed by the operator in response
// to lease state transitions. All three fields are required.
type TargetRef struct {
	// APIVersion is the API group and version of the target resource (e.g. "apps/v1").
	APIVersion string `json:"apiVersion"`
	// Kind is the resource kind of the target (e.g. "Deployment").
	Kind string `json:"kind"`
	// Name is the name of the target resource in the same namespace as the lease.
	Name string `json:"name"`
}

// LeaseAction describes an action the operator may take on a target workload
// when a lease state transition occurs. At most one of Suspend or Scale may
// be set on a single action.
// +kubebuilder:validation:XValidation:rule="!(has(self.suspend) && has(self.scale))",message="at most one of suspend or scale may be set"
type LeaseAction struct {
	// Suspend, when non-nil, sets the suspend field on the target workload.
	// Setting this to true pauses the workload; false resumes it. Applies to
	// workload kinds that expose a spec.suspend field, such as CronJob.
	Suspend *bool `json:"suspend,omitempty"`

	// Scale, when non-nil, sets the replica count on the target workload's
	// scale subresource. Applies to workload kinds that expose a scale
	// subresource, such as Deployment, StatefulSet, and ReplicaSet.
	Scale *ScaleAction `json:"scale,omitempty"`
}

// ScaleAction sets the replica count on the target workload's scale
// subresource.
type ScaleAction struct {
	// Replicas is the desired replica count. Use 0 to scale a workload down
	// to zero (for example, on lease release).
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`
}

// BerthLeaseStatus reports the observed state of a Berth lease.
type BerthLeaseStatus struct {
	// LeaseState is the current state of the lease (e.g. "held", "released", "expired").
	LeaseState string `json:"leaseState,omitempty"`

	// CurrentHolder is the identity of the entity currently holding the lease.
	CurrentHolder string `json:"currentHolder,omitempty"`

	// Tenant is the resolved tenant identifier for the current holder.
	Tenant string `json:"tenant,omitempty"`

	// AcquiredAt is the timestamp when the lease was last acquired.
	AcquiredAt *metav1.Time `json:"acquiredAt,omitempty"`

	// ExpiresAt is the timestamp when the lease will expire if not renewed.
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// LastHeartbeat is the timestamp of the most recent lease renewal.
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

	// Conditions represent the latest observations of the lease's state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// BerthLease is the schema for the BerthLease API.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=berthleases,scope=Namespaced
type BerthLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BerthLeaseSpec   `json:"spec,omitempty"`
	Status BerthLeaseStatus `json:"status,omitempty"`
}

// BerthLeaseList contains a list of BerthLease resources.
// +kubebuilder:object:root=true
type BerthLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BerthLease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BerthLease{}, &BerthLeaseList{})
}
