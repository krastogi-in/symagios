#!/usr/bin/env bash
# Full auth stack bring-up. Order matters: Vault → PKI certs → vault-mtls →
# Keycloak (needs certs + Vault mTLS) → OPA → Envoy.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

log() {
	echo "[install-auth] $*"
}

./scripts/generate-dev-secrets.sh
./scripts/install-vault.sh

log "issuing workload mTLS certs (required before Keycloak / vault-mtls)"
./scripts/issue-mtls-certs.sh

log "starting vault-mtls sidecar (Keycloak fetch talks to Vault on :8202)"
symagios_auth_compose up -d vault-mtls
# Sidecar has no healthcheck; give Envoy a moment to bind :8202 in vault's netns.
sleep 2

./scripts/install-keycloak.sh
./scripts/install-opa.sh
./scripts/install-envoy.sh

log "auth stack install finished"
