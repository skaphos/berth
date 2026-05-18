# Berth e2e harness

Three kind clusters wired together to validate cross-cluster lease
semantics that unit tests can't exercise: real `coordination.k8s.io/v1.Lease`
CAS, real operator-to-API-server TLS, and real controller-runtime cadence.

| Cluster              | Role                                        |
| -------------------- | ------------------------------------------- |
| `berth-e2e-coord`    | Hosts `berth-apiserver` + the Lease store.  |
| `berth-e2e-east`     | Runs `berth-operator --cluster-id=cluster-east`. |
| `berth-e2e-west`     | Runs `berth-operator --cluster-id=cluster-west`. |

All three share the default `kind` docker network. East and west
operators reach the coord API server at
`https://<coord-control-plane-IP>:<NodePort>` — discovered by
`up.sh` at install time.

## Running locally

Requires `kind`, `kubectl`, `helm`, `docker`, `openssl` on PATH.

```bash
make e2e-up      # ~2 min — creates clusters, builds + loads images, installs charts
make e2e         # runs ./test/e2e with -tags=e2e
make e2e-down    # tears everything down
```

`make e2e-all` chains all three for CI.

## What's intentionally not production-grade

- **TLS verification is disabled** in the operator (`berth.tls.insecureSkipVerify: true`).
  The API server cert SAN can't include a yet-to-be-assigned container IP, and
  pinning DNS through the docker bridge is more harness than it's worth.
  Production never sets this.
- **NodePort service.** Production fronts the API server with an Ingress or
  LoadBalancer.
- **Static-keys auth.** OIDC is exercised by unit tests + the sidecar
  token-broker; the e2e harness stays focused on lease semantics.

## Layout

```
fixtures/
  kind-coord.yaml         # kind cluster config — coord
  kind-east.yaml          # kind cluster config — east runner
  kind-west.yaml          # kind cluster config — west runner
  apiserver-values.yaml   # helm values for the apiserver release
  operator-values.yaml    # shared operator values (clusterID/apiServer set per-install)
  up.sh                   # idempotent-enough harness setup
  down.sh                 # teardown
```

Tools used at runtime are recorded in `up.sh`'s `require` checks.
