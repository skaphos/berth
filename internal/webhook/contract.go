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
)

// Environment variable names the injected helper reads. These mirror the
// flag env-var defaults in cmd/berth-acquire/main.go; keep the two in sync.
const (
	EnvLeaseName      = "BERTH_LEASE_NAME"
	EnvLeaseNamespace = "BERTH_LEASE_NAMESPACE"
	EnvMode           = "BERTH_MODE"
	EnvEnforce        = "BERTH_ENFORCE"
	EnvTTLSeconds     = "BERTH_TTL_SECONDS"
	EnvHeartbeatSecs  = "BERTH_HEARTBEAT_SECONDS"
	EnvEnforceGrace   = "BERTH_ENFORCE_GRACE_SECONDS"
	EnvReleaseOnDown  = "BERTH_RELEASE_ON_SHUTDOWN"
	EnvHolderIdentity = "BERTH_HOLDER_IDENTITY"
	EnvClusterID      = "BERTH_CLUSTER_ID"
	EnvWorkloadKind   = "BERTH_WORKLOAD_KIND"
	EnvWorkloadName   = "BERTH_WORKLOAD_NAME"
	EnvStateDir       = "BERTH_STATE_DIR"
	EnvAPIServer      = "BERTH_API_SERVER"
	EnvAPIKeyFile     = "BERTH_API_KEY_FILE"
	EnvCABundleFile   = "BERTH_CA_BUNDLE_FILE"
	EnvServerName     = "BERTH_SERVER_NAME"
	EnvInsecure       = "BERTH_INSECURE_SKIP_TLS_VERIFY"

	// Downward-API env the helper uses for holder-identity defaulting.
	EnvPodNamespace = "POD_NAMESPACE"
	EnvPodName      = "POD_NAME"
)
