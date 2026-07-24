# Contract: HTTP API changes

Base paths (unchanged):

```text
POST /v1alpha1/namespaces/{namespace}/leases/{name}/acquire
POST /v1alpha1/namespaces/{namespace}/leases/{name}/renew
POST /v1alpha1/namespaces/{namespace}/leases/{name}/release
```

## What changes

**New 400 responses** on all three endpoints when the key is invalid,
evaluated *after* authentication/authorization (no pre-auth oracle; 401/403
ordering unchanged):

| Condition | Status | Body (`error` field) |
|---|---|---|
| `{namespace}` is not an RFC 1123 DNS label (dots, uppercase, `_`, > 63 chars, …) | 400 | names the `namespace` field and the allowed format |
| `{name}` is not an RFC 1123 DNS subdomain | 400 | names the `name` field and the allowed format |
| `len(namespace) + 1 + len(name) > 253` | 400 | names the combined-length bound |

## What does not change

- Success response schema (`acquired`, `holder`, `fencingToken`,
  `expiresAt`, `acquiredAt`) — byte-for-byte identical.
- Conflict semantics: lost leases still return `200` with
  `acquired=false` and the current holder's state; release remains
  idempotent.
- `pkg/client` and the operator: keys sourced from Kubernetes
  namespace/CR names already satisfy the rules.

## Strengthened (not new) guarantees the API now actually delivers

- `fencingToken` in responses is strictly increasing per key across the
  entire life of the key's data — including across release, expiry, and GC —
  so downstream systems may safely reject any token ≤ the highest they have
  seen (the `docs/concepts.md` guidance becomes true as written).
- Two distinct accepted keys can never observe or affect each other's lease
  in any backend.
