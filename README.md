# berth

Distributed lease service for Kubernetes multi-cluster workloads.

## Status

This repository contains the initial Berth scaffolding:

- API server binary (`cmd/apiserver`)
- Operator binary (`cmd/operator`)
- CLI binary (`cmd/berth`)
- CRD API types (`api/v1alpha1`)

## Build

```bash
make build
```

## Test

```bash
make test
```
