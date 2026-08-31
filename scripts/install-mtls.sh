#!/usr/bin/env bash
# Issue Vault PKI certs used by the single Envoy config (edge TLS + east-west mTLS).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

log() {
	echo "[install-mtls] $*"
}

./scripts/issue-mtls-certs.sh

log "restarting envoy-ingress to pick up certs (one config: deploy/envoy/envoy.yaml)"
symagios_auth_compose up -d envoy-ingress

cat <<EOF

One Envoy config handles both TLS hops:

  Client --HTTPS--> envoy-ingress:10000
           DownstreamTlsContext (edge)
         --mTLS--> agent-gateway:8443
           UpstreamTlsContext (east-west)

Certs: deploy/certs/
Config: deploy/envoy/envoy.yaml
Docs:   deploy/docs/edge-tls.md , deploy/docs/mtls.md

EOF
