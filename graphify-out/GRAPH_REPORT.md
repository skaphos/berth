# Graph Report - berth  (2026-09-19)

## Corpus Check
- 249 files · ~224,293 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 2371 nodes · 5083 edges · 160 communities (134 shown, 13 thin omitted)
- Extraction: 88% EXTRACTED · 12% INFERRED · 0% AMBIGUOUS · INFERRED: 601 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `7452a241`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- Berth overview and concepts
- Operator CLI config parsing
- Go client and TLS options
- Acquire state directory markers
- Pod injector mutation logic
- Operator reconciler tests
- Webhook injection tests
- berth-acquire CLI tests
- Injector container helper tests
- Generated deepcopy API code
- Lease HTTP handlers
- Lease manager tests
- API and lease test harness
- API metrics middleware
- Lease client fakes
- API server bootstrap
- Authenticator implementations and tests
- Load harness scenarios
- Contributor guidelines and workflow
- apiserver store values schema
- SQL lease store
- End-to-end cluster tests
- Operator lease client
- apiserver chart values schema
- operator chart values schema
- Acquire enforcement engine
- Load result summarization
- Speckit bash scripts
- OIDC broker token loop
- Prometheus metrics registry
- Speckit tasks template
- Speckit analyze skill
- Lease renewal tests
- Lease manager and store core
- State-volume trust contracts
- Kubernetes lease store
- operator metrics values schema
- Load harness in-process tests
- Kubernetes client construction
- API auth middleware
- Workload gating design ADRs
- Kubernetes store unit tests
- Acquire and injector config
- File token source tests
- Acquire environment config
- operator image values schema
- Lease renewer loop
- Operator admission and reconcile
- Speckit converge skill
- apiserver image values schema
- operator gating defaults schema
- State volume trust boundary
- Berth project constitution
- apiserver service values schema
- operator sidecar broker schema
- Marker freshness evaluation
- Client test doubles
- apiserver OIDC values schema
- operator injection values schema
- Shared unit test fixtures
- API request observability context
- apiserver ServiceMonitor schema
- operator ServiceMonitor schema
- operator webhook policy schema
- Lease record store backends
- Speckit spec template
- Lease fencing safety spec
- State-volume trust tasks
- Metrics package tests
- Speckit plan skill
- Speckit specify skill
- Speckit tasks skill
- operator RBAC values schema
- Webhook rejection contract
- Acquire hold and state tests
- Metrics store wrapper
- Speckit constitution template
- Lease fencing safety tasks
- State-volume trust spec
- Acquire hold loop
- apiserver schema root
- apiserver cert-manager schema
- operator TLS values schema
- Pod-level gating model
- Architecture and code map docs
- Kubernetes store conflict retries
- Load harness config tests
- Lease fencing research
- State-volume trust plan
- Console HTTP server
- apiserver service account schema
- apiserver auth values schema
- apiserver client QPS schema
- operator Berth client schema
- berth-acquire CLI entrypoint
- Load harness configuration
- Speckit plan template
- Speckit checklist skill
- apiserver credentials schema
- operator schema root
- BerthLease API reference
- API routes and readiness
- Lease fencing plan
- Speckit clarify skill
- Speckit implement skill
- apiserver TLS values schema
- apiserver PodDisruptionBudget schema
- operator service account schema
- Managed pod registration ADR
- Configuration and storage docs
- Runtime enforcement ADRs
- Release and docs workflow
- Operator chart metrics tests
- State-volume trust research
- Speckit constitution skill
- Signal enforcement mode
- apiserver Helm notes
- Security policy
- Lease fencing quality checklist
- Lease fencing data model
- Lease fencing validation quickstart
- State-volume trust data model
- State-volume delivery strategy
- Load fixture bring-up
- Speckit tasks-to-issues skill
- apiserver coordination schema
- Kubernetes store test interceptors
- Release 0.4.1 notes
- Speckit checklist template
- Lease store contract
- State-volume user scenarios
- E2E fixture bring-up
- apiserver extra args schema
- Chart packaging script
- Image release script
- Coverage threshold script
- HTTP API contract
- State-volume task dependencies
- Release notes script
- berth CLI entrypoint
- Release notes test script
- Dead sidecar user story
- Load fixture run script
- E2E fixture teardown
- Load fixture teardown
- Go module definition
- Tools module definition

## God Nodes (most connected - your core abstractions)
1. `testInjector()` - 54 edges
2. `optInPod()` - 53 edges
3. `NewMemStore()` - 45 edges
4. `Record` - 42 edges
5. `NewMux()` - 39 edges
6. `NewManager()` - 39 edges
7. `newDeployment()` - 33 edges
8. `newScheme()` - 31 edges
9. `newTestRenewer()` - 29 edges
10. `reconcile()` - 27 edges

## Surprising Connections (you probably didn't know these)
- `Operator leader election (single active replica)` --semantically_similar_to--> `Lease concept (named, time-bounded, single-holder claim)`  [INFERRED] [semantically similar]
  deploy/helm/berth-operator/templates/NOTES.txt → docs/concepts.md
- `Build/test/dev task commands` --semantically_similar_to--> `Local validation tasks`  [INFERRED] [semantically similar]
  AGENTS.md → CONTRIBUTING.md
- `Project structure and module organization` --semantically_similar_to--> `CLAUDE.md repository guidelines`  [INFERRED] [semantically similar]
  AGENTS.md → CLAUDE.md
- `TestRunRejectsInvalidStartup()` --calls--> `run()`  [INFERRED]
  cmd/berth-oidc-broker/main_test.go → test/load/main.go
- `Operator leader election (single active replica)` --conceptually_related_to--> `Operator flags`  [INFERRED]
  deploy/helm/berth-operator/templates/NOTES.txt → docs/reference/configuration.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Runtime-singleton at-most-once enforcement path** — docs_workload_gating_injection_runtime_singleton, docs_workload_gating_injection_probe, docs_workload_gating_injection_signal, docs_workload_gating_injection_berth_acquire, docs_adr_0003_sidecar_runtime_enforcement_by_container_kill_adr [EXTRACTED 1.00]
- **Phased scalability and load-testing program** — docs_operations_scalability_sizing_model, docs_operations_scalability_phase1_store_benchmarks, docs_operations_scalability_phase2_metrics, docs_operations_scalability_phase3_load_driver, test_load_fixtures_readme_load_harness, docs_operations_benchmarks_mem_benchmark, docs_operations_benchmarks_sqlite_benchmark [EXTRACTED 1.00]
- **Tag-driven release pipeline (release PR to tag to artifacts to Linear)** — release_release_please, release_release_workflow, release_docs_workflow, docs_release_and_issue_workflow_linear_release_yml [EXTRACTED 1.00]

## Communities (160 total, 13 thin omitted)

### Community 0 - "Berth overview and concepts"
Cohesion: 0.17
Nodes (17): Operator leader election (single active replica), berth-operator Helm install NOTES, Lease concept (named, time-bounded, single-holder claim), Getting Started three-cluster failover tutorial, OIDC broker flags, Fixtures layout, Berth e2e harness, Intentionally non-production-grade choices (+9 more)

### Community 1 - "Operator CLI config parsing"
Cohesion: 0.27
Nodes (13): main(), parseConfig(), splitCSV(), newTestFlagSet(), TestManagedAdmissionRequiresOperatorIdentity(), TestParseConfigInjectorMapping(), TestParseConfigMinimalDefaults(), TestParseConfigRejectsMutuallyExclusiveKeys() (+5 more)

### Community 2 - "Go client and TLS options"
Cohesion: 0.09
Nodes (34): Option, run(), crypto/tls.Config, net/http.Client, sigs.k8s.io/controller-runtime.Manager, LoadTLSConfig(), generateTestCAPEM(), TestLoadTLSConfigEmpty() (+26 more)

### Community 3 - "Acquire state directory markers"
Cohesion: 0.21
Nodes (4): State, os.FileMode, copyFile(), writeFileAtomic()

### Community 4 - "Pod injector mutation logic"
Cohesion: 0.15
Nodes (14): TestLoopConfigValidate(), k8s.io/api/core/v1.EnvVar, k8s.io/api/core/v1.EnvVarSource, k8s.io/api/core/v1.Probe, appendIf(), controllerOwner(), fieldRef(), PodInjector (+6 more)

### Community 5 - "Operator reconciler tests"
Cohesion: 0.07
Nodes (88): addKnownTypes(), LeaseAction, TargetRef, k8s.io/api/admission/v1.Operation, k8s.io/api/apps/v1.Deployment, k8s.io/api/apps/v1.ReplicaSet, k8s.io/api/batch/v1.CronJob, k8s.io/apimachinery/pkg/runtime.Scheme (+80 more)

### Community 6 - "Webhook injection tests"
Cohesion: 0.10
Nodes (46): TestInjectRejectsContainerNameCollision(), TestInjectRejectsExistingLivenessProbe(), TestInjectRejectsForeignMountAtStateDir(), TestInjectRejectsNonEmptyDirStateVolume(), TestInjectRejectsWritableExistingStateMountAtOtherPath(), TestStartupGatePrependsAheadOfExistingInitContainers(), TestFinalAdmissionRejectsLateInitInjection(), TestFinalAdmissionSkipsUnmanagedAndControlPlanePods() (+38 more)

### Community 7 - "berth-acquire CLI tests"
Cohesion: 0.14
Nodes (22): markerAged(), TestCheckAbsentMarkerFailsWithDistinctReason(), TestCheckFreshMarkerPasses(), TestCheckNeedsNoEnvironmentOrConfig(), TestCheckStaleMarkerFailsWithReason(), TestCheckWithoutMaxAgeIsPresenceOnly(), envFrom(), TestConfigFlagOverridesEnv() (+14 more)

### Community 8 - "Injector container helper tests"
Cohesion: 0.15
Nodes (22): k8s.io/api/core/v1.Container, findVolume(), TestInjectAcceptsExistingEmptyDirStateVolume(), TestInjectAddsStateMountWhenExistingMountPathDiffers(), TestInjectForcesExistingStateMountReadOnly(), TestInjectMountsAuthSourcesIntoHelpers(), TestInjectNoAuthMountsWhenUnset(), TestInjectorConfigValidate() (+14 more)

### Community 9 - "Generated deepcopy API code"
Cohesion: 0.09
Nodes (10): BerthLease, BerthLeaseList, BerthLeaseSpec, BerthLeaseStatus, LeaseAction, PermittedPod, ScaleAction, TargetRef (+2 more)

### Community 10 - "Lease HTTP handlers"
Cohesion: 0.26
Nodes (23): AcquireRequest, errorResponse, LeaseManager, LeaseResponse, ReleaseRequest, RenewRequest, net/http.HandlerFunc, net/http.Request (+15 more)

### Community 11 - "Lease manager tests"
Cohesion: 0.19
Nodes (20): Store, newTestManager(), TestAcquireFreshLease(), TestAcquireReclaimsAfterExpiryAndBumpsToken(), TestAcquireRejectsAtFencingTokenCeiling(), TestAcquireRejectsCeilingFromTombstone(), TestAcquireRejectsEmptyHolderAndZeroTTL(), TestAcquireSameHolderIsRenewalNoTokenBump() (+12 more)

### Community 12 - "API and lease test harness"
Cohesion: 0.05
Nodes (81): Client, bytes.Buffer, log/slog.Logger, net/http/httptest.Server, net/http.Response, newAuthzServer(), postBearer(), TestAuthzAllowsInTenantHolderAcrossNamespaces() (+73 more)

### Community 13 - "API metrics middleware"
Cohesion: 0.27
Nodes (6): RequestMetrics, statusRecorder, MetricsMiddleware(), TestMetricsMiddlewareLabelsUnmatchedRoute(), TestMetricsMiddlewareRecordsMatchedRoute(), TestStatusRecorderDefaultsTo200OnBodyOnlyWrite()

### Community 14 - "Lease client fakes"
Cohesion: 0.16
Nodes (8): fakeClient, acquireCall, blockingAcquireClient, fakeLeaseClient, releaseCall, AcquireResult, Client, leasePath()

### Community 15 - "API server bootstrap"
Cohesion: 0.07
Nodes (48): Option, Server, oidcConfig, storeConfig, nonEmptyLines(), TestNewLogHandlerRejectsUnknownFormat(), TestParseLogLevel(), TestSetupLoggingJSON() (+40 more)

### Community 16 - "Authenticator implementations and tests"
Cohesion: 0.06
Nodes (54): fakeAuthenticator, NoOpAuthenticator, OIDCAuthenticator, OIDCConfig, StaticAuthenticator, testIssuer, crypto/rsa.PrivateKey, crypto/rsa.PublicKey (+46 more)

### Community 17 - "Load harness scenarios"
Cohesion: 0.35
Nodes (19): probeEnforcer, acquireResult, context.Context, leaseName(), forEachLease(), forEachLeaseBounded(), Config, LeaseClient (+11 more)

### Community 18 - "Contributor guidelines and workflow"
Cohesion: 0.22
Nodes (10): Build/test/dev task commands, Chart version bump rule, CLI vs Linux runtime split, Project structure and module organization, CLAUDE.md repository guidelines, DCO sign-off and signed commits, Generated artifacts kept in sync, Local validation tasks (+2 more)

### Community 19 - "apiserver store values schema"
Cohesion: 0.09
Nodes (23): enum, type, enum, type, type, properties, required, type (+15 more)

### Community 20 - "SQL lease store"
Cohesion: 0.06
Nodes (57): database/sql.DB, database/sql.TxOptions, testing.B, testing.TB, TestK8sStoreSafetyRegressions(), TestMemStoreConformance(), BenchmarkMemStore(), dialectFor() (+49 more)

### Community 21 - "End-to-end cluster tests"
Cohesion: 0.09
Nodes (45): clusterRef, k8s.io/api/core/v1.Pod, sigs.k8s.io/controller-runtime/pkg/client.Client, sigs.k8s.io/controller-runtime/pkg/client.ObjectKey, sigs.k8s.io/controller-runtime/pkg/webhook/admission.Warnings, testing.M, TestMain(), PodInjector (+37 more)

### Community 23 - "apiserver chart values schema"
Cohesion: 0.06
Nodes (34): type, type, type, type, type, type, type, type (+26 more)

### Community 24 - "operator chart values schema"
Cohesion: 0.05
Nodes (37): type, type, type, type, type, type, type, type (+29 more)

### Community 25 - "Acquire enforcement engine"
Cohesion: 0.16
Nodes (14): Enforcer, procScanner, Config, matchesTarget(), newEnforcer(), newSignalEnforcer(), osProcScanner(), fakeScanner() (+6 more)

### Community 26 - "Load result summarization"
Cohesion: 0.23
Nodes (9): recordingMetrics, time.Duration, Config, ms(), percentile(), resultFor(), OpResult, opSamples (+1 more)

### Community 27 - "Speckit bash scripts"
Cohesion: 0.13
Nodes (25): check-prerequisites.sh script, check_dir(), check_file(), find_specify_root(), format_speckit_command(), get_current_branch(), get_feature_paths(), get_invoke_separator() (+17 more)

### Community 28 - "OIDC broker token loop"
Cohesion: 0.11
Nodes (31): loopConfig, fetchToken(), main(), nextRefresh(), nextRetry(), parseScopes(), resolveSecret(), resolveTokenURL() (+23 more)

### Community 29 - "Prometheus metrics registry"
Cohesion: 0.14
Nodes (7): github.com/prometheus/client_golang/prometheus.CounterVec, github.com/prometheus/client_golang/prometheus.Gauge, github.com/prometheus/client_golang/prometheus.HistogramVec, github.com/prometheus/client_golang/prometheus.Registry, Metrics, promMetrics, newPromMetrics()

### Community 30 - "Speckit tasks template"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 31 - "Speckit analyze skill"
Cohesion: 0.08
Nodes (25): 1. Initialize Analysis Context, 2. Load Artifacts (Progressive Disclosure), 3. Build Semantic Models, 4. Detection Passes (Token-Efficient Analysis), 5. Severity Assignment, 6. Produce Compact Analysis Report, 7. Provide Next Actions, 8. Offer Remediation (+17 more)

### Community 32 - "Lease renewal tests"
Cohesion: 0.12
Nodes (31): TestDelayedSuccessCannotReopenExpiredLease(), TestHungRenewStopsAtLocalDeadline(), TestLeaseDeadlineUsesLocalRequestStart(), TestRunFencesBetweenHeartbeats(), TestSuccessfulRenewRefreshesTheMarker(), acquired(), heldByOther(), LeaseClient (+23 more)

### Community 33 - "Lease manager and store core"
Cohesion: 0.10
Nodes (14): failingManager, readyManager, database/sql.Tx, sync/atomic.Int64, time.Time, TestK8sLeaseNameInjectiveForValidKeys(), AcquireResult, Manager (+6 more)

### Community 34 - "State-volume trust contracts"
Cohesion: 0.09
Nodes (19): Compatibility, Contract: Admission, Decision table, Failure policy, Registered rules, Rejection message, Scope, Agreement with `State.IsHealthy()` (+11 more)

### Community 35 - "Kubernetes lease store"
Cohesion: 0.31
Nodes (12): k8s.io/client-go/kubernetes.Interface, k8s.io/client-go/rest.Config, apiStore(), envtestConfig(), interceptedStore(), readK8sRecord(), TestK8sConcurrentCASAPIStorage(), TestK8sMetadataConflictAPIStorage() (+4 more)

### Community 36 - "operator metrics values schema"
Cohesion: 0.11
Nodes (20): properties, type, type, type, properties, type, auth, bindAddress (+12 more)

### Community 37 - "Load harness in-process tests"
Cohesion: 0.47
Nodes (9): assertOp(), LeaseClient, keys(), newInProcessClient(), TestRunChurnInProcess(), TestRunColdStartInProcess(), TestRunFailoverInProcess(), TestRunSteadyInProcess() (+1 more)

### Community 38 - "Kubernetes client construction"
Cohesion: 0.31
Nodes (11): k8s.io/client-go/kubernetes.Clientset, buildConfig(), NewClientset(), TestBuildConfigAppliesRaisedDefaults(), TestBuildConfigHonorsExplicitOverrides(), TestBuildConfigInvalidKubeconfig(), TestBuildConfigNonPositiveFallsBackPerField(), TestNewClientsetBuildsWithValidKubeconfig() (+3 more)

### Community 39 - "API auth middleware"
Cohesion: 0.21
Nodes (14): identityCtxKey, AuthMiddleware(), bearerToken(), ChainMiddleware(), IdentityFromContext(), TestAuthMiddlewareRejectsAuthenticatorError(), TestAuthMiddlewareRejectsMissingHeader(), TestAuthMiddlewareRejectsNilIdentityWithoutError() (+6 more)

### Community 40 - "Workload gating design ADRs"
Cohesion: 0.12
Nodes (19): ADR-0001: Pod-level gating for injected singletons, Rejected alternative: hybrid helper-writes-status, ADR-0002: Opt into injection via labels/annotations, not a wrapper CRD, Rejected alternative: Manual injection, Rejected alternative: Wrapper CRD, Decision: label/annotation contract via mutating webhook, Rationale: native GitOps opt-in, no extra control loop, ADR Index (+11 more)

### Community 41 - "Kubernetes store unit tests"
Cohesion: 0.38
Nodes (12): newK8sStore(), sampleRecord(), TestK8sStoreEncodesNameAndAnnotates(), TestK8sStoreGetNotFound(), TestK8sStoreLegacyObjectReadsAsVersionOne(), TestK8sStoreListFiltersByLabel(), TestK8sStorePing(), TestK8sStorePutCASOnAbsentReturnsConflict() (+4 more)

### Community 42 - "Acquire and injector config"
Cohesion: 0.24
Nodes (6): k8s.io/api/core/v1.PullPolicy, Config, Enforce, Mode, InjectorConfig, validateMountableFile()

### Community 43 - "File token source tests"
Cohesion: 0.47
Nodes (8): NewFileTokenSource(), TestFileTokenSourcePicksUpRotationAfterTTL(), TestFileTokenSourceReturnsCachedOnReadError(), TestFileTokenSourceTrimsWhitespace(), TestNewFileTokenSourceFailsFastOnEmptyFile(), TestNewFileTokenSourceFailsFastOnMissingFile(), TestNewFileTokenSourceRequiresPath(), writeTokenFile()

### Community 44 - "Acquire environment config"
Cohesion: 0.38
Nodes (8): ConfigFromEnv(), Config, secondsEnv(), getter(), TestConfigFromEnvEmpty(), TestConfigFromEnvFull(), TestConfigFromEnvInvalid(), TestConfigFromEnvReleaseTrue()

### Community 45 - "operator image values schema"
Cohesion: 0.09
Nodes (25): pattern, type, properties, type, pattern, type, properties, type (+17 more)

### Community 46 - "Lease renewer loop"
Cohesion: 0.29
Nodes (5): Renewer, context.CancelFunc, Config, LeaseClient, NewRenewer()

### Community 47 - "Operator admission and reconcile"
Cohesion: 0.06
Nodes (55): BerthLease, BerthLeaseList, BerthLeaseSpec, BerthLeaseStatus, PermittedPod, ScaleAction, WorkloadStatus, github.com/go-logr/logr.Logger (+47 more)

### Community 48 - "Speckit converge skill"
Cohesion: 0.12
Nodes (15): 1. Initialize Convergence Context, 2. Load Artifacts (Progressive Disclosure), 3. Build the Intent Inventory, 4. Assess the Codebase and Classify Findings, 5. Assign Severity, 6. Present the In-Session Findings Summary, 7. Append Convergence Tasks (or report converged), 8. Provide Next Actions (Handoff) (+7 more)

### Community 49 - "apiserver image values schema"
Cohesion: 0.17
Nodes (12): properties, required, type, image, pullPolicy, repository, tag, enum (+4 more)

### Community 50 - "operator gating defaults schema"
Cohesion: 0.17
Nodes (12): properties, type, enum, type, enum, type, defaults, enforce (+4 more)

### Community 51 - "State volume trust boundary"
Cohesion: 0.12
Nodes (15): CHANGELOG managed by Release Please, 4. The shared state volume is a trust boundary, not shared scratch space, Consequences, Considered Options, Context and Problem Statement, Decision Drivers, Decision Outcome, Links (+7 more)

### Community 52 - "Berth project constitution"
Cohesion: 0.12
Nodes (15): Berth Constitution, Core Principles, Engineering Constraints, Governance, I. Explicit State Over Implicit Behavior, II. Git Is the Durable Desired-State Boundary, III. Deterministic, Reconstructible Operation, IV. Kubernetes-Native, Never Obscured (+7 more)

### Community 53 - "apiserver service values schema"
Cohesion: 0.15
Nodes (15): type, properties, type, properties, type, maximum, minimum, type (+7 more)

### Community 54 - "operator sidecar broker schema"
Cohesion: 0.14
Nodes (14): required, type, type, image, oidc, refreshSkew, resources, sidecarBroker (+6 more)

### Community 55 - "Marker freshness evaluation"
Cohesion: 0.23
Nodes (13): HealthResult, HealthVerdict, agedMarker(), TestEvaluateMarkerAbsentIsDistinctFromStale(), TestEvaluateMarkerBoundIsInclusive(), TestEvaluateMarkerFreshnessBoundary(), TestEvaluateMarkerIgnoresMarkerContents(), TestEvaluateMarkerUnreadableFailsClosed() (+5 more)

### Community 56 - "Client test doubles"
Cohesion: 0.24
Nodes (3): hangingClient, FileTokenSource, sync.Mutex

### Community 57 - "apiserver OIDC values schema"
Cohesion: 0.14
Nodes (14): type, type, type, properties, audience, issuerURL, jwksURL, requiredClaims (+6 more)

### Community 58 - "operator injection values schema"
Cohesion: 0.17
Nodes (12): items, type, items, type, properties, type, type, controlPlaneNamespaces (+4 more)

### Community 59 - "Shared unit test fixtures"
Cohesion: 0.07
Nodes (47): TestAdditionalDeepCopyHelpers(), TestAddToSchemeRegistersBerthLeaseTypes(), TestBerthLeaseDeepCopyCopiesNestedState(), TestBerthLeaseListDeepCopyCopiesItems(), testing.T, baseConfig(), Config, TestApplyDefaults() (+39 more)

### Community 60 - "API request observability context"
Cohesion: 0.23
Nodes (15): requestContext, requestContextKey, log/slog.Attr, attrsToLogAttrs(), ensureRequestContext(), isHex(), isSafeRequestID(), randomID() (+7 more)

### Community 61 - "apiserver ServiceMonitor schema"
Cohesion: 0.15
Nodes (13): type, type, type, additionalLabels, interval, metricRelabelings, relabelings, scrapeTimeout (+5 more)

### Community 62 - "operator ServiceMonitor schema"
Cohesion: 0.11
Nodes (19): type, type, type, type, type, bearerTokenFile, enabled, interval (+11 more)

### Community 63 - "operator webhook policy schema"
Cohesion: 0.12
Nodes (16): enum, type, type, type, failurePolicy, namespaceSelector, objectSelector, servicePort (+8 more)

### Community 64 - "Lease record store backends"
Cohesion: 0.18
Nodes (10): k8s.io/api/coordination/v1.Lease, applyRecordToLease(), leaseDuration(), leaseFromRecord(), recordFromLease(), versionFromLease(), Store, Record (+2 more)

### Community 65 - "Speckit spec template"
Cohesion: 0.15
Nodes (12): Assumptions, Edge Cases, Feature Specification: [FEATURE NAME], Functional Requirements, Key Entities *(include if feature involves data)*, Measurable Outcomes, Requirements *(mandatory)*, Success Criteria *(mandatory)* (+4 more)

### Community 66 - "Lease fencing safety spec"
Cohesion: 0.15
Nodes (13): Assumptions, Context, Edge Cases, Feature Specification: Lease Fencing and Isolation Safety, Functional Requirements, Key Entities, Measurable Outcomes, Requirements *(mandatory)* (+5 more)

### Community 67 - "State-volume trust tasks"
Cohesion: 0.15
Nodes (13): Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 3, Parallel Example: User Story 1, Path Conventions, Phase 1: Setup (Shared Infrastructure), Phase 2: Foundational (Blocking Prerequisites), Phase 3: User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) 🎯 MVP (+5 more)

### Community 68 - "Metrics package tests"
Cohesion: 0.33
Nodes (8): New(), TestHandlerExposesBerthSeries(), TestMetricsMiddlewareBoundsUnauthenticatedMethods(), TestObserveOutcomeCountsPerLabel(), TestObserveRequestBoundsMethodLabels(), TestObserveRequestCountsAndInflight(), TestServeShutsDownOnContextCancel(), TestWrapStoreIsTransparentAndRecords()

### Community 69 - "Speckit plan skill"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, Key rules, Mandatory Post-Execution Hooks, Outline, Phase 0: Outline & Research, Phase 1: Design & Contracts, Phases (+2 more)

### Community 70 - "Speckit specify skill"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, For AI Generation, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, Quick Guidelines, Section Requirements (+2 more)

### Community 71 - "Speckit tasks skill"
Cohesion: 0.18
Nodes (10): Checklist Format (REQUIRED), Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Phase Structure, Pre-Execution Checks, Task Generation Rules (+2 more)

### Community 72 - "operator RBAC values schema"
Cohesion: 0.11
Nodes (19): type, type, properties, type, type, berthLeaseVerbs, id, leaderElection (+11 more)

### Community 73 - "Webhook rejection contract"
Cohesion: 0.25
Nodes (4): k8s.io/api/core/v1.Volume, k8s.io/api/core/v1.VolumeMount, recordRejection(), RejectReason

### Community 74 - "Acquire hold and state tests"
Cohesion: 0.17
Nodes (14): Config, holdTestConfig(), TestHoldRetriesUntilAcquired(), TestHoldStopsOnContextCancel(), TestRestartGatesBeforeConfirmingHandoff(), TestWriteAcquiredLeavesMarkerFresh(), newHangingClient(), testLogger() (+6 more)

### Community 75 - "Metrics store wrapper"
Cohesion: 0.27
Nodes (4): TestOutcomeForClassifiesSentinels(), Metrics, outcomeFor(), meteredStore

### Community 76 - "Speckit constitution template"
Cohesion: 0.18
Nodes (10): Core Principles, Governance, [PRINCIPLE_1_NAME], [PRINCIPLE_2_NAME], [PRINCIPLE_3_NAME], [PRINCIPLE_4_NAME], [PRINCIPLE_5_NAME], [PROJECT_NAME] Constitution (+2 more)

### Community 77 - "Lease fencing safety tasks"
Cohesion: 0.18
Nodes (11): Dependencies, Implementation notes (deviations from the plan as written), Implementation strategy, Parallel opportunities, Phase 1: Setup, Phase 2: Foundational — version-CAS store contract (blocks US1, US2), Phase 3: User Story 1 — Failover never yields two holders (P1, #90), Phase 4: User Story 2 — Fencing tokens never repeat for a key (P2, #92 + #93) (+3 more)

### Community 78 - "State-volume trust spec"
Cohesion: 0.18
Nodes (11): Assumptions, Clarifications, Context, Feature Specification: State-Volume Trust and Marker Freshness, Functional Requirements, Key Entities, Measurable Outcomes, Requirements *(mandatory)* (+3 more)

### Community 79 - "Acquire hold loop"
Cohesion: 0.53
Nodes (4): LeaseClient, Config, Hold(), sleep()

### Community 80 - "apiserver schema root"
Cohesion: 0.40
Nodes (4): required, $schema, title, type

### Community 81 - "apiserver cert-manager schema"
Cohesion: 0.20
Nodes (10): properties, items, type, type, type, dnsNames, duration, issuerRef (+2 more)

### Community 82 - "operator TLS values schema"
Cohesion: 0.20
Nodes (10): properties, type, type, type, caBundleConfigMap, caBundleKey, insecureSkipVerify, serverName (+2 more)

### Community 83 - "Pod-level gating model"
Cohesion: 0.20
Nodes (10): Decision: pod-level gating (init hold + sidecar enforce), Rationale: helper in-pod vantage cannot scale, Active Enforcement (probe/signal), Injected berth-acquire helper, Native Sidecar (restartPolicy: Always), Pod-Level Gating activation model, Rationale: injected path is fallback due to larger fencing surface, Restart Re-Gating (+2 more)

### Community 84 - "Architecture and code map docs"
Cohesion: 0.12
Nodes (22): Tenant-scoped holder authorization, Lease API (acquire/renew/release endpoints), Lease model (holder, TTL, fencing token record), Observability (access log, RED metrics, lease outcomes counter), Operator reconcile flow (finalizer, acquire, actions, status), Why the operator does not watch target workloads, Code map (contributor ownership by path), API packages (internal/api: routes, leases, middleware) (+14 more)

### Community 85 - "Kubernetes store conflict retries"
Cohesion: 0.53
Nodes (8): k8s.io/client-go/kubernetes/fake.Clientset, renewalOf(), seedForConflict(), TestK8sStorePutConflictRetryBudgetIsBounded(), TestK8sStorePutConflictRetryHonoursCancellation(), TestK8sStorePutDoesNotRetryRealVersionChange(), TestK8sStorePutRetriesMetadataOnlyConflict(), k8sLeaseName()

### Community 86 - "Load harness config tests"
Cohesion: 0.28
Nodes (7): Config, TestConfigValidate(), TestLeaseNamingIsDistinctAndStable(), validConfig(), TestPercentileNearestRank(), TestRecorderHookInvokedConcurrently(), TestRecorderSummarize()

### Community 87 - "Lease fencing research"
Cohesion: 0.20
Nodes (10): D1 — CAS predicate: store-maintained per-record `Version`, D2 — Tombstones instead of deletion; `Store.Delete` removed, D3 — GC becomes a tombstoning sweep with a never-reused predicate, D4 — Token monotonicity semantics, D5 — Key validation at the API boundary, D6 — SQL schema migration (upgrade path, FR-011), D7 — Test strategy (FR-009), D8 — Documentation impact (FR-010) (+2 more)

### Community 88 - "State-volume trust plan"
Cohesion: 0.20
Nodes (10): Complexity Tracking, Constitution Check, Documentation (this feature), Gate finding: the security control is fail-open, Implementation Plan: State-Volume Trust and Marker Freshness, Post-Design Re-Check (after Phase 1), Project Structure, Source Code (repository root) (+2 more)

### Community 89 - "Console HTTP server"
Cohesion: 0.33
Nodes (6): serverConfig, Server, net/http.Handler, NewServer(), TestNewServerDefaultsHandler(), TestNewServerUsesProvidedHandler()

### Community 90 - "apiserver service account schema"
Cohesion: 0.22
Nodes (9): type, type, type, annotations, create, name, serviceAccount, properties (+1 more)

### Community 91 - "apiserver auth values schema"
Cohesion: 0.22
Nodes (9): properties, required, type, enum, type, type, auth, mode (+1 more)

### Community 92 - "apiserver client QPS schema"
Cohesion: 0.22
Nodes (9): minimum, type, properties, type, burst, client, qps, minimum (+1 more)

### Community 93 - "operator Berth client schema"
Cohesion: 0.10
Nodes (20): properties, type, type, properties, required, type, type, apiKey (+12 more)

### Community 94 - "berth-acquire CLI entrypoint"
Cohesion: 0.46
Nodes (5): cliFlags, main(), markerPath(), run(), github.com/spf13/cobra.Command

### Community 95 - "Load harness configuration"
Cohesion: 0.36
Nodes (3): Config, LeaseClient, Scenario

### Community 96 - "Speckit plan template"
Cohesion: 0.22
Nodes (8): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: [FEATURE], Project Structure, Source Code (repository root), Summary, Technical Context

### Community 97 - "Speckit checklist skill"
Cohesion: 0.25
Nodes (7): Anti-Examples: What NOT To Do, Checklist Purpose: "Unit Tests for English", Example Checklist Types & Sample Items, Execution Steps, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 98 - "apiserver credentials schema"
Cohesion: 0.22
Nodes (10): properties, type, kubeconfig, secretKey, secretName, staticKeys, type, type (+2 more)

### Community 99 - "operator schema root"
Cohesion: 0.40
Nodes (4): required, $schema, title, type

### Community 100 - "BerthLease API reference"
Cohesion: 0.32
Nodes (8): Rejected alternative: reuse BerthLease scale actions, Workload actions (suspend / scale), Lease semantics (at-most-once / at-least-once), Operator-as-Holder approach (SKA-271), BerthLeaseSpec, LeaseAction (suspend or scale), ScaleAction (replica count), TargetRef (workload target reference)

### Community 101 - "API routes and readiness"
Cohesion: 0.48
Nodes (5): ReadinessChecker, readinessGate, handleHealthz(), readyzHandler(), writePlain()

### Community 102 - "Lease fencing plan"
Cohesion: 0.25
Nodes (8): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: Lease Fencing and Isolation Safety, Project Structure, Source Code (repository root), Summary, Technical Context

### Community 103 - "Speckit clarify skill"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 104 - "Speckit implement skill"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 105 - "apiserver TLS values schema"
Cohesion: 0.29
Nodes (7): type, type, certManager, existingSecret, tls, properties, type

### Community 106 - "apiserver PodDisruptionBudget schema"
Cohesion: 0.29
Nodes (7): oneOf, oneOf, properties, type, maxUnavailable, minAvailable, podDisruptionBudget

### Community 107 - "operator service account schema"
Cohesion: 0.29
Nodes (7): type, type, annotations, name, serviceAccount, properties, type

### Community 108 - "Managed pod registration ADR"
Cohesion: 0.29
Nodes (7): 5. Register managed Pods before scheduling, Consequences, Considered Options, Context and Problem Statement, Decision Drivers, Decision Outcome, Links

### Community 109 - "Configuration and storage docs"
Cohesion: 0.29
Nodes (7): /readyz single-flight store-probe gate, Storage backends (mem / k8s / sql), Finding: k8s backend throttle-bound by client-go QPS=5/Burst=10, API server flags, Helm values reference (berth-apiserver / berth-operator), Static API key file format (key-id:sha256), Deployment topologies (cross-cluster / runner-local HA / edge-dev)

### Community 110 - "Runtime enforcement ADRs"
Cohesion: 0.17
Nodes (16): ADR-0003: enforce at-most-once by killing the main container, ADR-0003 rationale: probe default preserves isolation, signal opt-in for shell-less images, Failure behavior and split-brain window, Monotonic fencing token, Fencing token as stale-holder guard, TTL, heartbeat, and failover bound, Operator flags, berth-acquire injected helper (+8 more)

### Community 111 - "Release and docs workflow"
Cohesion: 0.21
Nodes (12): Berth documentation index, BerthLease API type (berth.skaphos.io/v1alpha1), BerthLeaseStatus, Maturity ladder (alpha / beta / GA compatibility ratchet), CRD API versioning standard (org mirror), Two-stage Linear issue lifecycle (Done vs Released), linear-release.yml release automation, Why Released is driven by the release event, not the release PR (+4 more)

### Community 112 - "Operator chart metrics tests"
Cohesion: 0.80
Nodes (4): flagValue(), operatorContainer(), TestChartHealthProbeBindAddressNormalized(), TestChartMetricsBindAddress()

### Community 113 - "State-volume trust research"
Cohesion: 0.29
Nodes (7): Phase 0 Research: State-Volume Trust and Marker Freshness, R1 — How does `check` learn the freshness bound without configuration?, R2 — A `signal`-mode backstop that does not consume the liveness slot, R3 — Which admission paths must the rule cover?, R4 — `failurePolicy: Ignore` and the value of US1, R5 — Keeping the two health tests from disagreeing (FR-009), R6 — Where the rejection counter lives (FR-011b)

### Community 114 - "Speckit constitution skill"
Cohesion: 0.33
Nodes (5): Outline, Post-Execution Checks, Pre-Execution Checks, Scope Guard, User Input

### Community 116 - "apiserver Helm notes"
Cohesion: 0.50
Nodes (4): berth-apiserver Helm NOTES, Authentication mode (none/static-keys/oidc), Coordination backend selection, SQLite single-writer constraint

### Community 117 - "Security policy"
Cohesion: 0.33
Nodes (5): Reporting a vulnerability, Scope notes, Security Policy, Supported versions, What to expect

### Community 118 - "Lease fencing quality checklist"
Cohesion: 0.33
Nodes (5): Content Quality, Feature Readiness, Notes, Requirement Completeness, Specification Quality Checklist: Lease Fencing and Isolation Safety

### Community 120 - "Lease fencing data model"
Cohesion: 0.33
Nodes (5): Data Model: Lease Fencing and Isolation Safety, Key, Per-backend representation, Record, Record states and transitions

### Community 121 - "Lease fencing validation quickstart"
Cohesion: 0.33
Nodes (6): 1. Full suite (race-enabled) — the primary gate, 2. SQL backends against real engines, 3. Key validation end-to-end, 4. Monotonicity smoke test over the API, 5. Docs and drift gates, Quickstart Validation: Lease Fencing and Isolation Safety

### Community 122 - "State-volume trust data model"
Cohesion: 0.33
Nodes (5): Freshness verdict, Mount classification, Phase 1 Data Model: State-Volume Trust and Marker Freshness, Rejection reason, State volume (`berth-state`)

### Community 123 - "State-volume delivery strategy"
Cohesion: 0.33
Nodes (6): Implementation Strategy, Incremental Delivery, MVP (User Story 1 only), Notes, Phase 1 investigation findings (T002, T003), Verified against a live cluster

### Community 124 - "Load fixture bring-up"
Cohesion: 0.60
Nodes (5): log(), patch_nodeport(), require(), up.sh script, wait_url()

### Community 125 - "Speckit tasks-to-issues skill"
Cohesion: 0.40
Nodes (4): Outline, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 126 - "apiserver coordination schema"
Cohesion: 0.25
Nodes (8): properties, required, type, type, type, coordination, inCluster, namespace

### Community 127 - "Kubernetes store test interceptors"
Cohesion: 0.50
Nodes (3): net/http.RoundTripper, sync.Once, interceptFirstPut

### Community 129 - "Speckit checklist template"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 130 - "Lease store contract"
Cohesion: 0.40
Nodes (4): Contract: `internal/lease.Store`, Interface (after this feature), Manager behavior on top of the contract, Semantics every backend must provide (conformance-tested)

### Community 131 - "State-volume user scenarios"
Cohesion: 0.40
Nodes (5): Edge Cases, User Scenarios & Testing *(mandatory)*, User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) — #96, User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2) — #98, User Story 3 - A rejected pod tells its owner exactly what to change (Priority: P3)

### Community 132 - "E2E fixture bring-up"
Cohesion: 0.70
Nodes (4): install_operator(), log(), require(), up.sh script

### Community 133 - "apiserver extra args schema"
Cohesion: 0.50
Nodes (4): items, type, type, extraArgs

### Community 136 - "Coverage threshold script"
Cohesion: 0.83
Nodes (3): check-coverage.sh script, skip_pkg(), threshold_for_pkg()

### Community 137 - "HTTP API contract"
Cohesion: 0.50
Nodes (4): Contract: HTTP API changes, Strengthened (not new) guarantees the API now actually delivers, What changes, What does not change

### Community 139 - "State-volume task dependencies"
Cohesion: 0.50
Nodes (4): Dependencies & Execution Order, Parallel Opportunities, Phase Dependencies, The US1 → US2 dependency is real

### Community 145 - "Dead sidecar user story"
Cohesion: 0.67
Nodes (3): Implementation for User Story 2, Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2), Tests for User Story 2

## Knowledge Gaps
- **555 isolated node(s):** `common.sh script`, `ScaleAction`, `PermittedPod`, `$schema`, `title` (+550 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 687 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **13 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `requestFor()` connect `Operator reconciler tests` to `Generated deepcopy API code`, `Shared unit test fixtures`, `Operator admission and reconcile`?**
  _High betweenness centrality (0.009) - this node is a cross-community bridge._
- **Why does `Record` connect `Lease record store backends` to `Lease manager and store core`, `Kubernetes lease store`, `Kubernetes store unit tests`, `Lease manager tests`, `API and lease test harness`, `Metrics store wrapper`, `SQL lease store`, `Kubernetes store conflict retries`, `Load result summarization`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._
- **Why does `NewPodInjector()` connect `Injector container helper tests` to `Acquire and injector config`, `Go client and TLS options`, `Pod injector mutation logic`, `Webhook injection tests`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._
- **Are the 30 inferred relationships involving `testInjector()` (e.g. with `TestInjectAcceptsExistingEmptyDirStateVolume()` and `TestInjectAddsStateMountWhenExistingMountPathDiffers()`) actually correct?**
  _`testInjector()` has 30 INFERRED edges - model-reasoned connections that need verification._
- **Are the 30 inferred relationships involving `optInPod()` (e.g. with `TestInjectAcceptsExistingEmptyDirStateVolume()` and `TestInjectAddsStateMountWhenExistingMountPathDiffers()`) actually correct?**
  _`optInPod()` has 30 INFERRED edges - model-reasoned connections that need verification._
- **What connects `common.sh script`, `ScaleAction`, `PermittedPod` to the rest of the system?**
  _555 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Go client and TLS options` be split into smaller, more focused modules?**
  _Cohesion score 0.08888888888888889 - nodes in this community are weakly interconnected._