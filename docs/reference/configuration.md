# Configuration Reference

This reference lists the runtime configuration surface that exists today. Helm
values are documented inline in each chart's `values.yaml`; this page captures
the behavior contributors and operators need to reason about across binaries.

## Precedence

Berth binaries currently use command-line flags. Helm charts render those
flags from chart values. Environment variables are only used where Kubernetes
manifests explicitly map them into container arguments or Secrets.

For deployed components, the practical precedence is:

1. Explicit binary flag.
2. Helm value rendered into a flag or mounted file.
3. Binary default.

## API Server Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--listen-addr` | `:8443` | HTTPS listen address. |
| `--metrics-addr` | `:8080` | Plain-HTTP address for the unauthenticated Prometheus `/metrics` endpoint, served on a separate port off the TLS/auth path (mirrors the operator). Empty disables it. Restrict the port to the monitoring stack with a NetworkPolicy. |
| `--log-level` | `info` | Log verbosity: `debug`, `info`, `warn`, or `error`. |
| `--log-format` | `json` | Log output format: `json` (machine-parseable, the default) or `text` (human-readable). |
| `--tls-cert-file` | empty | TLS certificate file. Required. |
| `--tls-key-file` | empty | TLS private key file. Required. |
| `--store-backend` | empty | `mem`, `k8s`, or `sql`. Empty uses the deprecated coordination-namespace heuristic. |
| `--coordination-kubeconfig` | empty | Kubeconfig for the coordination cluster. Empty means in-cluster config. Only valid with `k8s`. |
| `--coordination-namespace` | empty | Namespace where Kubernetes-backed lease objects are stored. Required with `k8s`. |
| `--sql-driver` | empty | `postgres`, `mysql`, or `sqlite`. Required with `sql`. |
| `--sql-dsn` | empty | SQL DSN. Mutually exclusive with `--sql-dsn-file`. For `mysql`, `loc` must be unset or `UTC`; `parseTime=true` is applied automatically. |
| `--sql-dsn-file` | empty | File containing the SQL DSN. Read once at startup; restart the API server after credential rotation. Mutually exclusive with `--sql-dsn`. |
| `--sql-migrate` | empty | `auto` or `off`. Defaults to `auto` for `sql`. With `off` the schema is verified at startup and the server refuses to start on drift. |
| `--auth-mode` | derived | `none`, `static-keys`, or `oidc`. Explicit value wins. |
| `--api-keys-file` | empty | Static key file with `<key-id>:<sha256-hex>` lines. Required with `static-keys`. Reloaded on SIGHUP. |
| `--oidc-issuer-url` | empty | OIDC issuer URL. Required with `oidc`. |
| `--oidc-audience` | empty | Expected JWT `aud` claim. Required with `oidc`. |
| `--oidc-required-claim` | repeatable | Required claim equality check, such as `groups=berth-clients`. |
| `--oidc-username-claim` | `sub` | Claim copied into the authenticated identity username. |
| `--oidc-tenant-claim` | `sub` | Claim copied into the tenant identity; its resolved value must not contain `/`. |
| `--oidc-jwks-url` | discovered | Override JWKS URL from issuer discovery. |

Auth defaults depend on the resolved backend: `mem` defaults to `none`; `k8s`
and `sql` default to `static-keys`.

The `sql` backend runs every Store operation inside an explicit SQL
transaction. Postgres and MariaDB/MySQL are the HA runner-local options.
SQLite is durable and ACID, but single-writer; use it only with one API server
replica for edge, dev, or CI deployments. In Kubernetes, mount the SQL DSN from
a Secret and pass `--sql-dsn-file`; the Helm chart does this from
`store.sql.dsnSecret`.

For MariaDB/MySQL the store validates the DSN at startup. Lease timestamps are
stored as zoneless `datetime(6)` and must round-trip in UTC, so a DSN whose
`loc` parameter names any other zone (including `Local`) is rejected with an
error naming the parameter. `parseTime=true` is enforced automatically. A
non-UTC `loc` would make renewed leases read back as already expired and let a
standby reclaim a lease its holder still believes it holds.

With `--sql-migrate auto` (the default), schema changes are applied
additively and idempotently at startup — including the `version` column that
upgrades tables created before it existed (legacy rows read as version 1).
With `--sql-migrate off`, apply the equivalent statement yourself before
upgrading, e.g. for Postgres:

```sql
ALTER TABLE berth_leases ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;
```

(MySQL/MariaDB and SQLite use the same column without `IF NOT EXISTS`.) With
`off`, the API server verifies at startup that `berth_leases` exists and
exposes every column it uses, without changing the schema. A missing table or
an unmigrated table fails startup with an error naming the problem, so drift
surfaces before the server is marked ready rather than on the first lease
operation. The check runs once; readiness probes remain a plain connection
ping.

## Operator Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--metrics-bind-address` | `:8080` | Metrics endpoint (set `0` to disable). Plain HTTP unless `--metrics-secure` is set. |
| `--metrics-secure` | `false` | Serve `/metrics` over HTTPS authenticated via Kubernetes TokenReview and authorized via SubjectAccessReview. Off by default (plain HTTP on a separate port, restrict with a NetworkPolicy); scrapers then need RBAC for `/metrics`. |
| `--health-probe-bind-address` | `:8081` | Health and readiness endpoint. |
| `--leader-elect` | `false` | Enable leader election so only one replica reconciles at a time. Required when running more than one replica (in-cluster HA). |
| `--leader-election-id` | `berth-operator-leader` | Name of the `coordination.k8s.io/Lease` electing the active replica. |
| `--leader-election-lease-duration` | `15s` | Duration non-leaders wait before force-acquiring leadership. |
| `--leader-election-renew-deadline` | `10s` | Duration the leader retries refreshing its lease before giving up. |
| `--leader-election-retry-period` | `2s` | Interval between leader-election attempts. |
| `--berth-api-server` | empty | API server base URL. Required. |
| `--berth-api-key` | empty | Static bearer token. Mutually exclusive with `--berth-api-key-file`. |
| `--berth-api-key-file` | empty | File containing bearer token. Re-read with a short cache for sidecar rotation. |
| `--cluster-id` | empty | Cluster-distinct holder identity. Overrides `spec.holderIdentity` when set. |
| `--berth-ca-bundle-file` | empty | PEM CA bundle appended to system trust for API server TLS verification. |
| `--berth-server-name` | API server host | SNI and certificate name override. |
| `--berth-insecure-skip-tls-verify` | `false` | Development-only TLS verification bypass. |
| `--enable-workload-admission` | `false` | Required for BerthLease targets; serves fail-closed workload admission. |
| `--workload-operator-user` | empty | Exact Kubernetes service-account username allowed to ungate registered Pods. |
| `--workload-operator-namespace` | empty | Operator namespace, excluded from target management and admission to permit recovery. |
| `--enable-injection-webhook` | `false` | Serve the `berth-acquire` pod-injection mutating webhook from the operator. |
| `--injection-helper-image` | empty | `berth-acquire` image stamped into opted-in Pods. Required when the webhook is enabled. |
| `--injection-helper-image-pull-policy` | `IfNotPresent` | Pull policy for both injected helper containers: `Always`, `IfNotPresent`, or `Never`. |
| `--injection-control-plane-namespaces` | `berth-system` | Comma-separated namespaces the webhook never mutates. |
| `--injection-helper-api-key-file` | empty | Path the injected helpers read the bearer token from, inside the workload Pod. Set together with `--injection-helper-api-key-secret`. |
| `--injection-helper-api-key-secret` | empty | Secret **in each opted-in workload's namespace** that the webhook mounts at `--injection-helper-api-key-file`. Set together with it. |
| `--injection-helper-api-key-secret-key` | `token` | Data key within that Secret holding the token. |
| `--injection-helper-ca-bundle-file` | empty | Path the injected helpers read the API server CA from. Only needed when TLS is not satisfied by system trust. Set together with `--injection-helper-ca-bundle-configmap`. |
| `--injection-helper-ca-bundle-configmap` | empty | ConfigMap in each opted-in workload's namespace that the webhook mounts at `--injection-helper-ca-bundle-file`. Set together with it. |
| `--injection-helper-ca-bundle-key` | `ca.crt` | Data key within that ConfigMap holding the CA bundle. |
| `--injection-state-dir` | `/berth` | Shared-volume mount path used by the injected init container and sidecar. Must be absolute. |
| `--injection-default-mode` | `runtime-singleton` | Default `berth.skaphos.io/mode` for Pods that omit the annotation. |
| `--injection-default-enforce` | `probe` | Default `berth.skaphos.io/enforce` for Pods that omit the annotation. |
| `--injection-default-ttl-seconds` | `30` | Default `berth.skaphos.io/ttl-seconds` for Pods that omit the annotation. |

See [Workload gating via injection](../workload-gating-injection.md) for the
opt-in label/annotation contract and usage.

## OIDC Broker Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--oidc-issuer-url` | empty | Issuer URL used for discovery. Required unless `--oidc-token-url` is set. |
| `--oidc-token-url` | empty | Token endpoint override. |
| `--oidc-client-id` | empty | OAuth2 client ID. Required. |
| `--oidc-client-secret` | empty | Client secret on command line. Prefer file form. |
| `--oidc-client-secret-file` | empty | File containing client secret. |
| `--oidc-audience` | empty | `audience` form parameter for IdPs that require it. |
| `--oidc-scopes` | empty | Comma-separated OAuth2 scopes. |
| `--output` | empty | Token output file. Required. Written atomically. |
| `--refresh-skew` | `60s` | Refresh this long before token expiry. |
| `--min-refresh-interval` | `30s` | Minimum refresh interval, and the first retry delay after a failed refresh. |
| `--fetch-timeout` | `30s` | Deadline for each token request. An endpoint that accepts the request and then stalls is abandoned after this long and retried. |
| `--max-retry-interval` | `5m` | Cap on the exponential retry backoff after a failed refresh. The last written token file is kept untouched until a refresh succeeds. |

## SQL Integration Test DSNs

The `test-integration` task runs SQL backend conformance tests against
operator-provided databases. This is intended for local kind-based test
topologies or CI environments with explicit database DSNs.

| Environment variable | Meaning |
| --- | --- |
| `BERTH_TEST_POSTGRES_DSN` | Postgres DSN used by `TestPostgresStoreConformance`. |
| `BERTH_TEST_MYSQL_DSN` | MariaDB/MySQL DSN used by `TestMariaDBStoreConformance`. |

If either variable is unset, that driver test is skipped. GitHub Actions only
runs the SQL integration task when at least one of these DSNs is configured as
a repository variable or secret.

## Helm Values

| Chart | Key | Meaning |
| --- | --- | --- |
| `berth-apiserver` | `store.backend` | Store backend rendered into `--store-backend`; one of `mem`, `k8s`, or `sql`. |
| `berth-apiserver` | `store.sql.*` | SQL driver, migration mode, and DSN source. Prefer `store.sql.dsnSecret` so the chart mounts a Secret and passes `--sql-dsn-file`. |
| `berth-apiserver` | `coordination.*` | Kubernetes Lease backend namespace, in-cluster mode, and optional kubeconfig Secret. |
| `berth-apiserver` | `tls.certManager.*`, `tls.existingSecret` | Exactly one TLS source is required. |
| `berth-apiserver` | `auth.mode` | `none`, `static-keys`, or `oidc`. Production should use `static-keys` or `oidc`. |
| `berth-apiserver` | `auth.staticKeys.secretName` | Secret containing the static API-key hash file. |
| `berth-apiserver` | `auth.oidc.*` | OIDC issuer, audience, JWKS override, claims, and tenant mapping. |
| `berth-apiserver` | `metrics.enabled`, `metrics.port` | Toggle the `/metrics` endpoint and its port (rendered into `--metrics-addr`). |
| `berth-apiserver` | `metrics.service.enabled` | Add the `metrics` port to the Service so a ServiceMonitor can target it. |
| `berth-apiserver` | `metrics.serviceMonitor.*` | Prometheus Operator `ServiceMonitor` (requires the `monitoring.coreos.com` CRDs): `enabled`, `interval`, `scrapeTimeout`, `additionalLabels`, relabelings. |
| `berth-apiserver` | `metrics.podAnnotations.enabled` | Alternative to a ServiceMonitor: render `prometheus.io/*` scrape annotations on the pod. |
| `berth-operator` | `clusterID` | Required for cross-cluster singleton deployments. Must differ per cluster. |
| `berth-operator` | `metrics.bindAddress` | `:<port>`, `<host>:<port>`, `[<ipv6>]:<port>`, a bare `<port>` (string or integer), or `0` to disable the listener, which also omits the metrics container port. Anything else, including URLs and unbracketed IPv6, fails rendering. |
| `berth-operator` | CRDs | The BerthLease CRD ships in `crds/`; Helm installs it on first install and never upgrades it. Pass `--skip-crds` to manage it out-of-band. There is no chart value for this. |
| `berth-operator` | `berth.apiServer` | Central API server URL. |
| `berth-operator` | `berth.apiKey.*` | Static token Secret source. |
| `berth-operator` | `berth.tokenFile.path` | Token file path written by a sidecar. |
| `berth-operator` | `berth.tls.*` | CA bundle, server name, and development-only insecure mode. |
| `berth-operator` | `sidecarBroker.*` | Optional OIDC broker sidecar configuration. |
| `berth-operator` | `workloadManagement.enabled` | Required for workload targets; uses shared `injection.webhook.tls`, independent of helper injection. See [upgrade and cleanup](../operations/operator-workload-cleanup.md). |
| `berth-operator` | `injection.enabled` | Serve the `berth-acquire` pod-injection webhook. Off by default. |
| `berth-operator` | `injection.helper.*` | Injected helper image and pull policy; the bearer-token pair (`apiKeyFile` + `apiKeySecret`) and CA pair (`caBundleFile` + `caBundleConfigMap`) the webhook mounts into the helper containers; and the shared `stateDir` (must be absolute). See [Authenticating injected Pods](../workload-gating-injection.md#authenticating-injected-pods). |
| `berth-operator` | `injection.defaults.*` | Default `mode`, `enforce`, and `ttlSeconds` for Pods that omit the annotation. |
| `berth-operator` | `injection.controlPlaneNamespaces` | Namespaces the webhook never mutates (the release namespace is always added). |
| `berth-operator` | `injection.webhook.*` | Shared mutating/validating admission `failurePolicy`, `timeoutSeconds`, service port, and object/namespace selectors. |
| `berth-operator` | `injection.webhook.tls.certManager.*`, `injection.webhook.tls.existingSecret` + `caBundle` | Exactly one serving-cert source is required when injection is enabled. |

## Static API Key File

`--api-keys-file` uses one entry per line:

```text
team-a:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

The value after `:` is the SHA-256 hex digest of the raw token. The raw token
is only distributed to clients. The key ID must be nonempty and must not contain
`/`; that character is reserved for holder names beneath a tenant. Invalid key
files fail startup or reload, with the previous keys retained on reload failure.
See [tenant migration](../architecture.md#migrating-slash-containing-tenants)
before upgrading a deployment with slash-containing tenant IDs.
