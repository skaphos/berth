# Repository Guidelines

## Project Structure & Module Organization
- `cmd/apiserver`, `cmd/operator`, `cmd/berth`, and `cmd/berth-oidc-broker`: entrypoints for the API server, operator, CLI, and OIDC token broker.
- `api/v1alpha1`: CRD API types and generated deepcopy code.
- `internal/`: private application packages for API wiring, auth, lease coordination, Kubernetes integration, and operator logic.
- `pkg/client`: public Go client surface for Berth.
- `config/`: generated CRDs, RBAC, and deployment-oriented manifests.
- `deploy/helm/`: Helm charts for the API server and operator.
- `docs/`: portable Markdown documentation; keep `README.md` as the short front door.
- `mkdocs.yml` and `.github/workflows/docs.yml`: MkDocs Material and `mike` publishing configuration.

## Build, Test, and Development Commands
- `go -C tools tool task generate`: regenerate deepcopy code.
- `go -C tools tool task manifests`: regenerate CRD manifests and sync the Helm CRD copy.
- `go -C tools tool task verify-generated`: fail if generated artifacts are stale.
- `go -C tools tool task build`: build local binaries into `bin/`.
- `go -C tools tool task test`: run the Go test suite with coverage output.
- `go -C tools tool task lint`: run `golangci-lint`.

## Coding Style & Testing Guidelines
- Go version: `go 1.26` (see `go.mod`).
- Keep `cmd/*` thin and place non-trivial behavior in `internal/` or `pkg/` packages.
- Prefer focused unit tests in the same package as the code under test.
- Generated files must be kept in sync with source changes; update them in the same change.

## Commit & Pull Request Guidelines
- Keep commits focused and use concise, imperative subjects.
- All commits for this repository must be cryptographically signed.
- Include a DCO sign-off when committing.
- Do not create unsigned commits and do not bypass signing.

## Agent Notes
- Update generated artifacts when API types or manifests change.
- Update `docs/` or top-level process docs when user-visible behavior, architecture, configuration, release, or contributor workflow changes.
- Preserve the split between cross-platform CLI concerns and Linux runtime components.
- Use [docs/code-map.md](docs/code-map.md) for package ownership and [docs/architecture.md](docs/architecture.md) for runtime behavior.
