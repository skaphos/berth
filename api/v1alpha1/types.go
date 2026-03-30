package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BerthLeaseSpec declares the desired state for a Berth lease.
type BerthLeaseSpec struct {
	LeaseName string `json:"leaseName"`

	HolderIdentity string `json:"holderIdentity"`

	TTLSeconds int32 `json:"ttlSeconds"`

	HeartbeatIntervalSeconds int32 `json:"heartbeatIntervalSeconds"`

	// +kubebuilder:validation:Enum=at-most-once;at-least-once
	Semantics string `json:"semantics"`

	Target *TargetRef `json:"target,omitempty"`

	AcquireAction *LeaseAction `json:"acquireAction,omitempty"`

	ReleaseAction *LeaseAction `json:"releaseAction,omitempty"`
}

// TargetRef identifies an optional workload target managed by the operator.
type TargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// LeaseAction describes an action the operator may take on a target workload.
type LeaseAction struct {
	Suspend *bool `json:"suspend,omitempty"`
}

// BerthLeaseStatus reports the observed state of a Berth lease.
type BerthLeaseStatus struct {
	LeaseState string `json:"leaseState,omitempty"`

	CurrentHolder string `json:"currentHolder,omitempty"`

	Tenant string `json:"tenant,omitempty"`

	AcquiredAt *metav1.Time `json:"acquiredAt,omitempty"`

	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`

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
