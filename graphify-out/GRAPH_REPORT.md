# Graph Report - .  (2026-07-23)

## Corpus Check
- 93 files · ~162,929 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1026 nodes · 2637 edges · 36 communities detected
- Extraction: 58% EXTRACTED · 42% INFERRED · 0% AMBIGUOUS · INFERRED: 1113 edges (avg confidence: 0.8)
- Token cost: 33,000 input · 12,500 output

## Community Hubs (Navigation)
- [[_COMMUNITY_ADRs & Design Docs|ADRs & Design Docs]]
- [[_COMMUNITY_Entrypoints & Acquire Config|Entrypoints & Acquire Config]]
- [[_COMMUNITY_API Authz Tests & Store Benchmarks|API Authz Tests & Store Benchmarks]]
- [[_COMMUNITY_Acquire Loop & Enforcement|Acquire Loop & Enforcement]]
- [[_COMMUNITY_Lease Stores (SQL & K8s)|Lease Stores (SQL & K8s)]]
- [[_COMMUNITY_Operator Scale Actions|Operator Scale Actions]]
- [[_COMMUNITY_Injection Webhook Edge Cases|Injection Webhook Edge Cases]]
- [[_COMMUNITY_Load Driver & API Routes Tests|Load Driver & API Routes Tests]]
- [[_COMMUNITY_Authentication (OIDC & Static)|Authentication (OIDC & Static)]]
- [[_COMMUNITY_CRD Types & Go Client|CRD Types & Go Client]]
- [[_COMMUNITY_Lease API Handlers|Lease API Handlers]]
- [[_COMMUNITY_E2E Cluster Tests|E2E Cluster Tests]]
- [[_COMMUNITY_Client Options & Config|Client Options & Config]]
- [[_COMMUNITY_Metrics & Readiness|Metrics & Readiness]]
- [[_COMMUNITY_API Server Lifecycle|API Server Lifecycle]]
- [[_COMMUNITY_K8s Client Builder|K8s Client Builder]]
- [[_COMMUNITY_Tenant Authorization|Tenant Authorization]]
- [[_COMMUNITY_Store Conformance & Integration|Store Conformance & Integration]]
- [[_COMMUNITY_Contributor Guidelines|Contributor Guidelines]]
- [[_COMMUNITY_Auth Interfaces|Auth Interfaces]]
- [[_COMMUNITY_No-Op Authenticator|No-Op Authenticator]]
- [[_COMMUNITY_Tenant Resolver|Tenant Resolver]]
- [[_COMMUNITY_Operator Lease Client|Operator Lease Client]]
- [[_COMMUNITY_API Group Registration|API Group Registration]]
- [[_COMMUNITY_Webhook Contract|Webhook Contract]]
- [[_COMMUNITY_Tenant Package Doc|Tenant Package Doc]]
- [[_COMMUNITY_Auth Package Doc|Auth Package Doc]]
- [[_COMMUNITY_K8s Package Doc|K8s Package Doc]]
- [[_COMMUNITY_Lease Package Doc|Lease Package Doc]]
- [[_COMMUNITY_API Package Doc|API Package Doc]]
- [[_COMMUNITY_Operator Package Doc|Operator Package Doc]]
- [[_COMMUNITY_Console Package Doc|Console Package Doc]]
- [[_COMMUNITY_API Types Package Doc|API Types Package Doc]]
- [[_COMMUNITY_Generated Deepcopy|Generated Deepcopy]]
- [[_COMMUNITY_Client Package Doc|Client Package Doc]]
- [[_COMMUNITY_Changelog|Changelog]]

## God Nodes (most connected - your core abstractions)
1. `New()` - 79 edges
2. `run()` - 61 edges
3. `Run()` - 39 edges
4. `NewServer()` - 30 edges
5. `NewMemStore()` - 29 edges
6. `NewMux()` - 27 edges
7. `optInPod()` - 26 edges
8. `testInjector()` - 25 edges
9. `newScheme()` - 25 edges
10. `NewRecorder()` - 20 edges

## Surprising Connections (you probably didn't know these)
- `run()` --calls--> `Scenario`  [INFERRED]
  test/load/main.go → internal/load/config.go
- `TestNewValidatesConfig()` --calls--> `New()`  [INFERRED]
  internal/lease/sqlstore/sqlstore_test.go → pkg/client/client.go
- `newScheme()` --calls--> `TestAddToSchemeRegistersBerthLeaseTypes()`  [INFERRED]
  internal/operator/reconciler_test.go → api/v1alpha1/types_test.go
- `Local validation tasks` --semantically_similar_to--> `Build/test/dev task commands`  [INFERRED] [semantically similar]
  CONTRIBUTING.md → AGENTS.md
- `Project structure and module organization` --semantically_similar_to--> `CLAUDE.md repository guidelines`  [INFERRED] [semantically similar]
  AGENTS.md → CLAUDE.md

## Hyperedges (group relationships)
- **Runtime-singleton at-most-once enforcement path** — workload_gating_injection_runtime_singleton, workload_gating_injection_probe, workload_gating_injection_signal, workload_gating_injection_berth_acquire, 0003_sidecar_runtime_enforcement_by_container_kill_adr [EXTRACTED 1.00]
- **Tag-driven release pipeline (release PR to tag to artifacts to Linear)** — release_release_please, release_release_workflow, release_docs_workflow, release_and_issue_workflow_linear_release_yml [EXTRACTED 1.00]
- **Phased scalability and load-testing program** — scalability_sizing_model, scalability_phase1_store_benchmarks, scalability_phase2_metrics, scalability_phase3_load_driver, readme_load_harness, mem_benchmark, sqlite_benchmark [EXTRACTED 1.00]

## Communities

### Community 0 - "ADRs & Design Docs"
Cohesion: 0.03
Nodes (110): ADR-0001: Pod-level gating for injected singletons, Rejected alternative: hybrid helper-writes-status, Rejected alternative: reuse BerthLease scale actions, Decision: pod-level gating (init hold + sidecar enforce), Rationale: helper in-pod vantage cannot scale, ADR-0002: Opt into injection via labels/annotations, not a wrapper CRD, Rejected alternative: Manual injection, Rejected alternative: Wrapper CRD (+102 more)

### Community 1 - "Entrypoints & Acquire Config"
Cohesion: 0.04
Nodes (80): Enforce, Mode, oidcConfig, storeConfig, cliFlags, ConfigFromEnv(), secondsEnv(), getter() (+72 more)

### Community 2 - "API Authz Tests & Store Benchmarks"
Cohesion: 0.06
Nodes (73): failingManager, newAuthzServer(), postBearer(), TestAuthzAllowsInTenantHolderAcrossNamespaces(), TestAuthzBareTenantHolderAllowed(), TestAuthzDeniesHolderOutsideTenant(), TestAuthzNoneModeBypassesHolderBinding(), benchmarkAcquireParallel() (+65 more)

### Community 3 - "Acquire Loop & Enforcement"
Cohesion: 0.06
Nodes (47): Enforcer, fakeClient, Hold(), probeEnforcer, procScanner, Renewer, signalEnforcer, State (+39 more)

### Community 4 - "Lease Stores (SQL & K8s)"
Cohesion: 0.06
Nodes (54): dialectFor(), applyRecordToLease(), k8sLeaseName(), leaseFromRecord(), NewK8sLeaseStore(), recordFromLease(), newK8sStore(), sampleRecord() (+46 more)

### Community 5 - "Operator Scale Actions"
Cohesion: 0.09
Nodes (48): applyAction(), cronJobTarget(), deploymentTarget(), newCountingClient(), newCronJob(), ptr(), TestApplyActionScaleAbsentConvergesToSingleWrite(), TestApplyActionSkipsUpdateWhenAlreadySuspended() (+40 more)

### Community 6 - "Injection Webhook Edge Cases"
Cohesion: 0.1
Nodes (46): findVolume(), TestInjectAcceptsExistingEmptyDirStateVolume(), TestInjectAddsStateMountWhenExistingMountPathDiffers(), TestInjectForcesExistingStateMountReadOnly(), TestInjectMountsAuthSourcesIntoHelpers(), TestInjectNoAuthMountsWhenUnset(), TestInjectorConfigValidate(), TestInjectPrependsAheadOfExistingInitContainers() (+38 more)

### Community 7 - "Load Driver & API Routes Tests"
Cohesion: 0.07
Nodes (46): readyManager, leaseName(), TestConfigValidate(), TestLeaseNamingIsDistinctAndStable(), validConfig(), Config, LeaseClient, OpResult (+38 more)

### Community 8 - "Authentication (OIDC & Static)"
Cohesion: 0.09
Nodes (41): OIDCAuthenticator, OIDCConfig, StaticAuthenticator, testIssuer, buildAuthenticator(), TestNoOpAuthenticatorAcceptsEverything(), claimAsString(), claimContains() (+33 more)

### Community 9 - "CRD Types & Go Client"
Cohesion: 0.07
Nodes (22): New(), TestPingRequiresBaseURL(), AcquireResult, Key, Manager, Record, Store, TestPingFailsOnNon2xx() (+14 more)

### Community 10 - "Lease API Handlers"
Cohesion: 0.08
Nodes (45): AcquireRequest, errorResponse, fakeAuthenticator, identityCtxKey, LeaseManager, LeaseResponse, ReleaseRequest, RenewRequest (+37 more)

### Community 11 - "E2E Cluster Tests"
Cohesion: 0.13
Nodes (28): LeaseClient, sleep(), clusterRef, applyInjectedDeployment(), copyTokenSecret(), ensureNamespace(), hasInitContainer(), initNames() (+20 more)

### Community 12 - "Client Options & Config"
Cohesion: 0.11
Nodes (26): Config, Option, TestNewAppliesOptionsAndTrimsBaseURL(), TestWithHTTPClientNilLeavesDefaultClient(), baseConfig(), TestApplyDefaults(), TestApplyDefaultsStartupGateReleaseFalse(), TestHolderExplicitOverride() (+18 more)

### Community 13 - "Metrics & Readiness"
Cohesion: 0.08
Nodes (16): ReadinessChecker, readinessGate, recordingMetrics, RequestMetrics, statusRecorder, Metrics, MetricsMiddleware(), TestHandlerExposesBerthSeries() (+8 more)

### Community 14 - "API Server Lifecycle"
Cohesion: 0.12
Nodes (17): Option, Server, serverConfig, promMetrics, serveMetrics(), newPromMetrics(), TestServeShutsDownOnContextCancel(), TestNewServerDefaultsAndOptions() (+9 more)

### Community 15 - "K8s Client Builder"
Cohesion: 0.21
Nodes (13): buildConfig(), NewClientset(), TestBuildConfigAppliesRaisedDefaults(), TestBuildConfigHonorsExplicitOverrides(), TestBuildConfigInvalidKubeconfig(), TestBuildConfigNonPositiveFallsBackPerField(), TestNewClientsetBuildsWithValidKubeconfig(), TestNewClientsetInvalidKubeconfig() (+5 more)

### Community 16 - "Tenant Authorization"
Cohesion: 0.24
Nodes (10): NewDefaultAuthorizer(), TestDefaultAuthorizerHolder(), TestDefaultAuthorizerHolderRejectsUntenantedIdentity(), TestDefaultAuthorizerNamespaceIsPermissive(), TestDefaultAuthorizerNamespaceRejectsUntenantedIdentity(), TestIdentityResolverFailsClosed(), TestIdentityResolverReturnsTenant(), Authorizer (+2 more)

### Community 17 - "Store Conformance & Integration"
Cohesion: 0.42
Nodes (8): BenchmarkMariaDBStore(), BenchmarkPostgresStore(), integrationDSN(), integrationStoreFactory(), TestMariaDBStoreConformance(), TestPostgresStoreConformance(), RunStoreConformance(), sampleRecord()

### Community 18 - "Contributor Guidelines"
Cohesion: 0.22
Nodes (10): Build/test/dev task commands, Chart version bump rule, CLI vs Linux runtime split, Project structure and module organization, CLAUDE.md repository guidelines, DCO sign-off and signed commits, Generated artifacts kept in sync, Local validation tasks (+2 more)

### Community 19 - "Auth Interfaces"
Cohesion: 0.67
Nodes (2): Authenticator, Identity

### Community 20 - "No-Op Authenticator"
Cohesion: 0.67
Nodes (1): NoOpAuthenticator

### Community 21 - "Tenant Resolver"
Cohesion: 1.0
Nodes (1): Resolver

### Community 22 - "Operator Lease Client"
Cohesion: 1.0
Nodes (1): LeaseClient

### Community 23 - "API Group Registration"
Cohesion: 1.0
Nodes (0): 

### Community 24 - "Webhook Contract"
Cohesion: 1.0
Nodes (0): 

### Community 25 - "Tenant Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 26 - "Auth Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 27 - "K8s Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 28 - "Lease Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 29 - "API Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 30 - "Operator Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 31 - "Console Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 32 - "API Types Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 33 - "Generated Deepcopy"
Cohesion: 1.0
Nodes (0): 

### Community 34 - "Client Package Doc"
Cohesion: 1.0
Nodes (0): 

### Community 35 - "Changelog"
Cohesion: 1.0
Nodes (1): CHANGELOG managed by Release Please

## Knowledge Gaps
- **82 isolated node(s):** `storeConfig`, `oidcConfig`, `metaOwner`, `Resolver`, `Identity` (+77 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **Thin community `Tenant Resolver`** (2 nodes): `resolver.go`, `Resolver`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Operator Lease Client`** (2 nodes): `leaseclient.go`, `LeaseClient`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `API Group Registration`** (2 nodes): `groupversion_info.go`, `addKnownTypes()`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Webhook Contract`** (1 nodes): `contract.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Tenant Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Auth Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `K8s Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Lease Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `API Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Operator Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Console Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `API Types Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Generated Deepcopy`** (1 nodes): `zz_generated.deepcopy.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Client Package Doc`** (1 nodes): `doc.go`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.
- **Thin community `Changelog`** (1 nodes): `CHANGELOG managed by Release Please`
  Too small to be a meaningful cluster - may be noise or needs more connections extracted.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `New()` connect `CRD Types & Go Client` to `Entrypoints & Acquire Config`, `API Authz Tests & Store Benchmarks`, `Acquire Loop & Enforcement`, `Lease Stores (SQL & K8s)`, `Operator Scale Actions`, `Load Driver & API Routes Tests`, `Authentication (OIDC & Static)`, `Lease API Handlers`, `Client Options & Config`, `Metrics & Readiness`, `API Server Lifecycle`, `K8s Client Builder`, `Tenant Authorization`, `Store Conformance & Integration`?**
  _High betweenness centrality (0.199) - this node is a cross-community bridge._
- **Why does `run()` connect `Entrypoints & Acquire Config` to `API Authz Tests & Store Benchmarks`, `Acquire Loop & Enforcement`, `Lease Stores (SQL & K8s)`, `Operator Scale Actions`, `Injection Webhook Edge Cases`, `Load Driver & API Routes Tests`, `Authentication (OIDC & Static)`, `CRD Types & Go Client`, `Client Options & Config`, `Metrics & Readiness`, `API Server Lifecycle`, `Tenant Authorization`?**
  _High betweenness centrality (0.173) - this node is a cross-community bridge._
- **Why does `Run()` connect `Load Driver & API Routes Tests` to `Entrypoints & Acquire Config`, `API Authz Tests & Store Benchmarks`, `Acquire Loop & Enforcement`, `Lease Stores (SQL & K8s)`, `Operator Scale Actions`, `Injection Webhook Edge Cases`, `Lease API Handlers`, `E2E Cluster Tests`, `Client Options & Config`, `K8s Client Builder`, `Tenant Authorization`, `Store Conformance & Integration`?**
  _High betweenness centrality (0.085) - this node is a cross-community bridge._
- **Are the 77 inferred relationships involving `New()` (e.g. with `run()` and `validateArgs()`) actually correct?**
  _`New()` has 77 INFERRED edges - model-reasoned connections that need verification._
- **Are the 37 inferred relationships involving `run()` (e.g. with `New()` and `.NewClient()`) actually correct?**
  _`run()` has 37 INFERRED edges - model-reasoned connections that need verification._
- **Are the 34 inferred relationships involving `Run()` (e.g. with `run()` and `TestCoordinationLost()`) actually correct?**
  _`Run()` has 34 INFERRED edges - model-reasoned connections that need verification._
- **Are the 28 inferred relationships involving `NewServer()` (e.g. with `run()` and `TestResolveTokenURLViaDiscovery()`) actually correct?**
  _`NewServer()` has 28 INFERRED edges - model-reasoned connections that need verification._