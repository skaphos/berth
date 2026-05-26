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

// The environment-variable names the injected helper reads are owned by
// package acquire (internal/acquire/env.go), which is the consumer side of
// this contract. The webhook references acquire.Env* directly so the two
// cannot drift; do not redeclare them here.
