---
name: docker-image
description: Use when writing or reviewing Dockerfiles or OCI container images — base selection (scratch → distroless → slim), multi-stage builds, non-root user, layer discipline, BuildKit secret handling, SBOM, signing, multi-arch, registry labels. Pin bases by digest. Pair with `kubernetes-dev` for runtime concerns.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# Dockerfile And Container Image Mode

## Purpose
Use this skill when writing, modifying, or reviewing Dockerfiles and OCI container images. It covers build structure, base image selection, layer discipline, security baseline, secret handling, SBOM, and signing.

This skill focuses on the image itself. For how the image is *run* inside a cluster, pair it with `kubernetes/dev.skill.md`.

## Skill Use
- Load this skill when the task is to create, modify, or review a Dockerfile, container build, or image publishing workflow.
- Treat this skill as the governing contract for image structure, base selection, and build-time security.
- Keep repository-specific build conventions (registry, base images, labels, caching strategy) in the invoking prompt.
- Match the repository's existing toolchain (BuildKit, buildx, ko, kaniko, apko) rather than introducing a new one.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Build the image yourself and inspect the result. `docker build`, `docker image inspect`, and `docker history` (or `buildah`, `podman`) give evidence you can't get from reading a Dockerfile alone.
- Run `dive`, `docker sbom`, `syft`, or `trivy image` against the built image before claiming size or vulnerability guarantees.
- Issue independent tool calls (multiple build stages, vulnerability scans, SBOM generation, size diff) in parallel.
- For multi-arch builds, verify each platform actually builds — cross-compilation failure modes are platform-specific.

## Core Principles
- Small, minimal, and non-root by default. Anything else needs justification.
- One purpose per image. Do not bundle unrelated tooling for "convenience."
- Reproducible: pinned base images, pinned dependencies, deterministic build steps.
- Secrets never touch the image, even in intermediate layers.
- Image metadata (labels, SBOM, signatures) is part of the artifact, not an afterthought.

## Base Image Selection
Prefer bases in this order. Move down the list only when the tier above cannot meet a genuine requirement:

1. **`scratch`** — no OS at all. The right answer for self-contained static binaries (Go with `CGO_ENABLED=0`, Rust with `-C target-feature=+crt-static`, musl-built binaries). Smallest attack surface, smallest image, deterministic.
2. **Distroless** (`gcr.io/distroless/*`, Chainguard Images, Wolfi) — no shell, no package manager, minimal runtime. Use when you need a language runtime (JRE, Python, Node) but don't need a shell. Chainguard and Wolfi images ship frequent CVE patches; `gcr.io/distroless/static` is the distroless equivalent of scratch for static binaries.
3. **Slim official images** (`alpine`, `debian:*-slim`, `ubuntu:*-minimal`) — when you need a shell or a package manager at runtime, or when the dependency tree genuinely needs glibc (Debian/Ubuntu slim) or musl (Alpine). Prefer Alpine for size, Debian slim for glibc compatibility.
4. **Full distro images** — only when a tier 1–3 base demonstrably cannot be made to work. A full `ubuntu` or `python:3.12` image is rarely warranted in production.

Rules for every tier:
- Pin the base by digest (`@sha256:...`), not just a tag. Tags drift. The digest is the artifact identity.
- For non-scratch bases, prefer images from publishers with a clear CVE-patching cadence (Chainguard, Wolfi, Google distroless, Debian).
- For Alpine, be aware that `musl` differs from `glibc` in DNS resolution and threading; some binaries that work on Debian break on Alpine.
- For distroless, use `:nonroot` variants (uid 65532) by default.

## Multi-Stage Builds
Every non-trivial image should use a multi-stage build. The runtime stage should contain only what the process needs to run.

```dockerfile
# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26
ARG BASE_DIGEST=sha256:...   # pin to a specific distroless or scratch image

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
# Cache module downloads in a dedicated layer
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download
# Build statically linked, no cgo, with reproducibility flags
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
        -ldflags="-s -w -buildid=" \
        -o /out/app ./cmd/app

FROM scratch AS runtime
COPY --from=build /out/app /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 65532:65532
ENTRYPOINT ["/app"]
```

Rules:
- Name stages. Unnamed stages make targeted `--target` builds brittle.
- The build stage owns the build toolchain; the runtime stage is lean.
- Copy *only* the final artifact across stages. Do not copy source trees into the runtime stage.
- Copy `/etc/ssl/certs/ca-certificates.crt` into scratch images if the binary makes outbound TLS connections.
- For Go, build with `-trimpath` and `-ldflags="-s -w -buildid="` for smaller, more reproducible binaries; `CGO_ENABLED=0` enables true static linking.
- For Python, copy the venv or wheels from the build stage; do not reinstall at runtime.
- For Node, copy `node_modules` from the build stage after a production-only install (`npm ci --omit=dev`).

## BuildKit And Secrets
Assume BuildKit. Modern features make images smaller and safer.

- Enable BuildKit explicitly via `# syntax=docker/dockerfile:1.7` (or newer) at the top of the Dockerfile.
- Use `RUN --mount=type=cache,target=<path>` for package caches (apt, apk, go mod, npm, pip). Caches don't land in the image.
- Use `RUN --mount=type=bind,source=...,target=...` to access files without `COPY` (keeps them out of the image).
- **Never** use `ARG SECRET=...` or `ENV SECRET=...` for credentials. Use `RUN --mount=type=secret,id=github-token,target=/run/secrets/github-token cat /run/secrets/github-token | ...`. The secret is mounted for one step and never persisted.
- Do not write secrets to disk even inside a build stage; `docker history` and intermediate layers have exposed secrets before.
- For SSH access to private repos, use `RUN --mount=type=ssh`.

## Layer Discipline
- One logical change per `RUN` — but do not split a single logical install across `RUN` boundaries (e.g., `apt update` and `apt install` must be in the same `RUN`).
- Clean up in the same layer: `rm -rf /var/lib/apt/lists/*` after `apt install`, `rm -rf /root/.cache/pip` after `pip install`. Anything removed in a later layer still costs space.
- Order `COPY` statements from least to most frequently changed to preserve cache hits (e.g., copy `go.mod/go.sum` before copying source).
- Prefer `COPY` over `ADD`. `ADD` auto-expands tarballs and fetches URLs — surprising and rarely what you want.
- Consolidate repeated `ENV` and `LABEL` blocks into a single instruction.

## User And Filesystem
- Every runtime stage must set a non-root `USER`. Use a numeric UID (`65532:65532` or `10001:10001`) so Kubernetes `runAsNonRoot: true` admission doesn't need to resolve names.
- If the image writes at runtime, mount an `emptyDir` at the writable path in Kubernetes rather than making the filesystem writable. The image itself should still be designed as read-only.
- Do not `chown` the entire filesystem — it doubles image size. Use `COPY --chown=65532:65532` or set ownership at build time.
- Create the user in the build stage (`adduser` / `useradd`) only if the base image has a package manager; otherwise write `/etc/passwd` entries directly.

## Entrypoint And Command
- Prefer **exec form** (`ENTRYPOINT ["/app"]`) over shell form (`ENTRYPOINT /app`). Shell form spawns a shell that swallows signals and breaks graceful shutdown.
- For PID-1 concerns: if the process reaps children, use it directly. If it doesn't and zombies will accumulate, use `tini` or `dumb-init`.
- Set `STOPSIGNAL` explicitly when the process expects a non-default signal (e.g., `SIGINT` for some runtimes).
- Use `CMD` for default arguments to `ENTRYPOINT`, not for the command itself. This makes overrides predictable.

## Metadata And Labels
Adopt the OCI standard labels on every image:

```dockerfile
ARG VCS_REF
ARG BUILD_DATE
ARG VERSION
LABEL org.opencontainers.image.source="https://github.com/org/repo" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.title="app" \
      org.opencontainers.image.description="..." \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="..."
```

These power SBOM tools, registry UIs, and provenance chains. Treat them as required, not decorative.

## Healthcheck
- Define `HEALTHCHECK` only when the image is used outside Kubernetes. Inside Kubernetes, the kubelet runs the probes defined in the pod spec; a Dockerfile `HEALTHCHECK` is ignored by most runtimes and adds build-time noise.
- If the image is used in both contexts, prefer the Kubernetes probe and omit `HEALTHCHECK`.

## `.dockerignore`
- Every Dockerfile needs a `.dockerignore`. Without it, the full working directory ships to the build context — slow and leaks secrets.
- Start permissive: ignore `.git`, `node_modules`, `dist`, `target`, `*.env`, `*.log`, IDE files, test fixtures that aren't needed for the build.
- For BuildKit with `--mount=type=bind`, `.dockerignore` still applies to the bind context.

## Reproducibility And Pinning
- Pin the base image by digest.
- Pin language-level dependencies via lockfiles (`go.sum`, `poetry.lock`, `package-lock.json`, `Cargo.lock`, `uv.lock`) and copy them before copying source.
- Pin system package versions when deterministic behavior matters (`apt-get install -y foo=1.2.3`). For rolling distros (Alpine edge), pin the base image to a specific release (e.g., `alpine:3.20`) rather than `latest`.
- For Go, use `-trimpath` and drop `-buildid`; for Rust, use `--locked`; for Node, use `npm ci` over `npm install`.
- Set `SOURCE_DATE_EPOCH` when reproducible timestamps matter.

## Multi-Arch
- Use `docker buildx` with `--platform linux/amd64,linux/arm64` by default.
- Prefer native cross-compilation (Go's `GOOS`/`GOARCH`, Rust cross-targets) over QEMU emulation — it's dramatically faster.
- Verify each platform actually runs: cross-compilation succeeds silently even when the binary is broken for the target.

## SBOM And Signing
- Generate an SBOM at build time: `docker buildx build --provenance=true --sbom=true ...`, `syft`, or `docker sbom`. Publish it with the image (registry attachment via `cosign attach` or `--provenance` BuildKit output).
- Sign images with `cosign sign` (keyless via Sigstore is the default). Sign the SBOM and provenance attestation as well.
- At admission, verify signatures with `policy-controller`, `kyverno verify-images`, or `connaisseur`. An unsigned image in production defeats the purpose.

## Size Budgets
Reasonable starting budgets for a single service:

- scratch-based Go/Rust: **< 20 MB**
- distroless-based JVM/Python/Node: **< 150 MB** (language runtime dominates)
- Alpine-based with shell: **< 60 MB**
- Debian slim: **< 120 MB**

Images over these without justification indicate bundled debug tools, unused dependencies, or uncleaned caches.

## Anti-Patterns To Reject
- Running as root (no `USER` directive)
- Tagging the base image by mutable tag (`:latest`, `:3`, `:stable`) in production
- `ADD` from a URL or tarball when `COPY` would do
- `RUN curl ... | bash` without verification
- `apt-get update` and `apt-get install` in separate `RUN` statements
- Caching `/var/lib/apt/lists/*` or `~/.cache/pip` into the final image
- Secrets in `ARG`, `ENV`, or written to disk in any layer
- Copying the whole repository (`COPY . .`) into the runtime stage
- `CMD` instead of `ENTRYPOINT` for the primary binary
- Shell-form `ENTRYPOINT`/`CMD` when exec form works
- Missing `.dockerignore`
- `HEALTHCHECK` in images used exclusively under Kubernetes
- Debug tools (`curl`, `bash`, `vim`, `ps`) present in production runtime images
- Multi-purpose "kitchen sink" images used across unrelated services
- Images without OCI metadata labels

## Completion Criteria
Do not consider a container-image task complete until all applicable items are true:
- the base image is the tightest tier that meets the real requirement, pinned by digest
- multi-stage build separates toolchain from runtime
- runtime stage runs as a non-root numeric UID
- `ENTRYPOINT` is exec form and handles signals correctly
- no secret appears in any layer; BuildKit secrets are used where needed
- caches and package manager state are absent from the final image
- OCI metadata labels are present
- SBOM and signature are generated for production images
- a size measurement against the expected budget was taken

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Dockerfile And Container Image Mode.
Build a production image for cmd/app in /path/to/repo.
Default to scratch; fall back to distroless only if the binary requires a runtime.
Pin base by digest, use BuildKit with module and build caches, strip the binary, run as 65532:65532.
Generate SBOM and provenance. Target size under 25 MB. Verify with dive and trivy.
```
