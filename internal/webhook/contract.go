// Package webhook implements the mutating admission webhook that injects
// the berth-acquire helper into opt-in workload pods (SKA-439). It is the
// label/annotation opt-in surface chosen in lieu of a CRD (ADR-0002) and
// the producer side of the contract the berth-acquire helper consumes.
package webhook

// Opt-in label. Standard workload controllers copy
// spec.template.metadata.labels onto created Pods, so this lives on the
// pod template (spec.jobTemplate.spec.template.metadata for CronJobs).
const (
	LabelInject        = "berth.skaphos.io/inject"
	InjectValueAcquire = "acquire"
)

// Annotation keys (behavior & configuration). All optional except
// AnnLeaseName. See the design doc "Proposed Label / Annotation Contract".
const (
	AnnLeaseName           = "berth.skaphos.io/lease-name"
	AnnLeaseNamespace      = "berth.skaphos.io/lease-namespace"
	AnnMode                = "berth.skaphos.io/mode"
	AnnHolderIdentity      = "berth.skaphos.io/holder-identity"
	AnnTTLSeconds          = "berth.skaphos.io/ttl-seconds"
	AnnHeartbeatSeconds    = "berth.skaphos.io/heartbeat-interval-seconds"
	AnnReleaseOnShutdown   = "berth.skaphos.io/release-on-shutdown"
	AnnEnforce             = "berth.skaphos.io/enforce"
	AnnEnforceGraceSeconds = "berth.skaphos.io/enforce-grace-seconds"
	// AnnSignalTarget scopes enforce=signal to the workload process, matched by
	// comm or executable basename (e.g. "nginx"). Without it the sidecar signals
	// every process in the shared PID namespace except berth's own, which can
	// terminate co-located sidecars on lease loss.
	AnnSignalTarget = "berth.skaphos.io/signal-target"

	// AnnInjected is set on a Pod once it has been mutated, so re-admission
	// of an already-injected Pod is a no-op (idempotency).
	AnnInjected = "berth.skaphos.io/injected"
)

// Names of the resources the webhook injects.
const (
	VolumeName           = "berth-state"
	InitContainerName    = "berth-acquire"
	SidecarContainerName = "berth-sidecar"

	// AuthTokenVolume / AuthCABundleVolume mount the bearer token and CA
	// bundle into the injected helper containers when those sources are
	// configured (SKA-444).
	AuthTokenVolume    = "berth-auth-token"
	AuthCABundleVolume = "berth-auth-ca"
)

// RejectReason identifies why admission refused a pod. It is both the
// classification carried in the error and the metric label, so the
// synchronous explanation to the submitter and the fleet-wide count stay
// described in the same vocabulary.
type RejectReason string

const (
	// ReasonWritableStateMount: a workload-authored container mounts the
	// state volume writably. The state volume is reserved — a writable
	// mount lets the workload forge the health marker and replace the
	// probe's check binary, defeating at-most-once enforcement.
	ReasonWritableStateMount RejectReason = "writable_state_mount"

	// ReasonWritableStateMountEphemeral: the same, arriving through the
	// pods/ephemeralcontainers subresource rather than pod creation. Kept
	// distinct because the operator experience differs — the running pod
	// is healthy and a debug request was refused.
	ReasonWritableStateMountEphemeral RejectReason = "writable_state_mount_ephemeral"
)

// The environment-variable names the injected helper reads are owned by
// package acquire (internal/acquire/env.go), which is the consumer side of
// this contract. The webhook references acquire.Env* directly so the two
// cannot drift; do not redeclare them here.
