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

1. `release-pr.yml` maintains a rolling `chore(release): release vX.Y.Z` PR on
   `main`, accumulating the changelog and version bump.
2. Merging that PR triggers `release-tag.yml`, which pushes the `vX.Y.Z` tag.
3. The tag triggers `release.yml`, which builds/pushes images and charts and
   **publishes the GitHub release** (with auto-generated notes).
4. The published release triggers `linear-release.yml`, which parses the release
   notes for `SKA-NNN` identifiers and moves each referenced issue to
   `Released` (skipping any that are `Canceled`/`Duplicate`), and leaves a
   "Released in `vX.Y.Z`" comment.

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
