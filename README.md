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

Install the API server in a coordination cluster:

```bash
helm install berth-apiserver deploy/helm/berth-apiserver \
  --namespace berth-system --create-namespace \
  --set store.backend=k8s \
  --set-string 'extraArgs[0]=--store-backend=k8s' \
  --set coordination.namespace=berth-coordination \
  --set coordination.inCluster=true \
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
