# Symagios Makefile
#
#   make all          # tidy + build every binary + run unit tests
#   make build        # build all binaries into bin/
#   make test         # unit tests for non-WIP packages
#   make docker-build # multi-binary container image

BIN_DIR := bin
GO      := cd pkg && go
LDFLAGS := -ldflags="-s -w"

BINS := \
	$(BIN_DIR)/symagios \
	$(BIN_DIR)/mcp-gateway \
	$(BIN_DIR)/search-broker

.PHONY: all build build-bins test tidy proto clean \
	docker-up docker-watch docker-build image-build \
	stack-start stack-stop stack-delete \
	auth-install vault-install secrets-generate vault-up vault-admin \
	keycloak-install keycloak-up keycloak-admin keycloak-sync-vault \
	opa-install opa-up envoy-install envoy-up mtls-issue mtls-up \
	search-broker mcp-gateway

## Default: build every binary + unit tests
## (run `make tidy` separately when go.sum needs refreshing)
all: build test

## Build every Go command into bin/
build: $(BIN_DIR) build-bins
	@echo "built: $(BINS)"

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

build-bins: $(BINS)

$(BIN_DIR)/symagios: $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o ../$(BIN_DIR)/symagios ./cmd/symagios

$(BIN_DIR)/mcp-gateway: $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o ../$(BIN_DIR)/mcp-gateway ./cmd/mcp-gateway

$(BIN_DIR)/search-broker: $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o ../$(BIN_DIR)/search-broker ./cmd/search-broker

## Unit tests (default packages; no WIP tags required)
test:
	cd pkg && go test ./... -count=1

tidy:
	cd pkg && go mod tidy

proto:
	./scripts/generate-proto.sh

clean:
	rm -rf $(BIN_DIR)

# Container runtime: Podman by default (scripts/lib/runtime.sh).
# Override: SYMAGIOS_CONTAINER_RUNTIME=docker make …
COMPOSE := ./scripts/compose.sh

docker-build image-build:
	bash -c 'source ./scripts/lib/runtime.sh && symagios_build -t symagios:local .'

docker-up:
	$(COMPOSE) up --build engine

docker-watch:
	$(COMPOSE) --profile auth --profile watch up -d --no-recreate watch

## Full stack lifecycle (Podman/Docker compose)
stack-start:
	./scripts/stack-start.sh $(STACK_START_FLAGS)

stack-stop:
	./scripts/stack-stop.sh

stack-delete:
	./scripts/stack-delete.sh $(STACK_DELETE_FLAGS)

vault-install:
	./scripts/install-vault.sh

secrets-generate:
	./scripts/generate-dev-secrets.sh

vault-up:
	$(COMPOSE) --profile auth up -d vault

vault-admin:
	@./scripts/vault-admin.sh help

keycloak-install:
	./scripts/install-keycloak.sh

keycloak-sync-vault:
	./scripts/keycloak-sync-secrets-from-vault.sh

keycloak-up:
	$(COMPOSE) --profile auth up -d keycloak

keycloak-admin:
	@./scripts/keycloak-admin.sh help

opa-install:
	./scripts/install-opa.sh

opa-up:
	$(COMPOSE) --profile auth up -d opa

envoy-install:
	./scripts/install-envoy.sh

envoy-up:
	$(COMPOSE) --profile auth up -d envoy-ingress envoy-egress

auth-install:
	./scripts/install-auth.sh

mtls-issue:
	./scripts/issue-mtls-certs.sh

mtls-up:
	./scripts/install-mtls.sh

## Dev run helpers (source, not bin/)
search-broker:
	cd pkg && go run ./cmd/search-broker

mcp-gateway:
	cd pkg && SYMAGIOS_MCP_REGISTRY=../deploy/mcp/registry.json go run ./cmd/mcp-gateway
