GO ?= go
CONTROLLER_GEN := $(GO) run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5
GOLANGCI_LINT := $(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
BIN_DIR ?= bin
TMP_CRD_DIR := .tmp/crd

.PHONY: generate manifests build test lint docker-build clean e2e-up e2e e2e-down e2e-all

generate:
	$(CONTROLLER_GEN) object paths=./api/...

manifests:
	rm -rf "$(TMP_CRD_DIR)"
	mkdir -p "$(TMP_CRD_DIR)" config/crd
	$(CONTROLLER_GEN) crd paths=./api/... output:crd:artifacts:config="$(TMP_CRD_DIR)"
	cp "$(TMP_CRD_DIR)/berth.skaphos.io_berthleases.yaml" config/crd/berthlease.yaml
	rm -rf "$(TMP_CRD_DIR)"

build:
	mkdir -p "$(BIN_DIR)"
	$(GO) build -o "$(BIN_DIR)/apiserver" ./cmd/apiserver
	$(GO) build -o "$(BIN_DIR)/operator" ./cmd/operator
	$(GO) build -o "$(BIN_DIR)/berth" ./cmd/berth
	$(GO) build -o "$(BIN_DIR)/berth-oidc-broker" ./cmd/berth-oidc-broker

test:
	$(GO) test ./...

lint:
	$(GOLANGCI_LINT) run ./...
	$(GO) vet ./...

docker-build:
	docker build -f Dockerfile.apiserver .
	docker build -f Dockerfile.operator .

clean:
	rm -rf "$(BIN_DIR)" .tmp

# --- e2e --------------------------------------------------------------
# Three-kind-cluster harness validating cross-cluster lease semantics.
# See test/e2e/fixtures/README.md for the topology rationale.

e2e-up:
	./test/e2e/fixtures/up.sh

e2e:
	$(GO) test -tags=e2e -count=1 -v -timeout=20m ./test/e2e/...

e2e-down:
	./test/e2e/fixtures/down.sh

e2e-all: e2e-up e2e e2e-down
