# Contributing

## Workflow

- Branch from `main`.
- Keep each pull request focused on one logical change.
- Use concise, imperative commit subjects.
- Sign commits cryptographically.
- Include a DCO sign-off on every commit.
- Explain what changed, why it changed, how it was tested, and any
  documentation impact in the pull request.

This repository expects pull-request-based change management. Do not bypass
review or required checks for normal development work.

## Commit Messages and Releases

Releases are cut by [Release Please](https://github.com/googleapis/release-please)
from [Conventional Commits](https://www.conventionalcommits.org/) on `main`.
Pull requests are **squash-merged**, and GitHub composes the squash commit from
the pull request **title and description** — so those two fields are the commit
message Release Please parses. Write them accordingly:

- The **title** is the commit subject: `type(scope): summary`. Use `fix:` for
  bug fixes, `feat:` for new behavior, `docs:`/`chore:`/`test:`/`ci:` for the
  rest. The type decides whether the change appears in the changelog at all.
- The **description** is the commit body.

### Breaking changes

A change is breaking if an existing, working deployment can stop working after
upgrading — refused pod shapes, changed defaults, removed or renamed
configuration, altered API semantics. Breaking changes need **both** of the
following, because they do different jobs:

1. **`!` after the type or scope** — `fix(gating)!:`, `feat(api)!:`. This marks
   the commit breaking, drives the version bump, and creates the
   `⚠ BREAKING CHANGES` heading in the changelog.
2. **A `BREAKING CHANGE:` footer at the end of the pull request description.**
   This supplies the *text* printed under that heading.

The `!` alone is not enough. Without the footer, Release Please falls back to
the commit subject, which produces a heading with no actionable content beneath
it — this happened in v0.4.0 and the changelog had to be repaired by hand on
the release pull request.

Write the footer for an operator deciding whether it is safe to upgrade: what
breaks, what they must change, and any escape hatch. One footer per breaking
change.

```text
BREAKING CHANGE: Pods whose own containers mount the `berth-state` volume
with write access are now refused at admission, at any mount path, with no
opt-out. Mark the mount `readOnly: true` or use a separate volume. See
docs/operations/upgrade-state-volume-trust.md for a recipe that finds affected
workloads before upgrading.
```

Anything user-visible and breaking also needs an entry in `docs/operations/`
upgrade notes; the footer should point at it rather than repeat it.

## Local Validation

Requires Go 1.26+. Task is declared in `tools/go.mod`.

```bash
go -C tools tool task verify-generated
go -C tools tool task lint
go -C tools tool task test
go -C tools tool task build
```

For end-to-end validation:

```bash
go -C tools tool task e2e-all
```

`verify-generated` reruns CRD and DeepCopy generation and fails if generated
artifacts drift. Run it whenever `api/v1alpha1` changes.

## Generated Artifacts

Keep generated files in the same change as their source edits:

- `api/v1alpha1/zz_generated.deepcopy.go`
- `config/crd/berthlease.yaml`
- `deploy/helm/berth-operator/crds/berthlease.yaml`
- `docs/reference/api.md`

Use:

```bash
go -C tools tool task generate
go -C tools tool task manifests
go -C tools tool task docs:api-ref
```

## Documentation

User-visible behavior changes should update `README.md` or `docs/`.
Architecture, configuration, release, or contributor workflow changes should
update the matching document in this repository.

The documentation source of truth is portable Markdown under `docs/`, with
`README.md` kept as the short entry point.
