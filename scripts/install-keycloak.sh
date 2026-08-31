#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
# shellcheck disable=SC1091
source ./scripts/lib/runtime.sh

# shellcheck disable=SC1091
source ./scripts/lib/load-env.sh

TOKEN_URL="${TOKEN_URL:-https://localhost:8443}"
KEYCLOAK_ADMIN="${KEYCLOAK_ADMIN:-admin}"
KEYCLOAK_REALM="${KEYCLOAK_REALM:-symagios}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-180}"

log() {
	echo "[install-keycloak] $*"
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required command: $1" >&2
		exit 1
	fi
}

wait_for_keycloak() {
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	while (( SECONDS < deadline )); do
		if symagios_auth_compose exec -T keycloak \
			sh -c "exec 3<>/dev/tcp/127.0.0.1/8080; echo -e 'GET /health/ready HTTP/1.1\r\nhost: localhost\r\nConnection: close\r\n\r\n' >&3; grep -q '200 OK' <&3" \
			>/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "keycloak did not become ready within ${WAIT_TIMEOUT}s" >&2
	return 1
}

require_command curl

symagios_require_compose

if [[ -z "${KEYCLOAK_ADMIN_PASSWORD:-}" ]]; then
	echo "KEYCLOAK_ADMIN_PASSWORD unset — run ./scripts/generate-dev-secrets.sh" >&2
	exit 1
fi

log "starting keycloak sidecar"
symagios_auth_compose up -d keycloak

log "waiting for keycloak (inside the container; port 8080 is not published)"
wait_for_keycloak

log "syncing client secret + bootstrap operator user from Vault"
./scripts/keycloak-sync-secrets-from-vault.sh

log "keycloak is ready"
cat <<EOF

Symagios Keycloak is running.

Realm         : ${KEYCLOAK_REALM}
Admin user    : ${KEYCLOAK_ADMIN}
Admin password: (see .env KEYCLOAK_ADMIN_PASSWORD)

CLI / agents do not use plaintext Keycloak.
Token URL     : ${TOKEN_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token
Issuer        : ${TOKEN_URL}/realms/${KEYCLOAK_REALM}
That URL is Envoy HTTPS. Envoy forwards it to Keycloak over mTLS.

Clients (imported on first start; secret from Vault):
  symagios-gateway  confidential
  symagios-cli      public client for local tooling

Operator user (roles from .env / Vault):
  username: ${SYMAGIOS_OPERATOR_USER:-operator}
  password: (see .env SYMAGIOS_OPERATOR_PASSWORD)
  roles:    ${SYMAGIOS_OPERATOR_ROLES:-operator,agent-runner}

Also bootstrapped (see deploy/docs/rbac.md):
  ${SYMAGIOS_ADMIN_USER:-symagios-admin}  roles=${SYMAGIOS_ADMIN_ROLES:-admin}
  ${SYMAGIOS_RUNNER_USER:-runner}         roles=${SYMAGIOS_RUNNER_ROLES:-agent-runner}

Keycloak admin CLI:
  ./scripts/keycloak-admin.sh user list
  ./scripts/keycloak-admin.sh token ${SYMAGIOS_OPERATOR_USER:-operator} "\$SYMAGIOS_OPERATOR_PASSWORD"

Stop Keycloak:
  ./scripts/compose.sh --profile auth stop keycloak

EOF
