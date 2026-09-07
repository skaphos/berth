# Release and Issue Workflow

This document describes how Berth issues move through Linear as code ships, and
the GitHub/Linear automation that drives it.

## Issue lifecycle

Berth uses a two-stage completion model that distinguishes *merged to trunk*
from *shipped in a release*:

| Stage | Linear status | Type | Driven by |
|-------|---------------|------|-----------|
| Work in progress | `In Progress` / `In Review` | started | Linear GitHub integration when a PR is opened against `main` |
| Merged to `main` | `Done` | completed | Linear GitHub integration when the PR merges (closing keyword) |
| Shipped in a release | `Released` | completed | `.github/workflows/linear-release.yml` on `release: published` |

`Done` means "on the trunk, not yet released." `Released` means "in a published
`vX.Y.Z` release and in users' hands."

## How releases are cut

Releases are produced by release-please:

1. `release-please.yml` runs stock `googleapis/release-please-action`, which
   maintains a rolling `chore(main): release X.Y.Z` PR on `main`, accumulating
   the changelog and version bump.
2. Merging that PR lets the same `release-please.yml` run push the `vX.Y.Z` tag.
3. The tag triggers `release.yml`, which builds/pushes images and charts and
   **publishes the GitHub release** using that version's reviewed section from
   `CHANGELOG.md`. Missing, duplicate or empty sections fail before artifact
   publication. GitHub's independent auto-generated notes are disabled.
4. The published release triggers `linear-release.yml`, which parses the release
   notes for `SKA-NNN` identifiers and moves each referenced issue to
   `Released` (skipping any that are `Canceled`/`Duplicate`), and leaves a
   "Released in `vX.Y.Z`" comment.

## Release artifact integrity (signing, SBOM, provenance)

`release.yml` hardens every published artifact with **keyless** Sigstore signing
(GitHub Actions OIDC → Fulcio short-lived certs, logged to Rekor). There are no
long-lived signing keys to manage.

Each released artifact carries:

- **Container images** (`berth-apiserver`, `berth-operator`, `berth-oidc-broker`):
  a BuildKit SBOM + provenance attestation (from `sbom: true` / `provenance:
  true`), a **cosign** signature over the image digest, and a SLSA
  build-provenance attestation pushed to GHCR via `actions/attest-build-provenance`.
- **Helm charts** (OCI artifacts in `ghcr.io/skaphos/charts`): a cosign signature
  over the pushed chart digest.
- **GitHub release assets**: an SPDX source SBOM (`berth-<version>.spdx.json`), a
  `checksums.txt` over the assets signed with `cosign sign-blob`
  (`checksums.txt.sig` + `checksums.txt.pem`), and a build-provenance attestation
  over the chart tarballs and SBOM.

### Verifying an image

```sh
cosign verify \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/skaphos/berth/\.github/workflows/release\.yml@refs/tags/v' \
  ghcr.io/skaphos/berth-apiserver@sha256:<digest>

# SLSA build provenance:
gh attestation verify oci://ghcr.io/skaphos/berth-apiserver@sha256:<digest> --repo skaphos/berth

Pin the issuer, the identity (repo + workflow + tag ref), and verify by digest —
omitting any of these makes the check meaningless.

### Verifying release blobs

```sh
cosign verify-blob \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/skaphos/berth/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate checksums.txt.pem --signature checksums.txt.sig checksums.txt
sha256sum -c checksums.txt
```

## Why `Released` is driven by the release, not the release PR

The release PR is an aggregated changelog: it re-surfaces every commit subject
since the last release, including any `closes SKA-NNN` keyword. Linear's GitHub
integration treats an **open** PR carrying a closing keyword as active work and
demotes the referenced issue back to a started state — so a freshly opened
release PR reopens every issue it bundles (this is what happened to SKA-275 on
the v0.1.0 release PR).

`linear-release.yml` therefore keys off the **published release event**, which
has no such side effect, instead of the release PR.

## Conventions that keep this clean

- **Keep closing keywords out of commit *subjects*.** Put `closes SKA-NNN`
  (or `fixes`/`resolves`) in the **PR description**, not the squash-commit
  subject. release-please echoes commit subjects into the release-PR changelog,
  so a keyword in the subject leaks and causes the reopen flap; a keyword in the
  PR body does not. A bare `(SKA-NNN)` reference in the subject is fine — it
  links the issue without toggling its state.
- One issue per PR where practical, so `Done`/`Released` transitions are
  unambiguous.

## Required Linear configuration

These are Linear console changes (not automatable via the API used here):

1. **Add the `Released` status.** Team **Skaphos** → Settings → Workflow /
   Statuses → add a status named exactly `Released` of type **Completed**,
   ordered after `Done`. The workflow looks it up by this exact name.
2. **Stop release PRs from reopening issues.** In the Linear GitHub integration
   settings, restrict automatic status changes to PRs targeting the **default
   branch** (or otherwise disable the "move to started when a linked PR opens"
   automation for `release/*` branches). This is the durable fix for the
   reopen flap; the commit-subject convention above is belt-and-suspenders.
3. **Keep PR-merge landing in `Done`.** Ensure the integration's "completed"
   target on merge stays `Done` (not `Released`) — `Released` is owned by the
   release event.

## Required GitHub configuration

- Add repo secret **`LINEAR_API_KEY`**: a Linear personal API key (Settings →
  Security & access → API → Personal API keys) or a service-account key with
  write access to the Skaphos team. Add it under repo Settings → Secrets and
  variables → Actions. Without it, `linear-release.yml` logs a notice and skips
  (it does not fail the release).

The team key and target status name are set in `linear-release.yml` env
(`TEAM_KEY: SKA`, `RELEASED_STATE: Released`); update them there if the team or
status name changes.

## Recording fixes merged through a security advisory

Advisory merges may land on `main` as `Merge commit from fork`. Those subjects
are not Conventional Commits, so release-please cannot infer a bug-fix entry.
Changing a private PR title does not repair an already-created upstream commit.

After the code is merged, create a normal follow-up PR with approved,
user-facing summaries and one `fix:` entry per delivered fix. Keep exploit
instructions and unpublished advisory details out of that public PR. Use
release-please's supported multi-change override at the end of the PR body:

```text
BEGIN_COMMIT_OVERRIDE
fix(component): describe the first delivered behavior change

fix(component): describe the second delivered behavior change
END_COMMIT_OVERRIDE
```

Squash-merge that PR so release-please can associate its commit with the PR
body. Keep the same entries in the signed commit message as a fallback. Do not
rewrite protected main history or manually edit the generated release PR as the
only record: regeneration can replace hand edits. The overrides become separate
changelog entries on the next release-please run. See the upstream
[release-note override documentation](https://github.com/googleapis/release-please#how-can-i-fix-release-notes).

Before merging the release PR, reconcile the private remediation ledger against
its rendered changelog: every merged fix needs an entry, migration requirements
need release notes, and no unmerged fix may be described as delivered. Keep the
advisories draft until the patched release and artifacts are available.

The publish workflow extracts this exact version section from the tagged
changelog with `scripts/release-notes.sh`; it does not replace those entries with
GitHub's PR-title summary. Test the extraction with
`scripts/test-release-notes.sh`.

## Dependency refresh baseline

The September 2026 refresh requires Go 1.27.1 for both application and development
tool modules. The Docker builders use that same release. Runtime containers use
Debian 13 distroless static images with numeric UID/GID 65532. This updates build
inputs; it does not migrate deployed databases or publish application images.

| Component | Baseline |
| --- | --- |
| Go | 1.27.1 |
| Kubernetes libraries / controller-runtime | 0.37.0 / 0.25.0 |
| controller-gen / staticcheck / golangci-lint | 0.22.0 / 0.8.1 / 2.13.2 |
| govulncheck / goimports | 1.7.0 / 0.49.0 |
| CI Python / stable Ubuntu runner | 3.14.7 / 24.04 |
| Helm / kind / Kubernetes test nodes | 4.2.4 / 0.33.0 / 1.37.0 |
| cert-manager / kube-prometheus-stack test infrastructure | 1.21.1 / 90.0.0 |
| Disposable load-test PostgreSQL | 18.6 Alpine |
| Buildx / BuildKit / cosign / Syft | 0.37.0 / 0.33.0 / 3.1.3 / 1.51.1 |

Action references and container images remain pinned to immutable commits and
image digests. Recheck publisher releases when refreshing; release-please owns
Berth application release numbers and chart `appVersion` packaging.

Kubernetes is a coordinated dependency family. Keep `k8s.io/kube-openapi` at
`v0.0.0-20260721132016-d427ff9ee9ad`, the revision selected by Kubernetes 1.37
and controller-runtime 0.25. Its newer development revision changes
structured-merge-diff schema types from v6 to v7 and does not compile with these
released clients. Revisit this exception when the Kubernetes family supports it.

The existing `github.com/google/cel-go` import family stays at v0.31.0. Its
v0.32.0 tag declares the new `cel.dev/cel-go` module path and cannot be upgraded
under the old path; migrate with the Kubernetes clients when they adopt it.

PostgreSQL's load fixture uses an emptyDir at `/var/lib/postgresql` and the
PostgreSQL 18 data path beneath it. Recreate disposable load fixtures when
upgrading. This is not an in-place PostgreSQL 16 data upgrade; production database
upgrades require their own migration. Existing benchmark snapshots describe
the versions originally measured and are not rewritten as new measurements.

Before merging a refresh, verify both module graphs, race tests, SQL integration,
generated artifacts, image builds, Helm renders and workflow syntax. Check
`govulncheck` using the repository's pinned Go version. Reverting the dependency
PR and rebuilding restores the previous code/toolchain inputs; it does not
reverse an external database upgrade.
