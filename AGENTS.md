# Repository Guidelines

## Project Structure & Module Organization
- `cmd/apiserver`, `cmd/operator`, and `cmd/berth`: entrypoints for the API server, operator, and CLI.
- `api/v1alpha1`: CRD API types and generated deepcopy code.
- `internal/`: private application packages for API wiring, auth, lease coordination, Kubernetes integration, and operator logic.
- `pkg/client`: public Go client surface for Berth.
- `config/`: generated CRDs, RBAC, and deployment-oriented manifests.
- `deploy/helm/`: Helm charts for the API server and operator.

## Build, Test, and Development Commands
- `make generate`: regenerate deepcopy code.
- `make manifests`: regenerate CRD manifests.
- `make build`: build local binaries into `bin/`.
- `make test`: run the Go test suite.
- `go test ./... -coverprofile=coverage.out`: run tests with coverage output.
- `make lint`: run `golangci-lint` and `go vet`.

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
- Preserve the split between cross-platform CLI concerns and Linux runtime components.
