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
