# Security Policy

Berth is a lease-coordination service: clusters trust it to guarantee "at most
one holder" for critical workloads. We treat reports that undermine that
guarantee — or the confidentiality and availability of the API server, operator,
webhook, or sidecar — as security issues, and we handle them through
coordinated disclosure.

## Reporting a vulnerability

**Please do not open a public issue for suspected vulnerabilities.**

Report privately via GitHub's private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Choose **Report a vulnerability** and fill in the advisory form.

Direct link: <https://github.com/skaphos/berth/security/advisories/new>

If you cannot use GitHub, email <shawn@skaphos.io> with the details.

Include what you can of: affected component (apiserver, operator, webhook,
acquire sidecar, store backend, Helm chart), a reproduction or proof of
concept, the impact you believe it has, and any suggested fix.

## What to expect

- **Acknowledgement** within 7 days.
- **Assessment** and severity triage in the advisory thread; we may ask
  follow-up questions there.
- **Fix development** happens in the advisory's private fork when the issue is
  exploitable; hardening-grade findings may be fixed in public PRs.
- **Disclosure**: we publish the GitHub security advisory together with the
  patched release and credit the reporter (unless you prefer otherwise).

## Supported versions

Berth is pre-1.0. Only the latest minor release line receives security fixes;
please upgrade to the most recent release before reporting version-specific
issues.

## Scope notes

- Findings that require a user to already control the pod spec being gated
  (e.g. weakening their own injected sidecar) are usually hardening issues
  rather than vulnerabilities, but report privately if in doubt — we would
  rather triage a non-issue than miss a real one.
- Denial of service against an unauthenticated endpoint, cross-tenant access
  to lease state, and anything that lets two holders exist at once are always
  in scope.
