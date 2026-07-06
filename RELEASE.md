# Release Process

Berth uses Release Please to maintain the release pull request and GitHub
Actions to publish release artifacts.

## Release Flow

1. Commits land on `main` through pull requests.
2. `.github/workflows/release-please.yml` runs stock
   `googleapis/release-please-action`, which maintains a single rolling Release
   Please pull request from the Conventional Commits on `main`.
3. A maintainer reviews and merges the release pull request.
4. The same `release-please.yml` run then pushes the `vX.Y.Z` tag (Release
   Please runs with `skip-github-release`, so `release.yml` owns the GitHub
   release; the tag is pushed with the release-bot app token, because a tag
   pushed by the default `GITHUB_TOKEN` would not trigger `release.yml`).
5. `.github/workflows/release.yml` builds and publishes artifacts for `v*`
   tags.
6. `.github/workflows/docs.yml` publishes the versioned documentation site for
   the same `v*` tag with `mike`.

## Published Artifacts

The release workflow publishes:

- multi-architecture container image `ghcr.io/skaphos/berth-apiserver:vX.Y.Z`
- multi-architecture container image `ghcr.io/skaphos/berth-operator:vX.Y.Z`
- multi-architecture container image `ghcr.io/skaphos/berth-oidc-broker:vX.Y.Z`
- Helm chart `oci://ghcr.io/skaphos/charts/berth-apiserver`
- Helm chart `oci://ghcr.io/skaphos/charts/berth-operator`
- GitHub release notes and chart archives
- versioned documentation at `https://skaphos.github.io/berth/`

## Required Credentials

The Release Please workflow mints a GitHub App token (to open the release PR
and push the tag) using:

| Name | Type | Purpose |
| --- | --- | --- |
| `RELEASE_BOT_APP_ID` | repository or organization variable | GitHub App ID used by `actions/create-github-app-token`. |
| `RELEASE_BOT_PRIVATE_KEY` | repository or organization secret | Private key for the release bot GitHub App. |

The tag release workflow uses the automatic `GITHUB_TOKEN` for GHCR package
publishing and GitHub release creation. The workflow declares `contents: write`,
`packages: write`, `attestations: write`, and `id-token: write`.

The docs workflow also uses `GITHUB_TOKEN` with `contents: write` so `mike` can
push the rendered site to the `gh-pages` branch.

## Local Checks Before Release PR Merge

The release PR should have green CI, chart checks, and any required e2e checks.
For local parity:

```bash
go -C tools tool task verify-generated
go -C tools tool task lint
go -C tools tool task test
go -C tools tool task build
```

## Versioning

Release Please owns `.release-please-manifest.json`, `release-please-config.json`,
and `CHANGELOG.md`. Do not hand-edit generated release entries except to fix
clear release-note mistakes before merging the release PR.
