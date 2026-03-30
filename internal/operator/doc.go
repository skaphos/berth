// Package operator implements the Kubernetes controller for Berth lease
// resources.
//
// [BerthLeaseReconciler] watches for BerthLease custom resources and
// reconciles their state. It is built on controller-runtime and registers
// itself with a [ctrl.Manager] via [BerthLeaseReconciler.SetupWithManager].
//
// The reconciler is responsible for observing lease state transitions and,
// in future phases, applying workload actions (suspend/resume) to target
// resources referenced by the lease.
package operator
