---
name: cicd-supply-chain
description: Use when adding or hardening supply-chain controls — SBOM generation (syft, CycloneDX), provenance attestations (SLSA), artifact signing/verification (cosign, sigstore), dependency pinning, vulnerability scanning (trivy, grype), signed commits, reproducible builds, and admission-time verification. Pair with `cicd-core` and the platform-specific CI skills.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# Supply Chain Security Mode

## Purpose
Use this skill when designing or hardening the software supply chain: SBOM generation, provenance attestations, artifact signing and verification, dependency pinning, vulnerability scanning, signed commits, and reproducible builds.

This skill is the release-integrity layer. Pair it with `cicd-core` for pipeline principles, the platform-specific CI/CD skills (`cicd-github-actions`, `cicd-azure-devops`) for wiring, and `security-review` for threat analysis.

## Skill Use
- Load this skill when the task is to add or review SBOM, provenance, signing, verification, or supply-chain policy in a repository.
- Treat this skill as the governing contract for what "a properly signed release" looks like.
- Keep organization-specific policy (required SLSA level, signing root of trust, verification enforcement point) in the invoking prompt.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Run the tools and read their output. `cosign verify`, `syft`, `grype`, `trivy`, `slsa-verifier`, `sigstore-rekor-cli` — evidence comes from invocations, not descriptions.
- Inspect published artifacts directly (`cosign tree`, `oras manifest fetch`) to confirm that SBOM, signature, and provenance are actually attached, not just claimed to be.
- Issue independent tool calls (scan source, scan image, fetch attestations, verify provenance) in parallel.
- When migrating policy, test enforcement in a staging admission controller before turning it on in production.

## Mental Model
Supply-chain security is a chain of custody from source to runtime. Each link should be verifiable without trusting the link before it:

1. **Source**: signed commits, branch protection, CODEOWNERS, review.
2. **Dependencies**: lockfiles, pinning, CVE scanning, license compliance, provenance where available.
3. **Build**: reproducible, hermetic, ephemeral, non-falsifiable identity (OIDC / hardware attested).
4. **Artifact**: SBOM, signature, build provenance attestation, all published alongside the artifact.
5. **Distribution**: registry with access control, signed manifests, replication policy.
6. **Admission**: runtime enforcement (Sigstore policy-controller, Kyverno, OPA/Gatekeeper) verifies everything above before execution.

A break in any link defeats the rest. Plan for the weakest link first.

## SLSA
**Supply-chain Levels for Software Artifacts** is the de facto framework. The numbers indicate increasing build-integrity requirements.

- **Build L1**: build process is documented and produces provenance. Low bar; still useful.
- **Build L2**: hosted build platform (GitHub Actions, ADO), version-controlled source, signed provenance.
- **Build L3**: hardened, ephemeral, isolated builder; provenance is non-falsifiable (e.g., signed by the builder identity, not the project). This is where most orgs should be aiming for production artifacts.

Rules:
- SLSA L3 requires that the build platform cannot be tampered with by the project team. GitHub Actions + SLSA generator workflows reach L3 for many cases.
- Meet the requirements, then claim the level. Self-attested "we're SLSA L3" without the mechanisms in place is meaningless.

## SBOM

### Format
- **SPDX** or **CycloneDX**. Both are industry-standard. Pick one per organization; mixing is a parsing burden for consumers.
- For container images, generate at build time (`docker buildx build --sbom=true`) or after (`syft <image>`).
- For source repos, `syft dir:.`, `syft <lockfile>`, or ecosystem-specific tools (`cyclonedx-gomod`, `cyclonedx-py`, `cyclonedx-npm`).

### Contents
A useful SBOM includes:
- all direct and transitive dependencies with pinned versions
- purl (package URL) identifiers for each component
- licenses (to the extent known)
- hashes for binary artifacts where applicable
- metadata: tool that generated it, timestamp, target artifact identity

### Publishing
- Attach the SBOM to the artifact in the registry (`cosign attest --predicate sbom.spdx.json --type spdxjson <image>`).
- Make SBOMs discoverable: link from releases, include in OCI referrers, expose an SBOM endpoint if appropriate.
- Keep SBOMs immutable per artifact. A changed SBOM is a different artifact.

## Provenance Attestations
Provenance records *how* an artifact was built: source repo, commit, builder identity, build parameters.

- **in-toto** is the underlying format; **SLSA provenance** is the specific predicate type.
- Generate with `slsa-github-generator`, GitHub's `actions/attest-build-provenance`, BuildKit's `--provenance` mode, or equivalents.
- Attach as a signed attestation: `cosign attest --type slsaprovenance --predicate provenance.json <artifact>`.
- Verify with `slsa-verifier verify-image --source-uri github.com/org/repo <image>` or `cosign verify-attestation --type slsaprovenance --certificate-identity ...`.

## Signing With Sigstore
Sigstore is the default signing system for open-source and increasingly enterprise use.

### Keyless Signing
- `cosign sign --yes <artifact>` — no long-lived key; signs using a short-lived certificate issued by Fulcio, bound to an OIDC identity (GitHub Actions OIDC token, email, workload identity).
- Transparency log entry goes to Rekor by default; verification includes proof that the signature is logged.
- For CI: the OIDC issuer is the build environment (GitHub Actions, ADO). Verification pins on the expected issuer and subject (repo and workflow).

### Verification Policy
```sh
cosign verify \
  --certificate-identity-regexp '^https://github.com/org/repo/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/org/repo@sha256:...
```

Pin:
- issuer (`--certificate-oidc-issuer`)
- subject identity (`--certificate-identity-regexp` constraining repo, workflow, and ref)
- digest (never tag)

Any of these not pinned turns the verification into theater.

### Key-Based Signing
- Keyed signing is still valid for closed environments, offline workflows, or high-assurance use.
- Use a KMS-backed key (AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault Transit). Never a plaintext private key on disk.
- Rotate and publish public keys through a verifiable source (Sigstore's [TUF](https://theupdateframework.io/) repository pattern, or an equivalent signed metadata repository).

### Signing SBOM And Provenance
- Sign the SBOM and provenance attestations, not just the artifact. An unsigned attestation is not an attestation, it's a claim.
- `cosign attest` signs the predicate at attach time; verify with `cosign verify-attestation`.

## Verification At Admission
Generate and sign all you want — if no one verifies, it doesn't help.

### Kubernetes
- **Sigstore policy-controller**: `ClusterImagePolicy` / `ImagePolicy` CRDs enforce cosign verification at admission.
- **Kyverno**: `verifyImages:` rule type; supports cosign, notary, and keyed verification.
- **Connaisseur**: older but still used in some orgs.
- **OPA/Gatekeeper + external-data provider**: possible but heavier.

Example Kyverno rule (rejects unsigned images from `ghcr.io/org`):
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-signed-images
spec:
  validationFailureAction: Enforce
  rules:
    - name: verify-ghcr-org
      match:
        any:
          - resources:
              kinds: [Pod]
      verifyImages:
        - imageReferences:
            - "ghcr.io/org/*"
          attestors:
            - entries:
                - keyless:
                    issuer: "https://token.actions.githubusercontent.com"
                    subject: "https://github.com/org/*/.github/workflows/release.yml@refs/tags/v*"
          mutateDigest: true
          required: true
```

- `mutateDigest: true` resolves tags to digests at admission so later image tampering can't move the tag.
- Roll out enforcement with `Audit` mode first, then flip to `Enforce` after the false-positive rate is acceptable.

### Non-Kubernetes
- For serverless (Lambda, Cloud Run, Functions): verify signatures during deploy, not at invocation. The deploy pipeline is the trust boundary.
- For VM-based deploys: verify in the image bake pipeline and pin by image ID.
- For package registries: npm, PyPI, and crates.io are exploring provenance. Prefer packages with attached provenance where available.

## Dependency Pinning
- Commit a lockfile for every language. The lockfile is part of the supply-chain contract.
- Pin **by exact version**, not range. A caret or tilde range defeats pinning.
- For container base images, pin **by digest** (`@sha256:...`).
- For CI actions, pin **by commit SHA** (see `cicd-github-actions`).
- For scripts fetched at runtime, pin by hash:

```sh
curl -fsSL https://example.com/installer.sh -o installer.sh
echo "<sha256>  installer.sh" | sha256sum -c -
sh installer.sh
```

Never `curl ... | bash` without verification.

### Update Flow
- Renovate or Dependabot raises PRs for pinned version bumps.
- Each PR runs full CI including vuln scans; a failing scan blocks merge.
- Auto-merge low-risk updates (patch versions with clean CI); require review for major versions.

## Vulnerability Scanning
- Scan **source** (`trivy fs`, `grype dir:.`, `govulncheck`, `pip-audit`, `npm audit`, `cargo audit`, `osv-scanner`).
- Scan **images** (`trivy image`, `grype <image>`).
- Scan **IaC** (`trivy config`, `checkov`, `tfsec`).
- Scan **secrets** (`gitleaks`, `trufflehog`) — every commit, every branch.

Rules:
- Fail CI on Critical or High with available fix. Warn on Medium/Low. Document exceptions with expiry dates.
- Track the full scan output as a CI artifact so historical regressions are traceable.
- For CVE noise (long tail of Low issues in base images), use a separate policy (Trivy's `.trivyignore`, vendor allowlists) with time-bound exceptions — not blanket suppression.
- Scan *after* publication too: a CVE disclosed tomorrow against an image you published today is still your problem. Periodic rescans on published artifacts.

## Signed Commits
- Require signed commits on protected branches.
- Prefer **gitsign** (keyless Sigstore signing tied to OIDC identity) over long-lived GPG keys where possible.
- Verify signatures in CI before merging, and at PR submission.
- CODEOWNERS + required reviews + signed commits is the minimum source-integrity stack.

## Reproducible Builds
- Reproducibility is a property, not a tool. A build is reproducible when the same source + build config produces bit-identical artifacts.
- Achieve by: pinning everything, eliminating timestamps (`-trimpath` for Go, `SOURCE_DATE_EPOCH` for others), hermetic builds (no network at build time beyond pinned dependencies), deterministic packaging.
- Useful even when not strictly achievable: two different CI runs producing the same digest is strong provenance evidence.

## Registry And Distribution
- Use an OCI registry that supports **referrers** (OCI 1.1) so SBOMs, signatures, and attestations can attach to the artifact manifest.
- Turn on registry scanning where available (ECR, GHCR, ACR, GCR native scanning) as a second layer on top of CI-time scans.
- Replicate production artifacts to a second registry (geo-redundancy, DR). Pin both copies identically.
- Enforce immutable tags for production (most registries have this setting). A mutable `:prod` tag is a foot-gun.
- For air-gapped environments, have a verifiable mirror process — not a tarball shipped by hand.

## Policy As Code
- Declare the policy: who can sign, what issuers are trusted, which artifacts require which attestations.
- Version the policy in Git; deploy it to the admission controller via GitOps.
- Evolve policy in `Audit` → `Enforce` stages. A policy that fails on production workloads at 3 AM is not a policy, it's an outage.
- Document exceptions with the original reason, the expiration date, and the fix plan.

## Anti-Patterns To Reject
- Signing without verifying
- Verification without pinning issuer, subject, and digest
- Floating tags in production (`:latest`, `:prod`, `:stable`)
- Long-lived signing keys stored in CI secrets
- `curl | bash` at any stage
- SBOMs generated but not attached or not signed
- Self-attested SLSA L3 without ephemeral, hardened builders
- Vulnerability scanners running but results ignored
- `.trivyignore` / `snyk.yaml` exception lists with no expiry dates
- Unpinned lockfiles or version ranges in production builds
- Dependency updates that bypass CI (manual merges, "urgent fix")
- Admission controller rules in `Audit` mode indefinitely
- Rebuilds per environment (breaks provenance chain)
- Signing commits optional on default branch
- Scanning source but not published images, or vice versa

## Completion Criteria
Do not consider a supply-chain task complete until all applicable items are true:
- lockfiles, base images, and CI actions/tasks are all pinned appropriately
- SBOM is generated, signed, and attached for published artifacts
- SLSA provenance is generated, signed, and attached
- signature verification is enforced at the admission boundary (cluster, deploy pipeline, package registry)
- vulnerability scanning runs on source, dependencies, and published artifacts, with failing criteria and exception process
- signed commits are required on protected branches
- the full chain (commit → build → artifact → admission) can be verified end-to-end from an external observer

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Supply Chain Security Mode together with cicd-github-actions.
Harden the release pipeline for /path/to/repo (Go service, GHCR container, deployed via Argo CD).
Generate CycloneDX SBOM at build, sign with cosign keyless (GitHub OIDC), attach SLSA provenance via slsa-github-generator.
Add Kyverno ClusterPolicy requiring cosign verification with issuer=token.actions.githubusercontent.com and subject constrained to this repo's release workflow.
Enable gitsign-signed commits on main; require signed commits in branch protection.
Report the verification command a consumer can run to validate an image from this release.
```
