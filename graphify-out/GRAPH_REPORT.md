# Graph Report - berth  (2026-08-10)

## Corpus Check
- 218 files · ~191,923 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 2378 nodes · 4390 edges · 161 communities (149 shown, 12 thin omitted)
- Extraction: 86% EXTRACTED · 14% INFERRED · 0% AMBIGUOUS · INFERRED: 633 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `4bf09f08`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- Berth (distributed lease coordination)
- parseConfig
- WithTimeout
- State
- Record
- .Now
- testInjector
- run
- T
- .DeepCopyInto
- api/leases.go
- MemStore
- NewMemStore
- MetricsMiddleware
- Server
- run
- Identity
- Recorder
- Project structure and module organization
- properties
- RunStoreBenchmarks
- TestDeadSidecarStopsTheWorkload
- leaseclient.go
- properties
- properties
- enforce.go
- lease_test.go
- common.sh
- berth-oidc-broker/main_test.go
- Metrics
- Tasks: [FEATURE NAME]
- speckit-analyze/SKILL.md
- newTestRenewer
- Key
- Contract: Admission
- CHANGELOG managed by Release Please
- properties
- NewMux
- sqlstore_test.go
- AuthMiddleware
- ADR-0003: enforce at-most-once by killing the main container
- newK8sStore
- NewServer
- NewFileTokenSource
- newTestServer
- properties
- Renewer
- types.go
- Execution Steps
- properties
- properties
- 4. The shared state volume is a trust boundary, not shared scratch space
- Core Principles
- enabled
- properties
- EvaluateMarker
- hangingClient
- properties
- properties
- baseConfig
- LoggingMiddleware
- properties
- properties
- properties
- K8sLeaseStore
- Feature Specification: [FEATURE NAME]
- Feature Specification: Lease Fencing and Isolation Safety
- Tasks: State-Volume Trust and Marker Freshness
- failingManager
- speckit-plan/SKILL.md
- speckit-specify/SKILL.md
- speckit-tasks/SKILL.md
- properties
- Monotonic fencing token
- acquired
- meteredStore
- Core Principles
- Tasks: Lease Fencing and Isolation Safety
- Feature Specification: State-Volume Trust and Marker Freshness
- Hold
- required
- properties
- properties
- Pod-Level Gating activation model
- Lease API (acquire/renew/release endpoints)
- Operator reconcile flow (finalizer, acquire, actions, status)
- newAuthzServer
- Phase 0 Research: Lease Fencing and Isolation Safety
- Implementation Plan: State-Volume Trust and Marker Freshness
- NewServer
- properties
- properties
- properties
- properties
- properties
- sqlstore.go
- Implementation Plan: [FEATURE]
- speckit-checklist/SKILL.md
- properties
- berth-operator/values.schema.json
- Workload actions (suspend / scale)
- dialect.go
- Implementation Plan: Lease Fencing and Isolation Safety
- speckit-clarify/SKILL.md
- speckit-implement/SKILL.md
- certManager
- properties
- properties
- properties
- Storage backends (mem / k8s / sql)
- Mutating admission webhook for berth-acquire injection
- Release Please release flow
- NewState
- Phase 0 Research: State-Volume Trust and Marker Freshness
- speckit-constitution/SKILL.md
- required
- enum
- Security Policy
- Specification Quality Checklist: Lease Fencing and Isolation Safety
- Data Model: Lease Fencing and Isolation Safety
- Quickstart Validation: Lease Fencing and Isolation Safety
- Phase 1 Data Model: State-Volume Trust and Marker Freshness
- Implementation Strategy
- load/fixtures/up.sh
- speckit-taskstoissues/SKILL.md
- properties
- enum
- tokenFile
- [CHECKLIST TYPE] Checklist: [FEATURE NAME]
- Contract: `internal/lease.Store`
- User Scenarios & Testing *(mandatory)*
- e2e/fixtures/up.sh
- extraArgs
- enum
- stateDir
- check-coverage.sh
- Contract: HTTP API changes
- Dependencies & Execution Order
- addKnownTypes
- berth/main.go
- apiKeySecret
- replicaCount
- TestNoOpAuthenticatorAcceptsEverything
- Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2)
- run.sh
- e2e/fixtures/down.sh
- load/fixtures/down.sh
- github.com/skaphos/berth
- github.com/skaphos/berth/tools

## God Nodes (most connected - your core abstractions)
1. `testInjector()` - 43 edges
2. `optInPod()` - 41 edges
3. `NewMemStore()` - 36 edges
4. `Record` - 35 edges
5. `NewMux()` - 32 edges
6. `NewRecorder()` - 28 edges
7. `NewManager()` - 27 edges
8. `newScheme()` - 25 edges
9. `newTestManager()` - 24 edges
10. `newTestRenewer()` - 23 edges

## Surprising Connections (you probably didn't know these)
- `Local validation tasks` --semantically_similar_to--> `Build/test/dev task commands`  [INFERRED] [semantically similar]
  CONTRIBUTING.md → AGENTS.md
- `Project structure and module organization` --semantically_similar_to--> `CLAUDE.md repository guidelines`  [INFERRED] [semantically similar]
  AGENTS.md → CLAUDE.md
- `Operator leader election (single active replica)` --semantically_similar_to--> `Lease concept (named, time-bounded, single-holder claim)`  [INFERRED] [semantically similar]
  deploy/helm/berth-operator/templates/NOTES.txt → docs/concepts.md
- `run()` --calls--> `MetricsMiddleware()`  [INFERRED]
  cmd/apiserver/main.go → internal/api/metrics.go
- `run()` --calls--> `LoggingMiddleware()`  [INFERRED]
  cmd/apiserver/main.go → internal/api/obs.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Runtime-singleton at-most-once enforcement path** — docs_workload_gating_injection_runtime_singleton, docs_workload_gating_injection_probe, docs_workload_gating_injection_signal, docs_workload_gating_injection_berth_acquire, docs_adr_0003_sidecar_runtime_enforcement_by_container_kill_adr [EXTRACTED 1.00]
- **Tag-driven release pipeline (release PR to tag to artifacts to Linear)** — release_release_please, release_release_workflow, release_docs_workflow, docs_release_and_issue_workflow_linear_release_yml [EXTRACTED 1.00]
- **Phased scalability and load-testing program** — docs_operations_scalability_sizing_model, docs_operations_scalability_phase1_store_benchmarks, docs_operations_scalability_phase2_metrics, docs_operations_scalability_phase3_load_driver, test_load_fixtures_readme_load_harness, docs_operations_benchmarks_mem_benchmark, docs_operations_benchmarks_sqlite_benchmark [EXTRACTED 1.00]

## Communities (161 total, 12 thin omitted)

### Community 0 - "Berth (distributed lease coordination)"
Cohesion: 0.13
Nodes (21): berth-apiserver Helm NOTES, Authentication mode (none/static-keys/oidc), Coordination backend selection, SQLite single-writer constraint, Operator leader election (single active replica), berth-operator Helm install NOTES, Lease concept (named, time-bounded, single-holder claim), Getting Started three-cluster failover tutorial (+13 more)

### Community 1 - "parseConfig"
Cohesion: 0.08
Nodes (36): Enforce, Mode, cliFlags, Config, Writer, main(), markerPath(), run() (+28 more)

### Community 2 - "WithTimeout"
Cohesion: 0.07
Nodes (51): AcquireResult, Option, run(), Client, Config, LoadTLSConfig(), generateTestCAPEM(), T (+43 more)

### Community 3 - "State"
Cohesion: 0.17
Nodes (7): HealthResult, HealthVerdict, State, FileMode, copyFile(), Duration, writeFileAtomic()

### Community 4 - "Record"
Cohesion: 0.18
Nodes (8): DB, Context, Duration, Time, Record, Store, Store, Tx

### Community 5 - ".Now"
Cohesion: 0.06
Nodes (82): T, TestAdditionalDeepCopyHelpers(), TestAddToSchemeRegistersBerthLeaseTypes(), TestBerthLeaseDeepCopyCopiesNestedState(), TestBerthLeaseListDeepCopyCopiesItems(), BerthLeaseSpec, BerthLeaseStatus, ConditionStatus (+74 more)

### Community 6 - "testInjector"
Cohesion: 0.05
Nodes (89): EnvVar, findVolume(), Pod, T, Volume, TestInjectAcceptsExistingEmptyDirStateVolume(), TestInjectAddsStateMountWhenExistingMountPathDiffers(), TestInjectForcesExistingStateMountReadOnly() (+81 more)

### Community 7 - "run"
Cohesion: 0.07
Nodes (38): Duration, T, markerAged(), TestCheckAbsentMarkerFailsWithDistinctReason(), TestCheckFreshMarkerPasses(), TestCheckNeedsNoEnvironmentOrConfig(), TestCheckStaleMarkerFailsWithReason(), TestCheckWithoutMaxAgeIsPresenceOnly() (+30 more)

### Community 8 - "T"
Cohesion: 0.16
Nodes (28): OIDCAuthenticator, OIDCConfig, testIssuer, Claims, IDTokenVerifier, claimAsString(), claimContains(), defaultStr() (+20 more)

### Community 9 - ".DeepCopyInto"
Cohesion: 0.12
Nodes (8): Object, BerthLease, BerthLeaseList, BerthLeaseSpec, BerthLeaseStatus, LeaseAction, ScaleAction, TargetRef

### Community 10 - "api/leases.go"
Cohesion: 0.08
Nodes (54): AcquireRequest, errorResponse, LeaseManager, LeaseResponse, ReadinessChecker, readinessGate, ReleaseRequest, RenewRequest (+46 more)

### Community 11 - "MemStore"
Cohesion: 0.36
Nodes (3): Context, Mutex, MemStore

### Community 12 - "NewMemStore"
Cohesion: 0.07
Nodes (62): NewManager(), Context, Duration, Once, Store, T, Time, newTestManager() (+54 more)

### Community 13 - "MetricsMiddleware"
Cohesion: 0.14
Nodes (11): recordingMetrics, RequestMetrics, statusRecorder, Handler, ResponseWriter, MetricsMiddleware(), Duration, T (+3 more)

### Community 15 - "run"
Cohesion: 0.07
Nodes (53): oidcConfig, storeConfig, Authenticator, Clientset, T, nonEmptyLines(), TestNewLogHandlerRejectsUnknownFormat(), TestParseLogLevel() (+45 more)

### Community 16 - "Identity"
Cohesion: 0.08
Nodes (38): fakeAuthenticator, Identity, NoOpAuthenticator, StaticAuthenticator, Context, Context, Context, hashToken() (+30 more)

### Community 17 - "Recorder"
Cohesion: 0.11
Nodes (46): leaseName(), Config, T, TestConfigValidate(), TestLeaseNamingIsDistinctAndStable(), validConfig(), forEachLease(), forEachLeaseBounded() (+38 more)

### Community 18 - "Project structure and module organization"
Cohesion: 0.22
Nodes (10): Build/test/dev task commands, Chart version bump rule, CLI vs Linux runtime split, Project structure and module organization, CLAUDE.md repository guidelines, DCO sign-off and signed commits, Generated artifacts kept in sync, Local validation tasks (+2 more)

### Community 19 - "properties"
Cohesion: 0.05
Nodes (38): enum, type, enum, type, type, properties, required, type (+30 more)

### Community 20 - "RunStoreBenchmarks"
Cohesion: 0.12
Nodes (31): T, TestK8sStoreSafetyRegressions(), TestMemStoreConformance(), BenchmarkMemStore(), B, BenchmarkMariaDBStore(), BenchmarkPostgresStore(), B (+23 more)

### Community 21 - "TestDeadSidecarStopsTheWorkload"
Cohesion: 0.11
Nodes (32): ObjectKey, T, TestInjectedAnnotationCannotSkipInjection(), appRestartCount(), appRestartCountFrom(), assertStaleProbeEvent(), copyTokenSecretTo(), gatedSingletonPod() (+24 more)

### Community 23 - "properties"
Cohesion: 0.06
Nodes (34): type, type, type, type, type, type, type, type (+26 more)

### Community 24 - "properties"
Cohesion: 0.06
Nodes (33): type, type, type, type, type, type, type, type (+25 more)

### Community 25 - "enforce.go"
Cohesion: 0.11
Nodes (22): Enforcer, probeEnforcer, procScanner, signalEnforcer, Config, Context, Duration, Logger (+14 more)

### Community 26 - "lease_test.go"
Cohesion: 0.19
Nodes (28): clusterRef, M, applyBerthLease(), applyTargetDeployment(), cleanupFixtures(), getDeploymentReplicas(), Client, Context (+20 more)

### Community 27 - "common.sh"
Cohesion: 0.09
Nodes (15): check-prerequisites.sh script, check_dir(), check_file(), get_feature_paths(), get_repo_root(), has_jq(), _persist_feature_json(), resolve_specify_init_dir() (+7 more)

### Community 28 - "berth-oidc-broker/main_test.go"
Cohesion: 0.16
Nodes (26): Config, Context, Duration, Time, main(), nextRefresh(), parseScopes(), resolveSecret() (+18 more)

### Community 29 - "Metrics"
Cohesion: 0.11
Nodes (17): CounterVec, Gauge, Context, Duration, Handler, HistogramVec, Metrics, Registry (+9 more)

### Community 30 - "Tasks: [FEATURE NAME]"
Cohesion: 0.07
Nodes (26): Dependencies & Execution Order, Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 2, Implementation for User Story 3, Implementation Strategy, Incremental Delivery, MVP First (User Story 1 Only) (+18 more)

### Community 31 - "speckit-analyze/SKILL.md"
Cohesion: 0.08
Nodes (25): 1. Initialize Analysis Context, 2. Load Artifacts (Progressive Disclosure), 3. Build Semantic Models, 4. Detection Passes (Token-Efficient Analysis), 5. Severity Assignment, 6. Produce Compact Analysis Report, 7. Provide Next Actions, 8. Offer Remediation (+17 more)

### Community 32 - "newTestRenewer"
Cohesion: 0.22
Nodes (24): newHangingClient(), Duration, LeaseClient, T, newHangingRenewer(), newTestRenewer(), runBounded(), TestLoadHandoffFallbackWhenStateMissing() (+16 more)

### Community 33 - "Key"
Cohesion: 0.15
Nodes (14): Context, Duration, Store, Time, tombstoneFrom(), New(), BenchmarkSQLiteStore(), B (+6 more)

### Community 34 - "Contract: Admission"
Cohesion: 0.09
Nodes (19): Compatibility, Contract: Admission, Decision table, Failure policy, Registered rules, Rejection message, Scope, Agreement with `State.IsHealthy()` (+11 more)

### Community 36 - "properties"
Cohesion: 0.11
Nodes (20): properties, type, type, type, properties, type, auth, bindAddress (+12 more)

### Community 37 - "NewMux"
Cohesion: 0.20
Nodes (17): T, TestInternalErrorCarriesIDWithoutObservabilityMiddleware(), TestInternalErrorsAreGenericButLoggedWithCorrelationID(), TestReleaseConflictStaysGenericNot500(), T, TestReadinessGateCachesWithinTTL(), TestReadinessGateNilCheckerAlwaysReady(), TestReadyzCollapsesRequestStormIntoOneProbe() (+9 more)

### Community 38 - "sqlstore_test.go"
Cohesion: 0.25
Nodes (19): Store, T, TB, newSQLiteStore(), sampleRecord(), sqliteTestName(), TestMySQLDialectCreateUsesDuplicateKeyErrorForConflict(), TestMySQLDialectUsesReadCommittedTransactions() (+11 more)

### Community 39 - "AuthMiddleware"
Cohesion: 0.21
Nodes (17): identityCtxKey, AuthMiddleware(), bearerToken(), ChainMiddleware(), Context, Handler, Request, IdentityFromContext() (+9 more)

### Community 40 - "ADR-0003: enforce at-most-once by killing the main container"
Cohesion: 0.16
Nodes (19): ADR-0001: Pod-level gating for injected singletons, Rejected alternative: hybrid helper-writes-status, ADR-0002: Opt into injection via labels/annotations, not a wrapper CRD, Rejected alternative: Manual injection, Rejected alternative: Wrapper CRD, Decision: label/annotation contract via mutating webhook, Rationale: native GitOps opt-in, no extra control loop, ADR-0003: enforce at-most-once by killing the main container (+11 more)

### Community 41 - "newK8sStore"
Cohesion: 0.29
Nodes (18): Interface, NewK8sLeaseStore(), Interface, T, newK8sStore(), sampleRecord(), TestK8sLeaseNameInjectiveForValidKeys(), TestK8sStoreEncodesNameAndAnnotates() (+10 more)

### Community 42 - "NewServer"
Cohesion: 0.26
Nodes (16): Option, serverConfig, Duration, Handler, NewServer(), T, TestNewServerDefaultsAndOptions(), TestServerStartRejectsNilContext() (+8 more)

### Community 43 - "NewFileTokenSource"
Cohesion: 0.24
Nodes (13): FileTokenSource, Duration, Mutex, Time, NewFileTokenSource(), T, TestFileTokenSourcePicksUpRotationAfterTTL(), TestFileTokenSourceReturnsCachedOnReadError() (+5 more)

### Community 44 - "newTestServer"
Cohesion: 0.35
Nodes (17): decode(), Response, Server, T, newTestServer(), postJSON(), TestAcquireRejectsBadInput(), TestAcquireRejectsUnknownFields() (+9 more)

### Community 45 - "properties"
Cohesion: 0.13
Nodes (17): pattern, type, pattern, type, properties, type, properties, apiKeyFile (+9 more)

### Community 46 - "Renewer"
Cohesion: 0.25
Nodes (8): Renewer, CancelFunc, Config, Context, LeaseClient, Logger, Time, NewRenewer()

### Community 47 - "types.go"
Cohesion: 0.16
Nodes (15): Condition, LeaseAction, TargetRef, Time, BerthLease, BerthLeaseList, BerthLeaseSpec, BerthLeaseStatus (+7 more)

### Community 48 - "Execution Steps"
Cohesion: 0.12
Nodes (15): 1. Initialize Convergence Context, 2. Load Artifacts (Progressive Disclosure), 3. Build the Intent Inventory, 4. Assess the Codebase and Classify Findings, 5. Assign Severity, 6. Present the In-Session Findings Summary, 7. Append Convergence Tasks (or report converged), 8. Provide Next Actions (Handoff) (+7 more)

### Community 49 - "properties"
Cohesion: 0.12
Nodes (16): properties, required, type, Always, IfNotPresent, Never, repository, image (+8 more)

### Community 50 - "properties"
Cohesion: 0.12
Nodes (16): properties, type, enum, type, enum, type, defaults, enforce (+8 more)

### Community 51 - "4. The shared state volume is a trust boundary, not shared scratch space"
Cohesion: 0.12
Nodes (14): 4. The shared state volume is a trust boundary, not shared scratch space, Consequences, Considered Options, Context and Problem Statement, Decision Drivers, Decision Outcome, Links, Breaking change 1 — the state volume is reserved (+6 more)

### Community 52 - "Core Principles"
Cohesion: 0.12
Nodes (15): Berth Constitution, Core Principles, Engineering Constraints, Governance, I. Explicit State Over Implicit Behavior, II. Git Is the Durable Desired-State Boundary, III. Deterministic, Reconstructible Operation, IV. Kubernetes-Native, Never Obscured (+7 more)

### Community 53 - "enabled"
Cohesion: 0.15
Nodes (15): type, properties, type, properties, type, maximum, minimum, type (+7 more)

### Community 54 - "properties"
Cohesion: 0.13
Nodes (15): required, type, repository, type, image, oidc, refreshSkew, resources (+7 more)

### Community 55 - "EvaluateMarker"
Cohesion: 0.34
Nodes (14): agedMarker(), Duration, T, TestEvaluateMarkerAbsentIsDistinctFromStale(), TestEvaluateMarkerBoundIsInclusive(), TestEvaluateMarkerFreshnessBoundary(), TestEvaluateMarkerIgnoresMarkerContents(), TestEvaluateMarkerUnreadableFailsClosed() (+6 more)

### Community 56 - "hangingClient"
Cohesion: 0.31
Nodes (6): fakeClient, hangingClient, AcquireResult, Context, Duration, Mutex

### Community 57 - "properties"
Cohesion: 0.14
Nodes (14): type, type, type, properties, audience, issuerURL, jwksURL, requiredClaims (+6 more)

### Community 58 - "properties"
Cohesion: 0.14
Nodes (14): items, type, type, items, type, properties, type, type (+6 more)

### Community 59 - "baseConfig"
Cohesion: 0.35
Nodes (13): baseConfig(), Config, T, TestApplyDefaults(), TestApplyDefaultsStartupGateReleaseFalse(), TestHolderExplicitOverride(), TestHolderIsOwnedByClusterTenant(), TestHolderRuntimeSingletonIncludesPodName() (+5 more)

### Community 60 - "LoggingMiddleware"
Cohesion: 0.28
Nodes (12): Buffer, Handler, Logger, LoggingMiddleware(), capturingLogger(), Logger, T, TestLoggingMiddlewareEmitsAccessLineAndHeader() (+4 more)

### Community 61 - "properties"
Cohesion: 0.15
Nodes (13): type, type, type, additionalLabels, interval, metricRelabelings, relabelings, scrapeTimeout (+5 more)

### Community 62 - "properties"
Cohesion: 0.15
Nodes (13): type, type, type, type, bearerTokenFile, interval, labels, namespace (+5 more)

### Community 63 - "properties"
Cohesion: 0.15
Nodes (13): type, type, namespaceSelector, objectSelector, servicePort, timeoutSeconds, maximum, minimum (+5 more)

### Community 64 - "K8sLeaseStore"
Cohesion: 0.37
Nodes (8): applyRecordToLease(), Context, k8sLeaseName(), leaseFromRecord(), recordFromLease(), versionFromLease(), Lease, K8sLeaseStore

### Community 65 - "Feature Specification: [FEATURE NAME]"
Cohesion: 0.15
Nodes (12): Assumptions, Edge Cases, Feature Specification: [FEATURE NAME], Functional Requirements, Key Entities *(include if feature involves data)*, Measurable Outcomes, Requirements *(mandatory)*, Success Criteria *(mandatory)* (+4 more)

### Community 66 - "Feature Specification: Lease Fencing and Isolation Safety"
Cohesion: 0.15
Nodes (13): Assumptions, Context, Edge Cases, Feature Specification: Lease Fencing and Isolation Safety, Functional Requirements, Key Entities, Measurable Outcomes, Requirements *(mandatory)* (+5 more)

### Community 67 - "Tasks: State-Volume Trust and Marker Freshness"
Cohesion: 0.15
Nodes (13): Format: `[ID] [P?] [Story] Description`, Implementation for User Story 1, Implementation for User Story 3, Parallel Example: User Story 1, Path Conventions, Phase 1: Setup (Shared Infrastructure), Phase 2: Foundational (Blocking Prerequisites), Phase 3: User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) 🎯 MVP (+5 more)

### Community 68 - "failingManager"
Cohesion: 0.25
Nodes (7): failingManager, readyManager, Int64, AcquireResult, Context, Duration, Context

### Community 69 - "speckit-plan/SKILL.md"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, Key rules, Mandatory Post-Execution Hooks, Outline, Phase 0: Outline & Research, Phase 1: Design & Contracts, Phases (+2 more)

### Community 70 - "speckit-specify/SKILL.md"
Cohesion: 0.18
Nodes (10): Completion Report, Done When, For AI Generation, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, Quick Guidelines, Section Requirements (+2 more)

### Community 71 - "speckit-tasks/SKILL.md"
Cohesion: 0.18
Nodes (10): Checklist Format (REQUIRED), Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Phase Structure, Pre-Execution Checks, Task Generation Rules (+2 more)

### Community 72 - "properties"
Cohesion: 0.18
Nodes (11): type, type, berthLeaseVerbs, leaderElection, rbac, targetVerbs, properties, required (+3 more)

### Community 73 - "Monotonic fencing token"
Cohesion: 0.24
Nodes (11): Failure behavior and split-brain window, Monotonic fencing token, Lease model (holder, TTL, fencing token record), Fencing token as stale-holder guard, TTL, heartbeat, and failover bound, Sizing model: 2,000 leases, 8 region pairs, ~400 write req/s, Berth documentation index, BerthLease API type (berth.skaphos.io/v1alpha1) (+3 more)

### Community 74 - "acquired"
Cohesion: 0.31
Nodes (9): Config, T, holdTestConfig(), TestHoldRetriesUntilAcquired(), TestHoldStopsOnContextCancel(), acquired(), Logger, heldByOther() (+1 more)

### Community 75 - "meteredStore"
Cohesion: 0.33
Nodes (5): Context, Metrics, Store, Time, meteredStore

### Community 76 - "Core Principles"
Cohesion: 0.18
Nodes (10): Core Principles, Governance, [PRINCIPLE_1_NAME], [PRINCIPLE_2_NAME], [PRINCIPLE_3_NAME], [PRINCIPLE_4_NAME], [PRINCIPLE_5_NAME], [PROJECT_NAME] Constitution (+2 more)

### Community 77 - "Tasks: Lease Fencing and Isolation Safety"
Cohesion: 0.18
Nodes (11): Dependencies, Implementation notes (deviations from the plan as written), Implementation strategy, Parallel opportunities, Phase 1: Setup, Phase 2: Foundational — version-CAS store contract (blocks US1, US2), Phase 3: User Story 1 — Failover never yields two holders (P1, #90), Phase 4: User Story 2 — Fencing tokens never repeat for a key (P2, #92 + #93) (+3 more)

### Community 78 - "Feature Specification: State-Volume Trust and Marker Freshness"
Cohesion: 0.18
Nodes (11): Assumptions, Clarifications, Context, Feature Specification: State-Volume Trust and Marker Freshness, Functional Requirements, Key Entities, Measurable Outcomes, Requirements *(mandatory)* (+3 more)

### Community 79 - "Hold"
Cohesion: 0.31
Nodes (8): LeaseClient, Config, AcquireResult, Context, Duration, Logger, Hold(), sleep()

### Community 80 - "required"
Cohesion: 0.20
Nodes (9): image, required, $schema, title, type, auth, coordination, store (+1 more)

### Community 81 - "properties"
Cohesion: 0.20
Nodes (10): properties, items, type, type, type, dnsNames, duration, issuerRef (+2 more)

### Community 82 - "properties"
Cohesion: 0.20
Nodes (10): properties, type, type, type, caBundleConfigMap, caBundleKey, insecureSkipVerify, serverName (+2 more)

### Community 83 - "Pod-Level Gating activation model"
Cohesion: 0.20
Nodes (10): Decision: pod-level gating (init hold + sidecar enforce), Rationale: helper in-pod vantage cannot scale, Active Enforcement (probe/signal), Injected berth-acquire helper, Native Sidecar (restartPolicy: Always), Pod-Level Gating activation model, Rationale: injected path is fallback due to larger fencing surface, Restart Re-Gating (+2 more)

### Community 84 - "Lease API (acquire/renew/release endpoints)"
Cohesion: 0.24
Nodes (10): Tenant-scoped holder authorization, Lease API (acquire/renew/release endpoints), Observability (access log, RED metrics, lease outcomes counter), Holder identity, Load-run result artifact schema (load.Summary + resources block), Metrics on a separate unauthenticated port, off the TLS/auth path, Phase 2: API-server Prometheus instrumentation (RED + store-call metrics), Phase 3: API-level load driver (test/load + internal/load) (+2 more)

### Community 85 - "Operator reconcile flow (finalizer, acquire, actions, status)"
Cohesion: 0.20
Nodes (10): Operator reconcile flow (finalizer, acquire, actions, status), Why the operator does not watch target workloads, Code map (contributor ownership by path), API packages (internal/api: routes, leases, middleware), Operator packages (internal/operator: reconciler, actions, leaseclient), Injection packages (internal/webhook, internal/acquire, cmd/berth-acquire), Lease core packages (internal/lease: store, manager, memstore, k8sstore, ttl), MemStore benchmark baseline (AcquireParallel/RenewSteadyState/FailoverFanout) (+2 more)

### Community 86 - "newAuthzServer"
Cohesion: 0.49
Nodes (9): Response, Server, T, newAuthzServer(), postBearer(), TestAuthzAllowsInTenantHolderAcrossNamespaces(), TestAuthzBareTenantHolderAllowed(), TestAuthzDeniesHolderOutsideTenant() (+1 more)

### Community 87 - "Phase 0 Research: Lease Fencing and Isolation Safety"
Cohesion: 0.20
Nodes (10): D1 — CAS predicate: store-maintained per-record `Version`, D2 — Tombstones instead of deletion; `Store.Delete` removed, D3 — GC becomes a tombstoning sweep with a never-reused predicate, D4 — Token monotonicity semantics, D5 — Key validation at the API boundary, D6 — SQL schema migration (upgrade path, FR-011), D7 — Test strategy (FR-009), D8 — Documentation impact (FR-010) (+2 more)

### Community 88 - "Implementation Plan: State-Volume Trust and Marker Freshness"
Cohesion: 0.20
Nodes (10): Complexity Tracking, Constitution Check, Documentation (this feature), Gate finding: the security control is fail-open, Implementation Plan: State-Volume Trust and Marker Freshness, Post-Design Re-Check (after Phase 1), Project Structure, Source Code (repository root) (+2 more)

### Community 89 - "NewServer"
Cohesion: 0.36
Nodes (6): Server, Handler, NewServer(), T, TestNewServerDefaultsHandler(), TestNewServerUsesProvidedHandler()

### Community 90 - "properties"
Cohesion: 0.22
Nodes (9): type, type, type, annotations, create, name, serviceAccount, properties (+1 more)

### Community 91 - "properties"
Cohesion: 0.22
Nodes (9): properties, required, type, type, auth, oidc, staticKeys, type (+1 more)

### Community 92 - "properties"
Cohesion: 0.22
Nodes (9): minimum, type, properties, type, burst, client, qps, minimum (+1 more)

### Community 93 - "properties"
Cohesion: 0.22
Nodes (9): type, properties, required, type, apiServer, berth, tls, type (+1 more)

### Community 94 - "properties"
Cohesion: 0.22
Nodes (9): type, properties, type, id, leaseDuration, renewDeadline, retryPeriod, type (+1 more)

### Community 95 - "sqlstore.go"
Cohesion: 0.33
Nodes (6): Time, parseTime(), parseTimeString(), scanRecord(), Config, rowScanner

### Community 96 - "Implementation Plan: [FEATURE]"
Cohesion: 0.22
Nodes (8): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: [FEATURE], Project Structure, Source Code (repository root), Summary, Technical Context

### Community 97 - "speckit-checklist/SKILL.md"
Cohesion: 0.25
Nodes (7): Anti-Examples: What NOT To Do, Checklist Purpose: "Unit Tests for English", Example Checklist Types & Sample Items, Execution Steps, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 98 - "properties"
Cohesion: 0.29
Nodes (8): properties, type, kubeconfig, secretKey, secretName, type, type, properties

### Community 99 - "berth-operator/values.schema.json"
Cohesion: 0.25
Nodes (7): image, required, $schema, title, type, berth, rbac

### Community 100 - "Workload actions (suspend / scale)"
Cohesion: 0.32
Nodes (8): Rejected alternative: reuse BerthLease scale actions, Workload actions (suspend / scale), Lease semantics (at-most-once / at-least-once), Operator-as-Holder approach (SKA-271), BerthLeaseSpec, LeaseAction (suspend or scale), ScaleAction (replica count), TargetRef (workload target reference)

### Community 101 - "dialect.go"
Cohesion: 0.36
Nodes (6): dialectFor(), Time, sqliteTimeValue(), sqlTimeValue(), dialect, TxOptions

### Community 102 - "Implementation Plan: Lease Fencing and Isolation Safety"
Cohesion: 0.25
Nodes (8): Complexity Tracking, Constitution Check, Documentation (this feature), Implementation Plan: Lease Fencing and Isolation Safety, Project Structure, Source Code (repository root), Summary, Technical Context

### Community 103 - "speckit-clarify/SKILL.md"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 104 - "speckit-implement/SKILL.md"
Cohesion: 0.29
Nodes (6): Completion Report, Done When, Mandatory Post-Execution Hooks, Outline, Pre-Execution Checks, User Input

### Community 105 - "certManager"
Cohesion: 0.29
Nodes (7): type, type, certManager, existingSecret, tls, properties, type

### Community 106 - "properties"
Cohesion: 0.29
Nodes (7): oneOf, oneOf, properties, type, maxUnavailable, minAvailable, podDisruptionBudget

### Community 107 - "properties"
Cohesion: 0.29
Nodes (7): type, type, annotations, name, serviceAccount, properties, type

### Community 108 - "properties"
Cohesion: 0.29
Nodes (7): properties, type, apiKey, secretKey, secretName, type, type

### Community 109 - "Storage backends (mem / k8s / sql)"
Cohesion: 0.29
Nodes (7): /readyz single-flight store-probe gate, Storage backends (mem / k8s / sql), Finding: k8s backend throttle-bound by client-go QPS=5/Burst=10, API server flags, Helm values reference (berth-apiserver / berth-operator), Static API key file format (key-id:sha256), Deployment topologies (cross-cluster / runner-local HA / edge-dev)

### Community 110 - "Mutating admission webhook for berth-acquire injection"
Cohesion: 0.33
Nodes (7): Operator flags, berth-acquire injected helper, Injection is a fallback: larger split-brain surface than operator-as-holder, Opt-in label/annotation contract (berth.skaphos.io/*), SKA-444: helper token/CA file not yet mounted, startup-gate mode (init container only), Mutating admission webhook for berth-acquire injection

### Community 111 - "Release Please release flow"
Cohesion: 0.33
Nodes (7): Two-stage Linear issue lifecycle (Done vs Released), linear-release.yml release automation, Why Released is driven by the release event, not the release PR, Keyless Sigstore signing, SBOM, and SLSA provenance, docs.yml versioned docs publishing (mike), Release Please release flow, release.yml artifact publishing workflow

### Community 112 - "NewState"
Cohesion: 0.57
Nodes (6): NewState(), T, TestInstallCheckBinary(), TestStateMarkerToggle(), TestStateReadMissing(), TestStateWriteAndRead()

### Community 113 - "Phase 0 Research: State-Volume Trust and Marker Freshness"
Cohesion: 0.29
Nodes (7): Phase 0 Research: State-Volume Trust and Marker Freshness, R1 — How does `check` learn the freshness bound without configuration?, R2 — A `signal`-mode backstop that does not consume the liveness slot, R3 — Which admission paths must the rule cover?, R4 — `failurePolicy: Ignore` and the value of US1, R5 — Keeping the two health tests from disagreeing (FR-009), R6 — Where the rejection counter lives (FR-011b)

### Community 114 - "speckit-constitution/SKILL.md"
Cohesion: 0.33
Nodes (5): Outline, Post-Execution Checks, Pre-Execution Checks, Scope Guard, User Input

### Community 115 - "required"
Cohesion: 0.33
Nodes (6): required, type, coordination, inCluster, kubeconfig, namespace

### Community 116 - "enum"
Cohesion: 0.33
Nodes (6): enum, type, mode, none, oidc, static-keys

### Community 117 - "Security Policy"
Cohesion: 0.33
Nodes (5): Reporting a vulnerability, Scope notes, Security Policy, Supported versions, What to expect

### Community 118 - "Specification Quality Checklist: Lease Fencing and Isolation Safety"
Cohesion: 0.33
Nodes (5): Content Quality, Feature Readiness, Notes, Requirement Completeness, Specification Quality Checklist: Lease Fencing and Isolation Safety

### Community 120 - "Data Model: Lease Fencing and Isolation Safety"
Cohesion: 0.33
Nodes (5): Data Model: Lease Fencing and Isolation Safety, Key, Per-backend representation, Record, Record states and transitions

### Community 121 - "Quickstart Validation: Lease Fencing and Isolation Safety"
Cohesion: 0.33
Nodes (6): 1. Full suite (race-enabled) — the primary gate, 2. SQL backends against real engines, 3. Key validation end-to-end, 4. Monotonicity smoke test over the API, 5. Docs and drift gates, Quickstart Validation: Lease Fencing and Isolation Safety

### Community 122 - "Phase 1 Data Model: State-Volume Trust and Marker Freshness"
Cohesion: 0.33
Nodes (5): Freshness verdict, Mount classification, Phase 1 Data Model: State-Volume Trust and Marker Freshness, Rejection reason, State volume (`berth-state`)

### Community 123 - "Implementation Strategy"
Cohesion: 0.33
Nodes (6): Implementation Strategy, Incremental Delivery, MVP (User Story 1 only), Notes, Phase 1 investigation findings (T002, T003), Verified against a live cluster

### Community 124 - "load/fixtures/up.sh"
Cohesion: 0.60
Nodes (5): log(), patch_nodeport(), require(), up.sh script, wait_url()

### Community 125 - "speckit-taskstoissues/SKILL.md"
Cohesion: 0.40
Nodes (4): Outline, Post-Execution Checks, Pre-Execution Checks, User Input

### Community 126 - "properties"
Cohesion: 0.40
Nodes (5): properties, type, type, inCluster, namespace

### Community 127 - "enum"
Cohesion: 0.40
Nodes (5): enum, type, failurePolicy, Fail, Ignore

### Community 128 - "tokenFile"
Cohesion: 0.40
Nodes (5): type, path, tokenFile, properties, type

### Community 129 - "[CHECKLIST TYPE] Checklist: [FEATURE NAME]"
Cohesion: 0.40
Nodes (4): [Category 1], [Category 2], [CHECKLIST TYPE] Checklist: [FEATURE NAME], Notes

### Community 130 - "Contract: `internal/lease.Store`"
Cohesion: 0.40
Nodes (4): Contract: `internal/lease.Store`, Interface (after this feature), Manager behavior on top of the contract, Semantics every backend must provide (conformance-tested)

### Community 131 - "User Scenarios & Testing *(mandatory)*"
Cohesion: 0.40
Nodes (5): Edge Cases, User Scenarios & Testing *(mandatory)*, User Story 1 - A writable state volume cannot subvert enforcement (Priority: P1) — #96, User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2) — #98, User Story 3 - A rejected pod tells its owner exactly what to change (Priority: P3)

### Community 132 - "e2e/fixtures/up.sh"
Cohesion: 0.70
Nodes (4): install_operator(), log(), require(), up.sh script

### Community 133 - "extraArgs"
Cohesion: 0.50
Nodes (4): items, type, type, extraArgs

### Community 134 - "enum"
Cohesion: 0.50
Nodes (4): Always, IfNotPresent, Never, enum

### Community 135 - "stateDir"
Cohesion: 0.50
Nodes (4): stateDir, minLength, pattern, type

### Community 137 - "Contract: HTTP API changes"
Cohesion: 0.50
Nodes (4): Contract: HTTP API changes, Strengthened (not new) guarantees the API now actually delivers, What changes, What does not change

### Community 139 - "Dependencies & Execution Order"
Cohesion: 0.50
Nodes (4): Dependencies & Execution Order, Parallel Opportunities, Phase Dependencies, The US1 → US2 dependency is real

### Community 142 - "apiKeySecret"
Cohesion: 0.67
Nodes (3): properties, type, apiKeySecret

### Community 143 - "replicaCount"
Cohesion: 0.67
Nodes (3): replicaCount, minimum, type

### Community 145 - "Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2)"
Cohesion: 0.67
Nodes (3): Implementation for User Story 2, Phase 4: User Story 2 - A dead sidecar cannot leave a workload running unleased (Priority: P2), Tests for User Story 2

## Knowledge Gaps
- **564 isolated node(s):** `common.sh script`, `TargetRef`, `ScaleAction`, `$schema`, `title` (+559 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **12 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `WithTimeout()` connect `WithTimeout` to `parseConfig`, `run`, `T`, `api/leases.go`, `NewMemStore`, `Renewer`, `run`, `Server`, `Recorder`, `lease_test.go`, `berth-oidc-broker/main_test.go`, `Metrics`?**
  _High betweenness centrality (0.058) - this node is a cross-community bridge._
- **Why does `LoggingMiddleware()` connect `LoggingMiddleware` to `.Now`, `NewMux`, `testInjector`, `api/leases.go`, `run`?**
  _High betweenness centrality (0.043) - this node is a cross-community bridge._
- **Why does `run()` connect `run` to `NewMux`, `testInjector`, `NewServer`, `NewMemStore`, `MetricsMiddleware`, `Identity`, `LoggingMiddleware`?**
  _High betweenness centrality (0.036) - this node is a cross-community bridge._
- **Are the 19 inferred relationships involving `testInjector()` (e.g. with `TestInjectAcceptsExistingEmptyDirStateVolume()` and `TestInjectAddsStateMountWhenExistingMountPathDiffers()`) actually correct?**
  _`testInjector()` has 19 INFERRED edges - model-reasoned connections that need verification._
- **Are the 19 inferred relationships involving `optInPod()` (e.g. with `TestInjectAcceptsExistingEmptyDirStateVolume()` and `TestInjectAddsStateMountWhenExistingMountPathDiffers()`) actually correct?**
  _`optInPod()` has 19 INFERRED edges - model-reasoned connections that need verification._
- **Are the 34 inferred relationships involving `NewMemStore()` (e.g. with `buildStore()` and `newAuthzServer()`) actually correct?**
  _`NewMemStore()` has 34 INFERRED edges - model-reasoned connections that need verification._
- **Are the 26 inferred relationships involving `NewMux()` (e.g. with `run()` and `newAuthzServer()`) actually correct?**
  _`NewMux()` has 26 INFERRED edges - model-reasoned connections that need verification._