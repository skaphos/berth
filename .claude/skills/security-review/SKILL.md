---
name: security-review
description: Use to run a focused security review of code, configuration, or infrastructure across Go, Python, Terraform, Helm, Kubernetes, CI/CD, or Dockerfiles. Produces prioritized findings backed by tool evidence (gosec, bandit, semgrep, trivy, gitleaks, checkov, tfsec, kubesec, kyverno test). Skip for deep architecture audits (use `*-audit`) or ordinary PR review.
---

<!-- SPDX-FileCopyrightText: 2026 Rillan AI LLC -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

<!-- version: 0.1.0 -->
# Security Review Mode

## Purpose
Use this skill to run a focused security review of code, configuration, or infrastructure. It produces prioritized findings with evidence and concrete fixes.

Security review is narrower than a deep audit (use `*-audit` for that) and broader than a PR review (use a code-review skill for that). It is reusable across stacks: Go services, Python APIs, Terraform modules, Helm charts, Kubernetes manifests, CI/CD pipelines, and Dockerfiles.

## Skill Use
- Load this skill when the task is a security-focused pass over a defined scope.
- Treat this skill as the governing contract for scope, evidence, severity, and output format.
- Keep scope, threat model, and known constraints in the invoking prompt.
- When this skill conflicts with casual review behavior, follow this skill.

## Tool Use
This skill is tool-agnostic and works with Claude Code, Codex, OpenCode, and similar assistants. Map its guidance to whatever file-reading, editing, search, and shell-execution tools your environment exposes.

- Every finding must be anchored to a tool invocation: the file read, the grep result, the scanner output, the command that surfaced it.
- Run the scanners that fit the stack (`gosec`, `bandit`, `semgrep`, `trivy`, `trufflehog`, `gitleaks`, `checkov`, `tfsec`, `kubesec`, `kube-score`, `kyverno test`) and treat their output as evidence.
- Issue independent tool calls (secret scans, dependency scans, config greps, multi-file reads) in parallel.
- If a check cannot be performed (inaccessible secret store, missing runtime access), record it under `UNREVIEWED` rather than guessing.

## When To Use
Use this skill for:
- a dedicated security pass on a repository, branch, or change set
- threat-surface analysis for a new feature before it ships
- pre-release hardening review
- post-incident security review focused on root causes and hardening opportunities
- routine periodic review against a known threat model

Do not use this skill for:
- deep architecture audits (use `*-audit`)
- ordinary PR review (use a code-review skill)
- legal or compliance audits — this skill is technical, not contractual

## Required Inputs
The invoking prompt must provide:
- scope: repository path, directory, branch, or change set
- what kind of system this is (internet-facing service, internal service, CLI, platform controller, library)

Recommended inputs:
- known threat actors or trust boundaries
- compliance constraints that raise or lower the bar for specific checks
- areas to exclude (e.g., "don't review experimental/")
- prior findings to re-check

If critical context is missing (internet-facing vs. internal, auth model), stop and ask.

## Operating Stance
- Find the vulnerability; do not re-derive the framework around it.
- Prefer evidence over intuition. If you can't anchor a finding to code or tool output, it isn't a finding yet.
- Severity is about blast radius and exploitability, not elegance of the fix.
- A missing control is only a finding when the threat model calls for it. Not every service needs every control.
- Report real findings, not theoretical ones. Hypothetical risks without a path to exploit are noise.

## Threat Surfaces
Walk the surfaces below in order. Skip surfaces that genuinely don't apply to the scope (and say so in the output).

### 1. Authentication And Session
- Who is the caller? How is identity established — mTLS, JWT, OIDC, API key, SPIFFE, cloud IAM?
- Is every entrypoint authenticated, or are there unauthenticated paths? Which ones, and is that intentional?
- How are tokens validated? Signature, issuer, audience, expiry, scope?
- Session fixation, replay, and rotation behavior.
- Service-to-service identity: shared secrets vs. workload identity. Prefer the latter.

### 2. Authorization
- Is every authenticated request also authorized against a policy?
- Role/permission model: is it consistently enforced at every layer, or only at the edge?
- IDOR: can a user access another user's resource by guessing or manipulating an identifier?
- Privilege escalation paths: admin endpoints, sudo-style capability grants, reflection over roles.
- Multi-tenancy: is tenant isolation enforced in queries, not just in the URL?

### 3. Input Validation And Output Encoding
- Every untrusted input should be validated at the boundary closest to ingress.
- SQL injection: are parameterized queries used everywhere? No string concatenation into SQL.
- NoSQL injection (query operators), command injection (exec, `shell=True`, template engines), LDAP injection.
- XSS (reflected, stored, DOM) — output encoding matches the output context (HTML, attribute, JS, URL).
- SSRF: requests to URLs derived from user input. Allowlist destinations; block link-local and cloud metadata (169.254.169.254, fd00:ec2::254).
- XXE: disable external entity resolution in XML parsers.
- Unsafe deserialization of untrusted data (Python's `pickle`, Ruby's `Marshal`, PyYAML's non-`SafeLoader` path, Java native serialization) — ban it, or require a signed/typed envelope.
- Regex denial-of-service (ReDoS): reject catastrophic backtracking patterns, use re2 where available.

### 4. Secrets
- No secret in source, config, or test fixtures. Run a secret scanner.
- Secrets injected via secret store (Vault, AWS/GCP/Azure Secret Manager, External Secrets, SOPS) — not environment variables when files work, because environment variables leak via process listings and crash reports.
- Rotation story: can a secret be rotated without code changes? What's the MTTR for a leaked secret?
- Do error messages, logs, or debug endpoints include secret material?

### 5. Transport And Storage
- TLS everywhere, including internal service-to-service calls. No plaintext in transit.
- Modern ciphers only. No SSLv3, TLS 1.0, 1.1, or known-weak ciphers.
- Certificate validation enabled — no `InsecureSkipVerify`, `verify=False`, `strictSSL: false`, or equivalents.
- At-rest encryption for databases, object stores, backups, and state.
- Data classification: is PII, credential, or secret material stored in a place that matches its sensitivity?

### 6. Dependencies And Supply Chain
- Lockfile present and committed. Versions pinned.
- Run a vulnerability scanner against dependencies (`govulncheck`, `pip-audit`, `npm audit`, `cargo audit`, `trivy fs`, `grype`).
- High/critical CVEs with available fixes are findings. CVEs without fixes are documented risks.
- Transitive dependencies: how deep is the tree, and how is it reviewed?
- For container images, scan the image itself, not just the source repo.
- For CI, check for unpinned third-party actions and scripts piped to shell.

### 7. Configuration And Deployment
- Default-deny network policy for Kubernetes workloads. No implicit 0.0.0.0/0 ingress.
- Principle of least privilege for IAM/RBAC: no `*` on `*`, no `cluster-admin` on application workloads.
- No public cloud buckets/databases unless explicitly intended.
- Kubernetes Pod Security: non-root, read-only root FS, drop all capabilities, no `privileged`, no `hostNetwork`.
- Secret material is not projected via `env` when a file mount would serve.
- Debug endpoints (`/debug/pprof`, admin UIs, liveness-bypass flags) not exposed in production.

### 8. Logging, Monitoring, And Incident Response
- Auth failures, authz denials, and input-validation rejections are logged with enough context to investigate.
- Logs do not contain secrets, tokens, full PII, or request bodies that may contain them.
- Correlation identifiers (trace ID, request ID) flow end-to-end.
- Alerts exist for the classes of events that indicate an incident in progress (sudden spike in 401s/403s, new IPs hitting admin endpoints).

### 9. Cryptography
- Use well-reviewed libraries; never hand-roll primitives.
- Deterministic algorithms only where safe (HMAC, yes; ECB mode, no; nonce reuse, no).
- Random values from cryptographically secure sources (`crypto/rand`, `secrets`, `os.urandom`) — not `math/rand`, `random.Random`.
- Key storage matches the threat model: HSM/KMS for high-value keys, memory-locked secret buffers for short-lived secrets, never plaintext on disk.
- Check for MD5/SHA1 used in security contexts (password hashing, signatures). Password hashing specifically needs Argon2id, scrypt, or bcrypt — never plain hash.

### 10. Business Logic And Abuse
- Rate limiting on every authenticated and unauthenticated entrypoint that can be abused.
- Anti-automation on signup, login, password reset, send-email, and other enumerable flows.
- Resource limits: request size, connection count, concurrent uploads, memory per request.
- Idempotency where replay can cause damage (payments, email sends, state transitions).
- Race conditions in check-then-act flows: transfer funds, assign unique identifiers, claim resources.

## Severity Guidance
Use **CVSS intuition**, not the full calculator. Pick the severity that matches how you'd explain it to an oncall engineer.

- **Critical**: remote, unauthenticated attacker can take over the service, steal secrets or data at scale, or run arbitrary code. Fix now; block release.
- **High**: authenticated attacker or chained exploit leads to data access, escalation, or denial of service. Fix before next release.
- **Medium**: information disclosure, hardening gap, or weakness that requires specific conditions to exploit. Fix in the next sprint.
- **Low**: defense-in-depth improvement, policy drift, missing non-critical control. Fix when convenient.
- **Info**: observation or educational note. Not a finding.

Do not inflate severity. A finding that gets downgraded by reviewers erodes trust in the review.

## Output Contract
- Output only Markdown.
- Group findings by severity (Critical → High → Medium → Low).
- Each finding has these fields:

```markdown
### [Severity] Short title

- **Location**: `path/to/file.ext:123` (symbol or resource name if relevant)
- **Evidence**: <exact quote, command output, or tool hit>
- **Impact**: <what an attacker gains, in one or two sentences>
- **Exploitability**: <preconditions; is it remote, authenticated, chainable>
- **Fix**: <concrete change; point at the exact line or add a short code sketch>
- **References**: <CWE, CVE, OWASP, vendor advisory if applicable>
```

End the review with:

```markdown
## Summary
- <n> Critical, <n> High, <n> Medium, <n> Low
- <one sentence on overall posture>

## UNREVIEWED
- <surface> — <why it wasn't reviewed and what would unblock it>
```

## Scoping Rules
- Respect the declared scope. If you find something important outside scope, note it in `UNREVIEWED` and recommend a follow-up, do not expand the review silently.
- Re-verify the threat model against the actual system. A stated threat model that doesn't match the code is itself a finding.
- For internal tools, adjust severity by exposure: an admin-only endpoint has a different risk profile than an internet-facing one.

## Anti-Patterns To Reject
- Generic "security checklists" without evidence
- Severity based on theoretical worst case, not actual exploitability
- Findings with no fix
- Reproducing a scanner's raw output as the review
- Inflating Low findings to Medium to pad the report
- Reviewing code without running at least one relevant tool
- Ignoring the stated threat model
- Moralizing ("this design is bad") in place of actionable findings
- Suggesting controls that don't match the threat model (e.g., requiring HSMs for a development tool)

## Completion Criteria
Do not consider a security review complete until all applicable items are true:
- every applicable threat surface was walked or explicitly skipped with reason
- at least one scanner appropriate to the stack was run
- each finding has location, evidence, impact, exploitability, and a concrete fix
- severities are calibrated and justified
- `UNREVIEWED` lists surfaces that could not be checked and what would unblock them
- the summary reflects reality, not aspirational completeness

## Invocation Template
Use this skill with a prompt that supplies repository-specific context. Example:

```text
Use Security Review Mode.
Scope: /path/to/service (internet-facing Go API, mTLS between services, Postgres, Redis, deployed to Kubernetes).
Threat model: untrusted external clients, trusted internal callers, no admin UI in this service.
Focus on input validation, authz, SSRF, and supply chain. Run gosec, govulncheck, trivy image, gitleaks.
Report findings grouped by severity with concrete fixes.
```
