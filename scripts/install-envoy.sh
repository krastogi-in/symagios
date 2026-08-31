#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh


# Primary edge is HTTPS only (plaintext listener removed).
ENVOY_INGRESS_HTTPS_URL="${ENVOY_INGRESS_HTTPS_URL:-https://localhost:8443}"
# Egress is mesh-only (not published). Reachable as https://envoy-egress:10001 inside compose.
ENVOY_EGRESS_URL="${ENVOY_EGRESS_URL:-https://envoy-egress:10001}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-120}"

log() {
	echo "[install-envoy] $*"
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required command: $1" >&2
		exit 1
	fi
}

wait_for_admin() {
	local service="$1"
	local port="$2"
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	while (( SECONDS < deadline )); do
		if symagios_auth_compose exec -T "${service}" \
			sh -c "exec 3<>/dev/tcp/127.0.0.1/${port}; echo -e 'GET /ready HTTP/1.1\r\nhost: localhost\r\nConnection: close\r\n\r\n' >&3; grep -q 200 <&3" \
			>/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "${service} admin did not become ready within ${WAIT_TIMEOUT}s" >&2
	return 1
}

require_command curl

symagios_require_compose

opa_ready() {
	symagios_auth_compose exec -T opa /opa version >/dev/null 2>&1
}

if ! opa_ready; then
	log "opa is not running; starting auth stack prerequisites"
	if [[ ! -f deploy/certs/ca.crt || ! -f deploy/certs/keycloak/tls.crt ]]; then
		log "mTLS certs missing; issuing before Keycloak"
		if ! symagios_auth_compose exec -T vault \
			wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1; then
			./scripts/install-vault.sh
		fi
		./scripts/issue-mtls-certs.sh
	fi
	symagios_auth_compose up -d vault-mtls
	sleep 2
	./scripts/install-keycloak.sh
	./scripts/install-opa.sh
fi

if [[ ! -f deploy/certs/envoy-ingress/tls.crt ]]; then
	log "edge TLS certs missing; issuing from Vault PKI"
	if ! symagios_auth_compose exec -T vault \
		wget -qO- http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1; then
		./scripts/install-vault.sh
	fi
	./scripts/issue-mtls-certs.sh
fi

log "starting envoy ingress and egress sidecars"
symagios_auth_compose up -d envoy-ingress envoy-egress

log "waiting for envoy-ingress admin (loopback inside container only)"
wait_for_admin envoy-ingress 9901

log "waiting for envoy-egress admin (loopback inside container only)"
wait_for_admin envoy-egress 9902

log "envoy is ready"
cat <<EOF

Symagios Envoy sidecars are running.

Edge TLS (primary trust boundary):
  HTTPS URL     : ${ENVOY_INGRESS_HTTPS_URL}
  Terminator    : envoy-ingress DownstreamTlsContext (NOT a front LB)
  Cert          : deploy/certs/envoy-ingress/tls.crt (Vault PKI)
  Docs          : deploy/docs/edge-tls.md

HTTP local-only : removed (ingress is TLS only)
Egress URL      : ${ENVOY_EGRESS_URL}  (mesh-only; mTLS required; not published)
  Source ID     : derived from workload client cert (not X-Symagios-Source)
Admin           : 127.0.0.1 inside the container only (not published)

Intentional auth details:
  /.well-known/*  — no JWT (public agent card)
  /tasks/list before /tasks/{id} regex — list not swallowed as id
  ext_authz failure_mode_allow: false — fail closed if OPA down
  egress require_client_certificate: true — fail closed without mesh cert

Example (JWT over TLS; -k if using Vault-issued local CA):
  source .env
  TOKEN=\$(./scripts/keycloak-admin.sh token "\$SYMAGIOS_OPERATOR_USER" "\$SYMAGIOS_OPERATOR_PASSWORD" | jq -r .access_token)
  curl -k -H "Authorization: Bearer \$TOKEN" ${ENVOY_INGRESS_HTTPS_URL}/tasks/list?contextId=demo

Stop Envoy:
  ./scripts/compose.sh --profile auth stop envoy-ingress envoy-egress

EOF
