# CRD API Versioning Standard

> **Canonical source:** [`skaphos-resources/standards/crd-api-versioning-standard.md`](https://github.com/skaphos/skaphos-resources/blob/main/standards/crd-api-versioning-standard.md).
> This file is an in-repo mirror kept for Berth contributors and the published
> docs site. Propose changes to the standard there first, then re-sync this copy.

How Skaphos operators version the Custom Resource Definitions they own and
serve. The governance standard in `repository-governance.md` keeps changes
reviewable and releasable; the documentation standard in
`documentation-standard.md` keeps the *generated* API reference honest. This
document governs the thing those two point at but do not define: **the version
string in a CRD's `apiVersion`, and the compatibility contract that string
promises.**

Use it when adding a field to a CRD, when a change to a CRD would break
existing objects, or when deciding whether a resource is ready to graduate from
alpha.

Reference implementations and current state:

| Repo | Group | Served versions | Storage version | Maturity |
| --- | --- | --- | --- | --- |
| [`fathom`](https://github.com/skaphos/fathom) | `fathom.skaphos.io` | `v1alpha1` | `v1alpha1` | alpha |
| [`berth`](https://github.com/skaphos/berth) | `berth.skaphos.io` | `v1alpha1` | `v1alpha1` | alpha |

Both reference operators are single-version and alpha today. The expensive
machinery below — conversion, storage migration, deprecation windows — does not
exist in either repo yet, and that is correct: you build it the day you
introduce a second version, not before. This standard exists now so that day
follows a written procedure instead of an improvisation.

Berth's `BerthLease` kind lives at `berth.skaphos.io/v1alpha1`. Its alpha status
is exactly why the CRD can still say `at-least-once` is "reserved for a future
behavior" (see `api/v1alpha1/doc.go`): reserving and reshaping fields is a
liberty alpha grants and beta/GA revoke. The generated reference for the surface
is [`api.md`](api.md); this document is the policy for changing it.

## The one rule everything else follows

**A CRD version string is a compatibility contract, not a feature or release
number.** It follows the upstream Kubernetes API versioning and deprecation
conventions verbatim — we do not invent our own scheme:

- [API versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning)
- [Deprecation policy](https://kubernetes.io/docs/reference/using-api/deprecation-policy/)
- [Changing the API — compatibility](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api_conventions.md#backward-compatibility-gotchas)

When this document and upstream disagree, upstream wins and this document is the
bug. Everything below is the application of those conventions to a Skaphos
operator built with kubebuilder v4 + controller-runtime.

## What this does *not* govern

Several things carry a "version" and are routinely confused with the CRD API
version. None of them are governed here:

- **The operator's release version** (the SemVer that `release-please` cuts and
  that tags the operator images). Orthogonal to the CRD API version — see
  "Relationship to the operator release version" below.
- **Any in-process contract version** an operator maintains between its own
  components. That is an internal Go compatibility boundary, not a Kubernetes API
  surface.
- **The served versions of *third-party* CRDs** an operator inspects at runtime.
  Detecting which version of someone else's CRD is installed is reconcile
  behavior, not our API contract.

If it does not appear in the `apiVersion:` of a resource this operator
*defines*, it is out of scope.

## The maturity ladder

Every served version sits at exactly one maturity level. The level is encoded
in the version string and dictates what you are allowed to change without
introducing a new version.

| Level | Version strings | Availability | In-place breaking changes | Min support after deprecation |
| --- | --- | --- | --- | --- |
| **Alpha** | `v1alpha1`, `v1alpha2`, … | Served, but documented as unstable | Discouraged, but permitted | None — may be removed in any release |
| **Beta** | `v1beta1`, `v1beta2`, … | Served and safe for pre-production | **Forbidden** | ≥ 9 months or 3 releases, whichever is longer |
| **Stable (GA)** | `v1`, `v2`, … | Served; the version users should adopt | **Forbidden** | ≥ 12 months or 3 releases, whichever is longer |

Read the ladder as a ratchet: maturity only ever increases for a given schema,
and the compatibility promise only ever tightens.

### Alpha — `vNalphaM`

The escape hatch for a schema still being designed.

- **No backward-compatibility guarantee.** The schema may change, and existing
  objects may be dropped or require manual migration across an operator upgrade.
- Prefer to bump the alpha counter (`v1alpha1` → `v1alpha2`) for a breaking
  change so users have a name for "the old shape," but changing an alpha version
  in place is *permitted*. It is the only level where that is true.
- May be **removed in any release**, ideally with a release-note heads-up.
- Document the instability plainly in the CRD docs and the getting-started
  guide. Users must opt in with eyes open.

New kinds start here. A kind stays alpha until its schema has survived real use
without a breaking change for long enough to make a beta promise credibly.

### Beta — `vNbetaM`

The schema is believed correct and is being hardened.

- **Backward compatibility within the beta version is mandatory.** Once
  `v1beta1` ships, you may not make an incompatible change to it — an
  incompatible change requires `v1beta2` (or GA).
- Compatible additions (a new optional field) are fine and do not need a new
  version.
- Safe for pre-production and for users who accept a documented upgrade path.
- Once deprecated, a beta version must remain served for **≥ 9 months or 3
  operator releases, whichever is longer.**

### Stable (GA) — `vN`

The long-term contract.

- **A GA version never receives an incompatible change.** Not in a patch, not in
  a minor, not ever. If the shape must break, that is a *new* GA version (`v2`),
  served alongside `v1` with conversion, and `v1` enters deprecation.
- Compatible additions remain allowed.
- Once deprecated, a GA version must remain served for **≥ 12 months or 3
  operator releases, whichever is longer** — and never while objects are still
  stored in it (see storage migration).

## What "compatible" means at the field level

The maturity rules above hinge on one distinction: is a schema change
*compatible* or not? Apply the Kubernetes API change guidelines.

**Compatible** (allowed within a served beta/GA version):

- Adding a **new optional field** whose zero value preserves prior behavior.
- Adding a new value to an open-ended enum (as long as old clients tolerate
  unknown values).
- Relaxing validation (widening an allowed range, loosening a pattern).
- Adding a new `status` field.

**Incompatible** (requires a new API version):

- Removing or renaming a field, or changing its JSON tag.
- Changing a field's type, units, or semantics.
- Making an optional field required, or adding a default that changes the
  meaning of existing stored objects.
- Tightening validation so that a previously-valid object is now rejected.
- Removing an enum value.

Two rules have no exceptions at any maturity level, because violating them
silently corrupts data rather than breaking loudly:

1. **Never reuse a field name or JSON tag with a different meaning.** A retired
   field's name is retired with it.
2. **Never change the semantics of a field under an unchanged version string.**
   The whole point of the version is that a given `(group, version, field)`
   means one thing forever.

## Storage and served versions

A CRD may serve several versions at once but stores objects in exactly one.

- **Exactly one served version is the storage version** (`storage: true`,
  marked in Go with `+kubebuilder:storageversion`). Zero or two is a bug that
  `controller-gen` will reject.
- **Every served version must round-trip losslessly through the storage
  version.** A read-modify-write via any served version must not silently drop
  fields. If a field exists in one served version but not the storage version,
  it is lost on write — that is a design error, not an acceptable trade-off.
- The storage version should generally be the **most mature** served version.
- A version can be **served but not stored**, or **stored-history but no longer
  served** (`+kubebuilder:unservedversion` marks a known-but-not-served
  version). Track what is stored via the CRD's `status.storedVersions`.

While a group has a single version there is no conversion and none of this bites.
It all activates the moment a second version appears.

## Conversion

Two or more served versions require **conversion** between them.

- Use a **conversion webhook** with the controller-runtime hub-and-spoke
  pattern: one version implements `conversion.Hub`; every other served version
  implements `conversion.Convertible` (`ConvertTo` / `ConvertFrom`) against the
  hub. The hub is normally the storage version.
- Conversion must be **lossless in both directions** for fields that exist in
  both versions, and must have a defined, documented behavior for fields that
  exist in only one (annotation round-tripping for otherwise-unrepresentable
  fields is the upstream escape hatch).
- **Round-trip conversion tests are mandatory** once more than one version is
  served: for each pair, `A → hub → A` and `hub → A → hub` must be identity on
  shared fields. These tests are the safety net that makes a version bump
  reviewable.

## Introducing a new version

Bump the version when — and only when — you need an **incompatible change**, or
you are **graduating maturity** (alpha → beta → GA). Never bump it for a
compatible addition; add the field to the existing version instead.

Procedure (kubebuilder v4):

1. **Scaffold** the new version alongside the old one, keeping the old one
   served:
   `kubebuilder create api --group <group> --version <new> --kind <Kind>`.
2. **Choose the storage version** and mark exactly one with
   `+kubebuilder:storageversion` (usually the new, more mature version once its
   schema is settled).
3. **Implement and test conversions** between all served versions, with the
   round-trip tests above.
4. **Regenerate everything and let the drift gate prove it:** CRDs, DeepCopy,
   and the API reference (`docs/reference/api.md`) all regenerate and are caught
   by the `verify-generated` gate (see `documentation-standard.md` → "Generated
   references must be drift-gated"). Update the `PROJECT` file's resource list.
5. **Announce the deprecation** of the version being superseded, with a concrete
   removal timeline that respects the support windows above. Mark the old
   version with `+kubebuilder:deprecatedversion` (optionally
   `:warning="..."`), which surfaces a warning to `kubectl`.
6. **Migrate stored objects** to the new storage version, then prune the old
   version from `status.storedVersions`, then — and only then — stop serving and
   remove the old version. Removing a stored version before migrating strands
   data the API can no longer decode.

## Deprecation and removal

Removing a served version is the one irreversible act in this standard; it
follows the Kubernetes deprecation policy as a hard floor.

- A served version, once shipped, **cannot be removed without a deprecation
  announcement and window.** The windows are the "min support after deprecation"
  column above: GA ≥ 12 months / 3 releases; beta ≥ 9 months / 3 releases; alpha
  0 (removable at any release, ideally announced). "Releases" means minor
  releases of *that operator*; take whichever bound is longer.
- Mark the deprecated version in-tree (`+kubebuilder:deprecatedversion`) so the
  CRD advertises `deprecated: true` and a `deprecationWarning`.
- **Never remove the storage version while objects are stored in it.** Migrate
  first; confirm via `status.storedVersions`.
- The removal itself is a **breaking change to consumers** even when done by the
  book — call it out in the changelog and, for GA removals, in release notes.

## Relationship to the operator release version

The CRD API version and the operator's SemVer release are **orthogonal axes.**

- A patch or minor operator release **must not** ship an incompatible CRD schema
  change under an unchanged served version. Incompatible CRD changes ride a new
  *API version*, not merely a new operator version.
- A new GA API version *may* coincide with a major operator release, but neither
  requires the other. `release-please` versions the software; this standard
  versions the API.
- Adding a compatible field is a `feat:` in the operator (minor bump) and no CRD
  version change at all.

## Enforcement and tooling

- **`PROJECT`** tracks every `(group, version, kind)`. Keep it accurate; the
  scaffolding tools rely on it.
- **kubebuilder markers** are the source of truth for version state:
  `+kubebuilder:storageversion`, `+kubebuilder:unservedversion`,
  `+kubebuilder:deprecatedversion`.
- **`verify-generated`** already fails the build when CRDs, DeepCopy, or the
  generated API reference drift from the Go types (per
  `documentation-standard.md`). A version bump that forgets to regenerate cannot
  merge.
- **Round-trip conversion tests** are required once more than one version is
  served (above).
- **Recommended:** add a CRD schema-compatibility check to CI (e.g. diffing the
  generated `config/crd/bases/*.yaml` against the previous release, or a
  kube-api-linter pass) so an *accidental* incompatible change under an unchanged
  version fails a check instead of shipping.

## What must never happen

A checklist of the failure modes this standard exists to prevent:

- An incompatible change to a served **beta or GA** version under its existing
  version string.
- A field name or JSON tag **reused** with a new meaning.
- A served version **removed** without a deprecation window, or the **storage
  version** removed before stored objects are migrated.
- A CRD with **zero or two** storage versions.
- A second served version added **without conversion** and round-trip tests.
- A schema **incompatibly changed across operator releases** under an unchanged
  version (outside alpha).

## When to deviate

Alpha is the sanctioned place for churn — that is what it is *for*, and moving
fast there is not a deviation. Everything at beta and GA is a hard contract with
users and does not bend for convenience.

If a real situation cannot be expressed within these rules — a security fix that
demands a breaking change to a GA version faster than the deprecation window
allows, say — that is a genuine exception. Open a PR against the canonical
standard describing the case. It either becomes a documented carve-out or is
driven back into the standard shape. Do not deviate silently: a quiet
incompatible change to a stable API is exactly the outage this standard is
written to prevent.

## Ownership

The API types and the generators that turn them into CRDs are high-leverage and
belong under `CODEOWNERS` per `repository-governance.md` §4–§5:

```
/api/**                       @skaphos/maintainers
/PROJECT                      @skaphos/maintainers
/config/crd/**                @skaphos/maintainers
```
