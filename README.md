# Berth

Distributed lease coordination for Kubernetes multi-cluster workloads.

Berth gives multiple clusters one shared lease surface so only the intended
holder runs a coordinated workload. It ships an HTTPS API server, a Kubernetes
operator for `BerthLease` custom resources, a small CLI scaffold, and an OIDC
token-broker sidecar for operator authentication.

## Components

| Component | Purpose |
| --- | --- |
| `apiserver` | HTTPS lease API backed by in-memory state, Kubernetes `Lease` objects in a coordination cluster, or a SQL database. |
| `operator` | Controller that reconciles `BerthLease` resources and applies suspend or scale actions to target workloads. |
| `berth` | CLI entrypoint. Lease commands currently exist as stubs while the Go client is the supported direct-integration path. |
| `berth-oidc-broker` | Sidecar that fetches OIDC client-credentials tokens and writes them to a file consumed by the operator. |

## Quickstart

Choose a topology first:

| Topology | Backend | When to use it |
| --- | --- | --- |
| Cross-cluster coordination plane | `k8s` | One central Berth API server coordinates workloads across multiple tenant clusters. |
| Runner-local HA | `sql` with Postgres or MariaDB | Berth runs inside the runner cluster and stores lease state in an external SQL database. |
| Runner-local edge/dev | `sql` with SQLite, or `mem` | Single API server replica only. SQLite persists locally; `mem` is ephemeral. |

Install the API server in a coordination cluster with the Kubernetes Lease
backend:

```bash
helm install berth-apiserver deploy/helm/berth-apiserver \
  --namespace berth-system --create-namespace \
  --set store.backend=k8s \
  --set coordination.namespace=berth-coordination \
  --set coordination.inCluster=true \
  --set tls.certManager.enabled=true \
  --set tls.certManager.issuerRef.name=berth-ca \
  --set tls.certManager.issuerRef.kind=ClusterIssuer \
  --set auth.mode=static-keys \
  --set auth.staticKeys.secretName=berth-api-keys
```

For a runner-local SQL deployment, store the DSN in a Kubernetes Secret and
let the chart mount it as the `--sql-dsn-file` input:

```bash
kubectl -n berth-system create secret generic berth-sql-dsn \
  --from-literal=dsn='postgres://berth:secret@postgres.berth.svc:5432/berth?sslmode=require'

helm install berth-apiserver deploy/helm/berth-apiserver \
  --namespace berth-system --create-namespace \
  --set store.backend=sql \
  --set store.sql.driver=postgres \
  --set store.sql.dsnSecret.name=berth-sql-dsn \
  --set tls.certManager.enabled=true \
  --set tls.certManager.issuerRef.name=berth-ca \
  --set tls.certManager.issuerRef.kind=ClusterIssuer \
  --set auth.mode=static-keys \
  --set auth.staticKeys.secretName=berth-api-keys
```

Install the operator in each tenant cluster with a distinct cluster identity:

```bash
helm install berth-operator deploy/helm/berth-operator \
  --namespace berth-system --create-namespace \
  --set clusterID=cluster-east \
  --set berth.apiServer=https://berth.example.com:8443 \
  --set berth.apiKey.secretName=berth-api-key \
  --set berth.tls.caBundleConfigMap=berth-ca-bundle
```

Apply the same `BerthLease` manifest to each cluster:

```yaml
apiVersion: berth.skaphos.io/v1alpha1
kind: BerthLease
metadata:
  name: ingest-worker
  namespace: pipeline
spec:
  leaseName: ingest-worker
  holderIdentity: fallback-holder
  ttlSeconds: 30
  heartbeatIntervalSeconds: 10
  semantics: at-most-once
  target:
    apiVersion: apps/v1
    kind: Deployment
    name: ingest-worker
  acquireAction:
    scale:
      replicas: 3
  releaseAction:
    scale:
      replicas: 0
```

Only the cluster whose operator holds the central lease scales the target
Deployment up. Standby clusters keep their target scaled down and retry
acquisition on the configured heartbeat cadence.

## Documentation

- [Documentation index](docs/README.md)
- [Architecture](docs/architecture.md)
- [Configuration reference](docs/reference/configuration.md)
- [Code map](docs/code-map.md)
- [Contributing](CONTRIBUTING.md)
- [Release process](RELEASE.md)

## Development

Requires Go 1.26+. Task is declared as a Go tool in `tools/go.mod`, so no
separate Task installation is required.

```bash
go -C tools tool task --list
go -C tools tool task build
go -C tools tool task test
go -C tools tool task lint
go -C tools tool task verify-generated
```

## License

MIT. See [LICENSE](LICENSE) and [LICENSES/MIT.txt](LICENSES/MIT.txt).
