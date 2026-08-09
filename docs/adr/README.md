# Architecture Decision Records

This directory records architecturally significant decisions for Berth — the
constraints that forced them, the options weighed, and the consequences. An ADR
is immutable once accepted: to change a decision, add a new ADR that supersedes
the old one rather than editing it.

## Conventions

- **Filename**: zero-padded sequential number + kebab-case title, e.g.
  `0001-pod-level-gating-for-injected-singletons.md`.
- **Format**: MADR-compatible (Status, Date, Deciders, Context, Decision
  Drivers, Considered Options, Decision Outcome, Consequences, Links).
- **Status**: `proposed` → `accepted` → (`deprecated` | `superseded by ADR-NNNN`).

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-pod-level-gating-for-injected-singletons.md) | Pod-level gating for injected singletons, not BerthLease scale actions | accepted |
| [0002](0002-label-annotation-opt-in-over-crd.md) | Opt into injection via pod-template labels/annotations, not a wrapper CRD | accepted |
| [0003](0003-sidecar-runtime-enforcement-by-container-kill.md) | Enforce at-most-once by killing the main container; probe default, signal opt-in | accepted (trust model amended by ADR-0004) |
| [0004](0004-state-volume-is-a-trust-boundary.md) | The shared state volume is a trust boundary, not shared scratch space | accepted |

ADRs 0001–0003 are extracted from
`docs/design/2026-05-workload-gating-injection-model.md` (SKA-437) and were
accepted on 2026-05-24.
