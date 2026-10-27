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
# JVM cold start is usually <15s; 90s covers slow laptops without masking a bad probe.
WAIT_TIMEOUT="${WAIT_TIMEOUT:-90}"
SYNC_ONLY=0

usage() {
	cat <<'EOF'
Usage: ./scripts/install-keycloak.sh [--sync-only]

  --sync-only   assume Keycloak is already up (or recreating); only wait + sync
EOF
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--sync-only) SYNC_ONLY=1 ;;
	-h | --help) usage 0 ;;
	*)
		echo "unknown flag: $1" >&2
		usage 1
		;;
	esac
	shift
done

log() {
	echo "[install-keycloak] $*"
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "missing required command: $1" >&2
		exit 1
	fi
}

# Keycloak 26 serves /health/* on management :9000 (HTTPS), NOT on app :8080.
# HTTP can be up before the entrypoint finishes kcadm bootstrap — wait for both.
keycloak_http_ready() {
	symagios_auth_compose exec -T keycloak \
		sh -c "exec 3<>/dev/tcp/127.0.0.1/8080; echo -e 'GET /realms/master HTTP/1.1\r\nhost: localhost\r\nConnection: close\r\n\r\n' >&3; grep -q '200 OK' <&3" \
		>/dev/null 2>&1
}

keycloak_bootstrap_done() {
	symagios_auth_compose exec -T keycloak \
		test -f /tmp/symagios-kc-bootstrap.done >/dev/null 2>&1
}

wait_for_keycloak() {
	local deadline=$((SECONDS + WAIT_TIMEOUT))
	local waited=0
	while (( SECONDS < deadline )); do
		if keycloak_http_ready && keycloak_bootstrap_done; then
			[[ "$waited" -gt 0 ]] && log "keycloak ready after ${waited}s"
			return 0
		fi
		sleep 2
		waited=$((waited + 2))
	done
	echo "keycloak did not become ready within ${WAIT_TIMEOUT}s" >&2
	echo "hint: need GET /realms/master on :8080 AND /tmp/symagios-kc-bootstrap.done" >&2
	echo "      (entrypoint writes the marker after client-secret kcadm)." >&2
	echo "      check: ./scripts/compose.sh --profile auth logs --tail 80 keycloak" >&2
	return 1
}

require_command curl

symagios_require_compose

if [[ -z "${KEYCLOAK_ADMIN_PASSWORD:-}" ]]; then
	echo "KEYCLOAK_ADMIN_PASSWORD unset — run ./scripts/generate-dev-secrets.sh" >&2
	exit 1
fi

if [[ "$SYNC_ONLY" -eq 0 ]]; then
	log "fetching Keycloak secrets from Vault into shared volume (run --rm)"
	symagios_keycloak_vault_fetch
	log "starting keycloak"
	symagios_auth_compose up -d keycloak
else
	log "sync-only — not recreating keycloak container"
fi

log "waiting for keycloak (HTTP :8080 + entrypoint bootstrap; :8080 is not published)"
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
