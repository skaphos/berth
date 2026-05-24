# Release Process

Berth uses Release Please to maintain the release pull request and GitHub
Actions to publish release artifacts.

## Release Flow

1. Commits land on `main` through pull requests.
2. `.github/workflows/release-pr.yml` updates the Release Please pull request.
3. A maintainer reviews and merges the release pull request.
4. `.github/workflows/release-tag.yml` pushes the release tag for merged
   release PRs.
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

Release PR and tag workflows mint a GitHub App token using:

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
