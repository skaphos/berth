# Berth Constitution

Derived from the Skaphos Constitution
(`skaphos-resources/standards/constitution.md`, v1.1.0). The upstream is
authoritative: this document inherits its principles in full, adds
Berth-specific principles and constraints, and MUST NOT weaken or contradict
anything upstream. When the upstream changes, this document is re-synced.

Berth is distributed lease coordination for Kubernetes multi-cluster
workloads: one shared lease surface so only the intended holder runs a
coordinated workload.

---

## Core Principles

### I. Explicit State Over Implicit Behavior

Operational concepts MUST be first-class declared primitives — durable objects
with lifecycle, status, policy, and history — not UI labels, wiki sections,
spreadsheet columns, or tribal knowledge. Systems MUST declare and enforce
desired state; behavior that depends on undocumented assumptions is a defect.

*Rationale: intent that is not explicit cannot be enforced, explained, or
recovered.*

### II. Git Is the Durable Desired-State Boundary

Every byte of intended platform and application state MUST be derivable from a
commit in a repository the organization controls. Intended state MUST
round-trip through Git before it becomes normal desired state. Break-glass
paths MUST exist, and MUST be explicit, auditable, attributable, and
reconciled back to Git. No tool may become an invisible second source of
truth.

*Rationale: this keeps recovery simple, audit grounded, and control-plane
behavior explainable.*

### III. Deterministic, Reconstructible Operation

Systems MUST behave predictably and be reconstructible from declared
configuration. Rendering and compilation steps MUST be deterministic.
Versioned artifacts are immutable and referenced by digest where the
ecosystem allows; identity-defining components are replaced, not mutated in
place.

*Rationale: determinism is what makes reconciliation trustworthy, drift
detectable, and disaster recovery a procedure instead of an archaeology
project.*

### IV. Kubernetes-Native, Never Obscured

Berth MUST integrate with Kubernetes primitives directly — CRDs, controllers,
reconciliation, status, events, ownership, admission — preferring operators
and controller-runtime over external orchestration where appropriate.
Higher-level APIs MUST preserve the control-plane model rather than bypass
it. Berth MUST NOT obscure or hide Kubernetes behavior; it clarifies and
enforces correct operation. Kubernetes-native does not mean Kubernetes-only
UX.

*Rationale: Kubernetes is a control-plane substrate, not a hosting product.*

### V. Compose, Don't Trap

Berth MUST do one important operational job well — distributed lease
coordination — expose its state clearly, and compose with other tools through
durable APIs, files, events, or Git. Berth MUST be independently adoptable
and provide concrete value standalone. As a Skaphos primitive, Berth MUST NOT
depend on the control planes that consume it. New hard dependencies between
Skaphos tools require an explicit, documented decision.

*Rationale: Skaphos is an ecosystem of focused platform-control tools, not a
monolith and not a trap.*

### VI. Explainable Reconciliation, Evidence-Grade Audit

For every lease transition, policy decision, or mutation, the system MUST be
able to show: input state, actor, target, action taken, observed result, and
failure reason. "Failed" without a reason and a next safe action is a defect.
Audit events MUST be structured, durable, correlated, and emitted by the same
system that made the decision — never reconstructed from log scraping.

*Rationale: a control plane that cannot explain its decisions is not
trustworthy.*

### VII. Read-Only Degradation Over Blindness

When mutation paths are degraded, operators MUST still be able to inspect
lease inventory, holders, health, and audit history. Designs MUST fail toward
read-only, never toward blindness.

*Rationale: read-only degradation is a feature; blindness during failure is
an architectural bug.*

### VIII. Topology Is Deployment State

Features that model delivery, policy, health, or audit MUST treat topology —
environment, region, cell, cluster type, failure domain, blast radius — as
part of deployment state, encoded in the data model, not reconstructed from
labels or convention.

*Rationale: a platform that cannot model where something runs cannot safely
answer basic operational questions.*

### IX. Technical Precision, Honest Scope

Documentation and specifications MUST describe actual, verified behavior —
not intent or aspiration — and MUST state plainly what Berth is *not* and
what its known limitations are. Marketing language and exaggerated claims are
forbidden in all repository content.

*Rationale: operational credibility is the product. A guarantee that is
documented but not delivered by the code is a defect in one of the two —
silent divergence is not an option.*

### X. Coordination Safety Is Non-Negotiable (Berth-specific)

Berth's core guarantee — a lease has at most one holder at any moment — is
the product. Any change touching the lease state machine, store CAS
semantics, fencing tokens, TTL/GC behavior, or store-key encoding MUST:

- preserve safety under arbitrary interleaving, process pauses, and retries
  (correctness MUST NOT depend on timing, sweep loops, or well-behaved
  clients);
- keep fencing tokens usable for downstream fencing exactly as documented —
  if the code provides a weaker property than the docs claim, one of them
  MUST change in the same unit of work;
- treat multi-tenant isolation as part of safety: no tenant may influence,
  observe, or collide with another tenant's lease through any encoding,
  namespace, or store-level artifact;
- ship with regression tests that exercise the concurrent interleaving or
  boundary being fixed or introduced, at the store boundary where the
  invariant is enforced.

*Rationale: Berth exists to make failover safe. A coordination service that
is correct only on the happy path is worse than no coordination service,
because users stop double-checking.*

---

## Engineering Constraints

Upstream standards are normative (`go-engineering-standard.md`,
`crd-api-versioning-standard.md`, `documentation-standard.md`,
`repository-governance.md`). Berth-specific bindings:

- **Stack**: Go (`go.mod` pins the toolchain); operator uses
  controller-runtime; CLI uses Cobra; `cmd/*` stays thin with behavior in
  `internal/` and `pkg/`. Preserve the split between cross-platform CLI
  concerns and Linux runtime components.
- **Store contract**: `internal/lease.Store` implementations (mem, k8s, sql)
  MUST provide linearizable per-key Get/Put (there is deliberately no delete
  operation — records are tombstoned, never removed, so fencing state is
  never reset) and share one conformance suite
  (`internal/lease/storetest`); a semantic change to the contract MUST
  update the contract doc, every backend, and the conformance suite in the
  same change.
- **Testing**: every regression fix ships with a covering test that fails
  before the fix; race-enabled CI; concurrency claims are tested with real
  interleavings, not comments.
- **Generated artifacts**: deepcopy, CRD manifests, and the Helm CRD copy are
  drift-gated (`verify-generated`); update them in the same change as source.
- **Helm**: any change under `deploy/helm/<chart>/` bumps that chart's
  `Chart.yaml` version in the same change (patch for docs/compatible, minor+
  for behavior).
- **Docs**: portable Markdown under `docs/`; update docs in the same change
  as user-visible behavior; `docs/code-map.md` and `docs/architecture.md`
  stay truthful to the code.
- **Commits**: signed, DCO signed-off, focused, imperative subjects; PR-based
  change management with CI as a required gate.

---

## Specification and Decision Workflow

- `skaphos-resources` is the canonical upstream for suite-level context
  (`FACTS.md`, `PROPOSAL.md`, `DECISIONS.md`, ADRs, `tools/BUILD-PLAN.md`,
  `tools/ECOSYSTEM.md`). Specs MUST cite settled findings rather than
  re-research them, and MUST NOT contradict an accepted ADR without proposing
  its supersession.
- Feature and fix work follows specify → plan → tasks, checked against this
  constitution. **Adopt before build** where `ECOSYSTEM.md` records mature
  prior art.
- Hard-to-reverse decisions get an ADR; ADRs are immutable and superseded,
  never rewritten.

---

## Governance

The Skaphos Constitution is the authoritative upstream; this document is its
Berth derivation. Amendments land by pull request against this file with
rationale in the PR description; upstream changes are re-synced here.
Version semantics: MAJOR for removing or redefining a principle, MINOR for
adding a principle or section, PATCH for clarifications.

Specs and plans are gated against this constitution. A deviation is either
justified in writing in the plan's complexity/deviation tracking, or a
proposed amendment — silent divergence is not an option.

**Version**: 1.0.1 | **Ratified**: 2026-07-24 | **Last Amended**: 2026-07-24
